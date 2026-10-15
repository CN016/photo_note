package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLedgerRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := Router(Dependencies{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ledger/records?from=2026-10-01&to=2026-10-31", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("ledger GET status = %d; want %d", response.Code, http.StatusUnauthorized)
	}
}
