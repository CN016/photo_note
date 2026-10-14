package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
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
	ListInspirations(context.Context, uuid.UUID, []uuid.UUID, string, int, int) ([]model.Inspiration, int64, error)
	FindInspirationByBusinessID(context.Context, uuid.UUID, uuid.UUID) (model.Inspiration, error)
	UpdateInspiration(context.Context, uuid.UUID, uuid.UUID, map[string]any) (model.Inspiration, error)
	DeleteInspiration(context.Context, uuid.UUID, uuid.UUID) error
}

// InspirationFileStore 提供按 storekey 读取和覆盖内容的能力，service 不依赖本地磁盘目录布局。
type InspirationFileStore interface {
	Open(string) (io.ReadCloser, int64, string, error)
	Replace(context.Context, string, []byte) error
}

// InspirationService 校验编辑数据、标签归属和图片文件信息，所有资源范围来自 access token 用户 UUID。
type InspirationService struct {
	inspirations  InspirationRepository
	files         InspirationFileStore
	maxImageBytes int64
	fileLocks     storeKeyLocks
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

// NewInspirationService 注入数据库、文件存储和上传大小上限，供图片压缩时限制输入内存。
func NewInspirationService(inspirations InspirationRepository, files InspirationFileStore, maxImageBytes int64) *InspirationService {
	return &InspirationService{inspirations: inspirations, files: files, maxImageBytes: maxImageBytes}
}

// InspirationInput 是创建或局部更新时已解析的业务数据，不包含 user_id 或数据库 id。
type InspirationInput struct {
	TagIDs   []uuid.UUID
	ImageKey string
	FileName string
	Data     json.RawMessage
}

// List 在数据库中按当前用户及 any/all 标签条件分页查询。
func (service *InspirationService) List(ctx context.Context, userID uuid.UUID, tagIDs []uuid.UUID, match string, page PageRequest) (Page[model.Inspiration], error) {
	if match == "" {
		match = "any"
	}
	if match != "any" && match != "all" {
		return Page[model.Inspiration]{}, fault.InvalidRequest("tag_match 只能是 any 或 all")
	}
	items, total, err := service.inspirations.ListInspirations(ctx, userID, tagIDs, match, page.Offset, page.PageSize)
	if err != nil {
		return Page[model.Inspiration]{}, err
	}
	return Page[model.Inspiration]{Items: items, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// Create 校验 JSON 和标签后压缩有效 storekey 图片、原位覆盖文件并为最终字节生成 SHA-256。
func (service *InspirationService) Create(ctx context.Context, userID uuid.UUID, input InspirationInput) (model.Inspiration, error) {
	input.ImageKey = strings.TrimSpace(input.ImageKey)
	if input.ImageKey == "" {
		return model.Inspiration{}, fault.InvalidField("image_key", "图片 key 不能为空")
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
	// 同一服务进程中的并发保存按 storekey 串行，防止同时改写同一文件造成哈希错配。
	unlockFile := service.fileLocks.acquire(input.ImageKey)
	defer unlockFile()
	fileHash, err := service.compressStoreKeyContent(ctx, input.ImageKey)
	if err != nil {
		return model.Inspiration{}, err
	}
	inspiration := model.Inspiration{
		BaseModel: model.BaseModel{BusinessID: uuid.New()},
		UserID:    userID,
		TagIDs:    uuidStrings(input.TagIDs),
		ImageKey:  input.ImageKey,
		FileName:  fileName,
		FileHash:  fileHash,
		Data:      model.JSONB(append([]byte(nil), input.Data...)),
	}
	if err := service.inspirations.CreateInspiration(ctx, &inspiration); err != nil {
		return model.Inspiration{}, err
	}
	return inspiration, nil
}

// Get 只允许从已认证用户的灵感图集合中按业务 UUID 读取详情。
func (service *InspirationService) Get(ctx context.Context, userID, inspirationID uuid.UUID) (model.Inspiration, error) {
	return service.inspirations.FindInspirationByBusinessID(ctx, userID, inspirationID)
}

// Update 保留未提交字段；提交 storekey 时先确认灵感图存在，再压缩覆盖文件并更新最终内容哈希。
func (service *InspirationService) Update(ctx context.Context, userID, inspirationID uuid.UUID, input InspirationInput, tagIDsSet, imageKeySet, fileNameSet, dataSet bool) (model.Inspiration, error) {
	updates := make(map[string]any, 5)
	if imageKeySet {
		input.ImageKey = strings.TrimSpace(input.ImageKey)
		if input.ImageKey == "" {
			return model.Inspiration{}, fault.InvalidField("image_key", "图片 key 不能为空")
		}
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
		// 锁覆盖图片写入和灵感图更新，保持共享 storekey 的文件字节与哈希顺序一致。
		unlockFile := service.fileLocks.acquire(input.ImageKey)
		defer unlockFile()
		fileHash, err := service.compressStoreKeyContent(ctx, input.ImageKey)
		if err != nil {
			return model.Inspiration{}, err
		}
		updates["image_key"] = input.ImageKey
		updates["file_hash"] = fileHash
	}
	if len(updates) == 0 {
		return model.Inspiration{}, fault.InvalidRequest("至少提供 tag_ids、image_key、file_name 或 data 中的一个字段")
	}
	return service.inspirations.UpdateInspiration(ctx, userID, inspirationID, updates)
}

// compressStoreKeyContent 对有效 storekey 读取并压缩图片，原位覆盖后再对实际存储字节计算 SHA-256。
func (service *InspirationService) compressStoreKeyContent(ctx context.Context, imageKey string) (*string, error) {
	if _, err := filestore.ParseKey(imageKey); err != nil {
		// 已声明 local/oss 类型前缀却格式错误时拒绝保存，防止坏 storekey 被当作普通图片 key 绕过哈希校验。
		if strings.HasPrefix(imageKey, "local_") || strings.HasPrefix(imageKey, "oss_") {
			return nil, fault.InvalidField("image_key", "storekey 格式无效")
		}
		return nil, nil
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
	if changed {
		if err := service.files.Replace(ctx, imageKey, compressed); err != nil {
			return nil, mapInspirationFileError(err)
		}
	}
	hashBytes := sha256.Sum256(compressed)
	hash := hex.EncodeToString(hashBytes[:])
	return &hash, nil
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
