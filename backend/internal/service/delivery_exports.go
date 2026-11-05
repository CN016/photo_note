package service

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

type deliveryExportRepository interface {
	CreateDeliveryExport(context.Context, *model.DeliveryExport) error
	FindDeliveryExport(context.Context, uuid.UUID, uuid.UUID) (model.DeliveryExport, error)
	ClaimDeliveryExport(context.Context) (model.DeliveryExport, bool, error)
	UpdateDeliveryExport(context.Context, model.DeliveryExport, map[string]any) error
}

// DeliveryExportView 不暴露磁盘 key 和内部主键，创建请求接受不代表打包完成。
type DeliveryExportView struct {
	BusinessID uuid.UUID `json:"business_id"`
	Status     string    `json:"status"`
	Progress   int       `json:"progress"`
	Total      int       `json:"total"`
	Bytes      int64     `json:"bytes"`
	ErrorCode  string    `json:"error_code"`
}

func exportView(j model.DeliveryExport) DeliveryExportView {
	return DeliveryExportView{j.BusinessID, j.Status, j.Progress, j.Total, j.Bytes, j.ErrorCode}
}

// CreateExport 为确认选片或已发布精修建立持久化快照；客户只能按当前下载权限创建作业。
func (s *DeliveryService) CreateExport(ctx context.Context, id uuid.UUID, kind string, public bool, subset ...[]string) (DeliveryExportView, error) {
	repository, ok := s.repository.(deliveryExportRepository)
	if !ok {
		return DeliveryExportView{}, fault.Unavailable(nil)
	}
	detail, err := s.Get(ctx, id, public)
	if err != nil {
		return DeliveryExportView{}, err
	}
	if kind != "proof" && kind != "final" {
		return DeliveryExportView{}, fault.InvalidRequest("无效导出类型")
	}
	if public && ((kind == "proof" && !detail.Task.AllowOriginal) || (kind == "final" && !detail.Task.AllowDownload)) {
		return DeliveryExportView{}, fault.NotFound()
	}
	chosen := map[string]bool{}
	if kind == "proof" && !public {
		for _, r := range detail.Rounds {
			if r.Number == detail.Task.CurrentRound && (r.Status == "submitted" || r.Status == "confirmed") {
				for _, v := range r.SelectedIDs {
					chosen[v] = true
				}
			}
		}
		if len(chosen) == 0 {
			return DeliveryExportView{}, fault.Conflict("没有正式提交的选片")
		}
	}
	ids := pq.StringArray{}
	for _, p := range detail.Photos {
		if p.Kind != kind {
			continue
		}
		if kind == "proof" && !public && !chosen[p.BusinessID.String()] {
			continue
		}
		if kind == "final" && p.PublishedBatch == 0 {
			continue
		}
		ids = append(ids, p.BusinessID.String())
	}
	if len(ids) == 0 {
		return DeliveryExportView{}, fault.Conflict("没有可打包文件")
	}
	// 客户端可按平台导出能力分包，但请求的 UUID 必须是服务端授权清单的子集。
	if len(subset) > 0 && len(subset[0]) > 0 {
		allowed := map[string]bool{}
		for _, v := range ids {
			allowed[v] = true
		}
		seen := map[string]bool{}
		filtered := pq.StringArray{}
		for _, v := range subset[0] {
			if !allowed[v] || seen[v] {
				return DeliveryExportView{}, fault.NotFound()
			}
			seen[v] = true
			filtered = append(filtered, v)
		}
		ids = filtered
	}
	scope, err := tenant.Require(ctx)
	if err != nil {
		return DeliveryExportView{}, err
	}
	job := model.DeliveryExport{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: scope.UserID, TaskID: id, TaskVersion: detail.Task.Version, Kind: kind, PhotoIDs: ids, Status: "pending", Total: len(ids), Public: public}
	err = repository.CreateDeliveryExport(ctx, &job)
	return exportView(job), err
}

// ExportStatus 对客户和管理端均验证任务关联；客户不得读取摄影师的原片导出作业。
func (s *DeliveryService) ExportStatus(ctx context.Context, id, jobID uuid.UUID, public bool) (DeliveryExportView, error) {
	j, err := s.exportJob(ctx, id, jobID, public)
	return exportView(j), err
}
func (s *DeliveryService) exportJob(ctx context.Context, id, jobID uuid.UUID, public bool) (model.DeliveryExport, error) {
	repository, ok := s.repository.(deliveryExportRepository)
	if !ok {
		return model.DeliveryExport{}, fault.Unavailable(nil)
	}
	j, err := repository.FindDeliveryExport(ctx, id, jobID)
	if err != nil {
		return j, err
	}
	detail, err := s.Get(ctx, id, public)
	if err != nil {
		return j, err
	}
	if j.TaskVersion != detail.Task.Version {
		return j, fault.Conflict("任务已变化，请重新打包")
	}
	if public && (!j.Public || (j.Kind == "proof" && !detail.Task.AllowOriginal) || (j.Kind == "final" && !detail.Task.AllowDownload)) {
		return j, fault.NotFound()
	}
	return j, nil
}

// OpenExport 完成后经 FileService 再次校验归属，不能直接把存储 key 返回客户。
func (s *DeliveryService) OpenExport(ctx context.Context, id, jobID uuid.UUID, public bool) (io.ReadCloser, int64, error) {
	j, err := s.exportJob(ctx, id, jobID, public)
	if err != nil {
		return nil, 0, err
	}
	if j.Status != "completed" {
		return nil, 0, fault.Conflict("打包尚未完成")
	}
	r, n, _, err := s.files.OpenContext(ctx, j.FileKey)
	return r, n, err
}

// RunExportWorker 顺序流式打包，临时文件在系统临时目录，任何退出路径都会关闭并回收。
// 数据库作业保留进度和安全错误分类，重启后可按租约恢复，不把请求接受当作完成。
func (s *DeliveryService) RunExportWorker(ctx context.Context) {
	repository, ok := s.repository.(deliveryExportRepository)
	if !ok {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.cleanExports(ctx)
		job, found, err := repository.ClaimDeliveryExport(ctx)
		if err != nil || !found {
			continue
		}
		scoped := tenant.WithScope(ctx, tenant.Scope{OrgID: job.OrgID, UserID: job.UserID})
		if err = s.processExport(scoped, repository, job); err != nil {
			_ = repository.UpdateDeliveryExport(context.WithoutCancel(scoped), job, map[string]any{"status": "failed", "error_code": "export_failed"})
		}
	}
}

// cleanExports 只清理数据库记录指向的临时 ZIP，绝不按目录扫描删除原图或精修文件。
func (s *DeliveryService) cleanExports(ctx context.Context) {
	repository, ok := s.repository.(interface {
		ExpiredDeliveryExports(context.Context, time.Time) ([]model.DeliveryExport, error)
		DeleteDeliveryExport(context.Context, model.DeliveryExport) error
	})
	if !ok {
		return
	}
	hours, err := s.config.DeliveryExportRetention(ctx)
	if err != nil {
		return
	}
	items, err := repository.ExpiredDeliveryExports(ctx, time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		return
	}
	for _, job := range items {
		scoped := tenant.WithScope(ctx, tenant.Scope{OrgID: job.OrgID, UserID: job.UserID})
		if job.FileKey != "" {
			if err = s.files.Delete(scoped, job.FileKey); err != nil {
				continue
			}
		}
		_ = repository.DeleteDeliveryExport(scoped, job)
	}
}

// processExport 按原始代次逐文件校验，不使用当前登录空间，不在内存累积原始图片。
func (s *DeliveryService) processExport(ctx context.Context, r deliveryExportRepository, j model.DeliveryExport) error {
	file, err := os.CreateTemp("", "photo-nini-delivery-*.zip")
	if err != nil {
		return err
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	z := zip.NewWriter(file)
	for i, v := range j.PhotoIDs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err = s.exportJob(ctx, j.TaskID, j.BusinessID, j.Public); err != nil {
			return err
		}
		id, err := uuid.Parse(v)
		if err != nil {
			return err
		}
		source, _, _, name, err := s.OpenPhoto(ctx, j.TaskID, id, true, j.Public)
		if err != nil {
			return err
		}
		dest, err := z.CreateHeader(&zip.FileHeader{Name: id.String() + "/" + name, Method: zip.Store})
		if err != nil {
			source.Close()
			return err
		}
		_, err = io.Copy(dest, source)
		source.Close()
		if err != nil {
			return err
		}
		if err = r.UpdateDeliveryExport(ctx, j, map[string]any{"progress": i + 1}); err != nil {
			return err
		}
	}
	if err = z.Close(); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	storeType, err := s.config.CurrentFileStoreType(ctx)
	if err != nil {
		return err
	}
	key, err := s.files.Store(ctx, storeType, "delivery.zip", file, info.Size())
	if err != nil {
		return err
	}
	if _, err = s.exportJob(ctx, j.TaskID, j.BusinessID, j.Public); err != nil {
		_ = s.files.Delete(context.WithoutCancel(ctx), key)
		return err
	}
	if err = r.UpdateDeliveryExport(ctx, j, map[string]any{"status": "completed", "file_key": key, "bytes": info.Size(), "error_code": ""}); err != nil {
		_ = s.files.Delete(context.WithoutCancel(ctx), key)
		return err
	}
	return nil
}
