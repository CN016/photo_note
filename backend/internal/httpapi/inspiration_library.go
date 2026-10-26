package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/service"
)

// registerInspirationLibraryRoutes 只注册在鉴权组，资源归属从已校验会话取得，不接受请求体指定用户或空间。
func registerInspirationLibraryRoutes(group *gin.RouterGroup, library *service.InspirationLibraryService) {
	if library == nil {
		return
	}
	group.GET("/inspirations/daily-goal", func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		data, err := library.GoalStatus(c.Request.Context(), claims.UserBusinessID, 0)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, data)
	})
	group.PUT("/inspirations/daily-goal", func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var input struct {
			Enabled      bool   `json:"enabled"`
			Target       int    `json:"target"`
			ReminderTime string `json:"reminder_time"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, fault.InvalidRequest("每日目标格式无效"))
			return
		}
		if err := library.SaveGoal(c.Request.Context(), claims.UserBusinessID, input.Enabled, input.Target, input.ReminderTime); err != nil {
			writeError(c, err)
			return
		}
		data, err := library.GoalStatus(c.Request.Context(), claims.UserBusinessID, 0)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, data)
	})
	group.GET("/inspirations/activity", func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		year, err := strconv.Atoi(c.DefaultQuery("year", strconv.Itoa(time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Year())))
		if err != nil || year < 1970 || year > 9999 {
			writeError(c, fault.InvalidRequest("年份无效"))
			return
		}
		data, err := library.GoalStatus(c.Request.Context(), claims.UserBusinessID, year)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, data)
	})
	group.POST("/inspirations/daily-goal/reminder/ack", func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var input struct {
			Day string `json:"day"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, fault.InvalidRequest("提醒日期无效"))
			return
		}
		claimed, err := library.Acknowledge(c.Request.Context(), claims.UserBusinessID, input.Day)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"acknowledged": claimed})
	})
	group.GET("/inspirations/browse-summary", func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		data, err := library.BrowseSummary(c.Request.Context(), claims.UserBusinessID, c.Query("date_from"), c.Query("date_to"))
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, data)
	})
	group.POST("/inspirations/:business_id/processing/retry", func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := uuid.Parse(c.Param("business_id"))
		if err != nil {
			writeError(c, fault.InvalidRequest("灵感图标识无效"))
			return
		}
		if err := library.Retry(c.Request.Context(), claims.UserBusinessID, id); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusAccepted, gin.H{"queued": true})
	})
}
