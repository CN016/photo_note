// Package filestore 根据 storekey 的存储类型前缀保存并读取文件；V1 不维护文件元数据或用户归属。
package filestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrInvalidStoreKey 表示 key 不符合服务端规定的格式，避免把客户端输入直接用于文件路径。
	ErrInvalidStoreKey = errors.New("storekey 格式无效")
	// ErrFileTooLarge 表示文件字节数超过配置表中的 files.max_upload_bytes。
	ErrFileTooLarge = errors.New("上传文件超过配置大小")
	// ErrFileNotFound 表示 key 对应的本地文件不存在。
	ErrFileNotFound = errors.New("文件不存在")
	// ErrBackendUnavailable 表示 key 指向的存储后端当前尚未接入。
	ErrBackendUnavailable = errors.New("文件存储类型暂不可用")
)

const (
	localType = "local"
	ossType   = "oss"
)

var extensionPattern = regexp.MustCompile(`^[a-zA-Z0-9]{1,16}$`)

// Manager 持有本地文件根目录；文件 key 是唯一索引，不依赖数据库记录或用户 ID。
type Manager struct {
	root string
}

// ParsedKey 是通过格式校验后的 storekey 字段，用于安全生成后端存储路径。
type ParsedKey struct {
	Type      string
	Date      time.Time
	UUID      uuid.UUID
	Extension string
	// Public 只用于反馈图片专属命名空间；公共与私有文件实际保存在不同目录。
	Public bool
}

// NewManager 校验并创建本地存储根目录；目录权限错误会在服务启动阶段暴露。
func NewManager(root string) (*Manager, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("文件存储根目录不能为空")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析文件存储根目录失败: %w", err)
	}
	if err := os.MkdirAll(absoluteRoot, 0o750); err != nil {
		return nil, fmt.Errorf("创建文件存储根目录失败: %w", err)
	}
	return &Manager{root: absoluteRoot}, nil
}

// Store 使用服务端生成的日期和随机 UUID 保存文件，且通过实际读取量执行配置大小限制。
func (manager *Manager) Store(ctx context.Context, storeType, originalFilename string, source io.Reader, maxBytes int64) (string, error) {
	return manager.store(ctx, storeType, originalFilename, source, maxBytes, false)
}

// StorePublic 仅供反馈图片使用公共 key，并将字节写入独立目录，避免给私有 key 加前缀即可公开读取。
func (manager *Manager) StorePublic(ctx context.Context, storeType, originalFilename string, source io.Reader, maxBytes int64) (string, error) {
	return manager.store(ctx, storeType, originalFilename, source, maxBytes, true)
}

// store 统一执行有大小和请求取消保护的本地写入；public 文件与普通文件使用分离的物理命名空间。
func (manager *Manager) store(ctx context.Context, storeType, originalFilename string, source io.Reader, maxBytes int64, public bool) (string, error) {
	if manager == nil || source == nil || maxBytes < 1 {
		return "", errors.New("文件存储参数无效")
	}
	if storeType == ossType {
		return "", ErrBackendUnavailable
	}
	if storeType != localType {
		return "", ErrInvalidStoreKey
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	now := time.Now().UTC()
	fileID, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("生成文件 UUID 失败: %w", err)
	}
	extension := safeExtension(originalFilename)
	key := ParsedKey{Type: storeType, Date: now, UUID: fileID, Extension: extension, Public: public}
	storekey := formatKey(key)
	path := manager.localPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("创建文件分片目录失败: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", fmt.Errorf("创建本地文件失败: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()

	limited := &contextReader{ctx: ctx, reader: io.LimitReader(source, maxBytes+1)}
	written, copyErr := io.Copy(file, limited)
	if copyErr != nil {
		return "", fmt.Errorf("写入本地文件失败: %w", copyErr)
	}
	if written > maxBytes {
		return "", ErrFileTooLarge
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("同步本地文件失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("关闭本地文件失败: %w", err)
	}
	complete = true
	return storekey, nil
}

// Open 按 storekey 类型分发文件读取；当前只接入 local，oss key 保留格式但明确返回未接入。
func (manager *Manager) Open(storekey string) (io.ReadCloser, int64, string, error) {
	if manager == nil {
		return nil, 0, "", ErrBackendUnavailable
	}
	key, err := ParseKey(storekey)
	if err != nil {
		return nil, 0, "", err
	}
	if key.Type == ossType {
		return nil, 0, "", ErrBackendUnavailable
	}
	file, err := os.Open(manager.localPath(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, "", ErrFileNotFound
		}
		return nil, 0, "", fmt.Errorf("打开本地文件失败: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, "", fmt.Errorf("读取本地文件信息失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, 0, "", ErrFileNotFound
	}
	// 图片可能在保留原 storekey 的情况下转换了编码格式，因此下载类型优先依据文件实际内容判断。
	contentType, err := detectContentType(file, key.Extension)
	if err != nil {
		_ = file.Close()
		return nil, 0, "", fmt.Errorf("读取本地文件类型失败: %w", err)
	}
	return file, info.Size(), contentType, nil
}

// Delete 按服务端格式校验后的 storekey 删除单个本地文件，用于数据库写入失败时回收新上传对象。
func (manager *Manager) Delete(ctx context.Context, storekey string) error {
	if manager == nil {
		return ErrBackendUnavailable
	}
	key, err := ParseKey(storekey)
	if err != nil {
		return err
	}
	if key.Type == ossType {
		return ErrBackendUnavailable
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	manager.removeThumbnails(manager.localPath(key), "")
	err = os.Remove(manager.localPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("删除本地文件失败: %w", err)
	}
	return nil
}

// Replace 原子覆盖已有本地 storekey 对应的文件，不创建新 key 或数据库元数据。
func (manager *Manager) Replace(ctx context.Context, storekey string, content []byte) error {
	if manager == nil || len(content) == 0 {
		return errors.New("文件覆盖参数无效")
	}
	key, err := ParseKey(storekey)
	if err != nil {
		return err
	}
	if key.Type == ossType {
		return ErrBackendUnavailable
	}
	targetPath := manager.localPath(key)
	info, err := os.Lstat(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrFileNotFound
		}
		return fmt.Errorf("检查待覆盖本地文件失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ErrFileNotFound
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	// 临时文件和目标文件位于同一目录，完成写入后通过 rename 切换，避免读取到半截内容。
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".photo-nini-replace-*")
	if err != nil {
		return fmt.Errorf("创建文件覆盖临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	complete := false
	defer func() {
		if !complete {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o640); err != nil {
		return fmt.Errorf("设置文件覆盖权限失败: %w", err)
	}
	limited := &contextReader{ctx: ctx, reader: bytes.NewReader(content)}
	if _, err := io.Copy(temporary, limited); err != nil {
		return fmt.Errorf("写入文件覆盖临时文件失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步文件覆盖临时文件失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭文件覆盖临时文件失败: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := os.Rename(temporaryPath, targetPath); err != nil {
		return fmt.Errorf("原子替换本地文件失败: %w", err)
	}
	complete = true
	manager.removeThumbnails(targetPath, "")
	return nil
}

// detectContentType 读取文件头识别实际编码，无法识别时回退到 storekey 扩展名映射。
func detectContentType(file *os.File, extension string) (string, error) {
	header := make([]byte, 512)
	count, err := file.Read(header)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	contentType := http.DetectContentType(header[:count])
	if contentType == "application/octet-stream" || contentType == "text/plain; charset=utf-8" {
		if extensionType := mime.TypeByExtension("." + extension); extensionType != "" {
			return extensionType, nil
		}
	}
	return contentType, nil
}

// ParseKey 解析并严格校验 key 的类型、日期、UUID 和扩展名，禁止路径分隔符和非规范 UUID。
func ParseKey(value string) (ParsedKey, error) {
	parts := strings.Split(value, "_")
	public := false
	if len(parts) == 4 && parts[0] == "public" {
		public = true
		parts = parts[1:]
	}
	if len(parts) != 3 || (parts[0] != localType && parts[0] != ossType) {
		return ParsedKey{}, ErrInvalidStoreKey
	}
	if len(parts[1]) != 8 {
		return ParsedKey{}, ErrInvalidStoreKey
	}
	date, err := time.Parse("20060102", parts[1])
	if err != nil || date.Format("20060102") != parts[1] {
		return ParsedKey{}, ErrInvalidStoreKey
	}
	name := strings.Split(parts[2], ".")
	if len(name) != 2 || !extensionPattern.MatchString(name[1]) {
		return ParsedKey{}, ErrInvalidStoreKey
	}
	fileID, err := uuid.Parse(name[0])
	if err != nil || fileID == uuid.Nil || fileID.String() != strings.ToLower(name[0]) {
		return ParsedKey{}, ErrInvalidStoreKey
	}
	return ParsedKey{Type: parts[0], Date: date, UUID: fileID, Extension: strings.ToLower(name[1]), Public: public}, nil
}

// formatKey 输出私有 <type>_yyyymmdd_uuid.ext 或公共 public_<type>_yyyymmdd_uuid.ext 格式。
func formatKey(key ParsedKey) string {
	prefix := key.Type
	if key.Public {
		prefix = "public_" + prefix
	}
	return fmt.Sprintf("%s_%s_%s.%s", prefix, key.Date.UTC().Format("20060102"), key.UUID.String(), key.Extension)
}

// localPath 将已校验字段映射到 日期/UUID前三位/UUID.ext 的本地目录结构。
func (manager *Manager) localPath(key ParsedKey) string {
	date := key.Date.UTC().Format("20060102")
	fileID := key.UUID.String()
	root := manager.root
	if key.Public {
		root = filepath.Join(root, "public")
	}
	return filepath.Join(root, date, fileID[:3], fileID+"."+key.Extension)
}

// safeExtension 丢弃路径部分，仅接受短 ASCII 扩展名；缺失或不安全时使用 bin。
func safeExtension(originalFilename string) string {
	extension := strings.TrimPrefix(filepath.Ext(filepath.Base(originalFilename)), ".")
	if !extensionPattern.MatchString(extension) {
		return "bin"
	}
	return strings.ToLower(extension)
}

// contextReader 在每次读取前检查请求取消状态，避免客户端断开后继续占用磁盘和 I/O。
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

// Read 透传读取操作；请求取消时及时中止文件复制。
func (reader *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(buffer)
	}
}
