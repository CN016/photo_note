package model

import (
	"time"

	"github.com/google/uuid"
)

// Organization 保存组织资料；OrgID 是序列生成的独立业务编号，不使用数据库内部 ID 建立关系。
type Organization struct {
	BaseModel
	Name   string `gorm:"column:name;type:varchar(120);not null"`
	Status string `gorm:"column:organization_status;type:varchar(16);not null;default:'active'"`
}

// TableName 将组织实体固定映射到版本化迁移定义的表。
func (Organization) TableName() string { return "organizations" }

// OrganizationMember 将一个全局账号关联到组织；移除仅改变成员状态，不迁移或删除其既有资源。
type OrganizationMember struct {
	BaseModel
	UserID    uuid.UUID  `gorm:"column:user_id;type:uuid;not null"`
	Role      string     `gorm:"column:member_role;type:varchar(16);not null;default:'member'"`
	Status    string     `gorm:"column:member_status;type:varchar(16);not null;default:'active'"`
	RemovedAt *time.Time `gorm:"column:removed_at;type:timestamptz"`
}

// TableName 将成员实体固定映射到版本化迁移定义的表。
func (OrganizationMember) TableName() string { return "organization_members" }

// OrganizationMembership 为环境列表返回最小组织资料和当前成员角色，不包含任何资源数据。
type OrganizationMembership struct {
	OrgID      int64     `gorm:"column:orgid"`
	BusinessID uuid.UUID `gorm:"column:business_id"`
	Name       string    `gorm:"column:name"`
	Role       string    `gorm:"column:member_role"`
	Status     string    `gorm:"column:organization_status"`
}

// OrganizationMemberView 是组织管理的最小成员投影，账号业务 UUID 作为唯一操作标识。
type OrganizationMemberView struct {
	BusinessID uuid.UUID `gorm:"column:business_id"`
	OrgID      int64     `gorm:"column:orgid"`
	UserID     uuid.UUID `gorm:"column:user_id"`
	Nickname   string    `gorm:"column:nickname"`
	Phone      string    `gorm:"column:phone"`
	Role       string    `gorm:"column:member_role"`
	Status     string    `gorm:"column:member_status"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// FileOwnership 仅记录私有文件的租户与创建者授权，不提供文件列表、删除或存储元数据管理。
// UserID 为空仅允许用于旧数据中无法可靠推断创建者的文件；这类记录不能被普通用户认领。
type FileOwnership struct {
	BaseModel
	StoreKey string     `gorm:"column:storekey;type:text;not null"`
	UserID   *uuid.UUID `gorm:"column:user_id;type:uuid"`
}

// TableName 将文件授权实体固定映射到版本化迁移定义的表。
func (FileOwnership) TableName() string { return "file_ownerships" }
