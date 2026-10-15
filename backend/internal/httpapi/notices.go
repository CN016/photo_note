package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/service"
)

// listNotificationsHandler 按认证用户返回站内消息和关联 webhook 投递状态。
func listNotificationsHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		page, pageSize := 1, 20
		if raw := c.Query("page"); raw != "" {
			page, err = strconv.Atoi(raw)
			if err != nil {
				writeError(c, fault.InvalidRequest("page 必须是整数"))
				return
			}
		}
		if raw := c.Query("page_size"); raw != "" {
			pageSize, err = strconv.Atoi(raw)
			if err != nil {
				writeError(c, fault.InvalidRequest("page_size 必须是整数"))
				return
			}
		}
		unreadOnly := false
		if raw := c.Query("unread_only"); raw != "" {
			unreadOnly, err = strconv.ParseBool(raw)
			if err != nil {
				writeError(c, fault.InvalidRequest("unread_only 必须是布尔值"))
				return
			}
		}
		result, err := notices.ListNotifications(c.Request.Context(), claims.UserBusinessID, unreadOnly, page, pageSize)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, result)
	}
}

// unreadNotificationCountHandler 返回未读角标数，不加载消息正文。
func unreadNotificationCountHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		count, err := notices.UnreadCount(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"unread_count": count})
	}
}

// markNotificationReadHandler 将单条本人消息标记为已读。
func markNotificationReadHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseNoticeID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := notices.MarkRead(c.Request.Context(), claims.UserBusinessID, id); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"read": true})
	}
}

// markAllNotificationsReadHandler 将当前用户所有未读消息标记为已读。
func markAllNotificationsReadHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		count, err := notices.MarkAllRead(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"updated": count})
	}
}

// listNoticeConfigsHandler 只返回掩码地址、配置状态和动态总数上限。
func listNoticeConfigsHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		result, err := notices.ListNoticeConfigs(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, result)
	}
}

// createNoticeConfigHandler 校验 JSON 配置并创建用户自己的 webhook 渠道。
func createNoticeConfigHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var input service.NoticeConfigInput
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, err)
			return
		}
		config, err := notices.CreateNoticeConfig(c.Request.Context(), claims.UserBusinessID, input)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, config)
	}
}

// updateNoticeConfigHandler 只接受当前用户配置的局部字段更新。
func updateNoticeConfigHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseNoticeID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var patch service.NoticeConfigPatch
		if err := decodeJSON(c, &patch); err != nil {
			writeError(c, err)
			return
		}
		config, err := notices.UpdateNoticeConfig(c.Request.Context(), claims.UserBusinessID, id, patch)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, config)
	}
}

// deleteNoticeConfigHandler 停止本人配置接收后续消息并软删除配置。
func deleteNoticeConfigHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseNoticeID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := notices.DeleteNoticeConfig(c.Request.Context(), claims.UserBusinessID, id); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// testNoticeConfigHandler 把测试投递保存为 outbox 并立即返回 202。
func testNoticeConfigHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseNoticeID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		delivery, err := notices.TestNoticeConfig(c.Request.Context(), claims.UserBusinessID, id)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusAccepted, delivery)
	}
}

// getNoticeDeliveryHandler 返回测试或业务通知 webhook 的异步状态。
func getNoticeDeliveryHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseNoticeID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		delivery, err := notices.GetNoticeDelivery(c.Request.Context(), claims.UserBusinessID, id)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, delivery)
	}
}

// retryNoticeDeliveryHandler 将本人最终失败的渠道消息重新放回异步 outbox。
func retryNoticeDeliveryHandler(notices *service.NoticeService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseNoticeID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		delivery, err := notices.RetryNoticeDelivery(c.Request.Context(), claims.UserBusinessID, id)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusAccepted, delivery)
	}
}

// parseNoticeID 将路由中的 business_id 解析为 UUID；不接受数据库自增 ID。
func parseNoticeID(c *gin.Context) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.Param("business_id"))
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fault.InvalidField("business_id", "必须是有效的业务 UUID")
	}
	return id, nil
}
