package service

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

type projectAIRepoStub struct {
	ProjectWorkflowRepository
	item  model.Project
	calls int
}

func (r *projectAIRepoStub) FindProject(context.Context, uuid.UUID, uuid.UUID) (model.Project, error) {
	r.calls++
	return r.item, nil
}

type projectAISettingsStub struct {
	settings ProjectAISettings
	err      error
}

func (s projectAISettingsStub) ProjectAI(context.Context) (ProjectAISettings, error) {
	return s.settings, s.err
}

type projectAIClientFunc func(*http.Request) (*http.Response, error)

func (f projectAIClientFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

// TestProjectAIGeneration 验证生成不回写、仅发送文本、错误脱敏、空配置和同策划并发限制。
func TestProjectAIGeneration(t *testing.T) {
	user, id := uuid.New(), uuid.New()
	ctx := tenant.Personal(context.Background(), user)
	repo := &projectAIRepoStub{item: model.Project{BaseModel: model.BaseModel{BusinessID: id}, Title: "拍摄", Revision: 4, ManualSummary: "光线不足", AIAnalysis: "保留原分析", Document: model.JSONB(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"尝试侧光"}]},{"type":"inspirationImage","attrs":{"src":"inspiration://private-resource"}},{"type":"projectAttachment","attrs":{"storekey":"local:private-storekey"}}]}`)}}
	settings := projectAISettingsStub{settings: ProjectAISettings{Endpoint: "https://ai.example.invalid/v1/chat/completions", Model: "configured-model", APIKey: "test-placeholder", Timeout: time.Second, MaxConcurrent: 2}}
	s := NewProjectWorkflowService(repo, settings)
	s.aiClient = projectAIClientFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), "private-resource") || strings.Contains(string(body), "private-storekey") {
			t.Error("泄漏资源标识")
		}
		if request.Header.Get("Authorization") != "Bearer test-placeholder" {
			t.Error("鉴权头错误")
		}
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil || payload["model"] != "configured-model" {
			t.Error("请求内容错误")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"下次准备补光灯"}}]}`))}, nil
	})
	view, err := s.GenerateAnalysis(ctx, user, id, ProjectAIInput{Revision: 4, Locale: "zh-CN"})
	if err != nil || view.Analysis != "下次准备补光灯" || view.SourceRevision != 4 || repo.item.AIAnalysis != "保留原分析" {
		t.Fatalf("预览错误: %#v %v", view, err)
	}
	if _, err = s.GenerateAnalysis(ctx, user, id, ProjectAIInput{Revision: 3, Locale: "zh-CN"}); !fault.Is(err, 409, "conflict") {
		t.Fatal("迟到版本未拒绝")
	}
	if _, err = s.GenerateAnalysis(context.Background(), user, id, ProjectAIInput{Revision: 4, Locale: "zh-CN"}); err == nil {
		t.Fatal("缺失范围可生成")
	}
	if _, err = s.GenerateAnalysis(tenant.System(ctx), user, id, ProjectAIInput{Revision: 4, Locale: "zh-CN"}); err == nil {
		t.Fatal("继承系统权限")
	}
	s.settings = nil
	if _, err = s.GenerateAnalysis(ctx, user, id, ProjectAIInput{Revision: 4, Locale: "zh-CN"}); !fault.Is(err, 503, "project_ai_unconfigured") {
		t.Fatal("空配置状态错误")
	}
	s.settings = settings
	s.aiClient = projectAIClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("private provider error and credential"))}, nil
	})
	_, err = s.GenerateAnalysis(ctx, user, id, ProjectAIInput{Revision: 4, Locale: "zh-CN"})
	if !fault.Is(err, 502, "project_ai_failed") || strings.Contains(err.Error(), "credential") {
		t.Fatal("远端错误未脱敏")
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	s.aiClient = projectAIClientFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"分析"}}]}`))}, nil
	})
	go func() {
		_, e := s.GenerateAnalysis(ctx, user, id, ProjectAIInput{Revision: 4, Locale: "en-US"})
		done <- e
	}()
	<-started
	if _, err = s.GenerateAnalysis(ctx, user, id, ProjectAIInput{Revision: 4, Locale: "en-US"}); !fault.Is(err, 429, "project_ai_busy") {
		t.Error("重复生成未限流")
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

// TestProjectAIPrivateAddresses 禁止把文本生成请求发送到本机、内网或链路地址。
func TestProjectAIPrivateAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.2", "169.254.169.254", "::1", "fc00::1", "fe80::1", "0.0.0.0", "100.64.0.1", "198.18.0.1", "2001:db8::1", "::ffff:127.0.0.1"} {
		if publicProjectAIIP(net.ParseIP(address)) {
			t.Errorf("允许内部地址 %s", address)
		}
	}
	if !publicProjectAIIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("错误拒绝公网地址")
	}
}

// TestProjectShootingValidation 覆盖逐步填写的草稿、非法类型及档期时长上限。
func TestProjectShootingValidation(t *testing.T) {
	start := time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)
	earlier := start.Add(-time.Hour)
	later := start.Add(25 * time.Hour)
	for _, sample := range []struct {
		name  string
		shoot model.ProjectShooting
		valid bool
	}{
		{"空草稿", model.ProjectShooting{}, true}, {"单个起点", model.ProjectShooting{StartAt: &start}, true}, {"倒置时间", model.ProjectShooting{StartAt: &start, EndAt: &earlier}, false}, {"超长区间", model.ProjectShooting{StartAt: &start, EndAt: &later}, false}, {"非法类型", model.ProjectShooting{Kind: "other"}, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			if err := validateProjectShooting(&sample.shoot); (err == nil) != sample.valid {
				t.Fatal("校验结果错误", err)
			}
		})
	}
}
