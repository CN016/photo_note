package filestore

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/photo-nini/backend/internal/imagecompress"
)

// OpenThumbnail 只由FileService授权后调用；缓存与源文件同目录持久保存，源文件每次都必须存在。
func (manager *Manager) OpenThumbnail(ctx context.Context, key string, options imagecompress.ThumbnailOptions) (io.ReadCloser, int64, error) {
	reader, size, _, err := manager.Open(key)
	if err != nil {
		return nil, 0, err
	}
	defer reader.Close()
	source, ok := reader.(*os.File)
	if !ok {
		return nil, 0, ErrBackendUnavailable
	}
	info, err := source.Stat()
	if err != nil {
		return nil, 0, err
	}
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf("v1:%d:%d:%d:%d", size, info.ModTime().UnixNano(), options.Dimension, options.Quality)))
	cache := source.Name() + fmt.Sprintf(".thumb-v1-%x.jpg", fingerprint[:16])
	if file, err := os.Open(cache); err == nil {
		stat, e := file.Stat()
		if e != nil {
			file.Close()
			return nil, 0, e
		}
		return file, stat.Size(), nil
	}
	if size < 1 || size > options.MaxBytes {
		return nil, 0, ErrFileTooLarge
	}
	content, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: source}, options.MaxBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(content)) > options.MaxBytes {
		return nil, 0, ErrFileTooLarge
	}
	output, err := imagecompress.Thumbnail(content, options)
	if err != nil {
		return nil, 0, err
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(cache), ".thumbnail-*.partial")
	if err != nil {
		return nil, 0, err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(output); err != nil {
		temporary.Close()
		return nil, 0, err
	}
	if err = temporary.Chmod(0640); err != nil {
		temporary.Close()
		return nil, 0, err
	}
	if err = temporary.Close(); err != nil {
		return nil, 0, err
	}
	// 生成期间若源图被替换，不发布旧副本；下一次请求将按新指纹重新生成。
	current, e := os.Stat(source.Name())
	if e != nil {
		return nil, 0, ErrFileNotFound
	}
	if current.Size() != size || !current.ModTime().Equal(info.ModTime()) {
		return nil, 0, ErrBackendUnavailable
	}
	if err = os.Rename(temporary.Name(), cache); err != nil {
		return nil, 0, err
	}
	manager.removeThumbnails(source.Name(), cache)
	file, err := os.Open(cache)
	return file, int64(len(output)), err
}

// removeThumbnails 只删除已验证源路径旁的派生JPEG，不递归删除目录，也不触及原文件。
func (manager *Manager) removeThumbnails(source, keep string) {
	entries, err := os.ReadDir(filepath.Dir(source))
	if err != nil {
		return
	}
	prefix := filepath.Base(source) + ".thumb-v1-"
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), ".jpg") {
			path := filepath.Join(filepath.Dir(source), entry.Name())
			if path != keep {
				_ = os.Remove(path)
			}
		}
	}
}
