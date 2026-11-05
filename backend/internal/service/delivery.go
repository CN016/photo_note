package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/imagecompress"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/security"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
)

// DeliveryRepository 将任务锁、租户查询和持久化事务置于同一边界，便于验证并发流转。
type DeliveryRepository interface {
	ListDeliveries(context.Context) ([]model.DeliveryTask, error)
	CreateDelivery(context.Context, *model.DeliveryTask) error
	ResolveDelivery(context.Context, string) (model.DeliveryTask, error)
	DeliveryTransaction(context.Context, uuid.UUID, bool, func(*store.DeliveryBundle) error) error
}

// DeliveryService 管理私有交付资源；压缩并发槽限制图片解码的内存峰值。
type DeliveryService struct {
	repository DeliveryRepository
	files      *FileService
	config     *RuntimeConfigService
	passwords  *security.PasswordManager
	maxUpload  int64
	slots      chan struct{}
	rateMu     sync.Mutex
	rates      map[string]deliveryRate
}
type deliveryRate struct {
	Until time.Time
	Count int
}

// NewDeliveryService 复用现有文件授权、统一配置和 Argon2id，不创建永久公开副本。
func NewDeliveryService(r DeliveryRepository, f *FileService, c *RuntimeConfigService, p *security.PasswordManager, max int64) *DeliveryService {
	return &DeliveryService{repository: r, files: f, config: c, passwords: p, maxUpload: max, slots: make(chan struct{}, 1), rates: map[string]deliveryRate{}}
}

// DeliveryTaskView 显式补充业务 UUID，隐藏数据库内部主键和秘密配置。
type DeliveryTaskView struct {
	model.DeliveryTask
	BusinessID    uuid.UUID `json:"business_id"`
	UpdatedAt     time.Time `json:"updated_at"`
	Shared        bool      `json:"shared"`
	HasAccessCode bool      `json:"has_access_code"`
	InternalNote  string    `json:"internal_note,omitempty"`
}

// DeliveryPhotoView 只暴露资产 UUID，不暴露物理 storekey。
type DeliveryPhotoView struct {
	model.DeliveryPhoto
	BusinessID uuid.UUID `json:"business_id"`
}

// DeliveryDetail 返回当前任务的受控工作集；公开响应经裁剪，不暴露历史轮次和操作记录。
type DeliveryDetail struct {
	Task   DeliveryTaskView      `json:"task"`
	Photos []DeliveryPhotoView   `json:"photos"`
	Rounds []model.DeliveryRound `json:"rounds"`
	Events []DeliveryEventView   `json:"events"`
}
type DeliveryEventView struct {
	Action string    `json:"action"`
	Stage  string    `json:"stage"`
	At     time.Time `json:"at"`
}

// DeliveryInput 是全量任务设置，归属始终从服务端上下文取得。
type DeliveryInput struct {
	Title           string     `json:"title"`
	Customer        string     `json:"customer"`
	Description     string     `json:"description"`
	InternalNote    string     `json:"internal_note"`
	LimitMode       string     `json:"limit_mode"`
	SelectionLimit  int        `json:"selection_limit"`
	Deadline        *time.Time `json:"deadline"`
	ExpiresAt       *time.Time `json:"expires_at"`
	AllowOriginal   bool       `json:"allow_original"`
	AllowDownload   bool       `json:"allow_download"`
	AccessCode      string     `json:"access_code"`
	ClearAccessCode bool       `json:"clear_access_code"`
}

// validateDeliveryInput 限制用户可控文本和数量，避免不可提交的选片规则。
func validateDeliveryInput(in DeliveryInput) error {
	if strings.TrimSpace(in.Title) == "" || len([]rune(in.Title)) > 120 || len([]rune(in.Customer)) > 120 || len(in.Description) > 8000 || len(in.InternalNote) > 8000 || len(in.AccessCode) > 64 {
		return fault.InvalidRequest("交付名称、说明或访问码格式无效")
	}
	if in.LimitMode != "unlimited" && in.LimitMode != "max" && in.LimitMode != "exact" {
		return fault.InvalidField("limit_mode", "无效的选片数量规则")
	}
	if in.SelectionLimit < 0 || in.SelectionLimit > 10000 || (in.LimitMode != "unlimited" && in.SelectionLimit < 1) {
		return fault.InvalidField("selection_limit", "选片数量不在允许范围")
	}
	return nil
}
func taskView(t model.DeliveryTask) DeliveryTaskView {
	return DeliveryTaskView{t, t.BusinessID, t.UpdatedAt, t.ShareHash != nil, t.AccessHash != "", t.InternalNote}
}

// List 返回创建者私有任务，包含业务标识而不泄露分享摘要。
func (s *DeliveryService) List(ctx context.Context) ([]DeliveryTaskView, error) {
	items, err := s.repository.ListDeliveries(ctx)
	out := []DeliveryTaskView{}
	for _, v := range items {
		out = append(out, taskView(v))
	}
	return out, err
}

// Create 新建上传阶段任务；访问码为空表示不开启额外口令。
func (s *DeliveryService) Create(ctx context.Context, in DeliveryInput) (DeliveryTaskView, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return DeliveryTaskView{}, err
	}
	if err = validateDeliveryInput(in); err != nil {
		return DeliveryTaskView{}, err
	}
	t := model.DeliveryTask{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: scope.UserID, Stage: "upload", Status: "active", Version: 1}
	if err = s.assignSettings(&t, in); err != nil {
		return DeliveryTaskView{}, err
	}
	err = s.repository.CreateDelivery(ctx, &t)
	return taskView(t), err
}

// assignSettings 只在已授权事务中调用；空访问码保留原值，显式清除才移除。
func (s *DeliveryService) assignSettings(t *model.DeliveryTask, in DeliveryInput) error {
	t.Title = strings.TrimSpace(in.Title)
	t.Customer = in.Customer
	t.Description = in.Description
	t.InternalNote = in.InternalNote
	t.LimitMode = in.LimitMode
	t.SelectionLimit = in.SelectionLimit
	t.Deadline = in.Deadline
	t.ExpiresAt = in.ExpiresAt
	t.AllowOriginal = in.AllowOriginal
	t.AllowDownload = in.AllowDownload
	if in.ClearAccessCode {
		t.AccessHash = ""
	}
	if in.AccessCode != "" {
		h, err := s.passwords.Hash(in.AccessCode)
		if err != nil {
			return err
		}
		t.AccessHash = h
	}
	return nil
}

// Get 的 public 参数仅供已验证分享授权的内部路径使用。
func (s *DeliveryService) Get(ctx context.Context, id uuid.UUID, public bool) (DeliveryDetail, error) {
	out := DeliveryDetail{Photos: []DeliveryPhotoView{}, Rounds: []model.DeliveryRound{}, Events: []DeliveryEventView{}}
	err := s.repository.DeliveryTransaction(ctx, id, false, func(b *store.DeliveryBundle) error {
		out.Task = taskView(b.Task)
		allowed := map[string]bool{}
		for _, r := range b.Rounds {
			if !public || r.Number == b.Task.CurrentRound {
				out.Rounds = append(out.Rounds, r)
			}
			if r.Number == b.Task.CurrentRound {
				for _, v := range r.PhotoIDs {
					allowed[v] = true
				}
			}
		}
		for _, p := range b.Photos {
			if !public || (p.Kind == "proof" && allowed[p.BusinessID.String()]) || (p.Kind == "final" && p.PublishedBatch > 0) {
				out.Photos = append(out.Photos, DeliveryPhotoView{p, p.BusinessID})
			}
		}
		if public {
			out.Task.InternalNote = ""
			out.Task.Customer = ""
			out.Task.Shared = false
			out.Task.HasAccessCode = false
		} else {
			for _, e := range b.Events {
				out.Events = append(out.Events, DeliveryEventView{e.Action, e.Stage, e.CreatedAt})
			}
		}
		return nil
	})
	return out, err
}

// DeliveryCommand 为每次管理操作携带乐观版本，避免旧页面覆盖新状态。
type DeliveryCommand struct {
	Version  int            `json:"version"`
	Action   string         `json:"action"`
	Value    string         `json:"value"`
	PhotoIDs []uuid.UUID    `json:"photo_ids"`
	SourceID *uuid.UUID     `json:"source_id"`
	Settings *DeliveryInput `json:"settings"`
}

// Command 执行显式状态转换；发布、退回、确认均保留历史轮次。
func (s *DeliveryService) Command(ctx context.Context, id uuid.UUID, in DeliveryCommand) (string, error) {
	token := ""
	err := s.repository.DeliveryTransaction(ctx, id, true, func(b *store.DeliveryBundle) error {
		t := &b.Task
		if in.Version != t.Version {
			return fault.Conflict("任务已更新，请刷新后重试")
		}
		switch in.Action {
		case "settings":
			if in.Settings == nil {
				return fault.InvalidRequest("缺少设置")
			}
			if err := validateDeliveryInput(*in.Settings); err != nil {
				return err
			}
			if err := s.assignSettings(t, *in.Settings); err != nil {
				return err
			}
		case "share":
			data := make([]byte, 32)
			if _, err := rand.Read(data); err != nil {
				return err
			}
			token = base64.RawURLEncoding.EncodeToString(data)
			hash := deliveryHash(token)
			t.ShareHash = &hash
		case "revoke":
			t.ShareHash = nil
		case "status":
			if in.Value != "active" && in.Value != "paused" && in.Value != "completed" && in.Value != "archived" && in.Value != "cancelled" {
				return fault.InvalidRequest("无效的任务状态")
			}
			if in.Value == "completed" && t.Stage != "delivered" {
				return fault.Conflict("请先发布精修交付")
			}
			t.Status = in.Value
		case "stage":
			if t.Status != "active" {
				return fault.Conflict("请先恢复任务")
			}
			switch in.Value {
			case "upload", "cull":
				t.Stage = in.Value
			case "retouch":
				r := currentRound(b)
				if r == nil || r.Status != "confirmed" {
					return fault.Conflict("请先确认客户选片")
				}
				t.Stage = "retouch"
			default:
				return fault.InvalidRequest("请通过发布或确认操作推进此阶段")
			}
		case "mark", "group", "link":
			if t.Status != "active" {
				return fault.Conflict("请先恢复任务")
			}
			if len(in.PhotoIDs) == 0 || len(in.PhotoIDs) > 10000 {
				return fault.InvalidRequest("请选择照片")
			}
			found := 0
			if in.Action == "mark" && in.Value != "pending" && in.Value != "keep" && in.Value != "exclude" {
				return fault.InvalidRequest("无效的筛选标记")
			}
			if in.Action == "group" && len([]rune(in.Value)) > 120 {
				return fault.InvalidRequest("分组名称过长")
			}
			if in.Action == "link" && in.SourceID != nil {
				valid := false
				for _, p := range b.Photos {
					if p.BusinessID == *in.SourceID && p.Kind == "proof" {
						valid = true
					}
				}
				if !valid {
					return fault.NotFound()
				}
			}
			for i := range b.Photos {
				p := &b.Photos[i]
				if containsUUID(in.PhotoIDs, p.BusinessID) {
					found++
					switch in.Action {
					case "mark":
						if p.Kind != "proof" {
							return fault.InvalidRequest("精修文件不能筛片")
						}
						p.Mark = in.Value
					case "group":
						p.GroupName = in.Value
					case "link":
						if p.Kind != "final" || p.PublishedBatch > 0 {
							return fault.Conflict("仅能关联未发布精修")
						}
						p.SourceID = in.SourceID
					}
				}
			}
			if found != len(in.PhotoIDs) {
				return fault.NotFound()
			}
		case "publish", "reopen":
			if t.Status != "active" {
				return fault.Conflict("请先恢复任务")
			}
			old := currentRound(b)
			if old != nil && old.Status == "open" && in.Action == "publish" {
				return fault.Conflict("当前仍在选片，请使用重新发布")
			}
			ids := pq.StringArray{}
			for _, p := range b.Photos {
				if p.Kind == "proof" && p.Mark != "exclude" {
					ids = append(ids, p.BusinessID.String())
				}
			}
			if len(ids) == 0 {
				return fault.Conflict("没有可发布照片")
			}
			if t.LimitMode == "exact" && len(ids) < t.SelectionLimit {
				return fault.Conflict("可选照片少于要求选片数量")
			}
			if t.Deadline != nil && !t.Deadline.After(time.Now()) {
				return fault.Conflict("选片截止时间已过，请调整设置")
			}
			t.CurrentRound++
			r := model.DeliveryRound{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: t.UserID, TaskID: id, Number: t.CurrentRound, Version: 1, Status: "open", PhotoIDs: ids, SelectedIDs: pq.StringArray{}, Notes: model.JSONB(`{}`), LimitMode: t.LimitMode, SelectionLimit: t.SelectionLimit, Deadline: t.Deadline}
			if old != nil {
				available := map[string]bool{}
				for _, v := range ids {
					available[v] = true
				}
				for _, v := range old.SelectedIDs {
					if available[v] {
						r.SelectedIDs = append(r.SelectedIDs, v)
					}
				}
				old.Status = "superseded"
			}
			b.Rounds = append(b.Rounds, r)
			t.Stage = "select"
		case "confirm":
			r := currentRound(b)
			if r == nil || r.Status != "submitted" {
				return fault.Conflict("客户尚未提交选片")
			}
			r.Status = "confirmed"
			t.Stage = "retouch"
		case "publish_final":
			if t.Status != "active" {
				return fault.Conflict("请先恢复任务")
			}
			r := currentRound(b)
			if r == nil || r.Status != "confirmed" {
				return fault.Conflict("请先确认选片")
			}
			batch := 0
			for _, p := range b.Photos {
				batch = max(batch, p.PublishedBatch)
			}
			count := 0
			for i := range b.Photos {
				if b.Photos[i].Kind == "final" && b.Photos[i].PublishedBatch == 0 {
					b.Photos[i].PublishedBatch = batch + 1
					count++
				}
			}
			if count == 0 {
				return fault.Conflict("没有待发布精修")
			}
			t.Stage = "delivered"
			t.ReceivedAt = nil
		default:
			return fault.InvalidRequest("不支持的交付操作")
		}
		t.Version++
		b.Events = append(b.Events, model.DeliveryEvent{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: t.UserID, TaskID: id, Action: in.Action, Stage: t.Stage})
		return nil
	})
	return token, err
}

// Upload 在有限解码并发中生成预览；成功落库之前检查任务版本，迟到上传不会写入新业务代次。
func (s *DeliveryService) Upload(ctx context.Context, id uuid.UUID, version int, kind, name, group string, source io.Reader) (uuid.UUID, error) {
	if kind != "proof" && kind != "final" {
		return uuid.Nil, fault.InvalidRequest("无效的照片类型")
	}
	if len([]rune(group)) > 120 {
		return uuid.Nil, fault.InvalidRequest("分组名称过长")
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if len(name) > 255 || name == "." || strings.ContainsAny(name, "\r\n\x00") {
		return uuid.Nil, fault.InvalidRequest("无效的文件名")
	}
	cfg, err := s.config.DeliverySettings(ctx)
	if err != nil {
		return uuid.Nil, fault.Unavailable(err)
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return uuid.Nil, ctx.Err()
	}
	raw, err := io.ReadAll(io.LimitReader(source, s.maxUpload+1))
	if err != nil {
		return uuid.Nil, err
	}
	if int64(len(raw)) > s.maxUpload {
		return uuid.Nil, fault.InvalidRequest("文件超过上传上限")
	}
	preview, err := imagecompress.DeliveryPreview(raw, cfg.PreviewBytes, cfg.LongEdge)
	if err != nil {
		return uuid.Nil, fault.InvalidRequest("图片无法生成预览，请使用有效的 JPG、PNG、WebP 或静态 GIF；RAW 请配套上传 JPG")
	}
	storeType, err := s.config.CurrentFileStoreType(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	key, err := s.files.Store(ctx, storeType, name, bytes.NewReader(raw), s.maxUpload)
	if err != nil {
		return uuid.Nil, err
	}
	pk, err := s.files.Store(ctx, storeType, "preview.jpg", bytes.NewReader(preview), s.maxUpload)
	if err != nil {
		_ = s.files.Delete(context.WithoutCancel(ctx), key)
		return uuid.Nil, err
	}
	photoID := uuid.New()
	digest := sha256.Sum256(raw)
	err = s.repository.DeliveryTransaction(ctx, id, true, func(b *store.DeliveryBundle) error {
		if b.Task.Version != version {
			return fault.Conflict("任务已更新，请刷新后重试")
		}
		if b.Task.Status != "active" {
			return fault.Conflict("任务当前不可上传")
		}
		if len(b.Photos) >= cfg.MaxPhotos {
			return fault.Conflict("照片数量已达上限")
		}
		total := int64(len(raw) + len(preview))
		for _, p := range b.Photos {
			total += p.Bytes + int64(p.PreviewBytes)
			if p.Kind == kind && p.Digest == hex.EncodeToString(digest[:]) && p.GroupName == group {
				return fault.Conflict("该文件已上传")
			}
		}
		if total > int64(cfg.MaxTaskBytes) {
			return fault.Conflict("任务存储容量已达上限")
		}
		p := model.DeliveryPhoto{BaseModel: model.BaseModel{BusinessID: photoID}, UserID: b.Task.UserID, TaskID: id, Kind: kind, Filename: name, GroupName: group, OriginalKey: key, PreviewKey: pk, Digest: hex.EncodeToString(digest[:]), Bytes: int64(len(raw)), PreviewBytes: len(preview), Mark: "pending"}
		if kind == "final" {
			var matches []uuid.UUID
			for _, v := range b.Photos {
				if v.Kind == "proof" && strings.EqualFold(fileStem(v.Filename), fileStem(name)) {
					matches = append(matches, v.BusinessID)
				}
			}
			if len(matches) == 1 {
				p.SourceID = &matches[0]
			}
		}
		b.Photos = append(b.Photos, p)
		b.Task.Version++
		return nil
	})
	if err != nil {
		_ = s.files.Delete(context.WithoutCancel(ctx), pk)
		_ = s.files.Delete(context.WithoutCancel(ctx), key)
	}
	return photoID, err
}

func fileStem(name string) string  { return strings.TrimSuffix(name, path.Ext(name)) }
func deliveryHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func containsUUID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
func currentRound(b *store.DeliveryBundle) *model.DeliveryRound {
	for i := range b.Rounds {
		if b.Rounds[i].Number == b.Task.CurrentRound {
			return &b.Rounds[i]
		}
	}
	return nil
}

// Resolve 验证随机令牌和到期状态；访问码使用慢哈希，限流须在调用本函数前执行。
func (s *DeliveryService) Resolve(ctx context.Context, token, code string) (context.Context, uuid.UUID, error) {
	if len(token) != 43 {
		return ctx, uuid.Nil, fault.NotFound()
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return ctx, uuid.Nil, fault.NotFound()
	}
	t, err := s.repository.ResolveDelivery(ctx, deliveryHash(token))
	if err != nil {
		return ctx, uuid.Nil, err
	}
	if t.Status == "paused" || t.Status == "archived" || t.Status == "cancelled" || (t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now())) {
		return ctx, uuid.Nil, fault.NotFound()
	}
	if t.AccessHash != "" {
		// 公开访问码哈希与解码共用有界槽，防止并发耗尽内存。
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		case <-ctx.Done():
			return ctx, uuid.Nil, ctx.Err()
		}
		ok, err := s.passwords.Verify(code, t.AccessHash)
		if err != nil || !ok {
			return ctx, uuid.Nil, fault.Unauthorized()
		}
	}
	return tenant.WithScope(ctx, tenant.Scope{OrgID: t.OrgID, UserID: t.UserID}), t.BusinessID, nil
}

// AllowPublic 限制单进程每客户端每分钟公开请求；不保存令牌或访问码，避免暴力尝试拖垮哈希计算。
func (s *DeliveryService) AllowPublic(ctx context.Context, client string) (bool, error) {
	limit, err := s.config.DeliveryPublicRate(ctx)
	if err != nil {
		return false, err
	}
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	if len(s.rates) > 10000 {
		for k, v := range s.rates {
			if now.After(v.Until) {
				delete(s.rates, k)
			}
		}
		if len(s.rates) > 10000 {
			return false, nil
		}
	}
	r := s.rates[client]
	if now.After(r.Until) {
		r = deliveryRate{Until: now.Add(time.Minute)}
	}
	r.Count++
	s.rates[client] = r
	return r.Count <= limit, nil
}

// DeliverySelection 使用轮次和版本防止旧页面覆盖新轮次或多设备修改；仅最终提交检查精确数量。
type DeliverySelection struct {
	Round       int               `json:"round"`
	Version     int               `json:"version"`
	SelectedIDs []string          `json:"selected_ids"`
	Notes       map[string]string `json:"notes"`
	Submit      bool              `json:"submit"`
}

// Select 保存客户选择并在提交后锁定；相同已提交请求可幂等返回。
func (s *DeliveryService) Select(ctx context.Context, id uuid.UUID, in DeliverySelection) error {
	return s.repository.DeliveryTransaction(ctx, id, true, func(b *store.DeliveryBundle) error {
		r := currentRound(b)
		if r == nil || r.Number != in.Round {
			return fault.Conflict("选片轮次已变化，请刷新")
		}
		if r.Status == "submitted" || r.Status == "confirmed" {
			if in.Submit && sameIDs(r.SelectedIDs, in.SelectedIDs) {
				return nil
			}
			return fault.Conflict("选片已锁定")
		}
		if b.Task.Status != "active" || b.Task.Stage != "select" || r.Status != "open" {
			return fault.Conflict("当前不能修改选片")
		}
		if r.Deadline != nil && !r.Deadline.After(time.Now()) {
			return fault.Conflict("选片已截止，请联系摄影师")
		}
		if r.Version != in.Version {
			return fault.Conflict("其他设备已更新选片，请刷新")
		}
		allowed := map[string]bool{}
		for _, v := range r.PhotoIDs {
			allowed[v] = true
		}
		seen := map[string]bool{}
		for _, v := range in.SelectedIDs {
			if !allowed[v] || seen[v] {
				return fault.InvalidRequest("选片包含重复或未发布照片")
			}
			seen[v] = true
		}
		if r.LimitMode != "unlimited" && len(in.SelectedIDs) > r.SelectionLimit {
			return fault.InvalidRequest("已超过可选张数")
		}
		if in.Submit && r.LimitMode == "exact" && len(in.SelectedIDs) != r.SelectionLimit {
			return fault.InvalidRequest("请选满要求张数后提交")
		}
		if len(in.Notes) > len(allowed)+1 {
			return fault.InvalidRequest("备注数量过多")
		}
		for k, v := range in.Notes {
			if k != "overall" && !allowed[k] {
				return fault.InvalidRequest("备注引用未发布照片")
			}
			if len(v) > 2000 {
				return fault.InvalidRequest("备注过长")
			}
		}
		notes, err := json.Marshal(in.Notes)
		if err != nil {
			return err
		}
		r.SelectedIDs = pq.StringArray(in.SelectedIDs)
		if r.SelectedIDs == nil {
			r.SelectedIDs = pq.StringArray{}
		}
		r.Notes = model.JSONB(notes)
		r.Version++
		if in.Submit {
			now := time.Now()
			r.Status = "submitted"
			r.SubmittedAt = &now
			b.Task.Version++
			b.Events = append(b.Events, model.DeliveryEvent{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: b.Task.UserID, TaskID: id, Action: "selection_submitted", Stage: b.Task.Stage})
		}
		return nil
	})
}
func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		m[v]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

// Receive 记录客户显式收片，不以页面访问或发起下载推断收片完成。
func (s *DeliveryService) Receive(ctx context.Context, id uuid.UUID) error {
	return s.repository.DeliveryTransaction(ctx, id, true, func(b *store.DeliveryBundle) error {
		if b.Task.Stage != "delivered" {
			return fault.Conflict("尚未发布精修")
		}
		if b.Task.ReceivedAt == nil {
			now := time.Now()
			b.Task.ReceivedAt = &now
			b.Task.Version++
			b.Events = append(b.Events, model.DeliveryEvent{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: b.Task.UserID, TaskID: id, Action: "received", Stage: b.Task.Stage})
		}
		return nil
	})
}

// OpenPhoto 在文件服务之前校验任务关联、发布快照和下载开关，不接受任意 storekey。
func (s *DeliveryService) OpenPhoto(ctx context.Context, id, photoID uuid.UUID, original, public bool) (io.ReadCloser, int64, string, string, error) {
	var photo model.DeliveryPhoto
	err := s.repository.DeliveryTransaction(ctx, id, false, func(b *store.DeliveryBundle) error {
		for _, p := range b.Photos {
			if p.BusinessID == photoID {
				photo = p
				break
			}
		}
		if photo.BusinessID == uuid.Nil {
			return fault.NotFound()
		}
		if public {
			allowed := false
			if photo.Kind == "proof" {
				r := currentRound(b)
				if r != nil {
					for _, v := range r.PhotoIDs {
						if v == photoID.String() {
							allowed = true
						}
					}
				}
				if original && !b.Task.AllowOriginal {
					return fault.NotFound()
				}
			} else {
				allowed = photo.PublishedBatch > 0
				if original && !b.Task.AllowDownload {
					return fault.NotFound()
				}
			}
			if !allowed {
				return fault.NotFound()
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, "", "", err
	}
	key := photo.PreviewKey
	name := "preview.jpg"
	if original {
		key = photo.OriginalKey
		name = photo.Filename
	}
	r, n, mime, err := s.files.OpenContext(ctx, key)
	return r, n, mime, name, err
}

// OpenPhotoThumbnail 先经过与大预览相同的发布授权，再使用文件服务的有界缩略图缓存。
func (s *DeliveryService) OpenPhotoThumbnail(ctx context.Context, id, photoID uuid.UUID, public bool) (io.ReadCloser, int64, error) {
	r, _, _, _, err := s.OpenPhoto(ctx, id, photoID, false, public)
	if err != nil {
		return nil, 0, err
	}
	r.Close()
	var key string
	err = s.repository.DeliveryTransaction(ctx, id, false, func(b *store.DeliveryBundle) error {
		for _, p := range b.Photos {
			if p.BusinessID == photoID {
				key = p.PreviewKey
				return nil
			}
		}
		return fault.NotFound()
	})
	if err != nil {
		return nil, 0, err
	}
	return s.files.OpenThumbnail(ctx, key)
}
