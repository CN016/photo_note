package httpapi

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/store"
	"net/http/httptest"
	"testing"
)

type noticeOnlyValidator struct{}

func (noticeOnlyValidator) ValidateAccess(context.Context, string) (store.TokenClaims, error) {
	return store.TokenClaims{UserBusinessID: uuid.New(), SessionID: "notice", OrgID: -1, SessionVersion: 1, Purpose: "notification", ParentSessionID: "web", ParentVersion: 1}, nil
}

// TestNotificationTokenRouteBoundary 验证通知令牌无法借由普通业务或可选鉴权接口升级权限。
func TestNotificationTokenRouteBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, optional := range []bool{false, true} {
		for _, tc := range []struct {
			method, path string
			allowed      bool
		}{
			{"GET", "/api/v1/notifications", true}, {"GET", "/api/v1/notifications/unread-count", true}, {"POST", "/api/v1/auth/logout", true},
			{"POST", "/api/v1/auth/notification-session", false}, {"GET", "/api/v1/users/me", false}, {"POST", "/api/v1/files", false}, {"POST", "/api/v1/feedback", false}, {"PATCH", "/api/v1/notifications/id/read", false},
		} {
			r := gin.New()
			if optional {
				r.Use(optionalAuthenticationMiddleware(noticeOnlyValidator{}))
			} else {
				r.Use(authenticationMiddleware(noticeOnlyValidator{}))
			}
			r.Handle(tc.method, tc.path, func(c *gin.Context) { c.Status(204) })
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer fixture")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			expected := 403
			if tc.allowed {
				expected = 204
			}
			if w.Code != expected {
				t.Fatalf("%s %s optional=%v: %d", tc.method, tc.path, optional, w.Code)
			}
		}
	}
}
