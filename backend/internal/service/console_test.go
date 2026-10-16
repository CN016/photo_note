package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
)

type consoleRepositoryFake struct {
	admin       bool
	config      model.AppConfig
	lastSearch  string
	lastSort    string
	updated     map[string]any
	feedbackIDs []uuid.UUID
}

func (repository *consoleRepositoryFake) IsAdministrator(context.Context, uuid.UUID) (bool, error) {
	return repository.admin, nil
}
func (*consoleRepositoryFake) ListConsoleUsers(context.Context, string, int, int) ([]model.ConsoleUser, int64, error) {
	return []model.ConsoleUser{}, 0, nil
}
func (*consoleRepositoryFake) SetUserAdministrator(context.Context, uuid.UUID, bool) error {
	return nil
}
func (repository *consoleRepositoryFake) ListConsoleConfigs(context.Context) ([]model.AppConfig, error) {
	return []model.AppConfig{repository.config}, nil
}
func (repository *consoleRepositoryFake) FindConsoleConfig(context.Context, string) (model.AppConfig, error) {
	return repository.config, nil
}
func (repository *consoleRepositoryFake) CreateConsoleConfig(_ context.Context, item *model.AppConfig) error {
	repository.config = *item
	return nil
}
func (repository *consoleRepositoryFake) UpdateConsoleConfig(_ context.Context, _ string, updates map[string]any) (model.AppConfig, error) {
	repository.updated = updates
	if value, ok := updates["config_value"].(string); ok {
		repository.config.ConfigValue = value
	}
	if description, ok := updates["description"].(string); ok {
		repository.config.Description = description
	}
	if public, ok := updates["is_public"].(bool); ok {
		repository.config.IsPublic = public
	}
	return repository.config, nil
}
func (repository *consoleRepositoryFake) ListConsoleInspirations(_ context.Context, search, sort string, _, _ int) ([]model.ConsoleInspiration, int64, error) {
	repository.lastSearch, repository.lastSort = search, sort
	return []model.ConsoleInspiration{}, 0, nil
}
func (*consoleRepositoryFake) ListConsoleFeedback(context.Context, string, int, int) ([]model.ConsoleFeedback, int64, error) {
	return []model.ConsoleFeedback{}, 0, nil
}
func (repository *consoleRepositoryFake) UpdateConsoleFeedbackStatus(_ context.Context, id uuid.UUID, _ string) error {
	repository.feedbackIDs = append(repository.feedbackIDs, id)
	return nil
}

// TestConsoleConfigSecretsNeverReachJSON 确保秘密原值只反映为 is_configured 状态。
func TestConsoleConfigSecretsNeverReachJSON(t *testing.T) {
	repository := &consoleRepositoryFake{config: model.AppConfig{
		BaseModel: model.BaseModel{BusinessID: uuid.New(), UpdatedAt: time.Now()},
		ConfigKey: "feedback.feishu_app_secret", ConfigValue: "highly-sensitive-placeholder", Description: "仅服务端使用", IsSecret: true,
	}}
	items, err := NewConsoleService(repository).ListConfigs(context.Background())
	if err != nil {
		t.Fatalf("ListConfigs returned an error: %v", err)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal configs: %v", err)
	}
	if strings.Contains(string(encoded), repository.config.ConfigValue) || strings.Contains(string(encoded), `"value"`) {
		t.Fatalf("secret value was serialized: %s", encoded)
	}
	if !items[0].IsConfigured || items[0].Value != nil {
		t.Fatalf("expected configured secret without returned value: %#v", items[0])
	}
}

// TestConsoleSecretUpdatesPreserveBlankAndRejectPublicAccess 验证秘密配置留空保留、公开设置被拒绝。
func TestConsoleSecretUpdatesPreserveBlankAndRejectPublicAccess(t *testing.T) {
	repository := &consoleRepositoryFake{config: model.AppConfig{
		BaseModel: model.BaseModel{BusinessID: uuid.New(), UpdatedAt: time.Now()},
		ConfigKey: "feedback.feishu_app_secret", ConfigValue: "kept-secret", Description: "旧说明", IsSecret: true,
	}}
	console := NewConsoleService(repository)
	updated, err := console.UpdateConfig(context.Background(), repository.config.ConfigKey, ConsoleConfigPatch{ValueSet: true, Value: "", DescriptionSet: true, Description: "新说明"})
	if err != nil {
		t.Fatalf("blank secret update failed: %v", err)
	}
	if repository.config.ConfigValue != "kept-secret" || updated.Value != nil {
		t.Fatalf("blank secret input changed or exposed the stored value: %#v", updated)
	}
	if _, err := console.UpdateConfig(context.Background(), repository.config.ConfigKey, ConsoleConfigPatch{IsPublicSet: true, IsPublic: true}); err == nil {
		t.Fatal("secret configuration was allowed to become public")
	}
}

// TestConsoleAdministratorRoleIsReadOnEveryCall 验证角色撤销立即改变下一次服务端判定。
func TestConsoleAdministratorRoleIsReadOnEveryCall(t *testing.T) {
	repository := &consoleRepositoryFake{admin: true}
	console := NewConsoleService(repository)
	userID := uuid.New()
	allowed, err := console.IsAdministrator(context.Background(), userID)
	if err != nil || !allowed {
		t.Fatalf("expected current administrator role, allowed=%v err=%v", allowed, err)
	}
	repository.admin = false
	allowed, err = console.IsAdministrator(context.Background(), userID)
	if err != nil || allowed {
		t.Fatalf("revoked role remained active, allowed=%v err=%v", allowed, err)
	}
}

// TestConsoleInspirationSearchAndSortAreValidatedAndPassedToStorage 验证筛选条件由服务端白名单后传入 repository。
func TestConsoleInspirationSearchAndSortAreValidatedAndPassedToStorage(t *testing.T) {
	repository := &consoleRepositoryFake{}
	console := NewConsoleService(repository)
	if _, err := console.ListInspirations(context.Background(), "poster", "updated_desc", PageRequest{Page: 2, PageSize: 20, Offset: 20}); err != nil {
		t.Fatalf("valid inspiration filter failed: %v", err)
	}
	if repository.lastSearch != "poster" || repository.lastSort != "updated_desc" {
		t.Fatalf("unexpected repository filters: search=%q sort=%q", repository.lastSearch, repository.lastSort)
	}
	if _, err := console.ListInspirations(context.Background(), "", "file_name_desc", PageRequest{}); err == nil {
		t.Fatal("unrecognized sort order was accepted")
	}
}

// TestConsoleFeedbackRejectsUnknownStatuses 验证只允许新反馈和已处理两种业务状态。
func TestConsoleFeedbackRejectsUnknownStatuses(t *testing.T) {
	service := NewConsoleService(&consoleRepositoryFake{})
	if err := service.UpdateFeedbackStatus(context.Background(), uuid.New(), "deleted"); err == nil {
		t.Fatal("unsupported feedback status was accepted")
	}
}
