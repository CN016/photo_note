package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/service"
)

// scheduleItemResponse 暴露档期业务字段，不返回数据库主键和用户内部 UUID。
type scheduleItemResponse struct {
	BusinessID    uuid.UUID `json:"business_id"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	StartAt       time.Time `json:"start_at"`
	EndAt         time.Time `json:"end_at"`
	Recurrence    string    `json:"recurrence"`
	Weekdays      []int64   `json:"weekdays"`
	RecurrenceEnd *string   `json:"recurrence_end"`
	Capacity      int       `json:"capacity"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// scheduleItemDTO 将数据库重复规则模型转成无自增键的管理接口响应。
func scheduleItemDTO(item model.ScheduleItem) scheduleItemResponse {
	weekdays := append([]int64{}, item.RecurrenceWeekdays...)
	var recurrenceEnd *string
	if item.RecurrenceEnd != nil {
		value := item.RecurrenceEnd.Format("2006-01-02")
		recurrenceEnd = &value
	}
	return scheduleItemResponse{BusinessID: item.BusinessID, Kind: item.Kind, Title: item.Title, Content: item.Content, StartAt: item.StartAt, EndAt: item.EndAt, Recurrence: item.Recurrence, Weekdays: weekdays, RecurrenceEnd: recurrenceEnd, Capacity: item.Capacity, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

// listScheduleItemsHandler 只查询 token 所属用户的日历发生项，并强制限定最多 90 天。
func listScheduleItemsHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		from, to, err := scheduleDateRange(c.Query("from"), c.Query("to"))
		if err != nil {
			writeError(c, err)
			return
		}
		items, err := schedule.List(c.Request.Context(), claims.UserBusinessID, from, to)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": items, "from": from.Format("2006-01-02"), "to": to.AddDate(0, 0, -1).Format("2006-01-02")})
	}
}

// createScheduleItemHandler 从已认证令牌取得拥有者并创建档期。
func createScheduleItemHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var fields service.ScheduleFields
		if err := decodeJSON(c, &fields); err != nil {
			writeError(c, err)
			return
		}
		item, err := schedule.Create(c.Request.Context(), claims.UserBusinessID, fields)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, scheduleItemDTO(item))
	}
}

// updateScheduleItemHandler 以用户 UUID 和目标档期 UUID 双重限定整条系列更新。
func updateScheduleItemHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		itemID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var fields service.ScheduleFields
		if err := decodeJSON(c, &fields); err != nil {
			writeError(c, err)
			return
		}
		item, err := schedule.Update(c.Request.Context(), claims.UserBusinessID, itemID, fields)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, scheduleItemDTO(item))
	}
}

// deleteScheduleItemHandler 删除整个系列；预约历史保留用于拥有者查看。
func deleteScheduleItemHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		itemID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := schedule.Delete(c.Request.Context(), claims.UserBusinessID, itemID); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// updateScheduleOccurrenceHandler 将指定重复日期改为一次覆盖，不影响系列的其他发生项。
func updateScheduleOccurrenceHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		itemID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		date, err := time.ParseInLocation("2006-01-02", c.Param("date"), time.FixedZone("Asia/Shanghai", 8*60*60))
		if err != nil {
			writeError(c, fault.InvalidRequest("发生日期必须是 YYYY-MM-DD"))
			return
		}
		var fields service.ScheduleFields
		if err := decodeJSON(c, &fields); err != nil {
			writeError(c, err)
			return
		}
		if err := schedule.SetOccurrence(c.Request.Context(), claims.UserBusinessID, itemID, date, &fields); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"updated": true})
	}
}

// cancelScheduleOccurrenceHandler 仅取消指定日期的发生项，并保留整条重复系列。
func cancelScheduleOccurrenceHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		itemID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		date, err := time.ParseInLocation("2006-01-02", c.Param("date"), time.FixedZone("Asia/Shanghai", 8*60*60))
		if err != nil {
			writeError(c, fault.InvalidRequest("发生日期必须是 YYYY-MM-DD"))
			return
		}
		if err := schedule.SetOccurrence(c.Request.Context(), claims.UserBusinessID, itemID, date, nil); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"cancelled": true})
	}
}

// publicScheduleSlotsHandler 验证随机分享令牌后仅返回开放时段和实时可用名额。
func publicScheduleSlotsHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		from, to, err := scheduleDateRange(c.Query("from"), c.Query("to"))
		if err != nil {
			writeError(c, err)
			return
		}
		items, err := schedule.PublicSlots(c.Request.Context(), c.Param("share_token"), from, to)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": items})
	}
}

// scheduleBookingHandler 接收游客申请并从 Gin 可信代理设置取得客户端 IP。
func scheduleBookingHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request struct {
			ScheduleID uuid.UUID `json:"schedule_id"`
			StartAt    time.Time `json:"start_at"`
			Name       string    `json:"name"`
			Phone      string    `json:"phone"`
			Note       string    `json:"note"`
		}
		if err := decodeJSON(c, &request); err != nil {
			writeError(c, err)
			return
		}
		booking, err := schedule.Request(c.Request.Context(), c.ClientIP(), c.Param("share_token"), request.ScheduleID, request.StartAt, request.Name, request.Phone, request.Note)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, gin.H{"request_code": booking.RequestCode, "status": booking.Status, "expires_at": booking.ExpiresAt})
	}
}

// listScheduleBookingsHandler 返回当前用户的申请队列及筛选状态。
func listScheduleBookingsHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		items, err := schedule.Bookings(c.Request.Context(), claims.UserBusinessID, c.DefaultQuery("status", "pending"))
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": items})
	}
}

// decideScheduleBookingHandler 确认或拒绝一项仍有效的待确认申请。
func decideScheduleBookingHandler(schedule *service.ScheduleService, decision string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		bookingID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := schedule.Decide(c.Request.Context(), claims.UserBusinessID, bookingID, decision); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"status": decision})
	}
}

// cancelScheduleBookingHandler 由拥有者取消待确认或已确认申请，释放开放时段名额。
func cancelScheduleBookingHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		bookingID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := schedule.Cancel(c.Request.Context(), claims.UserBusinessID, bookingID); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"status": "cancelled"})
	}
}

// getScheduleShareHandler 查询分享链接是否启用，不泄露已摘要的令牌。
func getScheduleShareHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		share, err := schedule.ShareStatus(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, share)
	}
}

// createScheduleShareHandler 生成新的随机预约地址并撤销之前创建的链接。
func createScheduleShareHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		share, err := schedule.CreateShare(c.Request.Context(), claims.UserBusinessID, "")
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, share)
	}
}

// revokeScheduleShareHandler 停用当前用户公开预约地址。
func revokeScheduleShareHandler(schedule *service.ScheduleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := schedule.RevokeShare(c.Request.Context(), claims.UserBusinessID); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"enabled": false})
	}
}

// scheduleDateRange 解析包含首尾日期的中国标准时间查询范围并转换成左闭右开区间。
func scheduleDateRange(fromValue, toValue string) (time.Time, time.Time, error) {
	if fromValue == "" || toValue == "" {
		return time.Time{}, time.Time{}, fault.InvalidRequest("必须提供 from 和 to 日期")
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	from, fromErr := time.ParseInLocation("2006-01-02", fromValue, location)
	to, toErr := time.ParseInLocation("2006-01-02", toValue, location)
	if fromErr != nil || toErr != nil || to.Before(from) || to.Sub(from) >= 90*24*time.Hour {
		return time.Time{}, time.Time{}, fault.InvalidRequest("日期范围必须是 YYYY-MM-DD 且不超过 90 天")
	}
	return from, to.AddDate(0, 0, 1), nil
}
