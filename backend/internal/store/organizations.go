package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListOrganizationMemberships 仅装载指定全局账号有效的组织环境，不把其他成员或组织资源带入登录响应。
func (store *PostgresStore) ListOrganizationMemberships(ctx context.Context, userID uuid.UUID) ([]model.OrganizationMembership, error) {
	items := make([]model.OrganizationMembership, 0)
	err := store.db.WithContext(tenant.System(ctx)).Table("organization_members AS m").
		Joins("JOIN organizations AS o ON o.orgid = m.orgid").
		Where("m.user_id = ? AND m.member_status = 'active' AND o.organization_status = 'active'", userID).
		Select("o.orgid, o.business_id, o.name, m.member_role, o.organization_status").Order("o.created_at ASC, o.orgid ASC").Scan(&items).Error
	return items, persistenceError(err)
}

// FindOrganization 供登录环境校验按独立租户业务编号读取组织；此精确查询不授予任何管理权限。
func (store *PostgresStore) FindOrganization(ctx context.Context, orgID int64) (model.Organization, error) {
	var item model.Organization
	err := store.db.WithContext(tenant.System(ctx)).Where("orgid = ?", orgID).First(&item).Error
	return item, persistenceError(err)
}

// FindOrganizationMember 供每次认证和角色校验读取最新成员状态，包含 removed 以避免缓存失效延迟。
func (store *PostgresStore) FindOrganizationMember(ctx context.Context, orgID int64, userID uuid.UUID) (model.OrganizationMember, error) {
	var item model.OrganizationMember
	err := store.db.WithContext(tenant.System(ctx)).Where("orgid = ? AND user_id = ?", orgID, userID).First(&item).Error
	return item, persistenceError(err)
}

// ListOrganizations 仅供平台管理员在显式系统上下文中分页选择组织。
func (store *PostgresStore) ListOrganizations(ctx context.Context, search string, offset, limit int) ([]model.Organization, int64, error) {
	if !tenant.IsSystem(ctx) {
		return nil, 0, fault.Unauthorized()
	}
	query := store.db.WithContext(ctx).Model(&model.Organization{})
	if search = strings.TrimSpace(search); search != "" {
		query = query.Where("name ILIKE ? ESCAPE E'\\\\'", escapedContainsPattern(search))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.Organization, 0)
	err := query.Order("created_at DESC, orgid DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, persistenceError(err)
}

// CreateOrganization 原子创建组织和首位管理员，租户编号来自专用序列且不暴露自增主键。
func (store *PostgresStore) CreateOrganization(ctx context.Context, item *model.Organization, initialAdmin uuid.UUID) error {
	if !tenant.IsSystem(ctx) || item == nil || initialAdmin == uuid.Nil {
		return fault.Unauthorized()
	}
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Select("business_id").Where("business_id = ?", initialAdmin).First(&user).Error; err != nil {
			return err
		}
		if err := tx.Raw("SELECT nextval('organization_orgid_seq')").Scan(&item.OrgID).Error; err != nil {
			return err
		}
		if item.BusinessID == uuid.Nil {
			item.BusinessID = uuid.New()
		}
		if item.Status == "" {
			item.Status = "active"
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		member := model.OrganizationMember{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: item.OrgID, CreatedAt: now, UpdatedAt: now}, UserID: initialAdmin, Role: "admin", Status: "active"}
		return tx.Create(&member).Error
	}))
}

// organizationContext 验证调用者选中的组织，平台操作必须由 service 显式传入系统上下文。
func organizationContext(ctx context.Context, orgID int64) (context.Context, error) {
	if orgID < 1 || orgID > 9007199254740991 {
		return nil, fault.NotFound()
	}
	if tenant.IsSystem(ctx) {
		return ctx, nil
	}
	scope, err := tenant.Require(ctx)
	if err != nil {
		return nil, err
	}
	if scope.OrgID != orgID {
		return nil, fault.NotFound()
	}
	return ctx, nil
}

// UpdateOrganization 只接受服务层白名单组织资料，禁止改写租户编号和业务 UUID。
func (store *PostgresStore) UpdateOrganization(ctx context.Context, orgID int64, updates map[string]any) (model.Organization, error) {
	ctx, err := organizationContext(ctx, orgID)
	if err != nil {
		return model.Organization{}, err
	}
	fields := map[string]any{"updated_at": time.Now().UTC()}
	for _, key := range []string{"name", "organization_status"} {
		if value, ok := updates[key]; ok {
			fields[key] = value
		}
	}
	result := store.db.WithContext(ctx).Model(&model.Organization{}).Where("orgid = ?", orgID).Updates(fields)
	if result.Error != nil {
		return model.Organization{}, persistenceError(result.Error)
	}
	if result.RowsAffected == 0 {
		return model.Organization{}, fault.NotFound()
	}
	return store.FindOrganization(ctx, orgID)
}

// ListOrganizationMembers 仅返回选定组织的有效成员资料；账号表始终是 -1 系统域。
func (store *PostgresStore) ListOrganizationMembers(ctx context.Context, orgID int64, offset, limit int) ([]model.OrganizationMemberView, int64, error) {
	ctx, err := organizationContext(ctx, orgID)
	if err != nil {
		return nil, 0, err
	}
	query := store.db.WithContext(ctx).Table("organization_members AS m").
		Joins("JOIN users AS u ON u.business_id = m.user_id AND u.orgid = -1").Where("m.orgid = ? AND m.member_status = 'active'", orgID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, persistenceError(err)
	}
	items := make([]model.OrganizationMemberView, 0)
	err = query.Select("m.business_id, m.orgid, m.user_id, m.member_role, m.member_status, m.created_at, u.nickname, u.phone").Order("m.created_at ASC, m.business_id ASC").Offset(offset).Limit(limit).Scan(&items).Error
	return items, total, persistenceError(err)
}

// UpsertOrganizationMember 复用已存在的全局账号，恢复已移除成员时保留同一成员业务 UUID。
// 所有成员管理都锁定组织行，防止并发降级或移除最后一位有效管理员。
func (store *PostgresStore) UpsertOrganizationMember(ctx context.Context, orgID int64, userID uuid.UUID, role string) error {
	ctx, err := organizationContext(ctx, orgID)
	if err != nil {
		return err
	}
	if role != "admin" && role != "member" {
		return fault.InvalidField("role", "组织角色无效")
	}
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOrganization(tx, orgID); err != nil {
			return err
		}
		var user model.User
		if err := tx.Select("business_id").Where("business_id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		var item model.OrganizationMember
		err := tx.Where("orgid = ? AND user_id = ?", orgID, userID).First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			item = model.OrganizationMember{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: orgID}, UserID: userID, Role: role, Status: "active"}
			return tx.Create(&item).Error
		}
		if err != nil {
			return err
		}
		if item.Status == "active" && item.Role == "admin" && role != "admin" {
			if err := retainOrganizationAdministrator(tx, orgID); err != nil {
				return err
			}
		}
		return tx.Model(&item).Where("orgid = ?", orgID).Updates(map[string]any{"member_role": role, "member_status": "active", "removed_at": nil, "updated_at": time.Now().UTC()}).Error
	}))
}

// RemoveOrganizationMember 移除成员资格而保留组织资源，任何后续请求必须重新校验成员状态。
func (store *PostgresStore) RemoveOrganizationMember(ctx context.Context, orgID int64, userID uuid.UUID) error {
	ctx, err := organizationContext(ctx, orgID)
	if err != nil {
		return err
	}
	return persistenceError(store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOrganization(tx, orgID); err != nil {
			return err
		}
		var item model.OrganizationMember
		if err := tx.Where("orgid = ? AND user_id = ?", orgID, userID).First(&item).Error; err != nil {
			return err
		}
		if item.Status == "removed" {
			return nil
		}
		if item.Role == "admin" {
			if err := retainOrganizationAdministrator(tx, orgID); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		return tx.Model(&item).Where("orgid = ?", orgID).Updates(map[string]any{"member_status": "removed", "removed_at": now, "updated_at": now}).Error
	}))
}

// lockOrganization 将成员管理串行化到目标组织，不阻塞其他组织。
func lockOrganization(tx *gorm.DB, orgID int64) error {
	var item model.Organization
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("orgid = ?", orgID).First(&item).Error
}

// retainOrganizationAdministrator 在组织行锁内保证至少保留一位有效管理员。
func retainOrganizationAdministrator(tx *gorm.DB, orgID int64) error {
	var count int64
	if err := tx.Model(&model.OrganizationMember{}).Where("orgid = ? AND member_status = 'active' AND member_role = 'admin'", orgID).Count(&count).Error; err != nil {
		return err
	}
	if count <= 1 {
		return fault.Conflict("组织至少需要保留一位有效管理员")
	}
	return nil
}

// resourceOrganizationAvailable 在后台任务发布前重新检查组织和成员，移除或停用不能继续外发组织内容。
func resourceOrganizationAvailable(tx *gorm.DB, orgID int64, userID uuid.UUID) (bool, error) {
	if orgID == tenant.GlobalOrgID {
		return true, nil
	}
	var count int64
	err := tx.WithContext(tenant.System(tx.Statement.Context)).Table("organizations AS o").
		Joins("JOIN organization_members AS m ON m.orgid = o.orgid").
		Where("o.orgid = ? AND o.organization_status = 'active' AND m.user_id = ? AND m.member_status = 'active'", orgID, userID).Count(&count).Error
	return count == 1, err
}

// RegisterFileOwnership 只为服务端刚创建的私有 key 登记授权，已有登记不可重新认领或迁移。
func (store *PostgresStore) RegisterFileOwnership(ctx context.Context, key string, userID uuid.UUID) error {
	scope, err := tenant.Require(ctx)
	if err != nil {
		return err
	}
	if key == "" || userID == uuid.Nil || scope.UserID != userID {
		return fault.NotFound()
	}
	item := model.FileOwnership{BaseModel: model.BaseModel{BusinessID: uuid.New(), OrgID: scope.OrgID}, StoreKey: key, UserID: &userID}
	result := store.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "storekey"}}, DoNothing: true}).Create(&item)
	if result.Error != nil {
		return persistenceError(result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	allowed, err := store.CanAccessFile(ctx, key, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return fault.NotFound()
	}
	return nil
}

// CanAccessFile 同时校验当前组织和创建者；缺失登记、冲突隔离的空创建者都不可访问。
func (store *PostgresStore) CanAccessFile(ctx context.Context, key string, userID uuid.UUID) (bool, error) {
	if _, err := tenant.Require(ctx); err != nil {
		return false, err
	}
	var count int64
	err := store.db.WithContext(ctx).Model(&model.FileOwnership{}).Where("storekey = ? AND user_id = ?", key, userID).Count(&count).Error
	return count == 1, persistenceError(err)
}

// FindConsoleInspiration 只在已经授权且单选的组织范围内取得图库原创建者，供复用原有编辑逻辑。
func (store *PostgresStore) FindConsoleInspiration(ctx context.Context, inspirationID uuid.UUID) (model.Inspiration, error) {
	if tenant.IsSystem(ctx) {
		return model.Inspiration{}, fault.InvalidRequest("图库管理必须选择一个组织")
	}
	if _, err := tenant.Require(ctx); err != nil {
		return model.Inspiration{}, err
	}
	var item model.Inspiration
	err := store.db.WithContext(ctx).Where("business_id = ?", inspirationID).First(&item).Error
	return item, persistenceError(err)
}
