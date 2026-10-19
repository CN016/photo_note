package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
)

type consoleRepositoryForHTTP struct {
	admin   bool
	configs []model.AppConfig
}

func (repository *consoleRepositoryForHTTP) IsAdministrator(context.Context, uuid.UUID) (bool, error) {
	return repository.admin, nil
}
func (*consoleRepositoryForHTTP) ListConsoleUsers(context.Context, string, int, int) ([]model.ConsoleUser, int64, error) {
	return []model.ConsoleUser{}, 0, nil
}
func (*consoleRepositoryForHTTP) SetUserAdministrator(context.Context, uuid.UUID, bool) error {
	return nil
}
func (repository *consoleRepositoryForHTTP) ListConsoleConfigs(context.Context) ([]model.AppConfig, error) {
	return repository.configs, nil
}
func (*consoleRepositoryForHTTP) FindConsoleConfig(context.Context, string) (model.AppConfig, error) {
	return model.AppConfig{}, nil
}
func (*consoleRepositoryForHTTP) CreateConsoleConfig(context.Context, *model.AppConfig) error {
	return nil
}
func (*consoleRepositoryForHTTP) UpdateConsoleConfig(context.Context, string, map[string]any) (model.AppConfig, error) {
	return model.AppConfig{}, nil
}
func (*consoleRepositoryForHTTP) ListConsoleInspirations(context.Context, string, string, int, int) ([]model.ConsoleInspiration, int64, error) {
	return []model.ConsoleInspiration{}, 0, nil
}
func (*consoleRepositoryForHTTP) ListConsoleFeedback(context.Context, string, int, int) ([]model.ConsoleFeedback, int64, error) {
	return []model.ConsoleFeedback{}, 0, nil
}
func (*consoleRepositoryForHTTP) GetConsoleFeedback(context.Context, uuid.UUID) (model.Feedback, error) {
	return model.Feedback{}, nil
}
func (*consoleRepositoryForHTTP) UpdateConsoleFeedbackStatus(context.Context, uuid.UUID, string) error {
	return nil
}
func (*consoleRepositoryForHTTP) UpdateConsoleFeedbackReply(context.Context, uuid.UUID, string, []string, []string, string, model.JSONB, []string) error {
	return nil
}

// TestConsoleAPIAuthorizationReadsCurrentRole 每个管理请求都读取最新角色状态，撤销后下一请求立即拒绝。
func TestConsoleAPIAuthorizationReadsCurrentRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	repository := &consoleRepositoryForHTTP{}
	router := Router(Dependencies{Tokens: &testTokenStore{userID: userID}, Console: service.NewConsoleService(repository)})
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/atelier-7f3c9a2e/users", nil)
		req.Header.Set("Authorization", "Bearer valid")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	if response := request(); response.Code != http.StatusForbidden {
		t.Fatalf("ordinary user got status %d, expected forbidden", response.Code)
	}
	repository.admin = true
	if response := request(); response.Code != http.StatusOK {
		t.Fatalf("administrator got status %d, expected success: %s", response.Code, response.Body.String())
	}
	repository.admin = false
	if response := request(); response.Code != http.StatusForbidden {
		t.Fatalf("revoked user got status %d, expected forbidden", response.Code)
	}
}

// TestConsoleFeedbackReplyRequiresAdministrator 验证非管理员不能通过回复专用路由读写反馈回复。
func TestConsoleFeedbackReplyRequiresAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &consoleRepositoryForHTTP{}
	router := Router(Dependencies{Tokens: &testTokenStore{userID: uuid.New()}, Console: service.NewConsoleService(repository)})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/atelier-7f3c9a2e/feedbacks/"+uuid.NewString()+"/reply", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-administrator reply request got status %d, expected forbidden: %s", response.Code, response.Body.String())
	}
}

// TestConsoleConfigResponseDoesNotExposeSecret 确认秘密配置接口只输出配置状态，不回显原值。
func TestConsoleConfigResponseDoesNotExposeSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	repository := &consoleRepositoryForHTTP{admin: true, configs: []model.AppConfig{{
		BaseModel: model.BaseModel{BusinessID: uuid.New(), UpdatedAt: time.Now()},
		ConfigKey: "feedback.feishu_app_secret", ConfigValue: "secret-value-must-not-leak", Description: "飞书密钥", IsSecret: true,
	}}}
	router := Router(Dependencies{Tokens: &testTokenStore{userID: userID}, Console: service.NewConsoleService(repository)})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/atelier-7f3c9a2e/configs", nil)
	req.Header.Set("Authorization", "Bearer valid")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("config list got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "secret-value-must-not-leak") || strings.Contains(recorder.Body.String(), `"value"`) {
		t.Fatalf("secret value was exposed: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"is_configured":true`) {
		t.Fatalf("configured status missing from response: %s", recorder.Body.String())
	}
}

// TestProfileCannotSetAdministratorRole 验证个人资料接口严格拒绝管理角色字段。
func TestProfileCannotSetAdministratorRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	users := newTestUserRepository()
	users.users["13800000001"] = model.User{BaseModel: model.BaseModel{BusinessID: userID}, Nickname: "测试用户", Phone: "13800000001"}
	router := Router(Dependencies{Tokens: &testTokenStore{userID: userID}, Users: service.NewUserService(users)})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me", strings.NewReader(`{"is_admin":true}`))
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("profile update got status %d, expected bad request", recorder.Code)
	}
	user, err := users.FindUserByBusinessID(context.Background(), userID)
	if err != nil || user.IsAdmin {
		t.Fatalf("profile update changed role: is_admin=%v err=%v", user.IsAdmin, err)
	}
}
