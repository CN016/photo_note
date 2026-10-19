package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/service"
)

// administratorMiddleware 对每个工作台 API 请求重新读取数据库角色；查询失败时拒绝访问。
func administratorMiddleware(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		if console == nil {
			writeError(c, fault.Unavailable(errors.New("management service is unavailable")))
			c.Abort()
			return
		}
		allowed, err := console.IsAdministrator(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		if !allowed {
			writeError(c, fault.New(http.StatusForbidden, "forbidden", "当前账号无权访问此工作台", nil, nil))
			c.Abort()
			return
		}
		c.Next()
	}
}

// consoleUserResponse 只暴露用户管理所需的资料和角色，不包含手机号以外的认证信息。
type consoleUserResponse struct {
	BusinessID uuid.UUID `json:"business_id"`
	Nickname   string    `json:"nickname"`
	Phone      string    `json:"phone"`
	IsAdmin    bool      `json:"is_admin"`
	CreatedAt  time.Time `json:"created_at"`
}

// consoleUsersHandler 支持昵称或手机号检索，并使用共享分页边界控制结果数量。
func consoleUsersHandler(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
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
		result, err := console.ListUsers(c.Request.Context(), query, page)
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]consoleUserResponse, 0, len(result.Items))
		for _, item := range result.Items {
			items = append(items, consoleUserResponse{BusinessID: item.BusinessID, Nickname: item.Nickname, Phone: item.Phone, IsAdmin: item.IsAdmin, CreatedAt: item.CreatedAt})
		}
		writeData(c, http.StatusOK, pageResponse[consoleUserResponse]{Items: items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
	}
}

// consoleUserRoleHandler 仅允许修改 is_admin 字段，资料和注册接口无法通过此入口被写入。
func consoleUserRoleHandler(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		fields, err := decodeObject(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := allowFields(fields, "is_admin"); err != nil {
			writeError(c, err)
			return
		}
		raw, exists := fields["is_admin"]
		var isAdmin bool
		if !exists || json.Unmarshal(raw, &isAdmin) != nil {
			writeError(c, fault.InvalidField("is_admin", "is_admin 必须是布尔值"))
			return
		}
		if err := console.SetUserAdministrator(c.Request.Context(), userID, isAdmin); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"business_id": userID, "is_admin": isAdmin})
	}
}

// consoleConfigsHandler 返回所有配置元信息；秘密值由 service 转成是否已设置的状态。
func consoleConfigsHandler(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := console.ListConfigs(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": items})
	}
}

// consoleConfigCreateRequest 是新增配置允许的字段集合。
type consoleConfigCreateRequest struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	IsPublic    bool   `json:"is_public"`
	IsSecret    bool   `json:"is_secret"`
}

// createConsoleConfigHandler 新增配置，秘密值永远不会在成功响应中回显。
func createConsoleConfigHandler(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		fields, err := decodeObject(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := allowFields(fields, "key", "value", "description", "is_public", "is_secret"); err != nil {
			writeError(c, err)
			return
		}
		var request consoleConfigCreateRequest
		if err := json.Unmarshal(mustMarshalFields(fields), &request); err != nil {
			writeError(c, fault.InvalidRequest("配置字段格式无效"))
			return
		}
		item, err := console.CreateConfig(c.Request.Context(), service.ConsoleConfigInput{
			Key: request.Key, Value: request.Value, Description: request.Description, IsPublic: request.IsPublic, IsSecret: request.IsSecret,
		})
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, item)
	}
}

// mustMarshalFields 构造已经由 decodeObject 验证的 JSON 对象，供局部结构体解码。
func mustMarshalFields(fields map[string]json.RawMessage) []byte {
	value, err := json.Marshal(fields)
	if err != nil {
		return []byte("{}")
	}
	return value
}

// updateConsoleConfigHandler 更新配置值或说明；is_secret 一经创建不可通过接口反转。
func updateConsoleConfigHandler(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		fields, err := decodeObject(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := allowFields(fields, "value", "clear_value", "description", "is_public"); err != nil {
			writeError(c, err)
			return
		}
		patch := service.ConsoleConfigPatch{}
		if raw, exists := fields["value"]; exists {
			patch.ValueSet = true
			if json.Unmarshal(raw, &patch.Value) != nil {
				writeError(c, fault.InvalidField("value", "value 必须是字符串"))
				return
			}
		}
		if raw, exists := fields["clear_value"]; exists {
			if json.Unmarshal(raw, &patch.ClearValue) != nil {
				writeError(c, fault.InvalidField("clear_value", "clear_value 必须是布尔值"))
				return
			}
		}
		if raw, exists := fields["description"]; exists {
			patch.DescriptionSet = true
			if json.Unmarshal(raw, &patch.Description) != nil {
				writeError(c, fault.InvalidField("description", "description 必须是字符串"))
				return
			}
		}
		if raw, exists := fields["is_public"]; exists {
			patch.IsPublicSet = true
			if json.Unmarshal(raw, &patch.IsPublic) != nil {
				writeError(c, fault.InvalidField("is_public", "is_public 必须是布尔值"))
				return
			}
		}
		item, err := console.UpdateConfig(c.Request.Context(), c.Param("config_key"), patch)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// consoleInspirationResponse 用于全站只读图库，响应中不包含个人编辑文档 JSON。
type consoleInspirationResponse struct {
	BusinessID    uuid.UUID `json:"business_id"`
	OwnerNickname string    `json:"owner_nickname"`
	TagIDs        []string  `json:"tag_ids"`
	ImageKey      string    `json:"image_key"`
	FileName      string    `json:"file_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// consoleInspirationsHandler 按文件名或用户昵称筛选全站图片，只读返回必要的预览和归属字段。
func consoleInspirationsHandler(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
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
		sort, err := optionalQueryString(c, "sort")
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := console.ListInspirations(c.Request.Context(), query, sort, page)
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]consoleInspirationResponse, 0, len(result.Items))
		for _, item := range result.Items {
			tagIDs := make([]string, 0, len(item.TagIDs))
			for _, tagID := range item.TagIDs {
				tagIDs = append(tagIDs, tagID)
			}
			items = append(items, consoleInspirationResponse{BusinessID: item.BusinessID, OwnerNickname: item.OwnerNickname, TagIDs: tagIDs,
				ImageKey: item.ImageKey, FileName: item.FileName, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
		}
		writeData(c, http.StatusOK, pageResponse[consoleInspirationResponse]{Items: items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
	}
}

// consoleFeedbackResponse 是反馈中心用于阅读正文、预览附件和变更处理状态的最小 DTO。
type consoleFeedbackResponse struct {
	BusinessID     uuid.UUID `json:"business_id"`
	Nickname       string    `json:"nickname"`
	Content        string    `json:"content"`
	ImageKeys      []string  `json:"image_keys"`
	ReplyContent   string    `json:"reply_content"`
	ReplyImageKeys []string  `json:"reply_image_keys"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// consoleFeedbacksHandler 可按 new/processed 筛选反馈并以最近提交时间排序。
func consoleFeedbacksHandler(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, err := parsePagination(c)
		if err != nil {
			writeError(c, err)
			return
		}
		status, err := optionalQueryString(c, "status")
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := console.ListFeedback(c.Request.Context(), status, page)
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]consoleFeedbackResponse, 0, len(result.Items))
		for _, item := range result.Items {
			imageKeys := make([]string, 0, len(item.ImageKeys))
			for _, imageKey := range item.ImageKeys {
				imageKeys = append(imageKeys, imageKey)
			}
			replyImageKeys := append(make([]string, 0, len(item.ReplyImageKeys)), item.ReplyImageKeys...)
			items = append(items, consoleFeedbackResponse{BusinessID: item.BusinessID, Nickname: item.Nickname, Content: item.Content,
				ImageKeys: imageKeys, ReplyContent: item.ReplyContent, ReplyImageKeys: replyImageKeys,
				Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
		}
		writeData(c, http.StatusOK, pageResponse[consoleFeedbackResponse]{Items: items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
	}
}

type consoleFeedbackReplyResponse struct {
	BusinessID     uuid.UUID `json:"business_id"`
	ReplyContent   string    `json:"reply_content"`
	ReplyImageKeys []string  `json:"reply_image_keys"`
}

// consoleFeedbackReplyHandler 仅允许管理员以 multipart 更新回复，并只清理本次上传但未成功提交的新文件。
func consoleFeedbackReplyHandler(console *service.ConsoleService, files fileStorage, maxUploadBytes int64, currentStoreType func(context.Context) (string, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if console == nil || files == nil || currentStoreType == nil {
			writeError(c, fault.Unavailable(errors.New("反馈回复服务未配置")))
			return
		}
		feedbackID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := parseMultipartRequest(c, multipartBodyLimit(maxUploadBytes, 5)); err != nil {
			writeError(c, err)
			return
		}
		defer removeMultipartTempFiles(c)
		form := c.Request.MultipartForm
		if err := validateMultipartKeys(form, []string{"reply_content", "kept_image_keys"}, []string{"images"}); err != nil {
			writeError(c, err)
			return
		}
		content, err := oneMultipartValue(form, "reply_content", false)
		if err != nil {
			writeError(c, err)
			return
		}
		keptJSON, err := oneMultipartValue(form, "kept_image_keys", false)
		if err != nil {
			writeError(c, err)
			return
		}
		var retainedKeys []string
		if !strings.HasPrefix(strings.TrimSpace(keptJSON), "[") || json.Unmarshal([]byte(keptJSON), &retainedKeys) != nil || retainedKeys == nil {
			writeError(c, fault.InvalidField("kept_image_keys", "必须提供 JSON 图片 key 数组"))
			return
		}
		imageFiles := form.File["images"]
		if len(retainedKeys)+len(imageFiles) > 5 {
			writeError(c, fault.InvalidField("images", "回复图片最多五张"))
			return
		}
		seen := make(map[string]struct{}, len(retainedKeys))
		for _, key := range retainedKeys {
			parsed, parseErr := filestore.ParseKey(key)
			if parseErr != nil || !parsed.Public {
				writeError(c, fault.InvalidField("kept_image_keys", "只能保留当前反馈已有的公开图片"))
				return
			}
			if _, duplicate := seen[key]; duplicate {
				writeError(c, fault.InvalidField("kept_image_keys", "回复图片列表不能包含重复项目"))
				return
			}
			seen[key] = struct{}{}
		}
		storeType := ""
		if len(imageFiles) > 0 {
			storeType, err = currentStoreType(c.Request.Context())
			if err != nil {
				writeError(c, fault.Unavailable(err))
				return
			}
		}
		newKeys := make([]string, 0, len(imageFiles))
		cleanup := func() {
			for _, key := range newKeys {
				_ = files.Delete(context.Background(), key)
			}
		}
		for _, header := range imageFiles {
			data, readErr := readValidatedImage(header, maxUploadBytes)
			if readErr != nil {
				cleanup()
				writeError(c, readErr)
				return
			}
			key, storeErr := files.StorePublic(c.Request.Context(), storeType, header.Filename, bytes.NewReader(data), maxUploadBytes)
			if storeErr != nil {
				cleanup()
				writeError(c, fileStoreError(storeErr))
				return
			}
			newKeys = append(newKeys, key)
		}
		savedKeys, err := console.UpdateFeedbackReply(c.Request.Context(), feedbackID, content, retainedKeys, newKeys)
		if err != nil {
			cleanup()
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, consoleFeedbackReplyResponse{BusinessID: feedbackID, ReplyContent: strings.TrimSpace(content), ReplyImageKeys: savedKeys})
	}
}

// consoleFeedbackStatusHandler 仅更新处理状态，反馈正文和附件始终只读。
func consoleFeedbackStatusHandler(console *service.ConsoleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		feedbackID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		fields, err := decodeObject(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := allowFields(fields, "status"); err != nil {
			writeError(c, err)
			return
		}
		var status string
		raw, exists := fields["status"]
		if !exists || json.Unmarshal(raw, &status) != nil {
			writeError(c, fault.InvalidField("status", "status 必须是字符串"))
			return
		}
		if err := console.UpdateFeedbackStatus(c.Request.Context(), feedbackID, status); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"business_id": feedbackID, "status": status})
	}
}
