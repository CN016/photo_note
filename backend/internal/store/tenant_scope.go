package store

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/tenant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// managedTenantTables 覆盖全部自建表；系统表显式固定在 -1 域，其余表没有可信上下文时拒绝访问。
var managedTenantTables = map[string]bool{
	"inspiration_goals": false, "inspiration_activities": false, "inspiration_processing_jobs": false,
	"users": true, "app_configs": true, "feedbacks": true, "feedback_notification_jobs": true,
	"error_logs": false, "tags": false, "inspirations": false, "ledger_records": false,
	"schedule_items": false, "schedule_exceptions": false, "schedule_bookings": false, "schedule_shares": false,
	"notifications": false, "notice_config": false, "notice_deliveries": false, "plans": false,
	"plan_reminders": false, "projects": false, "project_inspiration_usages": false, "project_attachments": false,
	"organizations": false, "organization_members": false, "file_ownerships": false,
	"portfolios": false, "portfolio_photos": false, "portfolio_projects": false,
}

// RegisterTenantCallbacks 为普通 GORM 读写建立不可省略的组织条件；原始 SQL 必须在所在 repository 显式限定。
// 注册是幂等的，数据库初始化和测试直接创建 repository 都会使用相同保护。
func RegisterTenantCallbacks(db *gorm.DB) error {
	if db.Callback().Query().Get("tenant:query") != nil {
		return nil
	}
	registrations := []error{
		db.Callback().Query().Before("gorm:query").Register("tenant:query", enforceTenantQuery),
		db.Callback().Row().Before("gorm:row").Register("tenant:row", enforceTenantQuery),
		db.Callback().Update().Before("gorm:update").Register("tenant:update", enforceTenantUpdate),
		db.Callback().Delete().Before("gorm:delete").Register("tenant:delete", enforceTenantQuery),
		db.Callback().Create().Before("gorm:before_create").Register("tenant:create", enforceTenantCreate),
	}
	for _, err := range registrations {
		if err != nil {
			return fmt.Errorf("注册组织隔离回调失败: %w", err)
		}
	}
	return nil
}

// tenantTable 识别模型、投影及带别名的主表，避免后台 Scan 投影跳过组织限制。
func tenantTable(db *gorm.DB) (string, bool, bool) {
	name := db.Statement.Table
	if db.Statement.TableExpr != nil {
		parts := strings.Fields(db.Statement.TableExpr.SQL)
		if len(parts) > 0 {
			// PostgreSQL 可用 public.table 或 "public"."table" 限定 schema，不能因此绕过表识别。
			qualified := strings.Split(parts[0], ".")
			name = strings.Trim(qualified[len(qualified)-1], "\"")
		}
	}
	if _, exists := managedTenantTables[name]; !exists && db.Statement.Schema != nil {
		name = db.Statement.Schema.Table
	}
	system, managed := managedTenantTables[name]
	return name, system, managed
}

// enforceTenantQuery 固定系统数据域，普通业务操作强制组织条件；组织编号不能通过 Updates 迁移。
func enforceTenantQuery(db *gorm.DB) {
	_, system, managed := tenantTable(db)
	if !managed || db.Error != nil {
		return
	}
	orgID := tenant.GlobalOrgID
	if !system {
		if tenant.IsSystem(db.Statement.Context) {
			return
		}
		scope, err := tenant.Require(db.Statement.Context)
		if err != nil {
			db.AddError(err)
			return
		}
		orgID = scope.OrgID
	}
	db.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Eq{Column: clause.Column{Table: clause.CurrentTable, Name: "orgid"}, Value: orgID}}})
}

// enforceTenantUpdate 将不可变字段校验限定在更新流程，查询和删除目的模型不会被误判为更新赋值。
func enforceTenantUpdate(db *gorm.DB) {
	if _, _, managed := tenantTable(db); managed && tenantOrganizationMutation(db) {
		db.AddError(fault.InvalidRequest("资源所属组织不可修改"))
		return
	}
	enforceTenantQuery(db)
}

// tenantOrganizationMutation 覆盖 map 的列名/Go 字段名和结构体更新，禁止通过不同命名改写组织。
func tenantOrganizationMutation(db *gorm.DB) bool {
	if updates, ok := db.Statement.Dest.(map[string]any); ok {
		for key := range updates {
			if strings.EqualFold(key, "orgid") {
				return true
			}
		}
		return false
	}
	value := reflect.ValueOf(db.Statement.Dest)
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return false
	}
	field := value.FieldByName("OrgID")
	if !field.IsValid() {
		return false
	}
	if !field.IsZero() {
		return true
	}
	for _, selected := range db.Statement.Selects {
		if selected == "*" || strings.EqualFold(selected, "orgid") {
			return true
		}
	}
	return false
}

// enforceTenantCreate 在编码 SQL 前写入可信组织编号，拒绝客户端携带的跨组织值与无上下文创建。
func enforceTenantCreate(db *gorm.DB) {
	_, system, managed := tenantTable(db)
	if !managed || db.Error != nil || db.Statement.Schema == nil {
		return
	}
	field := db.Statement.Schema.LookUpField("OrgID")
	if field == nil {
		db.AddError(fault.InvalidRequest("业务模型缺少组织字段"))
		return
	}
	orgID := tenant.GlobalOrgID
	systemScope := tenant.IsSystem(db.Statement.Context)
	if !system && !systemScope {
		scope, err := tenant.Require(db.Statement.Context)
		if err != nil {
			db.AddError(err)
			return
		}
		orgID = scope.OrgID
	}
	var apply func(reflect.Value)
	apply = func(value reflect.Value) {
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return
			}
			value = value.Elem()
		}
		if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
			for i := 0; i < value.Len(); i++ {
				apply(value.Index(i))
			}
			return
		}
		if value.Kind() != reflect.Struct {
			db.AddError(fault.InvalidRequest("组织数据必须使用实体模型写入"))
			return
		}
		current, _ := field.ValueOf(db.Statement.Context, value)
		id, _ := current.(int64)
		if systemScope && !system {
			if id != -1 && (id < 1 || id > 9007199254740991) {
				db.AddError(fault.InvalidRequest("系统任务缺少有效资源组织"))
				return
			}
			return
		}
		if id != 0 && id != orgID {
			db.AddError(fault.NotFound())
			return
		}
		if err := field.Set(db.Statement.Context, value, orgID); err != nil {
			db.AddError(err)
		}
	}
	apply(db.Statement.ReflectValue)
}

// scopedResourceContext 由已从受控全局队列或公开令牌读到的资源恢复工作上下文；不复制用户会话。
func scopedResourceContext(ctx context.Context, orgID int64, userID uuid.UUID) context.Context {
	return tenant.WithScope(ctx, tenant.Scope{OrgID: orgID, UserID: userID})
}
