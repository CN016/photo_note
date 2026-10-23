package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
)

// TestStatisticsChartMode 验证接口只修改 extra 下的单个对象键并拒绝非法模式。
func TestStatisticsChartMode(t *testing.T) {
	repository := newTestUserRepository()
	userID := uuid.New()
	repository.users["13800138000"] = model.User{BaseModel: model.BaseModel{BusinessID: userID}, Phone: "13800138000", Nickname: "原昵称", Extra: model.JSONB(`{"another_feature":{"enabled":true}}`)}
	repository.users["13900139000"] = model.User{BaseModel: model.BaseModel{BusinessID: uuid.New()}, Phone: "13900139000", Nickname: "其他用户", Extra: model.JSONB(`{"another_feature":{"enabled":false}}`)}
	router := testRouter(Dependencies{Users: service.NewUserService(repository), Tokens: &testTokenStore{userID: userID}})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer valid")
		request.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		router.ServeHTTP(result, request)
		return result
	}
	for _, body := range []string{`{"dimension":"camera_models","mode":"donut"}`, `{"dimension":"collection_months","mode":"area"}`} {
		if result := call(http.MethodPatch, "/api/v1/users/me/statistics-chart-mode", body); result.Code != http.StatusOK {
			t.Fatalf("保存偏好失败: %d %s", result.Code, result.Body.String())
		}
	}
	result := call(http.MethodGet, "/api/v1/users/me", "")
	for _, expected := range []string{`"another_feature"`, `"camera_models":"donut"`, `"collection_months":"area"`, `"nickname":"原昵称"`} {
		if !strings.Contains(result.Body.String(), expected) {
			t.Fatalf("资料缺少 %s: %s", expected, result.Body.String())
		}
	}
	if strings.Contains(string(repository.users["13900139000"].Extra), "statistics_chart_modes") {
		t.Fatal("其他用户偏好被修改")
	}
	for _, body := range []string{`{"dimension":"camera_models","mode":"area"}`, `{"dimension":"unknown","mode":"donut"}`, `{"dimension":"camera_models","mode":"donut","extra":{}}`} {
		if result := call(http.MethodPatch, "/api/v1/users/me/statistics-chart-mode", body); result.Code != http.StatusBadRequest && result.Code != http.StatusUnprocessableEntity {
			t.Fatalf("非法偏好未拒绝: %d %s", result.Code, result.Body.String())
		}
	}
}
