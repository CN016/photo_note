package tenant

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestScopeDoesNotInheritSystemPrivileges 验证领取任务后的范围收窄不会保留系统扫描权限。
func TestScopeDoesNotInheritSystemPrivileges(t *testing.T) {
	ctx := System(context.Background())
	if !IsSystem(ctx) {
		t.Fatal("系统上下文未生效")
	}
	ctx = WithScope(ctx, Scope{OrgID: 42, UserID: uuid.New()})
	if IsSystem(ctx) {
		t.Fatal("私有上下文继承了系统权限")
	}
	if _, err := Require(ctx); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []Scope{{OrgID: 0, UserID: uuid.New()}, {OrgID: -2, UserID: uuid.New()}, {OrgID: -1}} {
		if _, err := Require(WithScope(context.Background(), scope)); err == nil {
			t.Fatalf("无效身份被放行: %+v", scope)
		}
	}
}
