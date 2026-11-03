package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
)

// inspirationProjectsRepository 仅实现本接口需要的仓储动作，捕获已校验分页和会话用户。
type inspirationProjectsRepository struct {
	service.ProjectRepository
	calls   int
	user    uuid.UUID
	image   uuid.UUID
	offset  int
	failure error
}

func (r *inspirationProjectsRepository) ListInspirationProjects(_ context.Context, user, image uuid.UUID, offset, size int) ([]model.Project, int64, error) {
	r.calls++
	r.user = user
	r.image = image
	r.offset = offset
	if r.failure != nil {
		return nil, 0, r.failure
	}
	return []model.Project{{BaseModel: model.BaseModel{BusinessID: uuid.New(), UpdatedAt: time.Now()}, Title: "策划", UserID: user, Document: model.JSONB(`{"private":"hidden"}`)}}, 25, nil
}

// TestInspirationProjectsHTTP 验证未认证、非法参数和不可访问图片的响应，以及摘要最小字段约定。
func TestInspirationProjectsHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user, image := uuid.New(), uuid.New()
	for _, sample := range []struct {
		name, path    string
		authenticated bool
		status        int
		failure       error
	}{
		{"未认证", "/" + image.String() + "?page=1", false, 401, nil},
		{"非法UUID", "/invalid", true, 400, nil},
		{"非法分页", "/" + image.String() + "?page=0", true, 400, nil},
		{"图片不可访问", "/" + image.String(), true, 404, fault.NotFound()},
		{"成功最小摘要", "/" + image.String() + "?page=2&page_size=20", true, 200, nil},
	} {
		t.Run(sample.name, func(t *testing.T) {
			repository := &inspirationProjectsRepository{failure: sample.failure}
			router := gin.New()
			if sample.authenticated {
				router.Use(func(c *gin.Context) {
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), claimsKey{}, store.TokenClaims{UserBusinessID: user, SessionID: "test"}))
				})
			}
			router.GET("/:business_id", inspirationProjectsHandler(service.NewProjectService(repository, nil, 1)))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", sample.path, nil))
			if response.Code != sample.status {
				t.Fatalf("状态=%d 正文=%s", response.Code, response.Body.String())
			}
			if sample.status == 200 {
				var body struct {
					Data struct {
						Items []map[string]any `json:"items"`
						Page  int              `json:"page"`
						Total int              `json:"total"`
					}
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Data.Page != 2 || body.Data.Total != 25 || len(body.Data.Items) != 1 || len(body.Data.Items[0]) != 3 {
					t.Fatal("分页或最小摘要字段不符")
				}
				for _, key := range []string{"business_id", "title", "updated_at"} {
					if _, ok := body.Data.Items[0][key]; !ok {
						t.Fatalf("缺少字段 %s", key)
					}
				}
				if repository.user != user || repository.image != image || repository.offset != 20 {
					t.Fatal("未使用已认证用户及正确分页")
				}
			} else if sample.status != 404 && repository.calls != 0 {
				t.Fatal("非法请求进入了仓储")
			}
		})
	}
}
