package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/photo-nini/backend/internal/service"
	"net/http"
)

// portfolioListHandler 查询当前用户和空间的作品集。
func portfolioListHandler(portfolios *service.PortfolioService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		items, err := portfolios.List(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": items})
	}
}

// portfolioCreateHandler 创建默认不公开分享的作品集。
func portfolioCreateHandler(portfolios *service.PortfolioService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var input service.PortfolioInput
		if err = decodeJSON(c, &input); err != nil {
			writeError(c, err)
			return
		}
		item, err := portfolios.Create(c.Request.Context(), claims.UserBusinessID, input)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, item)
	}
}

// portfolioGetHandler 按业务 UUID 查询当前用户的作品集。
func portfolioGetHandler(portfolios *service.PortfolioService) gin.HandlerFunc {
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
		item, err := portfolios.Get(c.Request.Context(), claims.UserBusinessID, id)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// portfolioUpdateHandler 更新作品集及作者自己的策划关联。
func portfolioUpdateHandler(portfolios *service.PortfolioService) gin.HandlerFunc {
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
		var input service.PortfolioEditInput
		if err = decodeJSON(c, &input); err != nil {
			writeError(c, err)
			return
		}
		item, err := portfolios.Update(c.Request.Context(), claims.UserBusinessID, id, input)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// portfolioProjectsHandler 只向作者返回作品集关联的策划业务 UUID。
func portfolioProjectsHandler(portfolios *service.PortfolioService) gin.HandlerFunc {
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
		ids, err := portfolios.Projects(c.Request.Context(), claims.UserBusinessID, id)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": ids})
	}
}
