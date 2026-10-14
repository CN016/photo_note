package httpapi

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
)

const (
	// multipartMemoryLimit 限制 multipart 解析时保留在内存中的数据，其余内容由标准库写入临时文件。
	multipartMemoryLimit = 1 << 20
	// multipartOverhead 为边界和文件头预留空间；文件内容本身仍受配置的 files.max_upload_bytes 限制。
	multipartOverhead = 1 << 20
)

// uploadFileHandler 接收单个 multipart 文件；文件不写数据库，不记录用户归属，也不提供管理操作。
func uploadFileHandler(files fileStorage, maxUploadBytes int64, currentStoreType func(context.Context) (string, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if files == nil || maxUploadBytes < 1 || currentStoreType == nil {
			writeError(c, fault.Unavailable(errors.New("文件存储或上传大小配置未初始化")))
			return
		}
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
			writeError(c, fault.InvalidRequest("上传请求必须使用 multipart/form-data"))
			return
		}
		// 限制整份 multipart 请求体，文件大小限制之外仅额外允许有限的协议头和分隔符开销。
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes+multipartOverhead)
		if err := c.Request.ParseMultipartForm(multipartMemoryLimit); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(c, fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "上传文件超过配置大小限制", nil, err))
				return
			}
			writeError(c, fault.InvalidRequest("multipart 请求格式无效"))
			return
		}
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		form := c.Request.MultipartForm
		if form == nil || len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
			writeError(c, fault.InvalidRequest("上传请求只能包含一个名为 file 的文件"))
			return
		}
		fileHeader := form.File["file"][0]
		if fileHeader.Size < 1 {
			writeError(c, fault.InvalidRequest("上传文件不能为空"))
			return
		}
		if fileHeader.Size > maxUploadBytes {
			writeError(c, fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "上传文件超过配置大小限制", nil, nil))
			return
		}
		file, err := fileHeader.Open()
		if err != nil {
			writeError(c, fault.InvalidRequest("无法读取上传文件"))
			return
		}
		defer file.Close()
		storeType, err := currentStoreType(c.Request.Context())
		if err != nil {
			writeError(c, fault.Unavailable(err))
			return
		}
		storekey, err := files.Store(c.Request.Context(), storeType, fileHeader.Filename, file, maxUploadBytes)
		if errors.Is(err, filestore.ErrFileTooLarge) {
			writeError(c, fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "上传文件超过配置大小限制", nil, err))
			return
		}
		if errors.Is(err, filestore.ErrBackendUnavailable) {
			writeError(c, fault.New(http.StatusNotImplemented, "storage_backend_unavailable", "此文件存储类型暂不可用", nil, err))
			return
		}
		if err != nil {
			writeError(c, fault.Internal(err))
			return
		}
		writeData(c, http.StatusCreated, gin.H{"storekey": storekey})
	}
}

// downloadFileHandler 通过 storekey 读取文件；合法 key 即可定位文件，不做用户归属查询。
func downloadFileHandler(files fileStorage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if files == nil {
			writeError(c, fault.Unavailable(errors.New("文件存储未初始化")))
			return
		}
		storekey := c.Param("storekey")
		parsedKey, err := filestore.ParseKey(storekey)
		if err != nil {
			writeError(c, fault.InvalidRequest("storekey 格式无效"))
			return
		}
		reader, size, contentType, err := files.Open(storekey)
		switch {
		case errors.Is(err, filestore.ErrInvalidStoreKey):
			writeError(c, fault.InvalidRequest("storekey 格式无效"))
			return
		case errors.Is(err, filestore.ErrFileNotFound):
			writeError(c, fault.NotFound())
			return
		case errors.Is(err, filestore.ErrBackendUnavailable):
			writeError(c, fault.New(http.StatusNotImplemented, "storage_backend_unavailable", "此文件存储类型暂不可用", nil, err))
			return
		case err != nil:
			writeError(c, fault.Internal(err))
			return
		}
		defer reader.Close()

		// 统一以附件返回并禁用缓存，避免上传内容被浏览器当作本站可执行页面处理。
		filename := parsedKey.UUID.String() + "." + parsedKey.Extension
		c.DataFromReader(http.StatusOK, size, contentType, reader, map[string]string{
			"Content-Disposition":     fmt.Sprintf("attachment; filename=%q", filename),
			"Cache-Control":           "private, no-store",
			"X-Content-Type-Options":  "nosniff",
			"Content-Security-Policy": "sandbox",
			"Referrer-Policy":         "no-referrer",
		})
	}
}
