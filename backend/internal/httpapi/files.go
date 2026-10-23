package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/service"
)

const (
	// multipartMemoryLimit 限制 multipart 解析时保留在内存中的数据，其余内容由标准库写入临时文件。
	multipartMemoryLimit = 1 << 20
	// multipartOverhead 为边界和文件头预留空间；文件内容本身仍受配置的 files.max_upload_bytes 限制。
	multipartOverhead = 1 << 20
)

// uploadFileHandler 接收单个 multipart 文件，由已装配的文件服务登记可信组织及用户归属。
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

// downloadFileHandler 在可信组织及用户归属通过校验后读取私有文件。
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
		reader, size, contentType, err := service.OpenFile(c.Request.Context(), files, storekey)
		var accessError *fault.Error
		switch {
		case errors.As(err, &accessError):
			writeError(c, accessError)
			return
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
		filename := parsedKey.UUID.String() + "." + downloadExtension(contentType, parsedKey.Extension)
		c.DataFromReader(http.StatusOK, size, contentType, reader, map[string]string{
			"Content-Disposition":     fmt.Sprintf("attachment; filename=%q", filename),
			"Cache-Control":           "private, no-store",
			"X-Content-Type-Options":  "nosniff",
			"Content-Security-Policy": "sandbox",
			"Referrer-Policy":         "no-referrer",
		})
	}
}

// publicFeedbackImageHandler 无需 token 读取反馈专用 public key；普通 key、无效 key 和非图片内容统一拒绝。
func publicFeedbackImageHandler(files fileStorage) gin.HandlerFunc {
	return func(c *gin.Context) {
		if files == nil {
			writeError(c, fault.NotFound())
			return
		}
		storekey := c.Param("storekey")
		parsedKey, err := filestore.ParseKey(storekey)
		if err != nil || !parsedKey.Public {
			writeError(c, fault.NotFound())
			return
		}
		reader, size, _, err := files.Open(storekey)
		if err != nil {
			if errors.Is(err, filestore.ErrInvalidStoreKey) || errors.Is(err, filestore.ErrFileNotFound) || errors.Is(err, filestore.ErrBackendUnavailable) {
				writeError(c, fault.NotFound())
				return
			}
			writeError(c, fault.Internal(err))
			return
		}
		// 使用读取到的真实文件头检测 MIME，而不采用常规下载路径可能根据扩展名回退的类型。
		prefix, readErr := io.ReadAll(io.LimitReader(reader, 512))
		if readErr != nil || len(prefix) == 0 {
			_ = reader.Close()
			if readErr != nil {
				writeError(c, fault.Internal(readErr))
			} else {
				writeError(c, fault.NotFound())
			}
			return
		}
		contentType := http.DetectContentType(prefix)
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || !isPublicFeedbackImageType(mediaType) {
			_ = reader.Close()
			writeError(c, fault.NotFound())
			return
		}
		defer reader.Close()
		filename := parsedKey.UUID.String() + "." + downloadExtension(mediaType, parsedKey.Extension)
		c.DataFromReader(http.StatusOK, size, contentType, io.MultiReader(bytes.NewReader(prefix), reader), map[string]string{
			"Content-Disposition":     fmt.Sprintf("inline; filename=%q", filename),
			"Cache-Control":           "public, max-age=31536000, immutable",
			"X-Content-Type-Options":  "nosniff",
			"Content-Security-Policy": "sandbox",
			"Referrer-Policy":         "no-referrer",
		})
	}
}

// isPublicFeedbackImageType 只允许浏览器可安全内嵌的实际位图类型，不接受 SVG 或客户端声明的类型。
func isPublicFeedbackImageType(mediaType string) bool {
	switch strings.ToLower(mediaType) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// downloadExtension 在文件编码曾被压缩转换时按真实 Content-Type 设置下载文件后缀。
func downloadExtension(contentType, fallback string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fallback
	}
	switch mediaType {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return fallback
	}
}
