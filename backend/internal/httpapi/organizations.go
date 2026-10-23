package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
	"github.com/photo-nini/backend/internal/tenant"
)

type organizationRoleKey struct{}

// tenantScopeResolver 将已验证的会话映射为当前有效的租户范围，不从请求字段推断权限。
type tenantScopeResolver interface {
	ResolveScope(context.Context, uuid.UUID, int64, string, int64) (tenant.Scope, error)
}

// tenantScopeMiddleware 在每个已登录请求中核验账号、组织、成员状态；匿名公开请求不建立租户上下文。
func tenantScopeMiddleware(resolver tenantScopeResolver, organizations *service.OrganizationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := c.Request.Context().Value(claimsKey{}).(store.TokenClaims)
		if !ok {
			c.Next()
			return
		}
		if resolver == nil {
			writeError(c, fault.Unavailable(errors.New("组织鉴权服务未配置")))
			c.Abort()
			return
		}
		scope, err := resolver.ResolveScope(c.Request.Context(), claims.UserBusinessID, claims.OrgID, claims.SessionID, claims.SessionVersion)
		if err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		ctx := tenant.WithScope(c.Request.Context(), scope)
		role := "personal"
		if scope.OrgID > 0 {
			role = "member"
		}
		if organizations != nil {
			role, err = organizations.CurrentRole(ctx)
			if err != nil {
				writeError(c, err)
				c.Abort()
				return
			}
		}
		c.Request = c.Request.WithContext(context.WithValue(ctx, organizationRoleKey{}, role))
		c.Next()
	}
}

// personalAccountScopeMiddleware 将系统反馈归入当前账号个人域，不要求来源组织仍有效。
// 仍验证账号和会话，错误Bearer不会降级为游客，也不会授予其他个人资源权限。
func personalAccountScopeMiddleware(resolver tenantScopeResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := c.Request.Context().Value(claimsKey{}).(store.TokenClaims)
		if !ok {
			c.Next()
			return
		}
		if resolver == nil {
			writeError(c, fault.Unavailable(nil))
			c.Abort()
			return
		}
		scope, err := resolver.ResolveScope(c.Request.Context(), claims.UserBusinessID, tenant.GlobalOrgID, claims.SessionID, claims.SessionVersion)
		if err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(tenant.WithScope(c.Request.Context(), scope))
		c.Next()
	}
}

// organizationAdministratorMiddleware 只授权当前组织管理员，平台管理员不能隐式获得组织角色。
func organizationAdministratorMiddleware(organizations *service.OrganizationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if organizations == nil {
			writeError(c, fault.Unavailable(errors.New("组织服务未配置")))
			c.Abort()
			return
		}
		if err := organizations.RequireOrganizationAdmin(c.Request.Context()); err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

// selectedGalleryOrganizationMiddleware 平台图库必须显式选择单个数据域；不允许省略、多个编号或全站模式。
func selectedGalleryOrganizationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		values, ok := c.GetQueryArray("orgid")
		if !ok || len(values) != 1 {
			writeError(c, fault.InvalidField("orgid", "必须选择一个组织数据域"))
			c.Abort()
			return
		}
		orgID, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || !service.ValidOrgID(orgID) {
			writeError(c, fault.InvalidField("orgid", "组织编号无效"))
			c.Abort()
			return
		}
		scope, err := tenant.Require(c.Request.Context())
		if err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		scope.OrgID = orgID
		c.Request = c.Request.WithContext(tenant.WithScope(c.Request.Context(), scope))
		c.Next()
	}
}

// environmentsHandler 返回当前账号可选择的个人和组织空间，不返回其他账号的数据。
func environmentsHandler(organizations *service.OrganizationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if organizations == nil {
			writeError(c, fault.Unavailable(nil))
			return
		}
		items, err := organizations.ListEnvironments(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, gin.H{"items": items})
	}
}

// switchEnvironmentHandler 用刷新凭证实现原子切换与幂等恢复，避免依赖已经被切换撤销的access token。
func switchEnvironmentHandler(auth *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request struct {
			RefreshToken    string `json:"refresh_token"`
			OrgID           int64  `json:"orgid"`
			ExpectedVersion int64  `json:"expected_session_version"`
			SwitchID        string `json:"switch_id"`
		}
		if auth == nil {
			writeError(c, fault.Unavailable(nil))
			return
		}
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		pair, err := auth.SwitchEnvironment(c.Request.Context(), request.RefreshToken, request.OrgID, request.ExpectedVersion, request.SwitchID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, gin.H{"tokens": pair})
	}
}

// organizationResponse 显式返回业务编号与UUID，数据库内部自增主键不参与接口。
type organizationResponse struct {
	OrgID      int64     `json:"orgid"`
	BusinessID uuid.UUID `json:"business_id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func organizationDTO(item model.Organization) organizationResponse {
	return organizationResponse{OrgID: item.OrgID, BusinessID: item.BusinessID, Name: item.Name, Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

// organizationTarget 当前组织管理从令牌取orgid，平台管理仅接受明确的路径编号。
func organizationTarget(c *gin.Context, platform bool) (int64, error) {
	if !platform {
		scope, err := tenant.Require(c.Request.Context())
		if err != nil {
			return 0, err
		}
		return scope.OrgID, nil
	}
	id, err := strconv.ParseInt(c.Param("orgid"), 10, 64)
	if err != nil || id <= 0 || !service.ValidOrgID(id) {
		return 0, fault.InvalidField("orgid", "组织编号无效")
	}
	return id, nil
}
func organizationsHandler(organizations *service.OrganizationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if organizations == nil {
			writeError(c, fault.Unavailable(nil))
			return
		}
		page, err := parsePagination(c)
		if err != nil {
			writeError(c, err)
			return
		}
		q, err := optionalQueryString(c, "q")
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := organizations.ListOrganizations(c.Request.Context(), q, page)
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]organizationResponse, 0, len(result.Items))
		for _, item := range result.Items {
			items = append(items, organizationDTO(item))
		}
		writeData(c, 200, pageResponse[organizationResponse]{Items: items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
	}
}
func createOrganizationHandler(organizations *service.OrganizationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request struct {
			Name         string    `json:"name"`
			InitialAdmin uuid.UUID `json:"initial_admin_user_id"`
		}
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		item, err := organizations.CreateOrganization(c.Request.Context(), request.Name, request.InitialAdmin)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, organizationDTO(item))
	}
}
func getOrganizationHandler(organizations *service.OrganizationService, platform bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := organizationTarget(c, platform)
		if err != nil {
			writeError(c, err)
			return
		}
		item, err := organizations.GetOrganization(c.Request.Context(), id, platform)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, organizationDTO(item))
	}
}
func updateOrganizationHandler(organizations *service.OrganizationService, platform bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := organizationTarget(c, platform)
		if err != nil {
			writeError(c, err)
			return
		}
		var request struct {
			Name   *string `json:"name"`
			Status *string `json:"status"`
		}
		if err = decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		item, err := organizations.UpdateOrganization(c.Request.Context(), id, request.Name, request.Status, platform)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, organizationDTO(item))
	}
}

type organizationMemberResponse struct {
	BusinessID uuid.UUID `json:"business_id"`
	OrgID      int64     `json:"orgid"`
	UserID     uuid.UUID `json:"user_id"`
	Nickname   string    `json:"nickname"`
	Phone      string    `json:"phone"`
	Role       string    `json:"role"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func organizationMembersHandler(organizations *service.OrganizationService, platform bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := organizationTarget(c, platform)
		if err != nil {
			writeError(c, err)
			return
		}
		page, err := parsePagination(c)
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := organizations.ListMembers(c.Request.Context(), id, page, platform)
		if err != nil {
			writeError(c, err)
			return
		}
		items := make([]organizationMemberResponse, 0, len(result.Items))
		for _, m := range result.Items {
			items = append(items, organizationMemberResponse{BusinessID: m.BusinessID, OrgID: m.OrgID, UserID: m.UserID, Nickname: m.Nickname, Phone: m.Phone, Role: m.Role, Status: m.Status, CreatedAt: m.CreatedAt})
		}
		writeData(c, 200, pageResponse[organizationMemberResponse]{Items: items, Page: result.Page, PageSize: result.PageSize, Total: result.Total})
	}
}
func setOrganizationMemberHandler(organizations *service.OrganizationService, platform bool, updating bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := organizationTarget(c, platform)
		if err != nil {
			writeError(c, err)
			return
		}
		var userID uuid.UUID
		var role string
		if updating {
			userID, err = uuid.Parse(c.Param("user_id"))
			if err != nil {
				writeError(c, fault.InvalidField("user_id", "用户业务UUID无效"))
				return
			}
			var request struct {
				Role string `json:"role"`
			}
			if err = decodeJSON(c, &request); err != nil {
				writeError(c, err)
				return
			}
			role = request.Role
		} else {
			var request struct {
				UserID uuid.UUID `json:"user_id"`
				Role   string    `json:"role"`
			}
			if err = decodeJSON(c, &request); err != nil {
				writeError(c, err)
				return
			}
			userID = request.UserID
			role = request.Role
		}
		if err = organizations.SetMember(c.Request.Context(), id, userID, role, platform); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, gin.H{"user_id": userID, "role": role, "status": "active"})
	}
}
func removeOrganizationMemberHandler(organizations *service.OrganizationService, platform bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := organizationTarget(c, platform)
		if err != nil {
			writeError(c, err)
			return
		}
		userID, err := uuid.Parse(c.Param("user_id"))
		if err != nil {
			writeError(c, fault.InvalidField("user_id", "用户业务UUID无效"))
			return
		}
		if err = organizations.RemoveMember(c.Request.Context(), id, userID, platform); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// currentAvatarHandler 只读取当前账号登记的个人头像，组织会话不会获得一般个人文件访问能力。
func currentAvatarHandler(users *service.UserService, files fileStorage) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if users == nil || files == nil {
			writeError(c, fault.Unavailable(nil))
			return
		}
		user, err := users.GetCurrent(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		if user.AvatarKey == nil {
			writeError(c, fault.NotFound())
			return
		}
		reader, size, _, err := service.OpenFile(tenant.Personal(c.Request.Context(), claims.UserBusinessID), files, *user.AvatarKey)
		if err != nil {
			writeError(c, fault.NotFound())
			return
		}
		defer reader.Close()
		prefix, err := io.ReadAll(io.LimitReader(reader, 512))
		if err != nil || len(prefix) == 0 {
			writeError(c, fault.NotFound())
			return
		}
		kind := http.DetectContentType(prefix)
		if !isPublicFeedbackImageType(kind) {
			writeError(c, fault.NotFound())
			return
		}
		c.DataFromReader(200, size, kind, io.MultiReader(bytes.NewReader(prefix), reader), map[string]string{"Cache-Control": "private, no-store", "X-Content-Type-Options": "nosniff", "Content-Disposition": "inline; filename=avatar", "Content-Security-Policy": "sandbox", "Referrer-Policy": "no-referrer"})
	}
}

// personalAvatarUploadHandler 为账号资料建立明确个人作用域；普通文件上传继续留在当前组织。
func personalAvatarUploadHandler(files fileStorage, maximum int64, currentType func(context.Context) (string, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Request = c.Request.WithContext(tenant.Personal(c.Request.Context(), claims.UserBusinessID))
		uploadProjectCoverHandler(files, maximum, currentType)(c)
	}
}
