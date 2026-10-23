package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

const errorLogQueueSize = 512

var (
	// errorSecretPattern 遮蔽常见凭据字段及其值，不保存认证头、请求体或用户上传内容。
	errorSecretPattern = regexp.MustCompile(`(?i)\b([a-z0-9_.-]*(password|passwd|token|secret|authorization|api[_-]?key|cookie|credential|sign)[a-z0-9_.-]*)(\s*[:=]\s*)([^\s,;&]+)`)
	errorBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]+=*`)
	errorURLPattern    = regexp.MustCompile(`(?i)\b(https?|postgresql|postgres|redis)://[^\s"'<>]+`)
)

// ErrorLogRepository 负责将错误摘要写入日志表，并按需用手机号查找唯一接收用户。
type ErrorLogRepository interface {
	RecordErrorLog(context.Context, model.ErrorLog) (model.ErrorLog, bool, error)
	FindUserBusinessIDByPhone(context.Context, string) (uuid.UUID, error)
}

// ErrorLogNotificationSettingsReader 通过统一配置服务读取管理员通知开关和手机号。
type ErrorLogNotificationSettingsReader interface {
	ErrorLogNotificationSettings(context.Context) (bool, string, error)
}

// ErrorLogNotificationPublisher 只向管理员账户创建站内通知，不发送用户 webhook。
type ErrorLogNotificationPublisher interface {
	PublishErrorLog(context.Context, uuid.UUID, model.ErrorLog) error
}

// ErrorLogService 将 HTTP 5xx 和后台任务错误异步写入数据库，并在首次入库时按配置提醒管理员。
type ErrorLogService struct {
	repository ErrorLogRepository
	settings   ErrorLogNotificationSettingsReader
	notices    ErrorLogNotificationPublisher
	queue      chan model.ErrorLog
	dropped    atomic.Uint64
}

// NewErrorLogService 创建有界异步队列，避免错误写表延长业务 HTTP 请求。
func NewErrorLogService(repository ErrorLogRepository, settings ErrorLogNotificationSettingsReader, notices ErrorLogNotificationPublisher) *ErrorLogService {
	return &ErrorLogService{
		repository: repository,
		settings:   settings,
		notices:    notices,
		queue:      make(chan model.ErrorLog, errorLogQueueSize),
	}
}

// SafeErrorSummary 返回适合启动日志展示的原因类型和脱敏摘要，不包含认证头、数据库凭据或完整 URL。
func SafeErrorSummary(err error) (string, string) {
	return safeErrorCause(err)
}

// CaptureHTTP 记录服务端 HTTP 错误的安全摘要，并通过有界队列快速交给后台 worker。
func (service *ErrorLogService) CaptureHTTP(appError *fault.Error, requestID, method, route string) model.ErrorLog {
	if appError == nil {
		appError = fault.Internal(errors.New("HTTP handler returned an empty error"))
	}
	requestUUID, _ := uuid.Parse(requestID)
	var requestIDValue *uuid.UUID
	if requestUUID != uuid.Nil {
		requestIDValue = &requestUUID
	}
	causeType, causeMessage := safeErrorCause(appError.Cause)
	entry := model.ErrorLog{
		BaseModel:      model.BaseModel{BusinessID: uuid.New()},
		HTTPStatus:     appError.Status,
		ErrorCode:      appError.Code,
		ErrorMessage:   limitLogText(appError.Message, 512),
		CauseType:      causeType,
		CauseMessage:   causeMessage,
		SourceFile:     appError.SourceFile,
		SourceLine:     appError.SourceLine,
		SourceFunction: appError.SourceFunction,
		StackTrace:     limitLogText(appError.StackTrace, 4096),
		RequestID:      requestIDValue,
		HTTPMethod:     limitLogText(method, 16),
		HTTPRoute:      limitLogText(route, 256),
		Occurrences:    1,
		LastSeenAt:     time.Now().UTC(),
	}
	entry.DedupKey = errorLogDedupKey(entry.SourceFile, entry.SourceLine, entry.ErrorCode, entry.CauseType)
	service.enqueue(entry)
	return entry
}

// CaptureBackground 记录后台任务错误；调用行来自运行时调用栈，消息正文和用户数据不会被收集。
func (service *ErrorLogService) CaptureBackground(code string, cause error) model.ErrorLog {
	file, line, function, stack := captureBackgroundSource()
	causeType, causeMessage := safeErrorCause(cause)
	entry := model.ErrorLog{
		BaseModel:      model.BaseModel{BusinessID: uuid.New()},
		HTTPStatus:     500,
		ErrorCode:      limitLogText(code, 64),
		ErrorMessage:   "后台任务处理失败",
		CauseType:      causeType,
		CauseMessage:   causeMessage,
		SourceFile:     file,
		SourceLine:     line,
		SourceFunction: function,
		StackTrace:     limitLogText(stack, 4096),
		Occurrences:    1,
		LastSeenAt:     time.Now().UTC(),
	}
	entry.DedupKey = errorLogDedupKey(entry.SourceFile, entry.SourceLine, entry.ErrorCode, entry.CauseType)
	service.enqueue(entry)
	return entry
}

// Run 持续消费错误事件；标准日志、数据库写入和站内通知都只在此后台 goroutine 中执行。
func (service *ErrorLogService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer service.reportDropped()
	for {
		select {
		case entry := <-service.queue:
			service.reportDropped()
			service.logEntry(entry)
			service.persist(ctx, entry)
		case <-ticker.C:
			service.reportDropped()
		case <-ctx.Done():
			drainContext, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			for {
				select {
				case entry := <-service.queue:
					service.logEntry(entry)
					service.persist(drainContext, entry)
					if drainContext.Err() != nil {
						service.dropQueuedEntries()
						cancel()
						return
					}
				default:
					cancel()
					service.reportDropped()
					return
				}
			}
		}
	}
}

// enqueue 只做非阻塞投递；队列已满时累计丢弃数量，由后台 worker 延后输出告警。
func (service *ErrorLogService) enqueue(entry model.ErrorLog) {
	if service == nil || service.repository == nil || service.queue == nil {
		return
	}
	select {
	case service.queue <- entry:
	default:
		service.dropped.Add(1)
	}
}

// persist 保存新日志或增加一分钟内重复次数；每条事件的数据库和通知操作最多占用五秒。
func (service *ErrorLogService) persist(parent context.Context, entry model.ErrorLog) {
	// 全局诊断表与管理员通知属于系统路径，和原请求的组织私有资源访问隔离。
	ctx, cancel := context.WithTimeout(tenant.System(parent), 5*time.Second)
	defer cancel()
	recorded, inserted, err := service.repository.RecordErrorLog(ctx, entry)
	if err != nil {
		log.Printf("event=error_log_persist_failed cause_type=%T source=%s:%d", err, entry.SourceFile, entry.SourceLine)
		return
	}
	if !inserted || service.settings == nil || service.notices == nil {
		return
	}
	notify, phone, err := service.settings.ErrorLogNotificationSettings(ctx)
	if err != nil {
		log.Printf("event=error_log_notification_config_failed cause_type=%T", err)
		return
	}
	if !notify || phone == "" {
		return
	}
	userID, err := service.repository.FindUserBusinessIDByPhone(ctx, phone)
	if err != nil {
		if fault.Is(err, 404, "not_found") {
			log.Printf("event=error_log_admin_not_found")
		} else {
			log.Printf("event=error_log_admin_lookup_failed cause_type=%T", err)
		}
		return
	}
	var notificationErr error
	for _, delay := range []time.Duration{0, 100 * time.Millisecond, 250 * time.Millisecond} {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				notificationErr = ctx.Err()
			case <-timer.C:
			}
			if ctx.Err() != nil {
				break
			}
		}
		notificationErr = service.notices.PublishErrorLog(ctx, userID, recorded)
		if notificationErr == nil {
			return
		}
	}
	if notificationErr != nil {
		log.Printf("event=error_log_admin_notification_failed cause_type=%T", notificationErr)
	}
}

// logEntry 在 worker 中打印详细诊断，避免请求 goroutine 同步等待标准输出管道。
func (service *ErrorLogService) logEntry(entry model.ErrorLog) {
	if entry.RequestID != nil {
		log.Printf("request_id=%s status=%d code=%s cause_type=%s cause=%q source=%s:%d",
			entry.RequestID.String(), entry.HTTPStatus, entry.ErrorCode, entry.CauseType,
			entry.CauseMessage, entry.SourceFile, entry.SourceLine)
		return
	}
	log.Printf("event=%s cause_type=%s cause=%q source=%s:%d", entry.ErrorCode, entry.CauseType,
		entry.CauseMessage, entry.SourceFile, entry.SourceLine)
}

// reportDropped 将非阻塞队列满载时累计的丢弃数在后台线程告警，不增加请求线程 I/O。
func (service *ErrorLogService) reportDropped() {
	if count := service.dropped.Swap(0); count > 0 {
		log.Printf("event=error_log_queue_full dropped=%d", count)
	}
}

// dropQueuedEntries 在服务退出的六秒排空期限结束后释放残留队列，并汇总未写入数量。
func (service *ErrorLogService) dropQueuedEntries() {
	var count uint64
	for {
		select {
		case <-service.queue:
			count++
		default:
			if count > 0 {
				log.Printf("event=error_log_shutdown_queue_dropped count=%d", count)
			}
			service.reportDropped()
			return
		}
	}
}

// safeErrorCause 生成适合持久化的底层原因摘要；PostgreSQL 只保存结构化元数据，避免落库错误行中的用户值。
func safeErrorCause(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	causeType := fmt.Sprintf("%T", err)
	var postgresError *pq.Error
	if errors.As(err, &postgresError) {
		message := fmt.Sprintf("PostgreSQL SQLSTATE=%s severity=%s table=%s column=%s constraint=%s routine=%s",
			postgresError.Code, postgresError.Severity, postgresError.Table, postgresError.Column,
			postgresError.Constraint, postgresError.Routine)
		return causeType, limitLogText(message, 2048)
	}
	var postgresStateError interface{ SQLState() string }
	if errors.As(err, &postgresStateError) {
		return causeType, "PostgreSQL SQLSTATE=" + postgresStateError.SQLState()
	}
	message := err.Error()
	message = errorURLPattern.ReplaceAllString(message, "$1://[REDACTED]")
	message = errorBearerPattern.ReplaceAllString(message, "Bearer [REDACTED]")
	message = errorSecretPattern.ReplaceAllString(message, "$1$3[REDACTED]")
	return causeType, limitLogText(strings.TrimSpace(message), 2048)
}

// errorLogDedupKey 按源码文件和行号合并一分钟内的重复错误；无源码位置时再使用错误码和原因类型区分。
func errorLogDedupKey(file string, line int, code, causeType string) string {
	input := fmt.Sprintf("%s:%d", file, line)
	if file == "" || line < 1 {
		input = fmt.Sprintf("unknown|%s|%s", code, causeType)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}

// captureBackgroundSource 记录后台日志调用位置及有限源码栈，不采集函数参数或 goroutine 数据。
func captureBackgroundSource() (string, int, string, string) {
	pc, file, line, ok := runtime.Caller(1)
	if !ok {
		return "", 0, "", ""
	}
	function := ""
	if fn := runtime.FuncForPC(pc); fn != nil {
		function = fn.Name()
	}
	stack := make([]uintptr, 16)
	count := runtime.Callers(2, stack)
	frames := runtime.CallersFrames(stack[:count])
	var trace strings.Builder
	for frame, more := frames.Next(); more; frame, more = frames.Next() {
		if strings.HasPrefix(frame.Function, "runtime.") {
			continue
		}
		trace.WriteString(normalizeLogSource(frame.File))
		trace.WriteByte(':')
		trace.WriteString(strconv.Itoa(frame.Line))
		trace.WriteByte(' ')
		trace.WriteString(frame.Function)
		trace.WriteByte('\n')
		if trace.Len() >= 4096 {
			break
		}
	}
	return normalizeLogSource(file), line, function, trace.String()
}

// normalizeLogSource 将绝对文件名折叠为仓库内路径，避免记录宿主机目录结构。
func normalizeLogSource(file string) string {
	file = filepath.ToSlash(file)
	for _, marker := range []string{"/internal/", "/cmd/"} {
		if index := strings.LastIndex(file, marker); index >= 0 {
			return file[index+1:]
		}
	}
	return filepath.Base(file)
}

// limitLogText 按 UTF-8 字符数量截断日志字段，避免无效 UTF-8 或超大驱动错误占用数据库。
func limitLogText(value string, maximum int) string {
	if maximum < 1 || len(value) <= maximum {
		return value
	}
	if len(value) > maximum*4 {
		value = value[:maximum*4]
	}
	runes := []rune(value)
	if len(runes) > maximum {
		return string(runes[:maximum])
	}
	return value
}
