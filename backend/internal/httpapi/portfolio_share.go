package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/photo-nini/backend/internal/service"
	"net/http"
)

// portfolioShareCreateHandler 重置当前用户作品集的分享令牌，明文仅本次返回。
func portfolioShareCreateHandler(shares *service.PortfolioShareService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		token, err := shares.Reset(c.Request.Context(), claims.UserBusinessID, id)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		writeData(c, http.StatusOK, gin.H{"token": token})
	}
}

// portfolioShareRevokeHandler 立即撤销当前用户作品集的分享能力。
func portfolioShareRevokeHandler(shares *service.PortfolioShareService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := shares.Revoke(c.Request.Context(), claims.UserBusinessID, id); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"enabled": false})
	}
}
