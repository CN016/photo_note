package fault

import (
	"errors"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
)

// Error 表示可安全返回客户端的业务错误；Cause 仅供内部追踪，响应体不会包含底层错误文本。
type Error struct {
	Status         int
	Code           string
	Message        string
	Details        any
	Cause          error
	SourceFile     string
	SourceLine     int
	SourceFunction string
	StackTrace     string
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
	return newAtCaller(2, status, code, message, details, cause)
}

// InvalidRequest 表示 JSON、UUID、分页或查询参数的格式错误。
func InvalidRequest(message string) *Error {
	return newAtCaller(2, http.StatusBadRequest, "invalid_request", message, nil, nil)
}

// InvalidField 表示字段值未通过业务规则校验。
func InvalidField(field, message string) *Error {
	return newAtCaller(2, http.StatusUnprocessableEntity, "validation_failed", message, map[string]string{"field": field}, nil)
}

// Unauthorized 统一返回认证失败语义，避免泄露账户是否存在。
func Unauthorized() *Error {
	return newAtCaller(2, http.StatusUnauthorized, "unauthorized", "认证信息无效或已过期", nil, nil)
}

// NotFound 隐藏跨用户资源是否存在的差异。
func NotFound() *Error {
	return newAtCaller(2, http.StatusNotFound, "not_found", "资源不存在", nil, nil)
}

// Conflict 用于手机号等唯一值冲突。
func Conflict(message string) *Error {
	return newAtCaller(2, http.StatusConflict, "conflict", message, nil, nil)
}

// Unavailable 表示必需的 PostgreSQL 或 Redis 依赖不可用，调用方必须 fail closed。
func Unavailable(cause error) *Error {
	return newAtCaller(2, http.StatusServiceUnavailable, "dependency_unavailable", "服务依赖暂不可用，请稍后重试", nil, cause)
}

// Internal 隐藏未预期错误的实现细节，并保留内部原因供日志分类使用。
func Internal(cause error) *Error {
	return newAtCaller(2, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil, cause)
}

// AttachSourceFromCaller 将共享持久化错误映射到具体 repository 调用行，而不是记录包装 helper 本身。
func AttachSourceFromCaller(err error, skip int) error {
	var appError *Error
	if !errors.As(err, &appError) || appError.Status < http.StatusInternalServerError {
		return err
	}
	file, line, function, stack := captureSource(skip + 1)
	appError.SourceFile = file
	appError.SourceLine = line
	appError.SourceFunction = function
	appError.StackTrace = stack
	return err
}

// newAtCaller 创建统一错误；服务端错误保留创建位置和调用栈供内部日志定位，HTTP 响应仍只返回稳定消息。
func newAtCaller(skip int, status int, code, message string, details any, cause error) *Error {
	appError := &Error{Status: status, Code: code, Message: message, Details: details, Cause: cause}
	if status >= http.StatusInternalServerError {
		appError.SourceFile, appError.SourceLine, appError.SourceFunction, appError.StackTrace = captureSource(skip + 1)
	}
	return appError
}

// captureSource 跳过 fault 包装层后返回相对源码位置和有限长度的调用栈，不包含参数或请求内容。
func captureSource(skip int) (string, int, string, string) {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "", 0, "", ""
	}
	function := ""
	if fn := runtime.FuncForPC(pc); fn != nil {
		function = fn.Name()
	}
	file = normalizeSourcePath(file)
	stack := make([]uintptr, 16)
	count := runtime.Callers(skip+1, stack)
	frames := runtime.CallersFrames(stack[:count])
	var trace strings.Builder
	for frame, more := frames.Next(); more; frame, more = frames.Next() {
		if strings.Contains(frame.Function, "internal/domain/fault.") || strings.HasPrefix(frame.Function, "runtime.") {
			continue
		}
		trace.WriteString(normalizeSourcePath(frame.File))
		trace.WriteByte(':')
		trace.WriteString(runtimeLine(frame.Line))
		trace.WriteString(" ")
		trace.WriteString(frame.Function)
		trace.WriteByte('\n')
		if trace.Len() >= 4096 {
			break
		}
	}
	return file, line, function, trace.String()
}

// normalizeSourcePath 去掉容器或开发机绝对目录，日志表只保存项目内相对路径。
func normalizeSourcePath(file string) string {
	file = filepath.ToSlash(file)
	for _, marker := range []string{"/internal/", "/cmd/"} {
		if index := strings.LastIndex(file, marker); index >= 0 {
			return file[index+1:]
		}
	}
	return filepath.Base(file)
}

// runtimeLine 将调用栈行号格式化为十进制文本，避免引入 fmt 依赖到基础错误包。
func runtimeLine(line int) string {
	if line == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for line > 0 {
		index--
		digits[index] = byte('0' + line%10)
		line /= 10
	}
	return string(digits[index:])
}

// Is 检查错误链中是否存在指定 HTTP 状态和业务码的统一错误。
func Is(err error, status int, code string) bool {
	var appErr *Error
	return errors.As(err, &appErr) && appErr.Status == status && appErr.Code == code
}
