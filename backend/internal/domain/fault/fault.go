package fault

import (
	"errors"
	"net/http"
)

// Error 表示可安全返回客户端的业务错误；Cause 仅供内部追踪，响应体不会包含底层错误文本。
type Error struct {
	Status  int
	Code    string
	Message string
	Details any
	Cause   error
}

// Error 返回面向内部调用者的稳定错误信息，不拼接数据库或 Redis 的敏感诊断内容。
func (e *Error) Error() string {
	return e.Message
}

// Unwrap 保留错误链，便于调用方使用 errors.Is/As 判断错误类别。
func (e *Error) Unwrap() error {
	return e.Cause
}

// New 创建统一格式的 API 错误，并保留可选的内部原因。
func New(status int, code, message string, details any, cause error) *Error {
	return &Error{Status: status, Code: code, Message: message, Details: details, Cause: cause}
}

// InvalidRequest 表示 JSON、UUID、分页或查询参数的格式错误。
func InvalidRequest(message string) *Error {
	return New(http.StatusBadRequest, "invalid_request", message, nil, nil)
}

// InvalidField 表示字段值未通过业务规则校验。
func InvalidField(field, message string) *Error {
	return New(http.StatusUnprocessableEntity, "validation_failed", message, map[string]string{"field": field}, nil)
}

// Unauthorized 统一返回认证失败语义，避免泄露账户是否存在。
func Unauthorized() *Error {
	return New(http.StatusUnauthorized, "unauthorized", "认证信息无效或已过期", nil, nil)
}

// NotFound 隐藏跨用户资源是否存在的差异。
func NotFound() *Error {
	return New(http.StatusNotFound, "not_found", "资源不存在", nil, nil)
}

// Conflict 用于手机号等唯一值冲突。
func Conflict(message string) *Error {
	return New(http.StatusConflict, "conflict", message, nil, nil)
}

// Unavailable 表示必需的 PostgreSQL 或 Redis 依赖不可用，调用方必须 fail closed。
func Unavailable(cause error) *Error {
	return New(http.StatusServiceUnavailable, "dependency_unavailable", "服务依赖暂不可用，请稍后重试", nil, cause)
}

// Internal 隐藏未预期错误的实现细节，并保留内部原因供日志分类使用。
func Internal(cause error) *Error {
	return New(http.StatusInternalServerError, "internal_error", "服务器内部错误", nil, cause)
}

// Is 检查错误链中是否存在指定 HTTP 状态和业务码的统一错误。
func Is(err error, status int, code string) bool {
	var appErr *Error
	return errors.As(err, &appErr) && appErr.Status == status && appErr.Code == code
}
