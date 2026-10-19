package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/imagecompress"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/version"
)

const apiPrefix = "/api/v1"

// accessTokenValidator 是 HTTP 鉴权中间件依赖的 Redis 校验能力，不包含令牌存储细节。
type accessTokenValidator interface {
	ValidateAccess(context.Context, string) (store.TokenClaims, error)
}

// publicConfigReader 提供无需鉴权的只读配置能力，具体实现仍由统一配置服务负责。
type publicConfigReader interface {
	ListPublicConfigValues(context.Context, []string) ([]service.PublicConfigValue, error)
}

// Dependencies 汇总 HTTP 层使用的业务服务和健康检查依赖，路由不直接创建数据库连接。
type Dependencies struct {
	Auth                  *service.AuthService
	Users                 *service.UserService
	Tags                  *service.TagService
	Inspirations          *service.InspirationService
	InspirationStatistics *service.InspirationStatisticsService
	Tokens                accessTokenValidator
	PostgresPing          func(context.Context) error
	RedisPing             func(context.Context) error
	MaxBodyBytes          int64
	Files                 fileStorage
	MaxUploadBytes        int64
	CurrentFileStoreType  func(context.Context) (string, error)
	PublicConfigs         publicConfigReader
	Feedback              *service.FeedbackService
	Ledger                *service.LedgerService
	Schedule              *service.ScheduleService
	Notices               *service.NoticeService
	TrustedProxyCIDRs     []string
	ErrorLogs             *service.ErrorLogService
	Console               *service.ConsoleService
}

// fileStorage 是 HTTP 上传和读取使用的文件存储能力，不包含用户归属或文件元数据管理。
type fileStorage interface {
	Store(context.Context, string, string, io.Reader, int64) (string, error)
	StorePublic(context.Context, string, string, io.Reader, int64) (string, error)
	Open(string) (io.ReadCloser, int64, string, error)
	Delete(context.Context, string) error
}

// Router 创建 V1 HTTP 路由，并集中安装请求 ID、请求体限制和不泄露敏感数据的异常处理。
func Router(dependencies Dependencies) *gin.Engine {
	router := gin.New()
	router.HandleMethodNotAllowed = true
	// 只有配置表允许的代理网段可声明转发客户端 IP；不可信请求头由 Gin 忽略。
	if err := router.SetTrustedProxies(dependencies.TrustedProxyCIDRs); err != nil {
		if dependencies.ErrorLogs != nil {
			dependencies.ErrorLogs.CaptureBackground("trusted_proxy_config_invalid", err)
		} else {
			log.Printf("event=trusted_proxy_config_invalid")
		}
		_ = router.SetTrustedProxies(nil)
	}
	router.Use(requestIDMiddleware(), errorLogContextMiddleware(dependencies.ErrorLogs), bodyLimitMiddleware(dependencies.MaxBodyBytes), recoveryMiddleware())
	router.NoRoute(func(c *gin.Context) {
		writeError(c, fault.NotFound())
	})
	router.NoMethod(func(c *gin.Context) {
		writeError(c, fault.New(http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持", nil, nil))
	})

	api := router.Group(apiPrefix)
	api.POST("/auth/register", upgradeRequiredHandler())
	api.POST("/auth/login", loginHandler(dependencies.Auth))
	api.POST("/auth/refresh", refreshHandler(dependencies.Auth))
	api.GET("/configs/public", publicConfigsHandler(dependencies.PublicConfigs))
	api.POST("/feedback", optionalAuthenticationMiddleware(dependencies.Tokens), feedbackHandler(dependencies))
	api.GET("/public-feedback-images/:storekey", publicFeedbackImageHandler(dependencies.Files))
	api.GET("/book/:share_token/slots", publicScheduleSlotsHandler(dependencies.Schedule))
	api.POST("/book/:share_token/requests", scheduleBookingHandler(dependencies.Schedule))
	v2 := router.Group("/api/v2")
	v2.POST("/auth/register", registerV2Handler(dependencies))

	// readiness 只探测服务依赖，不访问用户数据，因此允许部署平台在无 token 时调用。
	router.GET("/healthz", healthHandler(dependencies.PostgresPing, dependencies.RedisPing))

	protected := api.Group("")
	protected.Use(authenticationMiddleware(dependencies.Tokens))
	protected.POST("/auth/logout", logoutHandler(dependencies.Auth))
	protected.POST("/files", uploadFileHandler(dependencies.Files, dependencies.MaxUploadBytes, dependencies.CurrentFileStoreType))
	protected.GET("/files/:storekey", downloadFileHandler(dependencies.Files))
	protected.GET("/users/me", getCurrentUserHandler(dependencies.Users))
	protected.PATCH("/users/me", patchCurrentUserHandler(dependencies.Users))
	protected.POST("/users/me/password", changePasswordHandler(dependencies.Auth))
	protected.GET("/tags", listTagsHandler(dependencies.Tags))
	protected.POST("/tags", createTagHandler(dependencies.Tags))
	protected.PATCH("/tags/:business_id", updateTagHandler(dependencies.Tags))
	protected.DELETE("/tags/:business_id", deleteTagHandler(dependencies.Tags))
	protected.GET("/inspirations", listInspirationsHandler(dependencies.Inspirations))
	if dependencies.InspirationStatistics != nil {
		protected.GET("/inspirations/statistics", inspirationStatisticsHandler(dependencies.InspirationStatistics))
	}
	protected.POST("/inspirations", createInspirationHandler(dependencies.Inspirations))
	protected.GET("/inspirations/:business_id", getInspirationHandler(dependencies.Inspirations))
	protected.PATCH("/inspirations/:business_id", patchInspirationHandler(dependencies.Inspirations))
	protected.DELETE("/inspirations/:business_id", deleteInspirationHandler(dependencies.Inspirations))
	protected.GET("/ledger/records", listLedgerRecordsHandler(dependencies.Ledger))
	protected.POST("/ledger/records", createLedgerDraftHandler(dependencies.Ledger))
	protected.PATCH("/ledger/records/:business_id/draft", saveLedgerDraftHandler(dependencies.Ledger))
	protected.POST("/ledger/records/:business_id/complete", completeLedgerRecordHandler(dependencies.Ledger))
	protected.DELETE("/ledger/records/:business_id", deleteLedgerRecordHandler(dependencies.Ledger))
	protected.POST("/ledger/import", importLedgerRecordsHandler(dependencies.Ledger))
	protected.GET("/schedule/items", listScheduleItemsHandler(dependencies.Schedule))
	protected.POST("/schedule/items", createScheduleItemHandler(dependencies.Schedule))
	protected.PATCH("/schedule/items/:business_id", updateScheduleItemHandler(dependencies.Schedule))
	protected.DELETE("/schedule/items/:business_id", deleteScheduleItemHandler(dependencies.Schedule))
	protected.PATCH("/schedule/items/:business_id/occurrences/:date", updateScheduleOccurrenceHandler(dependencies.Schedule))
	protected.DELETE("/schedule/items/:business_id/occurrences/:date", cancelScheduleOccurrenceHandler(dependencies.Schedule))
	protected.GET("/schedule/bookings", listScheduleBookingsHandler(dependencies.Schedule))
	protected.POST("/schedule/bookings/:business_id/confirm", decideScheduleBookingHandler(dependencies.Schedule, "confirmed"))
	protected.POST("/schedule/bookings/:business_id/reject", decideScheduleBookingHandler(dependencies.Schedule, "rejected"))
	protected.POST("/schedule/bookings/:business_id/cancel", cancelScheduleBookingHandler(dependencies.Schedule))
	protected.GET("/schedule/share", getScheduleShareHandler(dependencies.Schedule))
	protected.POST("/schedule/share", createScheduleShareHandler(dependencies.Schedule))
	protected.DELETE("/schedule/share", revokeScheduleShareHandler(dependencies.Schedule))
	protected.GET("/notifications", listNotificationsHandler(dependencies.Notices))
	protected.GET("/notifications/unread-count", unreadNotificationCountHandler(dependencies.Notices))
	protected.PATCH("/notifications/:business_id/read", markNotificationReadHandler(dependencies.Notices))
	protected.POST("/notifications/read-all", markAllNotificationsReadHandler(dependencies.Notices))
	protected.GET("/notice-configs", listNoticeConfigsHandler(dependencies.Notices))
	protected.POST("/notice-configs", createNoticeConfigHandler(dependencies.Notices))
	protected.PATCH("/notice-configs/:business_id", updateNoticeConfigHandler(dependencies.Notices))
	protected.DELETE("/notice-configs/:business_id", deleteNoticeConfigHandler(dependencies.Notices))
	protected.POST("/notice-configs/:business_id/test", testNoticeConfigHandler(dependencies.Notices))
	protected.GET("/notice-deliveries/:business_id", getNoticeDeliveryHandler(dependencies.Notices))
	protected.POST("/notice-deliveries/:business_id/retry", retryNoticeDeliveryHandler(dependencies.Notices))
	console := protected.Group("/atelier-7f3c9a2e")
	console.Use(administratorMiddleware(dependencies.Console))
	console.GET("/users", consoleUsersHandler(dependencies.Console))
	console.PATCH("/users/:business_id", consoleUserRoleHandler(dependencies.Console))
	console.GET("/configs", consoleConfigsHandler(dependencies.Console))
	console.POST("/configs", createConsoleConfigHandler(dependencies.Console))
	console.PATCH("/configs/:config_key", updateConsoleConfigHandler(dependencies.Console))
	console.GET("/inspirations", consoleInspirationsHandler(dependencies.Console))
	console.GET("/feedbacks", consoleFeedbacksHandler(dependencies.Console))
	console.PATCH("/feedbacks/:business_id/status", consoleFeedbackStatusHandler(dependencies.Console))
	return router
}

// claimsKey 是请求上下文中的私有键，避免认证声明与业务上下文值发生冲突。
type claimsKey struct{}

const errorLogContextKey = "photo_nini_error_log_service"

// requestIDMiddleware 为每次调用生成服务端 UUID，供成功响应、错误响应和安全日志关联。
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// errorLogContextMiddleware 将异步错误日志服务放入 Gin 请求上下文，供统一错误响应记录服务端故障。
func errorLogContextMiddleware(errorLogs *service.ErrorLogService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(errorLogContextKey, errorLogs)
		c.Next()
	}
}

// bodyLimitMiddleware 在 JSON 解析前限制请求体大小，避免大请求耗尽服务内存。
func bodyLimitMiddleware(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaType := strings.ToLower(strings.TrimSpace(strings.Split(c.GetHeader("Content-Type"), ";")[0]))
		// JSON 上限不应拦截 multipart；上传 handler 会按 files.max_upload_bytes 设置独立请求上限。
		if maxBytes > 0 && c.Request.Body != nil && mediaType != "multipart/form-data" {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

// recoveryMiddleware 将未捕获 panic 转换为统一 500 响应，同时不把 panic 内容返回给客户端。
func recoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		writeError(c, fault.Internal(fmt.Errorf("HTTP handler panic 类型 %T", recovered)))
	})
}

// authenticationMiddleware 对每个受保护请求都查询 Redis，Redis 不可用时拒绝放行并返回 503。
func authenticationMiddleware(tokens accessTokenValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || tokens == nil {
			writeError(c, fault.Unauthorized())
			return
		}
		claims, err := tokens.ValidateAccess(c.Request.Context(), parts[1])
		if err != nil {
			writeError(c, err)
			return
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), claimsKey{}, claims))
		c.Next()
	}
}

// optionalAuthenticationMiddleware 为公开接口可选解析 Bearer；无令牌允许游客访问，错误令牌不降级为游客。
func optionalAuthenticationMiddleware(tokens accessTokenValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if header == "" {
			c.Next()
			return
		}
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || tokens == nil {
			writeError(c, fault.Unauthorized())
			return
		}
		claims, err := tokens.ValidateAccess(c.Request.Context(), parts[1])
		if err != nil {
			writeError(c, err)
			return
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), claimsKey{}, claims))
		c.Next()
	}
}

// healthHandler 检查 PostgreSQL 和 Redis，并返回当前服务版本；任一依赖不可用时 readiness 返回 503。
func healthHandler(postgresPing, redisPing func(context.Context) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		if postgresPing == nil {
			writeError(c, fault.Unavailable(errors.New("PostgreSQL 健康检查未配置")))
			return
		}
		if err := postgresPing(ctx); err != nil {
			writeError(c, fault.Unavailable(err))
			return
		}
		if redisPing == nil {
			writeError(c, fault.Unavailable(errors.New("Redis 健康检查失败")))
			return
		}
		if err := redisPing(ctx); err != nil {
			writeError(c, fault.Unavailable(err))
			return
		}
		writeData(c, http.StatusOK, gin.H{"status": "ok", "version": version.Current})
	}
}

// publicConfigsHandler 允许客户端读取 is_public=true 的配置值；未开放或不存在的 key 一律不返回。
func publicConfigsHandler(configs publicConfigReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if configs == nil {
			writeError(c, fault.Unavailable(errors.New("公开配置服务未配置")))
			return
		}
		keys, err := requestedPublicConfigKeys(c)
		if err != nil {
			writeError(c, err)
			return
		}
		items, err := configs.ListPublicConfigValues(c.Request.Context(), keys)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": items})
	}
}

// requestedPublicConfigKeys 解析可重复传入的 keys 参数并限制批量查询数量，避免无界查询参数。
func requestedPublicConfigKeys(c *gin.Context) ([]string, error) {
	queryValues := c.QueryArray("keys")
	if len(queryValues) > 100 {
		return nil, fault.InvalidRequest("keys 参数最多允许指定 100 个配置 key")
	}
	keys := make([]string, 0, len(queryValues))
	seen := make(map[string]struct{}, len(queryValues))
	for _, value := range queryValues {
		key := strings.TrimSpace(value)
		if key == "" {
			return nil, fault.InvalidRequest("keys 参数中的配置 key 不能为空")
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys, nil
}

// authContext 返回认证中间件写入上下文的用户和会话标识；缺失时按未认证处理。
func authContext(c *gin.Context) (store.TokenClaims, error) {
	claims, ok := c.Request.Context().Value(claimsKey{}).(store.TokenClaims)
	if !ok || claims.UserBusinessID == uuid.Nil || claims.SessionID == "" {
		return store.TokenClaims{}, fault.Unauthorized()
	}
	return claims, nil
}

// successEnvelope 定义 V1 非 204 成功响应的统一外层结构。
type successEnvelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

// errorBody 只携带稳定业务错误信息，不包含底层数据库或 Redis 错误文本。
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details"`
}

// errorEnvelope 定义 V1 错误响应的统一外层结构和请求追踪标识。
type errorEnvelope struct {
	Error     errorBody `json:"error"`
	RequestID string    `json:"request_id"`
}

// writeData 输出 V1 统一成功封套，资料和列表 DTO 均由 handler 显式构造。
func writeData(c *gin.Context, status int, data any) {
	c.JSON(status, successEnvelope{Data: data, RequestID: requestID(c)})
}

// writeError 将服务错误映射为稳定 HTTP 响应，只暴露白名单中的业务错误字段。
func writeError(c *gin.Context, err error) {
	var appError *fault.Error
	if !errors.As(err, &appError) {
		appError = fault.Internal(err)
	}
	if appError.Status >= http.StatusInternalServerError {
		if value, exists := c.Get(errorLogContextKey); exists {
			if errorLogs, ok := value.(*service.ErrorLogService); ok && errorLogs != nil {
				errorLogs.CaptureHTTP(appError, requestID(c), c.Request.Method, c.FullPath())
			} else {
				log.Printf("request_id=%s status=%d code=%s cause_type=%T", requestID(c), appError.Status, appError.Code, appError.Cause)
			}
		} else {
			log.Printf("request_id=%s status=%d code=%s cause_type=%T", requestID(c), appError.Status, appError.Code, appError.Cause)
		}
	}
	c.AbortWithStatusJSON(appError.Status, errorEnvelope{
		Error:     errorBody{Code: appError.Code, Message: appError.Message, Details: appError.Details},
		RequestID: requestID(c),
	})
}

// requestID 读取中间件已生成的请求标识，兜底分支用于未安装中间件的路由单测或局部调用。
func requestID(c *gin.Context) string {
	if value, exists := c.Get("request_id"); exists {
		if id, ok := value.(string); ok {
			return id
		}
	}
	return "unknown"
}

// decodeJSON 严格解析单个 JSON 值，拒绝未知字段、空请求体和尾随的第二个 JSON 值。
func decodeJSON(c *gin.Context, target any) error {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(c.GetHeader("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		return fault.InvalidRequest("请求 Content-Type 必须是 application/json")
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fault.New(http.StatusRequestEntityTooLarge, "request_too_large", "请求体超过允许大小", nil, err)
		}
		return fault.InvalidRequest("请求体必须是有效且符合接口字段定义的 JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fault.New(http.StatusRequestEntityTooLarge, "request_too_large", "请求体超过允许大小", nil, err)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fault.InvalidRequest("请求体只能包含一个 JSON 值")
	}
	return fault.InvalidRequest("请求体只能包含一个 JSON 值")
}

// decodeObject 为 PATCH 请求保留字段存在性，并明确拒绝对象以外的 JSON 根值。
func decodeObject(c *gin.Context) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := decodeJSON(c, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fault.InvalidRequest("请求体必须是 JSON 对象")
	}
	return fields, nil
}

// allowFields 在手写 PATCH DTO 中执行未知字段检查，防止客户端误以为内部字段可写。
func allowFields(fields map[string]json.RawMessage, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range fields {
		if _, exists := set[key]; !exists {
			return fault.InvalidRequest("请求包含不支持的字段: " + key)
		}
	}
	return nil
}

// registerRequest 承载手机号、密码和昵称；未知字段由严格 JSON 解码器拒绝。
type registerRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

// registerHandler 创建用户并签发一对 token，响应 DTO 不包含数据库自增 ID 或密码哈希。
func registerHandler(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request registerRequest
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		result, err := auth.Register(c.Request.Context(), request.Phone, request.Password, request.Nickname)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, authResponse{User: userDTO(result.User), Tokens: result.Tokens})
	}
}

// upgradeRequiredHandler 要求旧客户端升级到头像必传的 multipart 注册协议。
func upgradeRequiredHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		writeError(c, fault.New(http.StatusUpgradeRequired, "upgrade_required", "请升级客户端后再注册", nil, nil))
	}
}

// registerV2Handler 校验头像后保存文件，再创建手机号账户并签发令牌；失败时回收未绑定文件。
func registerV2Handler(dependencies Dependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if dependencies.Auth == nil || dependencies.Files == nil || dependencies.CurrentFileStoreType == nil {
			writeError(c, fault.Unavailable(errors.New("注册服务未配置")))
			return
		}
		if err := parseMultipartRequest(c, multipartBodyLimit(dependencies.MaxUploadBytes, 1)); err != nil {
			writeError(c, err)
			return
		}
		defer removeMultipartTempFiles(c)
		form := c.Request.MultipartForm
		if err := validateMultipartKeys(form, []string{"phone", "password", "nickname"}, []string{"avatar"}); err != nil {
			writeError(c, err)
			return
		}
		phone, err := oneMultipartValue(form, "phone", true)
		if err != nil {
			writeError(c, err)
			return
		}
		password, err := oneMultipartValue(form, "password", true)
		if err != nil {
			writeError(c, err)
			return
		}
		nickname, err := oneMultipartValue(form, "nickname", true)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := dependencies.Auth.ValidateRegistration(phone, password, nickname); err != nil {
			writeError(c, err)
			return
		}
		avatarFiles := form.File["avatar"]
		if len(avatarFiles) != 1 {
			writeError(c, fault.InvalidField("avatar", "注册必须上传一张头像"))
			return
		}
		content, err := readValidatedImage(avatarFiles[0], dependencies.MaxUploadBytes)
		if err != nil {
			writeError(c, err)
			return
		}
		storeType, err := dependencies.CurrentFileStoreType(c.Request.Context())
		if err != nil {
			writeError(c, fault.Unavailable(err))
			return
		}
		avatarKey, err := dependencies.Files.Store(c.Request.Context(), storeType, avatarFiles[0].Filename, bytes.NewReader(content), dependencies.MaxUploadBytes)
		if err != nil {
			writeError(c, fileStoreError(err))
			return
		}
		result, err := dependencies.Auth.RegisterWithAvatar(c.Request.Context(), phone, password, nickname, avatarKey)
		if err != nil {
			if result.User.BusinessID == uuid.Nil {
				_ = dependencies.Files.Delete(context.Background(), avatarKey)
			}
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, authResponse{User: userDTO(result.User), Tokens: result.Tokens})
	}
}

// feedbackHandler 接收游客或 bearer 用户反馈；限流通过可信代理解析的 ClientIP 在写入前执行。
func feedbackHandler(dependencies Dependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if dependencies.Feedback == nil || dependencies.Files == nil || dependencies.CurrentFileStoreType == nil {
			writeError(c, fault.Unavailable(errors.New("反馈服务未配置")))
			return
		}
		if err := parseMultipartRequest(c, multipartBodyLimit(dependencies.MaxUploadBytes, 5)); err != nil {
			writeError(c, err)
			return
		}
		defer removeMultipartTempFiles(c)
		form := c.Request.MultipartForm
		if err := validateMultipartKeys(form, []string{"content"}, []string{"images"}); err != nil {
			writeError(c, err)
			return
		}
		content, err := optionalMultipartValue(form, "content")
		if err != nil {
			writeError(c, err)
			return
		}
		imageFiles := form.File["images"]
		if err := dependencies.Feedback.Validate(content, len(imageFiles)); err != nil {
			writeError(c, err)
			return
		}
		if err := dependencies.Feedback.Reserve(c.Request.Context(), c.ClientIP()); err != nil {
			writeError(c, err)
			return
		}

		storeType := ""
		if len(imageFiles) > 0 {
			storeType, err = dependencies.CurrentFileStoreType(c.Request.Context())
			if err != nil {
				writeError(c, fault.Unavailable(err))
				return
			}
		}
		imageKeys := make([]string, 0, len(imageFiles))
		cleanup := func() {
			for _, key := range imageKeys {
				_ = dependencies.Files.Delete(context.Background(), key)
			}
		}
		for _, header := range imageFiles {
			data, readErr := readValidatedImage(header, dependencies.MaxUploadBytes)
			if readErr != nil {
				cleanup()
				writeError(c, readErr)
				return
			}
			key, storeErr := dependencies.Files.StorePublic(c.Request.Context(), storeType, header.Filename, bytes.NewReader(data), dependencies.MaxUploadBytes)
			if storeErr != nil {
				cleanup()
				writeError(c, fileStoreError(storeErr))
				return
			}
			imageKeys = append(imageKeys, key)
		}
		var userID *uuid.UUID
		if claims, ok := c.Request.Context().Value(claimsKey{}).(store.TokenClaims); ok && claims.UserBusinessID != uuid.Nil {
			id := claims.UserBusinessID
			userID = &id
		}
		if err := dependencies.Feedback.Submit(c.Request.Context(), userID, content, imageKeys); err != nil {
			cleanup()
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, gin.H{"received": true})
	}
}

// parseMultipartRequest 限制 multipart 总体积，并把最大单图字节数留给单文件验证。
func parseMultipartRequest(c *gin.Context, maxBodyBytes int64) error {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(c.GetHeader("Content-Type"), ";")[0]))
	if contentType != "multipart/form-data" {
		return fault.InvalidRequest("请求 Content-Type 必须是 multipart/form-data")
	}
	if maxBodyBytes > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	}
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fault.New(http.StatusRequestEntityTooLarge, "request_too_large", "上传内容超过允许大小", nil, err)
		}
		return fault.InvalidRequest("multipart 请求格式无效")
	}
	if c.Request.MultipartForm == nil {
		return fault.InvalidRequest("multipart 请求格式无效")
	}
	return nil
}

// removeMultipartTempFiles 回收标准库在大 multipart 请求中落盘的临时文件。
func removeMultipartTempFiles(c *gin.Context) {
	if c.Request.MultipartForm != nil {
		_ = c.Request.MultipartForm.RemoveAll()
	}
}

// multipartBodyLimit 为图片文件上限预留边界头与正文空间，并避免乘法溢出。
func multipartBodyLimit(maxFileBytes int64, files int) int64 {
	const overhead = int64(1 << 20)
	if maxFileBytes < 1 || files < 1 || maxFileBytes > (math.MaxInt64-overhead)/int64(files) {
		return math.MaxInt64
	}
	return maxFileBytes*int64(files) + overhead
}

// validateMultipartKeys 拒绝未知 multipart 字段，避免客户端误以为未支持的数据已被保存。
func validateMultipartKeys(form *multipart.Form, valueKeys, fileKeys []string) error {
	allowedValues := make(map[string]struct{}, len(valueKeys))
	allowedFiles := make(map[string]struct{}, len(fileKeys))
	for _, key := range valueKeys {
		allowedValues[key] = struct{}{}
	}
	for _, key := range fileKeys {
		allowedFiles[key] = struct{}{}
	}
	for key := range form.Value {
		if _, ok := allowedValues[key]; !ok {
			return fault.InvalidRequest("请求包含不支持的表单字段: " + key)
		}
	}
	for key := range form.File {
		if _, ok := allowedFiles[key]; !ok {
			return fault.InvalidRequest("请求包含不支持的文件字段: " + key)
		}
	}
	return nil
}

// oneMultipartValue 读取且只允许出现一次的必需 multipart 字段。
func oneMultipartValue(form *multipart.Form, key string, required bool) (string, error) {
	values := form.Value[key]
	if len(values) != 1 || (required && strings.TrimSpace(values[0]) == "") {
		return "", fault.InvalidField(key, "表单字段缺失或重复")
	}
	return values[0], nil
}

// optionalMultipartValue 允许反馈正文缺省，但拒绝重复字段。
func optionalMultipartValue(form *multipart.Form, key string) (string, error) {
	values := form.Value[key]
	if len(values) > 1 {
		return "", fault.InvalidField(key, "表单字段不能重复")
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}

// readValidatedImage 对单张图片执行字节上限和完整解码校验，返回的缓冲区仅在当前请求使用。
func readValidatedImage(header *multipart.FileHeader, maxBytes int64) ([]byte, error) {
	if header.Size > maxBytes {
		return nil, fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "图片超过服务端允许的大小", nil, nil)
	}
	file, err := header.Open()
	if err != nil {
		return nil, fault.InvalidField("images", "图片文件无法读取")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fault.InvalidField("images", "图片文件无法读取")
	}
	if int64(len(data)) > maxBytes {
		return nil, fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "图片超过服务端允许的大小", nil, nil)
	}
	if err := imagecompress.Validate(data); err != nil {
		return nil, fault.New(http.StatusBadRequest, "invalid_image", "图片格式或内容无效", nil, err)
	}
	return data, nil
}

// fileStoreError 将文件存储边界错误映射为稳定公开业务错误。
func fileStoreError(err error) error {
	if errors.Is(err, filestore.ErrFileTooLarge) {
		return fault.New(http.StatusRequestEntityTooLarge, "file_too_large", "图片超过服务端允许的大小", nil, err)
	}
	if errors.Is(err, filestore.ErrBackendUnavailable) {
		return fault.Unavailable(err)
	}
	return fault.Unavailable(err)
}

// loginHandler 验证手机号和密码，并为当前设备创建独立 Redis 会话。
func loginHandler(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request loginRequest
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		result, err := auth.Login(c.Request.Context(), request.Phone, request.Password)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, authResponse{User: userDTO(result.User), Tokens: result.Tokens})
	}
}

// loginRequest 只允许手机号和密码，注册专用昵称字段不能混入登录请求。
type loginRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

// refreshRequest 只接受旧 Refresh Token，避免客户端传入用户或会话标识。
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// refreshHandler 消费请求中的旧 Refresh Token，并返回 Redis 原子轮换后签发的新 token pair。
func refreshHandler(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request refreshRequest
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		tokens, err := auth.Refresh(c.Request.Context(), request.RefreshToken)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"tokens": tokens})
	}
}

// logoutHandler 按当前 Redis 会话索引撤销该设备的 access 和 refresh token。
func logoutHandler(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := auth.Logout(c.Request.Context(), claims.SessionID); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// authResponse 组合脱敏用户资料和签发后的 token pair。
type authResponse struct {
	User   userResponse    `json:"user"`
	Tokens store.TokenPair `json:"tokens"`
}

// userResponse 只包含对外业务 UUID、用户资料和时间字段，不暴露内部自增主键。
type userResponse struct {
	BusinessID uuid.UUID `json:"business_id"`
	Nickname   string    `json:"nickname"`
	Phone      string    `json:"phone"`
	AvatarKey  *string   `json:"avatar_key"`
	IsAdmin    bool      `json:"is_admin"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// userDTO 显式映射公开资料字段，确保数据库主键、密码哈希和内部 user_id 不会被序列化。
func userDTO(user model.User) userResponse {
	return userResponse{BusinessID: user.BusinessID, Nickname: user.Nickname, Phone: user.Phone,
		AvatarKey: user.AvatarKey, IsAdmin: user.IsAdmin, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}
}

// getCurrentUserHandler 按 token 中的用户业务 UUID 返回当前用户资料。
func getCurrentUserHandler(users *service.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		user, err := users.GetCurrent(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, userDTO(user))
	}
}

// patchCurrentUserHandler 区分头像字段省略与显式 null，分别表示保持不变和清除头像。
func patchCurrentUserHandler(users *service.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		fields, err := decodeObject(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := allowFields(fields, "nickname", "avatar_key"); err != nil {
			writeError(c, err)
			return
		}
		var patch service.ProfilePatch
		if raw, exists := fields["nickname"]; exists {
			patch.NicknameSet = true
			if err := json.Unmarshal(raw, &patch.Nickname); err != nil {
				writeError(c, fault.InvalidRequest("nickname 必须是字符串"))
				return
			}
		}
		if raw, exists := fields["avatar_key"]; exists {
			patch.AvatarSet = true
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				var value string
				if err := json.Unmarshal(raw, &value); err != nil {
					writeError(c, fault.InvalidRequest("avatar_key 必须是字符串或 null"))
					return
				}
				patch.AvatarKey = &value
			}
		}
		user, err := users.UpdateCurrent(c.Request.Context(), claims.UserBusinessID, patch)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, userDTO(user))
	}
}

// tagResponse 是不包含 user_id 与数据库 id 的标签公开 DTO。
type tagResponse struct {
	BusinessID uuid.UUID `json:"business_id"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// tagDTO 映射标签公开字段，不暴露标签所属用户字段或数据库自增 ID。
func tagDTO(tag model.Tag) tagResponse {
	return tagResponse{BusinessID: tag.BusinessID, Content: tag.Content, CreatedAt: tag.CreatedAt, UpdatedAt: tag.UpdatedAt}
}

// pageResponse 统一输出分页项目、页号、页大小与总记录数。
type pageResponse[T any] struct {
	Items    []T   `json:"items"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

// listTagsHandler 按当前用户隔离并返回标准分页标签集合。
// changePasswordHandler 仅接受当前密码和新密码；当前用户与会话只能来自已验证令牌。
func changePasswordHandler(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var request struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		if err := auth.ChangePassword(c.Request.Context(), claims.UserBusinessID, claims.SessionID, request.CurrentPassword, request.NewPassword); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func listTagsHandler(tags *service.TagService) gin.HandlerFunc {
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
		query, err := optionalQueryString(c, "q")
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := tags.List(c.Request.Context(), claims.UserBusinessID, query, page)
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]tagResponse, 0, len(result.Items))
		for _, tag := range result.Items {
			items = append(items, tagDTO(tag))
		}
		writeData(c, http.StatusOK, pageResponse[tagResponse]{Items: items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
	}
}

// tagRequest 仅接受标签内容，归属用户始终由已认证 token 决定。
type tagRequest struct {
	Content string `json:"content"`
}

// createTagHandler 为当前用户创建标签，所有者 UUID 来源于已校验 token。
func createTagHandler(tags *service.TagService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var request tagRequest
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		tag, err := tags.Create(c.Request.Context(), claims.UserBusinessID, request.Content)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, tagDTO(tag))
	}
}

// parseBusinessID 解析路由中的业务 UUID，拒绝空值、非法 UUID 和全零 UUID。
func parseBusinessID(c *gin.Context) (uuid.UUID, error) {
	value, err := uuid.Parse(c.Param("business_id"))
	if err != nil || value == uuid.Nil {
		return uuid.Nil, fault.InvalidRequest("business_id 必须是有效 UUID")
	}
	return value, nil
}

// updateTagHandler 仅根据业务 UUID 更新当前用户自己的标签内容。
func updateTagHandler(tags *service.TagService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		tagID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var request tagRequest
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		tag, err := tags.Update(c.Request.Context(), claims.UserBusinessID, tagID, request.Content)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, tagDTO(tag))
	}
}

// deleteTagHandler 事务内清理灵感图标签数组后删除当前用户的标签。
func deleteTagHandler(tags *service.TagService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		tagID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := tags.Delete(c.Request.Context(), claims.UserBusinessID, tagID); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// inspirationResponse 输出灵感图业务字段，并保留 data 的 JSON 对象结构。
type inspirationResponse struct {
	BusinessID    uuid.UUID                       `json:"business_id"`
	TagIDs        []string                        `json:"tag_ids"`
	TagNames      []string                        `json:"tag_names"`
	ImageKey      string                          `json:"image_key"`
	FileName      string                          `json:"file_name"`
	FileHash      *string                         `json:"file_hash"`
	FileSize      *int64                          `json:"file_size"`
	ImageMetadata *model.InspirationImageMetadata `json:"image_metadata,omitempty"`
	Data          json.RawMessage                 `json:"data"`
	CreatedAt     time.Time                       `json:"created_at"`
	UpdatedAt     time.Time                       `json:"updated_at"`
}

// inspirationDTO 显式输出业务 UUID、标签业务 UUID 和 JSONB 文档，不暴露用户归属字段。
func inspirationDTO(inspiration model.Inspiration, includeImageMetadata bool) inspirationResponse {
	tagIDs := make([]string, 0, len(inspiration.TagIDs))
	for _, tagID := range inspiration.TagIDs {
		tagIDs = append(tagIDs, tagID)
	}
	tagNames := append([]string{}, inspiration.TagNames...)
	data := json.RawMessage(append([]byte(nil), inspiration.Data...))
	response := inspirationResponse{BusinessID: inspiration.BusinessID, TagIDs: tagIDs, TagNames: tagNames, ImageKey: inspiration.ImageKey,
		FileName: inspiration.FileName, FileHash: inspiration.FileHash, FileSize: inspiration.FileSize,
		Data: data, CreatedAt: inspiration.CreatedAt, UpdatedAt: inspiration.UpdatedAt}
	if includeImageMetadata {
		metadata := service.NormalizeImageMetadata(inspiration.ImageMetadata)
		response.ImageMetadata = &metadata
	}
	return response
}

// listInspirationsHandler 解析重复标签查询参数及 any/all 模式后返回当前用户的分页灵感图。
func listInspirationsHandler(inspirations *service.InspirationService) gin.HandlerFunc {
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
		tagIDs, err := parseTagQuery(c)
		if err != nil {
			writeError(c, err)
			return
		}
		match := c.Query("tag_match")
		search, err := optionalQueryString(c, "q")
		if err != nil {
			writeError(c, err)
			return
		}
		sort, err := optionalQueryString(c, "sort")
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := inspirations.List(c.Request.Context(), claims.UserBusinessID, tagIDs, match, search, sort, page)
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]inspirationResponse, 0, len(result.Items))
		for _, inspiration := range result.Items {
			items = append(items, inspirationDTO(inspiration, false))
		}
		writeData(c, http.StatusOK, pageResponse[inspirationResponse]{Items: items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
	}
}

// inspirationStatisticsHandler 使用 token 中的用户业务 UUID 查询其图片属性统计。
func inspirationStatisticsHandler(statistics *service.InspirationStatisticsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := statistics.Get(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, result)
	}
}

// inspirationRequest 描述创建灵感图时允许客户端提交的字段集合。
type inspirationRequest struct {
	TagIDs        []uuid.UUID                     `json:"tag_ids"`
	ImageKey      string                          `json:"image_key"`
	FileName      string                          `json:"file_name"`
	Data          json.RawMessage                 `json:"data"`
	ImageMetadata *model.InspirationImageMetadata `json:"image_metadata"`
}

// dereferenceImageMetadata 将可选请求对象转换为服务层使用的零值或完整值。
func dereferenceImageMetadata(metadata *model.InspirationImageMetadata) model.InspirationImageMetadata {
	if metadata == nil {
		return model.InspirationImageMetadata{}
	}
	return *metadata
}

// createInspirationHandler 校验请求字段、标签归属及 JSON 文档版本后创建灵感图。
func createInspirationHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var request inspirationRequest
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		inspiration, err := inspirations.Create(c.Request.Context(), claims.UserBusinessID, service.InspirationInput{
			TagIDs: request.TagIDs, ImageKey: request.ImageKey, FileName: request.FileName, Data: request.Data,
			ImageMetadata: dereferenceImageMetadata(request.ImageMetadata),
		})
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, inspirationDTO(inspiration, true))
	}
}

// getInspirationHandler 使用当前用户业务 UUID 和资源业务 UUID 双重限定详情查询。
func getInspirationHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		inspirationID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		inspiration, err := inspirations.Get(c.Request.Context(), claims.UserBusinessID, inspirationID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, inspirationDTO(inspiration, true))
	}
}

// patchInspirationHandler 根据字段存在性执行局部更新；tag_ids 与 tag_names 空数组都用于清除全部标签。
func patchInspirationHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		inspirationID, err := parseBusinessID(c)
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
		if tagIDsSet && tagNamesSet {
			writeError(c, fault.InvalidRequest("tag_ids 和 tag_names 不能同时传入"))
			return
		}
		_, imageKeySet := fields["image_key"]
		_, fileNameSet := fields["file_name"]
		_, dataSet := fields["data"]
		_, imageMetadataSet := fields["image_metadata"]
		if raw, exists := fields["tag_ids"]; exists {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				writeError(c, fault.InvalidRequest("tag_ids 必须是 UUID 数组；清空标签请传空数组"))
				return
			}
			if err := json.Unmarshal(raw, &input.TagIDs); err != nil || input.TagIDs == nil {
				writeError(c, fault.InvalidRequest("tag_ids 必须是 UUID 数组"))
				return
			}
		}
		if raw, exists := fields["tag_names"]; exists {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				writeError(c, fault.InvalidRequest("tag_names 必须是字符串数组；清空标签请传空数组"))
				return
			}
			if err := json.Unmarshal(raw, &input.TagNames); err != nil || input.TagNames == nil {
				writeError(c, fault.InvalidRequest("tag_names 必须是字符串数组"))
				return
			}
		}
		if raw, exists := fields["image_key"]; exists {
			if err := json.Unmarshal(raw, &input.ImageKey); err != nil {
				writeError(c, fault.InvalidRequest("image_key 必须是字符串"))
				return
			}
		}
		if raw, exists := fields["file_name"]; exists {
			if err := json.Unmarshal(raw, &input.FileName); err != nil {
				writeError(c, fault.InvalidRequest("file_name 必须是字符串"))
				return
			}
		}
		if raw, exists := fields["data"]; exists {
			input.Data = append(json.RawMessage(nil), raw...)
		}
		if raw, exists := fields["image_metadata"]; exists {
			trimmed := bytes.TrimSpace(raw)
			if len(trimmed) == 0 || trimmed[0] != '{' {
				writeError(c, fault.InvalidRequest("image_metadata 必须是 JSON 对象"))
				return
			}
			if err := json.Unmarshal(trimmed, &input.ImageMetadata); err != nil {
				writeError(c, fault.InvalidRequest("image_metadata 格式无效"))
				return
			}
		}
		inspiration, err := inspirations.Update(c.Request.Context(), claims.UserBusinessID, inspirationID,
			input, tagIDsSet, tagNamesSet, imageKeySet, fileNameSet, dataSet, imageMetadataSet)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, inspirationDTO(inspiration, true))
	}
}

// deleteInspirationHandler 物理删除当前用户拥有的灵感图。
func deleteInspirationHandler(inspirations *service.InspirationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		inspirationID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := inspirations.Delete(c.Request.Context(), claims.UserBusinessID, inspirationID); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// parsePagination 执行默认 page=1/page_size=20 及最大 100 条的限制，并阻止偏移整数溢出。
func parsePagination(c *gin.Context) (service.PageRequest, error) {
	page, err := queryInteger(c, "page", 1)
	if err != nil || page < 1 {
		return service.PageRequest{}, fault.InvalidRequest("page 必须是大于零的整数")
	}
	pageSize, err := queryInteger(c, "page_size", 20)
	if err != nil || pageSize < 1 || pageSize > 100 {
		return service.PageRequest{}, fault.InvalidRequest("page_size 必须是 1 至 100 的整数")
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return service.PageRequest{}, fault.InvalidRequest("分页偏移超出允许范围")
	}
	return service.PageRequest{Page: page, PageSize: pageSize, Offset: (page - 1) * pageSize}, nil
}

// queryInteger 读取单值整数查询参数，拒绝重复、空值和溢出输入。
func queryInteger(c *gin.Context, name string, fallback int) (int, error) {
	values, exists := c.Request.URL.Query()[name]
	if !exists {
		return fallback, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, errors.New("重复或空查询参数")
	}
	value, err := strconv.Atoi(values[0])
	if err != nil {
		return 0, err
	}
	return value, nil
}

// optionalQueryString 读取可选单值查询参数并拒绝重复键，避免不同代理和客户端对参数顺序解释不一致。
func optionalQueryString(c *gin.Context, name string) (string, error) {
	values, exists := c.Request.URL.Query()[name]
	if !exists {
		return "", nil
	}
	if len(values) != 1 {
		return "", fault.InvalidRequest(name + " 只能出现一次")
	}
	return values[0], nil
}

// parseTagQuery 解析重复传入的 tag_ids，去重并限制一次检索最多筛选 100 个标签。
func parseTagQuery(c *gin.Context) ([]uuid.UUID, error) {
	values := c.QueryArray("tag_ids")
	if len(values) > 100 {
		return nil, fault.InvalidRequest("tag_ids 最多支持 100 个")
	}
	seen := make(map[uuid.UUID]struct{}, len(values))
	result := make([]uuid.UUID, 0, len(values))
	for _, raw := range values {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return nil, fault.InvalidRequest("tag_ids 必须全部是有效 UUID")
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	if values, exists := c.Request.URL.Query()["tag_match"]; exists && len(values) != 1 {
		return nil, fault.InvalidRequest("tag_match 只能出现一次")
	}
	return result, nil
}
