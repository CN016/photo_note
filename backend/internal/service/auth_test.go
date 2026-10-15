package service

import "testing"

// TestPhoneNormalization 验证新手机号保存格式及登录阶段唯一保留的旧格式兼容规则。
func TestPhoneNormalization(t *testing.T) {
	t.Run("registration accepts eleven digits", func(t *testing.T) {
		got, err := normalizeRegistrationPhone("13800138000")
		if err != nil || got != "13800138000" {
			t.Fatalf("normalizeRegistrationPhone() = %q, %v", got, err)
		}
	})
	t.Run("registration rejects legacy prefix", func(t *testing.T) {
		if _, err := normalizeRegistrationPhone("+8613800138000"); err == nil {
			t.Fatal("expected E.164 registration input to be rejected")
		}
	})
	t.Run("login maps legacy prefix to stored digits", func(t *testing.T) {
		got, err := normalizeLoginPhone("+8613800138000")
		if err != nil || got != "13800138000" {
			t.Fatalf("normalizeLoginPhone() = %q, %v", got, err)
		}
	})
	t.Run("login rejects other country prefixes", func(t *testing.T) {
		if _, err := normalizeLoginPhone("+14155550134"); err == nil {
			t.Fatal("expected non-+86 international input to be rejected")
		}
	})
}
