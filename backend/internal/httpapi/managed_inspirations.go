package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/service"
)

// managedInspirationGetHandler 仅挂在已验证管理角色和目标组织的路由组中。
func managedInspirationGetHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		item, err := inspirations.GetManaged(c.Request.Context(), id)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, inspirationDTO(item, true))
	}
}

// managedInspirationPatchHandler 复用普通编辑语义，但由专用 service 在当前组织找到原所有者。
func managedInspirationPatchHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		fields, err := decodeObject(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := allowFields(fields, "tag_ids", "tag_names", "image_key", "file_name", "data", "image_metadata"); err != nil {
			writeError(c, err)
			return
		}
		var input service.InspirationInput
		_, tagIDsSet := fields["tag_ids"]
		_, tagNamesSet := fields["tag_names"]
		_, imageKeySet := fields["image_key"]
		_, fileNameSet := fields["file_name"]
		_, dataSet := fields["data"]
		_, metadataSet := fields["image_metadata"]
		if tagIDsSet && tagNamesSet {
			writeError(c, fault.InvalidRequest("tag_ids 和 tag_names 不能同时传入"))
			return
		}
		// 两组数组都必须显式提供数组；null 不能表示清空，清空使用 []。
		if raw, ok := fields["tag_ids"]; ok {
			if err := json.Unmarshal(raw, &input.TagIDs); err != nil || input.TagIDs == nil {
				writeError(c, fault.InvalidRequest("tag_ids 必须是 UUID 数组"))
				return
			}
		}
		if raw, ok := fields["tag_names"]; ok {
			if err := json.Unmarshal(raw, &input.TagNames); err != nil || input.TagNames == nil {
				writeError(c, fault.InvalidRequest("tag_names 必须是字符串数组"))
				return
			}
		}
		for name, target := range map[string]*string{"image_key": &input.ImageKey, "file_name": &input.FileName} {
			if raw, ok := fields[name]; ok {
				if err := json.Unmarshal(raw, target); err != nil {
					writeError(c, fault.InvalidRequest(name+" 必须是字符串"))
					return
				}
			}
		}
		if raw, ok := fields["data"]; ok {
			input.Data = append(json.RawMessage(nil), raw...)
		}
		if raw, ok := fields["image_metadata"]; ok {
			trimmed := bytes.TrimSpace(raw)
			if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &input.ImageMetadata) != nil {
				writeError(c, fault.InvalidRequest("image_metadata 必须是有效 JSON 对象"))
				return
			}
		}
		item, err := inspirations.UpdateManaged(c.Request.Context(), id, input, tagIDsSet, tagNamesSet, imageKeySet, fileNameSet, dataSet, metadataSet)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, inspirationDTO(item, true))
	}
}

// managedInspirationDeleteHandler 删除受当前管理组织约束的图片，并保留项目引用保护。
func managedInspirationDeleteHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := inspirations.DeleteManaged(c.Request.Context(), id); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"deleted": true})
	}
}

// managedInspirationTagsHandler 只返回目标图片真实所有者在当前组织的标签。
func managedInspirationTagsHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		page, err := parsePagination(c)
		if err != nil {
			writeError(c, err)
			return
		}
		query, err := optionalQueryString(c, "q")
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := inspirations.ListManagedTags(c.Request.Context(), id, query, page)
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]tagResponse, 0, len(result.Items))
		for _, item := range result.Items {
			items = append(items, tagDTO(item))
		}
		writeData(c, http.StatusOK, pageResponse[tagResponse]{Items: items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
	}
}

// managedInspirationFilesAdapter 将通用上传和下载校验绑定到已授权图片，不能用于公共上传或任意文件删除。
type managedInspirationFilesAdapter struct {
	inspirations *service.InspirationService
	id           uuid.UUID
}

func (files managedInspirationFilesAdapter) Store(ctx context.Context, kind, name string, source io.Reader, maximum int64) (string, error) {
	return files.inspirations.StoreManagedFile(ctx, files.id, kind, name, source, maximum)
}
func (files managedInspirationFilesAdapter) OpenContext(ctx context.Context, key string) (io.ReadCloser, int64, string, error) {
	return files.inspirations.OpenManagedFile(ctx, files.id, key)
}
func (files managedInspirationFilesAdapter) StorePublic(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", fault.NotFound()
}
func (files managedInspirationFilesAdapter) Open(string) (io.ReadCloser, int64, string, error) {
	return nil, 0, "", fault.NotFound()
}
func (files managedInspirationFilesAdapter) Delete(context.Context, string) error {
	return fault.NotFound()
}

// managedInspirationUploadHandler 复用普通上传的请求体和大小限制，文件归属由图片记录决定。
func managedInspirationUploadHandler(inspirations *service.InspirationService, maximum int64, currentType func(context.Context) (string, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		uploadFileHandler(managedInspirationFilesAdapter{inspirations: inspirations, id: id}, maximum, currentType)(c)
	}
}

// managedInspirationFileHandler 只下载该图片已关联的工作图或原图，不接受其他私有 key。
func managedInspirationFileHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		// 管理端使用查询参数携带不透明 key，原路径参数形式保留给内部复用。
		if c.Param("storekey") == "" {
			keys := c.QueryArray("storekey")
			if len(keys) != 1 || keys[0] == "" {
				writeError(c, fault.InvalidRequest("storekey 缺失或重复"))
				return
			}
			c.Params = append(c.Params, gin.Param{Key: "storekey", Value: keys[0]})
		}
		downloadFileHandler(managedInspirationFilesAdapter{inspirations: inspirations, id: id})(c)
	}
}
