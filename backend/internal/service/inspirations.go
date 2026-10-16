package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/imagecompress"
	"github.com/photo-nini/backend/internal/model"
)

// InspirationRepository 定义灵感图 CRUD、分页检索及标签业务 UUID 所有权校验接口。
type InspirationRepository interface {
	ValidateTagOwnership(context.Context, uuid.UUID, []uuid.UUID) error
	CreateInspiration(context.Context, *model.Inspiration) error
	ListInspirations(context.Context, uuid.UUID, []uuid.UUID, string, string, string, int, int) ([]model.Inspiration, int64, error)
	FindInspirationByBusinessID(context.Context, uuid.UUID, uuid.UUID) (model.Inspiration, error)
	UpdateInspiration(context.Context, uuid.UUID, uuid.UUID, map[string]any) (model.Inspiration, error)
	UpdateInspirationWithAsyncVersion(context.Context, uuid.UUID, uuid.UUID, map[string]any) (model.Inspiration, error)
	UpdateInspirationFileMetadataIfVersion(context.Context, uuid.UUID, uuid.UUID, string, int64, *string, *int64) (bool, error)
	DeleteInspiration(context.Context, uuid.UUID, uuid.UUID) error
}

// InspirationFileStore 提供按 storekey 读取和覆盖内容的能力，service 不依赖本地磁盘目录布局。
type InspirationFileStore interface {
	Open(string) (io.ReadCloser, int64, string, error)
	Replace(context.Context, string, []byte) error
}

// InspirationService 校验编辑数据、标签归属和图片文件信息，所有资源范围来自 access token 用户 UUID。
type InspirationService struct {
	inspirations       InspirationRepository
	files              InspirationFileStore
	errorLogs          *ErrorLogService
	compressionNotices InspirationCompressionNotifier
	maxImageBytes      int64
	fileLocks          storeKeyLocks
	compressionMutex   sync.Mutex
	compressionCond    *sync.Cond
	compressionJobs    []inspirationCompressionJob
	compressionActive  int
}

// InspirationCompressionNotifier 只接收可展示的异步处理终态，外部 webhook 本身由 notice outbox worker 独立发送。
type InspirationCompressionNotifier interface {
	PublishInspirationCompressionResult(context.Context, uuid.UUID, uuid.UUID, string, int64, bool) error
}

const (
	// 灵感图压缩会解码图片并占用较多 CPU 和内存，因此固定后台并发数，避免请求高峰放大资源占用。
	inspirationCompressionWorkers = 2
	inspirationCompressionTimeout = 5 * time.Minute
)

// inspirationCompressionJob 只携带业务 UUID、图片 key 和创建/更新时捕获的异步代次。
type inspirationCompressionJob struct {
	userID        uuid.UUID
	inspirationID uuid.UUID
	imageKey      string
	asyncVersion  int64
}

// storeKeyLockEntry 为同一进程内同一个 storekey 的压缩与数据库写入提供串行区间。
type storeKeyLockEntry struct {
	mutex sync.Mutex
	users int
}

// storeKeyLocks 按 key 懒创建互斥锁，并在最后一个调用完成后删除，避免长期累积文件名。
type storeKeyLocks struct {
	mutex sync.Mutex
	items map[string]*storeKeyLockEntry
}

// acquire 持有某个 storekey 的互斥锁，调用方应在数据库写入结束后释放。
func (locks *storeKeyLocks) acquire(storekey string) func() {
	locks.mutex.Lock()
	if locks.items == nil {
		locks.items = make(map[string]*storeKeyLockEntry)
	}
	entry := locks.items[storekey]
	if entry == nil {
		entry = &storeKeyLockEntry{}
		locks.items[storekey] = entry
	}
	entry.users++
	locks.mutex.Unlock()

	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()
		locks.mutex.Lock()
		entry.users--
		if entry.users == 0 {
			delete(locks.items, storekey)
		}
		locks.mutex.Unlock()
	}
}

// NewInspirationService 注入数据库和文件存储，并启动固定并发的后台图片处理工作线程。
func NewInspirationService(inspirations InspirationRepository, files InspirationFileStore, maxImageBytes int64) *InspirationService {
	service := &InspirationService{inspirations: inspirations, files: files, maxImageBytes: maxImageBytes}
	service.compressionCond = sync.NewCond(&service.compressionMutex)
	for worker := 0; worker < inspirationCompressionWorkers; worker++ {
		go service.runCompressionWorker()
	}
	return service
}

// SetErrorLogService 将后台图片处理故障接入统一错误日志表。
func (service *InspirationService) SetErrorLogService(errorLogs *ErrorLogService) {
	service.errorLogs = errorLogs
}

// SetCompressionNotifier 注入异步任务终态发布器；未配置时图片压缩仍可独立运行。
func (service *InspirationService) SetCompressionNotifier(notifier InspirationCompressionNotifier) {
	service.compressionNotices = notifier
}

// InspirationInput 是创建或局部更新时已解析的业务数据，不包含 user_id 或数据库 id。
type InspirationInput struct {
	TagIDs   []uuid.UUID
	ImageKey string
	FileName string
	Data     json.RawMessage
}

// inspirationFileMetadata 描述 storekey 文件压缩完成后按最终实际字节计算的元数据。
type inspirationFileMetadata struct {
	// fileHash 是最终落盘字节的 SHA-256 摘要。
	fileHash *string
	// fileSize 是最终落盘字节数，单位为字节。
	fileSize *int64
}

// inspirationFileCompression 保存后台压缩出的字节和元数据；文件覆盖须在确认异步代次仍有效后进行。
type inspirationFileCompression struct {
	content  []byte
	changed  bool
	metadata inspirationFileMetadata
}

// List 在数据库中按当前用户及 any/all 标签条件分页查询。
func (service *InspirationService) List(ctx context.Context, userID uuid.UUID, tagIDs []uuid.UUID, match, query, sort string, page PageRequest) (Page[model.Inspiration], error) {
	if match == "" {
		match = "any"
	}
	if match != "any" && match != "all" {
		return Page[model.Inspiration]{}, fault.InvalidRequest("tag_match 只能是 any 或 all")
	}
	query = strings.TrimSpace(query)
	if len(query) > 200 {
		return Page[model.Inspiration]{}, fault.InvalidRequest("q 长度不能超过 200 个字节")
	}
	if sort == "" {
		sort = "created_desc"
	}
	if sort != "created_desc" && sort != "created_asc" && sort != "updated_desc" {
		return Page[model.Inspiration]{}, fault.InvalidRequest("sort 只支持 created_desc、created_asc 或 updated_desc")
	}
	items, total, err := service.inspirations.ListInspirations(ctx, userID, tagIDs, match, query, sort, page.Offset, page.PageSize)
	if err != nil {
		return Page[model.Inspiration]{}, err
	}
	return Page[model.Inspiration]{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// Create 同步校验并保存灵感图记录；有效 storekey 的压缩、文件覆盖和元数据计算进入后台队列。
func (service *InspirationService) Create(ctx context.Context, userID uuid.UUID, input InspirationInput) (model.Inspiration, error) {
	input.ImageKey = strings.TrimSpace(input.ImageKey)
	if input.ImageKey == "" {
		return model.Inspiration{}, fault.InvalidField("image_key", "图片 key 不能为空")
	}
	storeKeyImage, err := service.validateInspirationImageKey(input.ImageKey)
	if err != nil {
		return model.Inspiration{}, err
	}
	fileName, err := normalizeInspirationFileName(input.FileName)
	if err != nil {
		return model.Inspiration{}, err
	}
	if err := validateEditorData(input.Data); err != nil {
		return model.Inspiration{}, err
	}
	if err := service.inspirations.ValidateTagOwnership(ctx, userID, input.TagIDs); err != nil {
		return model.Inspiration{}, err
	}
	inspiration := model.Inspiration{
		BaseModel: model.BaseModel{BusinessID: uuid.New()},
		UserID:    userID,
		TagIDs:    uuidStrings(input.TagIDs),
		ImageKey:  input.ImageKey,
		FileName:  fileName,
		FileHash:  nil,
		FileSize:  nil,
		Data:      model.JSONB(append([]byte(nil), input.Data...)),
	}
	if err := service.inspirations.CreateInspiration(ctx, &inspiration); err != nil {
		return model.Inspiration{}, err
	}
	if storeKeyImage {
		service.enqueueCompression(inspirationCompressionJob{userID: userID, inspirationID: inspiration.BusinessID,
			imageKey: input.ImageKey, asyncVersion: inspiration.AsyncVersion})
	}
	return inspiration, nil
}

// Get 只允许从已认证用户的灵感图集合中按业务 UUID 读取详情。
func (service *InspirationService) Get(ctx context.Context, userID, inspirationID uuid.UUID) (model.Inspiration, error) {
	return service.inspirations.FindInspirationByBusinessID(ctx, userID, inspirationID)
}

// Update 保留未提交字段；提交有效 storekey 时先递增异步代次并保存记录，再排队处理图片。
func (service *InspirationService) Update(ctx context.Context, userID, inspirationID uuid.UUID, input InspirationInput, tagIDsSet, imageKeySet, fileNameSet, dataSet bool) (model.Inspiration, error) {
	updates := make(map[string]any, 5)
	storeKeyImage := false
	if imageKeySet {
		input.ImageKey = strings.TrimSpace(input.ImageKey)
		if input.ImageKey == "" {
			return model.Inspiration{}, fault.InvalidField("image_key", "图片 key 不能为空")
		}
		validStoreKey, err := service.validateInspirationImageKey(input.ImageKey)
		if err != nil {
			return model.Inspiration{}, err
		}
		storeKeyImage = validStoreKey
		// 先确认更新目标存在，避免无效 business_id 也触发对 storekey 文件的覆盖。
		if _, err := service.inspirations.FindInspirationByBusinessID(ctx, userID, inspirationID); err != nil {
			return model.Inspiration{}, err
		}
	}
	if tagIDsSet {
		if err := service.inspirations.ValidateTagOwnership(ctx, userID, input.TagIDs); err != nil {
			return model.Inspiration{}, err
		}
		// 使用 pq.StringArray 明确编码空数组为 PostgreSQL '{}'，避免驱动将空切片写成 NULL。
		updates["tag_ids"] = pq.StringArray(uuidStrings(input.TagIDs))
	}
	if fileNameSet {
		fileName, err := normalizeInspirationFileName(input.FileName)
		if err != nil {
			return model.Inspiration{}, err
		}
		updates["file_name"] = fileName
	}
	if dataSet {
		if err := validateEditorData(input.Data); err != nil {
			return model.Inspiration{}, err
		}
		updates["data"] = model.JSONB(append([]byte(nil), input.Data...))
	}
	if imageKeySet {
		updates["image_key"] = input.ImageKey
		// 新图片尚未处理完成时清空旧文件元数据，避免把旧图的哈希或大小返回给前端。
		updates["file_hash"] = nil
		updates["file_size"] = nil
	}
	if len(updates) == 0 {
		return model.Inspiration{}, fault.InvalidRequest("至少提供 tag_ids、image_key、file_name 或 data 中的一个字段")
	}
	if storeKeyImage {
		updated, err := service.inspirations.UpdateInspirationWithAsyncVersion(ctx, userID, inspirationID, updates)
		if err != nil {
			return model.Inspiration{}, err
		}
		service.enqueueCompression(inspirationCompressionJob{userID: userID, inspirationID: inspirationID,
			imageKey: input.ImageKey, asyncVersion: updated.AsyncVersion})
		return updated, nil
	}
	return service.inspirations.UpdateInspiration(ctx, userID, inspirationID, updates)
}

// validateInspirationImageKey 同步拒绝格式错误的 storekey，并在提交异步任务前检查文件处理依赖。
func (service *InspirationService) validateInspirationImageKey(imageKey string) (bool, error) {
	if _, err := filestore.ParseKey(imageKey); err == nil {
		if service.files == nil || service.maxImageBytes < 1 {
			return false, fault.Unavailable(errors.New("灵感图文件存储或图片大小配置未初始化"))
		}
		return true, nil
	}
	// 已声明 local/oss 类型前缀却格式错误时拒绝保存，避免错误 key 被当作普通图片标识绕过校验。
	if strings.HasPrefix(imageKey, "local_") || strings.HasPrefix(imageKey, "oss_") {
		return false, fault.InvalidField("image_key", "storekey 格式无效")
	}
	return false, nil
}

// compressStoreKeyContent 在后台读取并压缩有效 storekey 图片，但暂不覆盖文件以便先检查任务代次。
func (service *InspirationService) compressStoreKeyContent(ctx context.Context, imageKey string) (*inspirationFileCompression, error) {
	if _, err := filestore.ParseKey(imageKey); err != nil {
		return nil, fault.InvalidField("image_key", "storekey 格式无效")
	}
	if service.files == nil || service.maxImageBytes < 1 {
		return nil, fault.Unavailable(errors.New("灵感图文件存储或图片大小配置未初始化"))
	}
	select {
	case <-ctx.Done():
		return nil, fault.Unavailable(ctx.Err())
	default:
	}
	reader, size, _, err := service.files.Open(imageKey)
	if err != nil {
		return nil, mapInspirationFileError(err)
	}
	if reader == nil {
		return nil, fault.Internal(errors.New("文件读取服务未返回内容流"))
	}
	defer reader.Close()
	if size > service.maxImageBytes {
		return nil, fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "storekey 对应的图片超过允许处理大小", nil, filestore.ErrFileTooLarge)
	}
	original, err := io.ReadAll(io.LimitReader(reader, service.maxImageBytes+1))
	if err != nil {
		return nil, fault.Internal(err)
	}
	if int64(len(original)) > service.maxImageBytes {
		return nil, fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "storekey 对应的图片超过允许处理大小", nil, filestore.ErrFileTooLarge)
	}
	compressed, changed, err := imagecompress.Compress(original)
	if err != nil {
		switch {
		case errors.Is(err, imagecompress.ErrUnsupportedImage):
			return nil, fault.InvalidField("image_key", "storekey 内容必须是有效的 JPEG、PNG、GIF 或 WebP 图片")
		case errors.Is(err, imagecompress.ErrImageDimensions):
			return nil, fault.InvalidField("image_key", "图片像素总量超过服务端处理上限")
		case errors.Is(err, imagecompress.ErrAnimatedImage):
			return nil, fault.InvalidField("image_key", "超过 1 MB 的动画图片暂不支持保存为灵感图")
		case errors.Is(err, imagecompress.ErrCompression):
			return nil, fault.InvalidField("image_key", "图片无法压缩到 1 MB 以内")
		default:
			return nil, fault.Internal(err)
		}
	}
	hashBytes := sha256.Sum256(compressed)
	hash := hex.EncodeToString(hashBytes[:])
	finalSize := int64(len(compressed))
	return &inspirationFileCompression{content: compressed, changed: changed,
		metadata: inspirationFileMetadata{fileHash: &hash, fileSize: &finalSize}}, nil
}

// enqueueCompression 只将轻量任务描述加入进程内队列；同一记录保留最新代次，避免连续更新堆积过期任务。
func (service *InspirationService) enqueueCompression(job inspirationCompressionJob) {
	service.compressionMutex.Lock()
	for index := range service.compressionJobs {
		queued := &service.compressionJobs[index]
		if queued.inspirationID != job.inspirationID {
			continue
		}
		if job.asyncVersion > queued.asyncVersion {
			*queued = job
		}
		service.compressionCond.Signal()
		service.compressionMutex.Unlock()
		return
	}
	service.compressionJobs = append(service.compressionJobs, job)
	service.compressionCond.Signal()
	service.compressionMutex.Unlock()
}

// runCompressionWorker 按固定并发数消费后台任务，异步失败只记录错误类型，不记录图片 key 或底层路径。
func (service *InspirationService) runCompressionWorker() {
	for {
		service.compressionMutex.Lock()
		for len(service.compressionJobs) == 0 {
			service.compressionCond.Wait()
		}
		job := service.compressionJobs[0]
		service.compressionJobs[0] = inspirationCompressionJob{}
		service.compressionJobs = service.compressionJobs[1:]
		if len(service.compressionJobs) == 0 {
			service.compressionJobs = nil
		}
		service.compressionActive++
		service.compressionMutex.Unlock()

		if err := service.processCompressionJob(job); err != nil {
			if service.errorLogs != nil {
				service.errorLogs.CaptureBackground("inspiration_compression_failed", err)
			} else {
				log.Printf("灵感图异步图片处理失败，错误类型=%T", err)
			}
			if service.compressionNotices != nil {
				service.publishCompressionNotice(job, false)
			}
		}

		service.compressionMutex.Lock()
		service.compressionActive--
		service.compressionMutex.Unlock()
	}
}

// processCompressionJob 在 storekey 锁内压缩文件，并在文件覆盖及元数据写回前分别核对异步代次。
func (service *InspirationService) processCompressionJob(job inspirationCompressionJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), inspirationCompressionTimeout)
	defer cancel()
	unlockFile := service.fileLocks.acquire(job.imageKey)
	defer unlockFile()

	current, err := service.inspirations.FindInspirationByBusinessID(ctx, job.userID, job.inspirationID)
	if err != nil {
		if fault.Is(err, http.StatusNotFound, "not_found") {
			return nil
		}
		return err
	}
	if current.ImageKey != job.imageKey || current.AsyncVersion != job.asyncVersion {
		return nil
	}

	compressed, err := service.compressStoreKeyContent(ctx, job.imageKey)
	if err != nil {
		return err
	}
	// 图片解码可能耗时；处理结束后重读当前代次，丢弃期间已被新请求替代的结果。
	current, err = service.inspirations.FindInspirationByBusinessID(ctx, job.userID, job.inspirationID)
	if err != nil {
		if fault.Is(err, http.StatusNotFound, "not_found") {
			return nil
		}
		return err
	}
	if current.ImageKey != job.imageKey || current.AsyncVersion != job.asyncVersion {
		return nil
	}
	if compressed.changed {
		if err := service.files.Replace(ctx, job.imageKey, compressed.content); err != nil {
			return mapInspirationFileError(err)
		}
	}
	updated, err := service.inspirations.UpdateInspirationFileMetadataIfVersion(ctx, job.userID, job.inspirationID,
		job.imageKey, job.asyncVersion, compressed.metadata.fileHash, compressed.metadata.fileSize)
	if err != nil || !updated {
		return err
	}
	if service.compressionNotices != nil {
		service.publishCompressionNotice(job, true)
	}
	return nil
}

// publishCompressionNotice 在后台有限重试创建终态通知；通知存储短暂不可用时不把成功压缩误报成失败。
func (service *InspirationService) publishCompressionNotice(job inspirationCompressionJob, succeeded bool) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		lastErr = service.compressionNotices.PublishInspirationCompressionResult(ctx, job.userID, job.inspirationID, job.imageKey, job.asyncVersion, succeeded)
		cancel()
		if lastErr == nil {
			return
		}
		if attempt < 2 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if service.errorLogs != nil {
		service.errorLogs.CaptureBackground("inspiration_compression_notice_failed", lastErr)
	} else {
		log.Printf("灵感图异步通知创建失败，错误类型=%T", lastErr)
	}
}

// WaitForCompression 等待队列清空，供进程收到退出信号后在超时前完成已接受的图片处理任务。
func (service *InspirationService) WaitForCompression(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		service.compressionMutex.Lock()
		drained := len(service.compressionJobs) == 0 && service.compressionActive == 0
		service.compressionMutex.Unlock()
		if drained {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// mapInspirationFileError 将文件后端错误转为稳定 API 语义，不向客户端泄露本地路径。
func mapInspirationFileError(err error) error {
	switch {
	case errors.Is(err, filestore.ErrInvalidStoreKey):
		return fault.InvalidField("image_key", "storekey 格式无效")
	case errors.Is(err, filestore.ErrFileNotFound):
		return fault.NotFound()
	case errors.Is(err, filestore.ErrFileTooLarge):
		return fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "storekey 对应的图片超过允许处理大小", nil, err)
	case errors.Is(err, filestore.ErrBackendUnavailable):
		return fault.New(http.StatusNotImplemented, "storage_backend_unavailable", "此文件存储类型暂不可用", nil, err)
	default:
		return fault.Internal(err)
	}
}

// normalizeInspirationFileName 清除路径部分并限制展示文件名长度，避免保存客户端设备上的本地目录信息。
func normalizeInspirationFileName(fileName string) (string, error) {
	fileName = strings.TrimSpace(strings.ReplaceAll(fileName, "\\", "/"))
	if fileName != "" {
		fileName = path.Base(fileName)
	}
	if fileName == "." || fileName == "/" {
		fileName = ""
	}
	if utf8.RuneCountInString(fileName) > 255 {
		return "", fault.InvalidField("file_name", "文件名最多允许 255 个字符")
	}
	return fileName, nil
}

// Delete 物理删除当前用户拥有的灵感图。
func (service *InspirationService) Delete(ctx context.Context, userID, inspirationID uuid.UUID) error {
	return service.inspirations.DeleteInspiration(ctx, userID, inspirationID)
}

// validateEditorData 只接受 JSON 对象并校验正整数 schema_version，保留其余编辑器字段原样存储。
func validateEditorData(data json.RawMessage) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return fault.InvalidField("data", "data 必须是有效的 JSON 对象")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil || object == nil {
		return fault.InvalidField("data", "data 必须是 JSON 对象")
	}
	versionJSON, exists := object["schema_version"]
	if !exists {
		return fault.InvalidField("data.schema_version", "data 必须包含正整数 schema_version")
	}
	var version json.Number
	decoder := json.NewDecoder(bytes.NewReader(versionJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&version); err != nil {
		return fault.InvalidField("data.schema_version", "schema_version 必须是正整数")
	}
	parsed, err := strconv.ParseInt(version.String(), 10, 64)
	if err != nil || parsed < 1 {
		return fault.InvalidField("data.schema_version", "schema_version 必须是正整数")
	}
	return nil
}

// uuidStrings 将业务 UUID 转为 PostgreSQL uuid[] 驱动可扫描的字符串数组。
func uuidStrings(values []uuid.UUID) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.String())
	}
	return result
}
