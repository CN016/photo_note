package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/service"
)

// listPlansHandler 只返回访问令牌所属用户的计划和当前提醒设置。
func listPlansHandler(plans *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		items, err := plans.List(c.Request.Context(), claims.UserBusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": items})
	}
}

// createPlanHandler 校验创建字段并返回新计划业务 UUID，不暴露数据库主键。
func createPlanHandler(plans *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var fields service.PlanFields
		if err := decodeJSON(c, &fields); err != nil {
			writeError(c, err)
			return
		}
		item, err := plans.Create(c.Request.Context(), claims.UserBusinessID, fields)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, item)
	}
}

// getPlanHandler 查询单条计划时仍执行用户范围校验，供通知跳转和编辑页使用。
func getPlanHandler(plans *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		planID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		item, err := plans.Get(c.Request.Context(), claims.UserBusinessID, planID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// patchPlanHandler 保留 PATCH 字段存在性，防止空的提醒数组或封面字段被误当成未提供。
func patchPlanHandler(plans *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		planID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		fields, err := decodeObject(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := allowFields(fields, "title", "description", "starts_at", "cover_emoji", "cover_image_key", "reminder_offsets"); err != nil {
			writeError(c, err)
			return
		}
		patch := service.PlanPatch{}
		if err := decodePlanPatchField(fields, "title", &patch.Title); err != nil {
			writeError(c, err)
			return
		}
		if err := decodePlanPatchField(fields, "description", &patch.Description); err != nil {
			writeError(c, err)
			return
		}
		if raw, ok := fields["starts_at"]; ok {
			var value time.Time
			if err := decodePlanValue(raw, &value); err != nil {
				writeError(c, fault.InvalidField("starts_at", "计划时间必须包含时区并使用 RFC3339 格式"))
				return
			}
			patch.StartsAt = &value
		}
		if err := decodePlanPatchField(fields, "cover_emoji", &patch.CoverEmoji); err != nil {
			writeError(c, err)
			return
		}
		if err := decodePlanPatchField(fields, "cover_image_key", &patch.CoverImageKey); err != nil {
			writeError(c, err)
			return
		}
		if raw, ok := fields["reminder_offsets"]; ok {
			var values []int
			if err := decodePlanValue(raw, &values); err != nil {
				writeError(c, fault.InvalidField("reminder_offsets", "提醒时间必须为整数数组"))
				return
			}
			patch.ReminderOffsets = &values
		}
		item, err := plans.Update(c.Request.Context(), claims.UserBusinessID, planID, patch)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// decodePlanPatchField 解析可空值时保留指针语义；null 会被拒绝而不是静默清空字段。
func decodePlanPatchField[T any](fields map[string]json.RawMessage, name string, destination **T) error {
	raw, exists := fields[name]
	if !exists {
		return nil
	}
	var value T
	if err := decodePlanValue(raw, &value); err != nil {
		return fault.InvalidField(name, "字段格式无效且不能为 null")
	}
	*destination = &value
	return nil
}

// decodePlanValue 只解析单个 PATCH 字段 JSON 值，拒绝 null 和畸形类型。
func decodePlanValue(raw json.RawMessage, destination any) error {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return fault.InvalidRequest("字段不能为 null")
	}
	return json.Unmarshal(raw, destination)
}

// completePlanHandler 标记计划已完成，服务端会同时取消尚未发送的提醒。
func completePlanHandler(plans *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		planID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		item, err := plans.Complete(c.Request.Context(), claims.UserBusinessID, planID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// deletePlanHandler 软删除当前用户的计划，并取消其未发送提醒。
func deletePlanHandler(plans *service.PlanService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		planID, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := plans.Delete(c.Request.Context(), claims.UserBusinessID, planID); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}
