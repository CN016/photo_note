package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/imagecompress"
	"github.com/photo-nini/backend/internal/service"
)

// listProjectsHandler 使用当前令牌用户范围查询项目，正文与标题由 repository 执行短词索引检索。
func listProjectsHandler(projects *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		page, err := parsePagination(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if len(c.QueryArray("q")) > 1 {
			writeError(c, fault.InvalidRequest("q 查询参数不能重复"))
			return
		}
		result, err := projects.List(c.Request.Context(), claims.UserBusinessID, c.Query("q"), page)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, result)
	}
}

// createProjectHandler 创建项目及其第一份版本化正文快照。
func createProjectHandler(projects *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var input service.ProjectSave
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, err)
			return
		}
		item, err := projects.Create(c.Request.Context(), claims.UserBusinessID, input)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, item)
	}
}

// getProjectHandler 返回当前用户可见的项目正文及封面信息。
func getProjectHandler(projects *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		item, err := projects.Get(c.Request.Context(), claims.UserBusinessID, id)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// patchProjectHandler 保存完整正文快照并依赖 revision 拒绝过期自动保存。
func patchProjectHandler(projects *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var input service.ProjectSave
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, err)
			return
		}
		item, err := projects.Update(c.Request.Context(), claims.UserBusinessID, id, input)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// deleteProjectHandler 软删除项目并保留关联记录，确保历史正文继续保护所引用的灵感图。
func deleteProjectHandler(projects *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := projects.Delete(c.Request.Context(), claims.UserBusinessID, id); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// projectAttachmentHandler 通过项目和附件双重业务 ID 验权后以内联方式流式返回内容。
func projectAttachmentHandler(projects *service.ProjectService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		projectID, err := uuid.Parse(c.Param("business_id"))
		if err != nil {
			writeError(c, fault.InvalidRequest("项目业务 ID 格式无效"))
			return
		}
		storeKey := c.Query("storekey")
		if storeKey == "" || len(c.QueryArray("storekey")) != 1 {
			writeError(c, fault.InvalidRequest("附件 storekey 缺失或重复"))
			return
		}
		item, reader, err := projects.Attachment(c.Request.Context(), claims.UserBusinessID, projectID, storeKey)
		if err != nil {
			writeError(c, err)
			return
		}
		defer reader.Close()
		inlineName := strings.ReplaceAll(strings.ReplaceAll(item.FileName, "\"", ""), "\r", "")
		inlineName = strings.ReplaceAll(inlineName, "\n", "")
		c.DataFromReader(http.StatusOK, item.FileSize, item.ContentType, reader, map[string]string{
			"Content-Disposition": fmt.Sprintf("inline; filename=%q", inlineName),
			"Cache-Control":       "private, no-store", "X-Content-Type-Options": "nosniff",
			"Content-Security-Policy": "sandbox", "Referrer-Policy": "no-referrer",
		})
	}
}

// uploadProjectCoverHandler 压缩封面图片后使用现有私有文件存储，不建立通用文件元数据或删除接口。
func uploadProjectCoverHandler(files fileStorage, maxUploadBytes int64, currentStoreType func(context.Context) (string, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if files == nil || maxUploadBytes < 1 || currentStoreType == nil {
			writeError(c, fault.Unavailable(errors.New("项目封面上传配置未初始化")))
			return
		}
		if err := parseMultipartRequest(c, multipartBodyLimit(maxUploadBytes, 1)); err != nil {
			writeError(c, err)
			return
		}
		defer removeMultipartTempFiles(c)
		form := c.Request.MultipartForm
		if err := validateMultipartKeys(form, nil, []string{"file"}); err != nil {
			writeError(c, err)
			return
		}
		filesInForm := form.File["file"]
		if len(form.File) != 1 || len(filesInForm) != 1 || len(form.Value) != 0 {
			writeError(c, fault.InvalidRequest("封面上传只允许一个名为 file 的图片"))
			return
		}
		data, err := readValidatedImage(filesInForm[0], maxUploadBytes)
		if err != nil {
			writeError(c, err)
			return
		}
		compressed, _, err := imagecompress.Compress(data)
		if err != nil {
			writeError(c, fault.InvalidField("file", "封面图片无法压缩"))
			return
		}
		storeType, err := currentStoreType(c.Request.Context())
		if err != nil {
			writeError(c, fault.Unavailable(err))
			return
		}
		name := filesInForm[0].Filename
		key, err := files.Store(c.Request.Context(), storeType, name, bytes.NewReader(compressed), maxUploadBytes)
		if err != nil {
			writeError(c, fileStoreError(err))
			return
		}
		reader, size, contentType, err := files.Open(key)
		if err != nil {
			_ = files.Delete(context.Background(), key)
			writeError(c, fault.Internal(err))
			return
		}
		_ = reader.Close()
		mediaType, _, _ := mime.ParseMediaType(contentType)
		if size < 1 || !strings.HasPrefix(mediaType, "image/") {
			_ = files.Delete(context.Background(), key)
			writeError(c, fault.InvalidField("file", "封面内容无效"))
			return
		}
		switch mediaType {
		case "image/jpeg":
			name = replaceFilenameExtension(name, ".jpg")
		case "image/png":
			name = replaceFilenameExtension(name, ".png")
		case "image/webp":
			name = replaceFilenameExtension(name, ".webp")
		case "image/gif":
			name = replaceFilenameExtension(name, ".gif")
		}
		writeData(c, http.StatusCreated, gin.H{"storekey": key, "file_name": name, "file_size": size, "content_type": mediaType})
	}
}

func replaceFilenameExtension(name, extension string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" {
		base = "cover"
	}
	return base + extension
}

// projectUploadError 映射项目上传的文件存储错误，不泄露底层存储路径。
func projectUploadError(err error) error {
	if errors.Is(err, filestore.ErrFileTooLarge) {
		return fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "上传文件超过配置大小限制", nil, err)
	}
	if errors.Is(err, filestore.ErrBackendUnavailable) {
		return fault.New(http.StatusNotImplemented, "storage_backend_unavailable", "此文件存储类型暂不可用", nil, err)
	}
	return fault.Internal(err)
}
