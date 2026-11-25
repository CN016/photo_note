package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
)

// validateProjectShooting 允许未完成的草稿；已填写的时刻必须有效，完整区间遵守既有档期上限。
func validateProjectShooting(s *model.ProjectShooting) error {
	s.Location, s.Person = strings.TrimSpace(s.Location), strings.TrimSpace(s.Person)
	if utf8.RuneCountInString(s.Location) > 240 || utf8.RuneCountInString(s.Person) > 120 {
		return fault.InvalidField("shooting", "地点最多 240 个字符，人物称呼最多 120 个字符")
	}
	if s.Kind != "" && s.Kind != "photo" && s.Kind != "video" {
		return fault.InvalidField("shooting.kind", "拍摄类型只能为照片或视频")
	}
	for _, v := range []*time.Time{s.StartAt, s.EndAt} {
		if v != nil && (v.IsZero() || v.Year() < 1970 || v.Year() > 9999) {
			return fault.InvalidField("shooting", "拍摄时间无效")
		}
	}
	if s.StartAt != nil && s.EndAt != nil && (!s.StartAt.Before(*s.EndAt) || s.EndAt.Sub(*s.StartAt) > 24*time.Hour) {
		return fault.InvalidField("shooting.end_at", "结束时间必须晚于开始时间且单次时长不超过 24 小时")
	}
	return nil
}

// ProjectListRepository 按持久化状态筛选，计数覆盖完整搜索结果，避免浏览器只对一页分组。
type ProjectListRepository interface {
	ListProjectsFiltered(context.Context, uuid.UUID, string, string, int, int) ([]model.Project, int64, model.ProjectStatusCounts, error)
}

// ProjectListView 扩展原有分页形状，保留旧客户端依赖的字段。
type ProjectListView struct {
	Items        []ProjectSummary          `json:"items"`
	Page         int                       `json:"page"`
	PageSize     int                       `json:"page_size"`
	Total        int64                     `json:"total"`
	StatusCounts model.ProjectStatusCounts `json:"status_counts"`
}

// ListFiltered 只接受已知状态，搜索规范化与原列表一致。
func (s *ProjectService) ListFiltered(ctx context.Context, user uuid.UUID, query, status string, page PageRequest) (ProjectListView, error) {
	query = normalizeProjectText(query)
	if utf8.RuneCountInString(query) > 120 {
		return ProjectListView{}, fault.InvalidRequest("搜索内容最多 120 个字符")
	}
	if status != "" && status != "unfinished" && status != "executed" && status != "completed" {
		return ProjectListView{}, fault.InvalidRequest("策划状态无效")
	}
	repo, ok := s.repository.(ProjectListRepository)
	if !ok {
		return ProjectListView{}, fault.Unavailable(nil)
	}
	items, total, counts, err := repo.ListProjectsFiltered(ctx, user, query, status, page.Offset, page.PageSize)
	if err != nil {
		return ProjectListView{}, err
	}
	view := ProjectListView{Items: make([]ProjectSummary, 0, len(items)), Page: page.Page, PageSize: page.PageSize, Total: total, StatusCounts: counts}
	for _, item := range items {
		view.Items = append(view.Items, projectSummary(item))
	}
	return view, nil
}

// projectScheduleState 仅报告可操作状态；实际错误不会破坏已保存的策划内容。
func projectScheduleState(item model.Project) string {
	if item.Shooting.Complete() && item.ScheduleFingerprint != item.Shooting.Fingerprint() && (item.ScheduleID == nil || item.ScheduleOwned) {
		return "pending"
	}
	if item.ScheduleID != nil {
		return "linked"
	}
	return ""
}
