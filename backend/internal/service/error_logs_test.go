package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
)

// errorLogRecorder 只承接队列中的后台错误，帮助测试确认真实原因是否被保留。
type errorLogRecorder struct{}

func (errorLogRecorder) RecordErrorLog(_ context.Context, entry model.ErrorLog) (model.ErrorLog, bool, error) {
	return entry, true, nil
}

func (errorLogRecorder) FindUserBusinessIDByPhone(context.Context, string) (uuid.UUID, error) {
	return uuid.Nil, nil
}

// TestInspirationCapturePreservesSafeCause 防止后台任务再次把真实异常覆盖成通用错误码。
func TestInspirationCapturePreservesSafeCause(t *testing.T) {
	errorLogs := NewErrorLogService(errorLogRecorder{}, nil, nil)
	library := &InspirationLibraryService{errors: errorLogs}

	library.capture("inspiration_processing_failed", errors.New("invalid_auto_tags"))

	select {
	case entry := <-errorLogs.queue:
		if entry.CauseType != "*errors.errorString" || entry.CauseMessage != "invalid_auto_tags" {
			t.Fatalf("后台异常未被保留: type=%s message=%q", entry.CauseType, entry.CauseMessage)
		}
	default:
		t.Fatal("后台异常没有进入错误日志队列")
	}
}

// TestBackgroundErrorRedactsSecrets 确保保留原因时仍不会写入 URL 凭据或认证令牌。
func TestBackgroundErrorRedactsSecrets(t *testing.T) {
	errorLogs := NewErrorLogService(errorLogRecorder{}, nil, nil)
	entry := errorLogs.CaptureBackground(
		"inspiration_processing_failed",
		errors.New("request https://private.example/path failed Authorization: Bearer top-secret-token"),
	)

	if strings.Contains(entry.CauseMessage, "private.example") || strings.Contains(entry.CauseMessage, "top-secret-token") {
		t.Fatalf("错误原因未脱敏: %q", entry.CauseMessage)
	}
	if !strings.Contains(entry.CauseMessage, "[REDACTED]") {
		t.Fatalf("错误原因缺少脱敏标记: %q", entry.CauseMessage)
	}
}

// TestErrorLogDedupSeparatesCauses 同源码行的不同安全原因必须保留为不同诊断行。
func TestErrorLogDedupSeparatesCauses(t *testing.T) {
	first := errorLogDedupKey("internal/service/inspiration_library.go", 393, "inspiration_processing_failed", "*errors.errorString", "invalid_ai_result")
	second := errorLogDedupKey("internal/service/inspiration_library.go", 393, "inspiration_processing_failed", "*errors.errorString", "ai_inference_unavailable")
	if first == second {
		t.Fatal("不同后台错误原因被合并为同一去重键")
	}
}
