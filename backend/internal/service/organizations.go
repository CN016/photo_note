package service

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// OrganizationRepository 集中组织和成员的持久化；关联均使用业务标识，不创建数据库外键。
type OrganizationRepository interface {
	FindUserByBusinessID(context.Context, uuid.UUID) (model.User, error)
	IsAdministrator(context.Context, uuid.UUID) (bool, error)
	ListOrganizationMemberships(context.Context, uuid.UUID) ([]model.OrganizationMembership, error)
	FindOrganization(context.Context, int64) (model.Organization, error)
	FindOrganizationMember(context.Context, int64, uuid.UUID) (model.OrganizationMember, error)
	ListOrganizations(context.Context, string, int, int) ([]model.Organization, int64, error)
	CreateOrganization(context.Context, *model.Organization, uuid.UUID) error
	UpdateOrganization(context.Context, int64, map[string]any) (model.Organization, error)
	ListOrganizationMembers(context.Context, int64, int, int) ([]model.OrganizationMemberView, int64, error)
	UpsertOrganizationMember(context.Context, int64, uuid.UUID, string) error
	RemoveOrganizationMember(context.Context, int64, uuid.UUID) error
}

// OrganizationService 验证账号、组织状态和角色；平台权限与组织管理权限始终分别检查。
type OrganizationService struct{ repository OrganizationRepository }

func NewOrganizationService(repository OrganizationRepository) *OrganizationService {
	return &OrganizationService{repository: repository}
}

// Environment 是可选择的数据空间；个人空间没有组织实体，名称由客户端按语言显示。
type Environment struct {
	OrgID      int64      `json:"orgid"`
	BusinessID *uuid.UUID `json:"business_id"`
	Name       string     `json:"name"`
	Role       string     `json:"role"`
	Status     string     `json:"status"`
}

// ResolveScope 每次请求重新验证组织和成员状态；失败不会自动降级到个人空间。
func (s *OrganizationService) ResolveScope(ctx context.Context, userID uuid.UUID, orgID int64, sessionID string, version int64) (tenant.Scope, error) {
	if userID == uuid.Nil || version < 1 || sessionID == "" || !ValidOrgID(orgID) {
		return tenant.Scope{}, fault.Unauthorized()
	}
	if _, err := s.repository.FindUserByBusinessID(tenant.System(ctx), userID); err != nil {
		return tenant.Scope{}, err
	}
	if err := s.ValidateEnvironment(ctx, userID, orgID); err != nil {
		return tenant.Scope{}, err
	}
	return tenant.Scope{OrgID: orgID, UserID: userID, SessionID: sessionID, Version: version}, nil
}

// ValidOrgID 保留 -1 作为个人/系统域，组织业务编号使用 JavaScript 可准确表达的正整数。
func ValidOrgID(orgID int64) bool {
	return orgID == tenant.GlobalOrgID || (orgID > 0 && orgID <= 9007199254740991)
}

// ValidateEnvironment 在切换和刷新前核验目标成员关系，拒绝已移除或已停用组织。
func (s *OrganizationService) ValidateEnvironment(ctx context.Context, userID uuid.UUID, orgID int64) error {
	if !ValidOrgID(orgID) {
		return fault.InvalidField("orgid", "组织编号无效")
	}
	if orgID == tenant.GlobalOrgID {
		return nil
	}
	org, err := s.repository.FindOrganization(tenant.System(ctx), orgID)
	if err != nil {
		if fault.Is(err, 404, "not_found") {
			return environmentUnavailable()
		}
		return err
	}
	member, err := s.repository.FindOrganizationMember(tenant.System(ctx), orgID, userID)
	if err != nil {
		if fault.Is(err, 404, "not_found") {
			return environmentUnavailable()
		}
		return err
	}
	if org.Status != "active" || member.Status != "active" {
		return environmentUnavailable()
	}
	return nil
}
func environmentUnavailable() error {
	return fault.New(http.StatusForbidden, "environment_unavailable", "当前组织已停用或成员资格已失效", nil, nil)
}

// ListEnvironments 保留个人空间，同时列出当前账号成员关系；停用组织可显示但不能进入。
func (s *OrganizationService) ListEnvironments(ctx context.Context) ([]Environment, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	memberships, err := s.repository.ListOrganizationMemberships(tenant.System(ctx), scope.UserID)
	if err != nil {
		return nil, err
	}
	out := []Environment{{OrgID: tenant.GlobalOrgID, Role: "personal", Status: "active"}}
	for _, m := range memberships {
		id := m.BusinessID
		out = append(out, Environment{OrgID: m.OrgID, BusinessID: &id, Name: m.Name, Role: m.Role, Status: m.Status})
	}
	return out, nil
}

// CurrentRole 读取当前组织角色；个人空间绝不继承平台管理员为组织管理员。
func (s *OrganizationService) CurrentRole(ctx context.Context) (string, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return "", err
	}
	if scope.OrgID == tenant.GlobalOrgID {
		return "personal", nil
	}
	m, err := s.repository.FindOrganizationMember(tenant.System(ctx), scope.OrgID, scope.UserID)
	if err != nil {
		return "", err
	}
	if m.Status != "active" {
		return "", environmentUnavailable()
	}
	return m.Role, nil
}

// RequirePlatform 单独读取平台角色；客户端组织角色或组织参数不能提升该权限。
func (s *OrganizationService) RequirePlatform(ctx context.Context) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	ok, err := s.repository.IsAdministrator(tenant.System(ctx), scope.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return fault.New(403, "forbidden", "需要平台管理员权限", nil, nil)
	}
	return nil
}

// RequireOrganizationAdmin 只允许当前有效组织的管理员管理资料、成员和图库。
func (s *OrganizationService) RequireOrganizationAdmin(ctx context.Context) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if scope.OrgID <= 0 {
		return fault.New(403, "forbidden", "请进入具有管理权限的组织", nil, nil)
	}
	if err := s.ValidateEnvironment(ctx, scope.UserID, scope.OrgID); err != nil {
		return err
	}
	role, err := s.CurrentRole(ctx)
	if err != nil {
		return err
	}
	if role != "admin" {
		return fault.New(403, "forbidden", "需要当前组织管理员权限", nil, nil)
	}
	return nil
}

// organizationContext 对管理入口显式授权后选择一个组织；不存在跨组织自动回退。
func (s *OrganizationService) organizationContext(ctx context.Context, orgID int64, platform bool) (context.Context, error) {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	if orgID <= 0 || !ValidOrgID(orgID) {
		return nil, fault.InvalidField("orgid", "请选择有效组织")
	}
	if platform {
		if err = s.RequirePlatform(ctx); err != nil {
			return nil, err
		}
	} else {
		if orgID != scope.OrgID {
			return nil, fault.NotFound()
		}
		if err = s.RequireOrganizationAdmin(ctx); err != nil {
			return nil, err
		}
	}
	if _, err = s.repository.FindOrganization(tenant.System(ctx), orgID); err != nil {
		return nil, err
	}
	scope.OrgID = orgID
	return tenant.WithScope(ctx, scope), nil
}

// ListOrganizations 仅供平台列出组织，支持后端分页与名称检索。
func (s *OrganizationService) ListOrganizations(ctx context.Context, q string, page PageRequest) (Page[model.Organization], error) {
	if err := s.RequirePlatform(ctx); err != nil {
		return Page[model.Organization]{}, err
	}
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > 120 {
		return Page[model.Organization]{}, fault.InvalidField("q", "检索词不能超过120个字符")
	}
	items, total, err := s.repository.ListOrganizations(tenant.System(ctx), q, page.Offset, page.PageSize)
	return Page[model.Organization]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, err
}
func organizationName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 120 {
		return "", fault.InvalidField("name", "组织名称长度必须为1至120个字符")
	}
	return name, nil
}

// CreateOrganization 同一事务创建组织与首位管理员；服务器生成组织业务编号。
func (s *OrganizationService) CreateOrganization(ctx context.Context, name string, adminID uuid.UUID) (model.Organization, error) {
	if err := s.RequirePlatform(ctx); err != nil {
		return model.Organization{}, err
	}
	name, err := organizationName(name)
	if err != nil {
		return model.Organization{}, err
	}
	if adminID == uuid.Nil {
		return model.Organization{}, fault.InvalidField("initial_admin_user_id", "必须指定首位管理员")
	}
	if _, err = s.repository.FindUserByBusinessID(tenant.System(ctx), adminID); err != nil {
		return model.Organization{}, err
	}
	item := model.Organization{BaseModel: model.BaseModel{BusinessID: uuid.New()}, Name: name, Status: "active"}
	if err = s.repository.CreateOrganization(tenant.System(ctx), &item, adminID); err != nil {
		return model.Organization{}, err
	}
	return item, nil
}

// GetOrganization 返回管理者有权查看的单个组织资料。
func (s *OrganizationService) GetOrganization(ctx context.Context, orgID int64, platform bool) (model.Organization, error) {
	ctx, err := s.organizationContext(ctx, orgID, platform)
	if err != nil {
		return model.Organization{}, err
	}
	return s.repository.FindOrganization(tenant.System(ctx), orgID)
}

// UpdateOrganization 组织管理员只能改名称，平台管理员另可启停；停用不删除数据和成员关系。
func (s *OrganizationService) UpdateOrganization(ctx context.Context, orgID int64, name, status *string, platform bool) (model.Organization, error) {
	ctx, err := s.organizationContext(ctx, orgID, platform)
	if err != nil {
		return model.Organization{}, err
	}
	updates := map[string]any{}
	if name != nil {
		v, e := organizationName(*name)
		if e != nil {
			return model.Organization{}, e
		}
		updates["name"] = v
	}
	if status != nil {
		if !platform {
			return model.Organization{}, fault.New(403, "forbidden", "组织管理员不能变更组织启停状态", nil, nil)
		}
		if *status != "active" && *status != "disabled" {
			return model.Organization{}, fault.InvalidField("status", "组织状态仅支持active或disabled")
		}
		updates["organization_status"] = *status
	}
	if len(updates) == 0 {
		return model.Organization{}, fault.InvalidRequest("至少提供一个可更新字段")
	}
	return s.repository.UpdateOrganization(ctx, orgID, updates)
}

// ListMembers 只返回选定组织的成员；手机号仅用于明确身份的管理场景。
func (s *OrganizationService) ListMembers(ctx context.Context, orgID int64, page PageRequest, platform bool) (Page[model.OrganizationMemberView], error) {
	ctx, err := s.organizationContext(ctx, orgID, platform)
	if err != nil {
		return Page[model.OrganizationMemberView]{}, err
	}
	items, total, err := s.repository.ListOrganizationMembers(ctx, orgID, page.Offset, page.PageSize)
	return Page[model.OrganizationMemberView]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, err
}

// SetMember 按用户业务 UUID 添加或恢复成员；修改角色时数据库保护最后一位有效管理员。
func (s *OrganizationService) SetMember(ctx context.Context, orgID int64, userID uuid.UUID, role string, platform bool) error {
	ctx, err := s.organizationContext(ctx, orgID, platform)
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fault.InvalidField("user_id", "用户业务UUID无效")
	}
	if role != "admin" && role != "member" {
		return fault.InvalidField("role", "成员角色仅支持admin或member")
	}
	if _, err = s.repository.FindUserByBusinessID(tenant.System(ctx), userID); err != nil {
		return err
	}
	return s.repository.UpsertOrganizationMember(ctx, orgID, userID, role)
}

// RemoveMember 只撤销成员访问资格；既有资源保留原orgid和创建者，不搬迁到个人空间。
func (s *OrganizationService) RemoveMember(ctx context.Context, orgID int64, userID uuid.UUID, platform bool) error {
	ctx, err := s.organizationContext(ctx, orgID, platform)
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		return fault.InvalidField("user_id", "用户业务UUID无效")
	}
	return s.repository.RemoveOrganizationMember(ctx, orgID, userID)
}
