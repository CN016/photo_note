package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
)

// organizationRepositoryHTTP 按业务UUID模拟组织及实时成员角色，不模拟客户端传入的授权标记。
type organizationRepositoryHTTP struct {
	user          model.User
	organization  model.Organization
	member        model.OrganizationMember
	lastMemberOrg int64
	writes        int
}

func (r *organizationRepositoryHTTP) FindUserByBusinessID(context.Context, uuid.UUID) (model.User, error) {
	return r.user, nil
}
func (r *organizationRepositoryHTTP) IsAdministrator(context.Context, uuid.UUID) (bool, error) {
	return r.user.IsAdmin, nil
}
func (r *organizationRepositoryHTTP) FindOrganization(_ context.Context, id int64) (model.Organization, error) {
	if id != r.organization.OrgID {
		return model.Organization{}, fault.NotFound()
	}
	return r.organization, nil
}
func (r *organizationRepositoryHTTP) FindOrganizationMember(_ context.Context, id int64, userID uuid.UUID) (model.OrganizationMember, error) {
	if id != r.organization.OrgID || userID != r.user.BusinessID {
		return model.OrganizationMember{}, fault.NotFound()
	}
	return r.member, nil
}
func (r *organizationRepositoryHTTP) ListOrganizationMemberships(context.Context, uuid.UUID) ([]model.OrganizationMembership, error) {
	return []model.OrganizationMembership{{OrgID: r.organization.OrgID, BusinessID: r.organization.BusinessID, Name: r.organization.Name, Role: r.member.Role, Status: r.organization.Status}}, nil
}
func (r *organizationRepositoryHTTP) ListOrganizations(context.Context, string, int, int) ([]model.Organization, int64, error) {
	return []model.Organization{r.organization}, 1, nil
}
func (r *organizationRepositoryHTTP) CreateOrganization(_ context.Context, item *model.Organization, _ uuid.UUID) error {
	item.OrgID = 88
	r.writes++
	return nil
}
func (r *organizationRepositoryHTTP) UpdateOrganization(_ context.Context, _ int64, updates map[string]any) (model.Organization, error) {
	r.writes++
	if name, ok := updates["name"].(string); ok {
		r.organization.Name = name
	}
	return r.organization, nil
}
func (r *organizationRepositoryHTTP) ListOrganizationMembers(ctx context.Context, id int64, _ int, _ int) ([]model.OrganizationMemberView, int64, error) {
	scope, _ := tenant.FromContext(ctx)
	r.lastMemberOrg = scope.OrgID
	return []model.OrganizationMemberView{{OrgID: id, UserID: r.user.BusinessID, Role: r.member.Role, Status: r.member.Status}}, 1, nil
}
func (r *organizationRepositoryHTTP) UpsertOrganizationMember(context.Context, int64, uuid.UUID, string) error {
	r.writes++
	return nil
}
func (r *organizationRepositoryHTTP) RemoveOrganizationMember(context.Context, int64, uuid.UUID) error {
	r.writes++
	return nil
}

type organizationTokensHTTP struct {
	userID  uuid.UUID
	orgID   int64
	version int64
}

func (t organizationTokensHTTP) ValidateAccess(_ context.Context, raw string) (store.TokenClaims, error) {
	if raw != "valid" {
		return store.TokenClaims{}, fault.Unauthorized()
	}
	return store.TokenClaims{UserBusinessID: t.userID, OrgID: t.orgID, SessionID: "session", SessionVersion: t.version}, nil
}
func organizationHTTPFixture(role string, platform bool) (*organizationRepositoryHTTP, *consoleRepositoryForHTTP, Dependencies) {
	id := uuid.New()
	repo := &organizationRepositoryHTTP{user: model.User{BaseModel: model.BaseModel{BusinessID: id, OrgID: -1}, IsAdmin: platform}, organization: model.Organization{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: 17}, Name: "甲组织", Status: "active"}, member: model.OrganizationMember{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: 17}, UserID: id, Role: role, Status: "active"}}
	console := &consoleRepositoryForHTTP{admin: platform}
	return repo, console, Dependencies{Organizations: service.NewOrganizationService(repo), Console: service.NewConsoleService(console), Tokens: organizationTokensHTTP{userID: id, orgID: 17, version: 1}}
}
func organizationHTTPCall(deps Dependencies, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	Router(deps).ServeHTTP(response, request)
	return response
}

// TestOrganizationRoleAndActiveMembershipAreReadEveryRequest 角色撤销、移除成员及停用组织立即影响后续请求。
func TestOrganizationRoleAndActiveMembershipAreReadEveryRequest(t *testing.T) {
	repo, _, deps := organizationHTTPFixture("member", false)
	if r := organizationHTTPCall(deps, "GET", "/api/v1/organization/members", ""); r.Code != 403 {
		t.Fatalf("普通成员访问管理接口:%d", r.Code)
	}
	repo.member.Role = "admin"
	if r := organizationHTTPCall(deps, "GET", "/api/v1/organization/members?orgid=18", ""); r.Code != 200 || repo.lastMemberOrg != 17 {
		t.Fatalf("查询参数覆盖当前组织:%d org=%d", r.Code, repo.lastMemberOrg)
	}
	repo.member.Role = "member"
	if r := organizationHTTPCall(deps, "GET", "/api/v1/organization/members", ""); r.Code != 403 {
		t.Fatal("撤销组织管理员未即时生效")
	}
	repo.member.Role = "admin"
	repo.member.Status = "removed"
	if r := organizationHTTPCall(deps, "GET", "/api/v1/organization/members", ""); r.Code != 403 || !strings.Contains(r.Body.String(), "environment_unavailable") {
		t.Fatal("已移除成员仍可访问")
	}
	repo.member.Status = "active"
	repo.organization.Status = "disabled"
	if r := organizationHTTPCall(deps, "GET", "/api/v1/organization/members", ""); r.Code != 403 {
		t.Fatal("已停用组织仍可访问")
	}
}

// TestPlatformGalleryRequiresOneExplicitOrganization 平台图库没有全站默认值，并且只有平台权限可以选择其他数据域。
func TestPlatformGalleryRequiresOneExplicitOrganization(t *testing.T) {
	_, console, deps := organizationHTTPFixture("member", true)
	for _, query := range []string{"", "?orgid=0", "?orgid=17&orgid=18", "?orgid=all"} {
		if r := organizationHTTPCall(deps, "GET", "/api/v1/atelier-7f3c9a2e/inspirations"+query, ""); r.Code != 422 {
			t.Fatalf("错误查询%s 状态=%d", query, r.Code)
		}
	}
	if r := organizationHTTPCall(deps, "GET", "/api/v1/atelier-7f3c9a2e/inspirations?orgid=18", ""); r.Code != 200 || console.galleryOrg != 18 {
		t.Fatalf("没有绑定显式组织:%d org=%d", r.Code, console.galleryOrg)
	}
	if r := organizationHTTPCall(deps, "GET", "/api/v1/atelier-7f3c9a2e/inspirations?orgid=-1", ""); r.Code != 200 || console.galleryOrg != -1 {
		t.Fatal("平台无法明确选择个人数据域")
	}
}

// TestOrganizationAdminCannotManagePlatformFeedbackOrStatus 组织角色不得触达全局配置、反馈或停用组织的权限。
func TestOrganizationAdminCannotManagePlatformFeedbackOrStatus(t *testing.T) {
	repo, _, deps := organizationHTTPFixture("admin", false)
	for _, path := range []string{"/api/v1/atelier-7f3c9a2e/feedbacks", "/api/v1/atelier-7f3c9a2e/configs", "/api/v1/atelier-7f3c9a2e/organizations"} {
		if r := organizationHTTPCall(deps, "GET", path, ""); r.Code != 403 {
			t.Fatalf("组织管理员获得平台权限:%s %d", path, r.Code)
		}
	}
	if r := organizationHTTPCall(deps, "PATCH", "/api/v1/organization", `{"status":"disabled"}`); r.Code != 403 || repo.writes != 0 {
		t.Fatal("组织管理员能够变更组织启停状态")
	}
	if r := organizationHTTPCall(deps, "PATCH", "/api/v1/organization", `{"name":"新的名称"}`); r.Code != 200 || repo.writes != 1 {
		t.Fatalf("组织名称更新失败:%d", r.Code)
	}
	if r := organizationHTTPCall(deps, "GET", "/api/v1/organization/feedbacks", ""); r.Code != http.StatusNotFound {
		t.Fatal("意外提供组织反馈管理入口")
	}
}

// TestTenantMiddlewareRejectsLegacyClaims 升级前的缺失空间声明不能在HTTP层被补成个人权限。
func TestTenantMiddlewareRejectsLegacyClaims(t *testing.T) {
	repo, _, deps := organizationHTTPFixture("admin", true)
	deps.Tokens = organizationTokensHTTP{userID: repo.user.BusinessID}
	if r := organizationHTTPCall(deps, "GET", "/api/v1/environments", ""); r.Code != 401 {
		t.Fatalf("旧token状态=%d", r.Code)
	}
}

// TestSystemFeedbackSurvivesOrganizationDeactivation 反馈是账号/系统能力，来源组织失效不会降为匿名或阻止提交。
func TestSystemFeedbackSurvivesOrganizationDeactivation(t *testing.T) {
	repo, _, deps := organizationHTTPFixture("member", false)
	repo.organization.Status = "disabled"
	repo.member.Status = "removed"
	feedbacks := &testFeedbackRepository{}
	deps.Feedback = service.NewFeedbackService(feedbacks, &testFeedbackLimiter{}, 2000, 5, 5)
	deps.Files = newTestFileStorage()
	deps.MaxUploadBytes = 1 << 20
	deps.CurrentFileStoreType = func(context.Context) (string, error) { return "local", nil }
	response := feedbackRequest(t, Router(deps), "组织停用后的个人反馈", 0, "valid")
	if response.Code != 201 || len(feedbacks.records) != 1 || feedbacks.records[0].UserID == nil || *feedbacks.records[0].UserID != repo.user.BusinessID {
		t.Fatalf("系统反馈未保留账号身份:%d", response.Code)
	}
}

type avatarOwnershipUsers struct {
	*testUserRepository
	allowed     bool
	observedOrg int64
}

func (users *avatarOwnershipUsers) CanAccessFile(ctx context.Context, _ string, _ uuid.UUID) (bool, error) {
	scope, _ := tenant.FromContext(ctx)
	users.observedOrg = scope.OrgID
	return users.allowed, nil
}

// TestAvatarRejectsActiveContentAndCrossOrganizationReferences 账号头像不能内联执行HTML，也不能引用组织或其他用户的私有文件。
func TestAvatarRejectsActiveContentAndCrossOrganizationReferences(t *testing.T) {
	repo, _, deps := organizationHTTPFixture("member", false)
	files := newTestFileStorage()
	key, err := files.Store(context.Background(), "local", "unsafe.html", strings.NewReader("<html><script>alert(1)</script></html>"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	base := newTestUserRepository()
	user := repo.user
	user.AvatarKey = &key
	base.users["13800138000"] = user
	users := &avatarOwnershipUsers{testUserRepository: base, allowed: false}
	deps.Users = service.NewUserService(users)
	deps.Files = files
	deps.MaxUploadBytes = 1 << 20
	deps.CurrentFileStoreType = func(context.Context) (string, error) { return "local", nil }
	if r := organizationHTTPCall(deps, "GET", "/api/v1/users/me/avatar", ""); r.Code != 404 {
		t.Fatalf("头像内联了非图片:%d", r.Code)
	}
	if r := organizationHTTPCall(deps, "PATCH", "/api/v1/users/me", `{"avatar_key":"`+key+`"}`); r.Code != 404 || users.observedOrg != -1 {
		t.Fatalf("头像引用未按个人归属拒绝:%d org=%d", r.Code, users.observedOrg)
	}
	files.files[key] = pngBytes(t)
	if r := organizationHTTPCall(deps, "GET", "/api/v1/users/me/avatar", ""); r.Code != 200 || r.Header().Get("Content-Security-Policy") != "sandbox" || r.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("安全位图头像读取失败:%d", r.Code)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "unsafe.html")
	_, _ = part.Write([]byte("<html>unsafe</html>"))
	_ = writer.Close()
	request := httptest.NewRequest("POST", "/api/v1/users/me/avatar", &body)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	Router(deps).ServeHTTP(response, request)
	if response.Code != 400 || len(files.files) != 1 {
		t.Fatalf("头像上传接收HTML或留下文件:%d", response.Code)
	}
}
