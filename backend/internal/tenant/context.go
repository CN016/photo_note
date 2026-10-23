// Package tenant 定义服务端验证后的空间上下文；客户端传入的组织编号不能直接成为授权依据。
package tenant

import (
	"context"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
)

// GlobalOrgID 表示个人空间或明确的系统表范围，个人业务仍必须同时校验用户 UUID。
const GlobalOrgID int64 = -1

// Scope 是一次请求或后台任务不可变的空间身份，不在业务操作中重新读取当前全局会话。
type Scope struct {
	OrgID     int64
	UserID    uuid.UUID
	SessionID string
	Version   int64
}

type scopeKey struct{}
type systemKey struct{}

// WithScope 写入已验证的空间并移除系统扫描权限，防止后台任务继承跨组织能力。
func WithScope(ctx context.Context, scope Scope) context.Context {
	ctx = context.WithValue(ctx, systemKey{}, false)
	return context.WithValue(ctx, scopeKey{}, scope)
}

// FromContext 只接受个人 -1 或正组织编号；零值和缺失身份均不能访问私有业务。
func FromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	return scope, ok && (scope.OrgID == GlobalOrgID || scope.OrgID > 0) && scope.UserID != uuid.Nil
}

// Require 要求调用方具备有效的租户与用户身份；不得把缺失身份静默解释为个人空间。
func Require(ctx context.Context) (Scope, error) {
	scope, ok := FromContext(ctx)
	if !ok {
		return Scope{}, fault.Unauthorized()
	}
	return scope, nil
}

// System 仅供已鉴权的平台管理和后台队列领取使用；业务 handler 不得从请求参数开启它。
func System(ctx context.Context) context.Context { return context.WithValue(ctx, systemKey{}, true) }

// IsSystem 标记显式的系统级存储调用，不代表普通用户拥有跨组织权限。
func IsSystem(ctx context.Context) bool { value, _ := ctx.Value(systemKey{}).(bool); return value }

// Personal 为账号资料、系统反馈回复及其个人通知建立明确的个人空间上下文。
func Personal(ctx context.Context, userID uuid.UUID) context.Context {
	return WithScope(ctx, Scope{OrgID: GlobalOrgID, UserID: userID})
}
