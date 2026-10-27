package service

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/photo-nini/backend/internal/model"
)

// TestLibraryFiltersValidation 确保查询白名单拒绝伪日期、SQL 片段和不支持的分类。
func TestLibraryFiltersValidation(t *testing.T) {
	for _, f := range []model.InspirationFilters{{DateFrom: "2026-02-30"}, {DateFrom: "2026-10-28", DateTo: "2026-10-01"}, {People: "0 OR 1=1"}, {Color: "h12"}, {Project: "other-user"}, {EmptyShot: "unknown"}, {Status: "failed|succeeded"}} {
		if ValidateInspirationFilters(f) == nil {
			t.Errorf("应拒绝: %+v", f)
		}
	}
	for _, f := range []model.InspirationFilters{{}, {People: "3+", EmptyShot: "false", Color: "h11", Project: "unassigned", Status: "succeeded"}, {DateFrom: "2026-10-01", DateTo: "2026-10-31", Color: "gray"}} {
		if e := ValidateInspirationFilters(f); e != nil {
			t.Errorf("合法条件被拒绝: %v", e)
		}
	}
}

// TestDailyScanBoundary 覆盖启动、北京时间三点和次日边界。
func TestDailyScanBoundary(t *testing.T) {
	at := func(value string) time.Time {
		d, e := time.ParseInLocation("2006-01-02 15:04", value, inspirationChina)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	cases := []struct {
		previous  string
		last, now string
		due       bool
	}{{"", "2026-10-27", "2026-10-27 01:00", false}, {"", "2026-10-27", "2026-10-27 03:10", false}, {"02:59", "2026-10-27", "2026-10-27 03:00", true}, {"03:00", "2026-10-27", "2026-10-27 03:01", false}, {"02:59", "2026-10-27", "2026-10-28 03:00", true}}
	for _, c := range cases {
		if inspirationScanDue(c.last, c.previous, at(c.now), "03:00") != c.due {
			t.Errorf("扫描边界错误: %+v", c)
		}
	}
}

// readinessTransport 模拟进程已经启动但模型尚未完成加载的私有健康接口。
type readinessTransport struct{ ready *atomic.Bool }

func (t readinessTransport) RoundTrip(*http.Request) (*http.Response, error) {
	body := `{"ready":false}`
	if t.ready.Load() {
		body = `{"ready":true}`
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

// TestAIChildProcess 仅作为监控测试的可终止子进程，不加载真实模型。
func TestAIChildProcess(t *testing.T) {
	if os.Getenv("PHOTO_NINI_TEST_AI_CHILD") != "1" {
		return
	}
	time.Sleep(20 * time.Second)
}

// TestAIReadinessLifecycle 验证健康就绪前不能许可任务，退出后撤销许可，避免冷启动等待重试。
func TestAIReadinessLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAIChildProcess$")
	cmd.Env = append(os.Environ(), "PHOTO_NINI_TEST_AI_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var ready atomic.Bool
	s := &InspirationLibraryService{client: &http.Client{Transport: readinessTransport{&ready}}}
	done := make(chan error, 1)
	go func() { done <- s.monitorAI(ctx, cmd, InspirationProcessingConfig{PollMS: 20, TimeoutSeconds: 1}) }()
	time.Sleep(60 * time.Millisecond)
	if s.aiReady.Load() {
		cancel()
		<-done
		t.Fatal("模型未就绪即许可了任务")
	}
	ready.Store(true)
	deadline := time.Now().Add(time.Second)
	for !s.aiReady.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !s.aiReady.Load() {
		cancel()
		<-done
		t.Fatal("模型就绪后未开放任务")
	}
	cancel()
	<-done
	if s.aiReady.Load() {
		t.Fatal("进程退出后仍标记为就绪")
	}
}
