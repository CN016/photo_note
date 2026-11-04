package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/service"
)

// syncSavedProjectSchedule 在正文已经成功保存后处理本地档期联动。
// 候选需用户确认；同步失败只返回可重试状态，不撤销保存，也不要求编辑页保持打开。
func syncSavedProjectSchedule(ctx context.Context, user uuid.UUID, view service.ProjectView, workflow *service.ProjectWorkflowService) service.ProjectView {
	if workflow == nil || !view.ScheduleSyncNeeded {
		return view
	}
	link, err := workflow.SyncSchedule(ctx, user, view.BusinessID, service.ProjectScheduleInput{Revision: view.Revision})
	if err != nil {
		view.ScheduleSyncState = "failed"
		var appError *fault.Error
		if errors.As(err, &appError) && appError.Code == "project_schedule_candidates" {
			view.ScheduleSyncState = "candidates"
		}
		return view
	}
	view.ScheduleID, view.ScheduleOwned, view.ScheduleSyncState = link.ScheduleID, link.Owned, "linked"
	return view
}

// workflowHandler 统一建立鉴权身份和业务 UUID；业务服务始终使用请求绑定的原始租户上下文。
func workflowHandler(withID bool, status int, action func(context.Context, uuid.UUID, uuid.UUID, *gin.Context) (any, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var id uuid.UUID
		if withID {
			id, err = parseBusinessID(c)
			if err != nil {
				writeError(c, err)
				return
			}
		}
		result, err := action(c.Request.Context(), claims.UserBusinessID, id, c)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, status, result)
	}
}

// registerProjectWorkflow 注册策划联动与独立总结接口，保留原有策划 CRUD 兼容入口。
func registerProjectWorkflow(routes *gin.RouterGroup, s *service.ProjectWorkflowService) {
	if s == nil {
		return
	}
	routes.GET("/projects/:business_id/schedule/candidates", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		items, err := s.ScheduleCandidates(ctx, user, id)
		return gin.H{"items": items}, err
	}))
	routes.POST("/projects/:business_id/schedule", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		var input service.ProjectScheduleInput
		if err := decodeJSON(c, &input); err != nil {
			return nil, err
		}
		return s.SyncSchedule(ctx, user, id, input)
	}))
	routes.GET("/projects/:business_id/review", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		return s.Review(ctx, user, id)
	}))
	routes.POST("/projects/:business_id/review", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		var input struct {
			Revision int64 `json:"revision"`
		}
		if err := decodeJSON(c, &input); err != nil {
			return nil, err
		}
		return s.CompleteReview(ctx, user, id, input.Revision)
	}))
	routes.GET("/project-reviews", workflowHandler(false, http.StatusOK, func(ctx context.Context, user, _ uuid.UUID, c *gin.Context) (any, error) {
		page, err := parsePagination(c)
		if err != nil {
			return nil, err
		}
		if len(c.QueryArray("q")) > 1 {
			return nil, fault.InvalidRequest("q 查询参数不能重复")
		}
		result, err := s.Reviews(ctx, user, c.Query("q"), page)
		return pageResponse[service.ProjectReviewView]{Items: result.Items, Page: result.Page, PageSize: result.PageSize, Total: result.Total}, err
	}))
	routes.GET("/project-reviews/:business_id", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		return s.GetReview(ctx, user, id)
	}))
	routes.PATCH("/project-reviews/:business_id", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		var input service.ProjectReviewInput
		if err := decodeJSON(c, &input); err != nil {
			return nil, err
		}
		return s.SaveReview(ctx, user, id, input)
	}))
	routes.GET("/projects/:business_id/portfolios", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		items, err := s.Portfolios(ctx, user, id)
		return gin.H{"items": items}, err
	}))
	routes.PUT("/projects/:business_id/portfolios", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		var input struct {
			IDs []uuid.UUID `json:"portfolio_ids"`
		}
		if err := decodeJSON(c, &input); err != nil {
			return nil, err
		}
		if err := s.SetPortfolios(ctx, user, id, input.IDs); err != nil {
			return nil, err
		}
		items, err := s.Portfolios(ctx, user, id)
		return gin.H{"items": items}, err
	}))
	routes.POST("/projects/:business_id/ai-analysis", workflowHandler(true, http.StatusOK, func(ctx context.Context, user, id uuid.UUID, c *gin.Context) (any, error) {
		var input service.ProjectAIInput
		if err := decodeJSON(c, &input); err != nil {
			return nil, err
		}
		return s.GenerateAnalysis(ctx, user, id, input)
	}))
}
