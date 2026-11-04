package service

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
)

// ProjectWorkflowRepository 复用现有策划、档期及作品集存储，事务性副作用由 repository 保证。
type ProjectWorkflowRepository interface {
	FindProject(context.Context, uuid.UUID, uuid.UUID) (model.Project, error)
	ListScheduleData(context.Context, uuid.UUID) ([]model.ScheduleItem, []model.ScheduleException, error)
	SyncProjectSchedule(context.Context, uuid.UUID, uuid.UUID, int64, *uuid.UUID, bool, model.ScheduleItem, func(model.ScheduleItem, []model.ScheduleItem, []model.ScheduleException, bool) error) (model.Project, error)
	FindProjectReview(context.Context, uuid.UUID, uuid.UUID) (*model.ProjectReview, error)
	CreateProjectReview(context.Context, uuid.UUID, uuid.UUID, int64, string) (model.ProjectReview, error)
	ListProjectReviews(context.Context, uuid.UUID, string, int, int) ([]model.ProjectReview, int64, error)
	GetProjectReview(context.Context, uuid.UUID, uuid.UUID) (model.ProjectReview, error)
	UpdateProjectReview(context.Context, uuid.UUID, uuid.UUID, int64, string, string) (model.ProjectReview, error)
	ListProjectPortfolios(context.Context, uuid.UUID, uuid.UUID) ([]model.Portfolio, error)
	SetProjectPortfolios(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID) error
}

// ProjectWorkflowService 协调策划外部关联；业务副作用不与编辑器自动保存混为一次 HTTP 请求。
type ProjectWorkflowService struct {
	repository ProjectWorkflowRepository
	settings   projectAISettingsReader
	aiClient   projectAIClient
	aiMu       sync.Mutex
	aiActive   int
	aiProjects map[string]bool
}

// NewProjectWorkflowService 装配共享配置及受限的文本 AI 客户端；并发生成上限保护主服务。
func NewProjectWorkflowService(repository ProjectWorkflowRepository, settings projectAISettingsReader) *ProjectWorkflowService {
	return &ProjectWorkflowService{repository: repository, settings: settings, aiClient: newProjectAIClient(), aiProjects: make(map[string]bool)}
}

// ProjectScheduleInput 指定当前策划修订及用户确认的已有档期，空 ID 表示在无候选时新建。
type ProjectScheduleInput struct {
	Revision   int64      `json:"revision"`
	ScheduleID *uuid.UUID `json:"schedule_id"`
	Detach     bool       `json:"detach"`
	// CreateNew 表示用户确认候选均不对应本次拍摄，允许另建工作事项。
	CreateNew bool `json:"create_new"`
}

// ProjectScheduleLink 只返回关联状态，前端不应拿它覆盖请求期间的正文或拍摄草稿。
type ProjectScheduleLink struct {
	ScheduleID *uuid.UUID `json:"schedule_id"`
	Owned      bool       `json:"owned"`
}

// ScheduleCandidates 以真实发生项时间重叠提供候选，不根据人物名称推测账号或私有资源归属。
func (s *ProjectWorkflowService) ScheduleCandidates(ctx context.Context, user, project uuid.UUID) ([]ScheduleOccurrence, error) {
	source, err := s.repository.FindProject(ctx, user, project)
	if err != nil {
		return nil, err
	}
	if !source.Shooting.Complete() {
		return []ScheduleOccurrence{}, nil
	}
	items, exceptions, err := s.repository.ListScheduleData(ctx, user)
	if err != nil {
		return nil, err
	}
	return projectScheduleCandidates(source.Shooting, items, exceptions), nil
}

// projectScheduleCandidates 复用重复与例外展开逻辑，候选仅包含与拍摄实际区间重叠的工作事项。
func projectScheduleCandidates(shoot model.ProjectShooting, items []model.ScheduleItem, exceptions []model.ScheduleException) []ScheduleOccurrence {
	result := make([]ScheduleOccurrence, 0)
	if !shoot.Complete() {
		return result
	}
	for _, item := range expandSchedules(items, exceptions, localDate(*shoot.StartAt), localDate(*shoot.EndAt).AddDate(0, 0, 1)) {
		if item.Kind == "work" && item.StartAt.Before(*shoot.EndAt) && item.EndAt.After(*shoot.StartAt) {
			result = append(result, item)
		}
	}
	return result
}

// SyncSchedule 验证拍摄信息后提交关联；候选与预约冲突会在数据库用户锁内重复检查。
func (s *ProjectWorkflowService) SyncSchedule(ctx context.Context, user, project uuid.UUID, input ProjectScheduleInput) (ProjectScheduleLink, error) {
	if input.Revision < 1 || input.ScheduleID != nil && *input.ScheduleID == uuid.Nil || input.Detach && (input.ScheduleID != nil || input.CreateNew) || input.CreateNew && input.ScheduleID != nil {
		return ProjectScheduleLink{}, fault.InvalidRequest("档期关联参数无效")
	}
	source, err := s.repository.FindProject(ctx, user, project)
	if err != nil {
		return ProjectScheduleLink{}, err
	}
	var candidate model.ScheduleItem
	if !input.Detach {
		if !source.Shooting.Complete() {
			return ProjectScheduleLink{}, fault.InvalidField("shooting", "请填写完整的拍摄时间、地点、人物和类型")
		}
		label := "照片"
		if source.Shooting.Kind == "video" {
			label = "视频"
		}
		title := source.Shooting.StartAt.In(chinaLocation).Format("2006年01月02日") + source.Shooting.Person + label
		if runes := []rune(title); len(runes) > 120 {
			title = string(runes[:120])
		}
		candidate, err = scheduleModel(user, ScheduleFields{Kind: "work", Title: title, Content: source.Shooting.Location + "\n" + source.Shooting.Person, StartAt: *source.Shooting.StartAt, EndAt: *source.Shooting.EndAt, Recurrence: "none", Capacity: 1})
		if err != nil {
			return ProjectScheduleLink{}, err
		}
	}
	updated, err := s.repository.SyncProjectSchedule(ctx, user, project, input.Revision, input.ScheduleID, input.Detach, candidate, func(actual model.ScheduleItem, items []model.ScheduleItem, exceptions []model.ScheduleException, owned bool) error {
		if owned {
			return validateScheduleConflicts(actual, items, exceptions)
		}
		candidates := projectScheduleCandidates(source.Shooting, items, exceptions)
		if input.ScheduleID != nil {
			for _, item := range candidates {
				if item.BusinessID == *input.ScheduleID {
					return nil
				}
			}
			return fault.Conflict("所选档期已变化，请重新选择")
		}
		if len(candidates) > 0 && !input.CreateNew {
			return fault.New(409, "project_schedule_candidates", "发现可能对应的档期，请先确认关联", nil, nil)
		}
		return validateScheduleConflicts(candidate, items, exceptions)
	})
	if err != nil {
		return ProjectScheduleLink{}, err
	}
	return ProjectScheduleLink{ScheduleID: updated.ScheduleID, Owned: updated.ScheduleOwned}, nil
}

// ProjectReviewView 仅返回独立总结业务信息，不暴露内部 ID 或所有者。
type ProjectReviewView struct {
	BusinessID uuid.UUID `json:"business_id"`
	ProjectID  uuid.UUID `json:"project_id"`
	Title      string    `json:"title"`
	Content    string    `json:"content,omitempty"`
	Revision   int64     `json:"revision"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func projectReviewView(item model.ProjectReview) ProjectReviewView {
	return ProjectReviewView{BusinessID: item.BusinessID, ProjectID: item.ProjectID, Title: item.Title, Content: item.Content, Revision: item.Revision, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

// Review 查询策划已有的总结副本，未创建时返回 nil，方便界面显示明确的创建按钮。
func (s *ProjectWorkflowService) Review(ctx context.Context, user, project uuid.UUID) (*ProjectReviewView, error) {
	item, err := s.repository.FindProjectReview(ctx, user, project)
	if err != nil || item == nil {
		return nil, err
	}
	view := projectReviewView(*item)
	return &view, nil
}

// CompleteReview 从已保存的人工总结创建一次副本；不要求拍摄信息完整，不阻止先写复盘的用户。
func (s *ProjectWorkflowService) CompleteReview(ctx context.Context, user, project uuid.UUID, revision int64) (ProjectReviewView, error) {
	if revision < 1 {
		return ProjectReviewView{}, fault.InvalidField("revision", "修订号无效")
	}
	source, err := s.repository.FindProject(ctx, user, project)
	if err != nil {
		return ProjectReviewView{}, err
	}
	parts := make([]string, 0, 4)
	if source.Shooting.StartAt != nil {
		parts = append(parts, source.Shooting.StartAt.In(chinaLocation).Format("2006-01-02 15:04"))
	}
	for _, value := range []string{source.Shooting.Location, source.Shooting.Person} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	if source.Shooting.Kind == "photo" {
		parts = append(parts, "照片")
	}
	if source.Shooting.Kind == "video" {
		parts = append(parts, "视频")
	}
	title := strings.Join(parts, " · ")
	if title == "" {
		title = source.Title
	}
	item, err := s.repository.CreateProjectReview(ctx, user, project, revision, title)
	if err != nil {
		return ProjectReviewView{}, err
	}
	return projectReviewView(item), nil
}

// Reviews 分页读取独立总结，搜索长度与策划列表一致。
func (s *ProjectWorkflowService) Reviews(ctx context.Context, user uuid.UUID, query string, page PageRequest) (Page[ProjectReviewView], error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > 120 {
		return Page[ProjectReviewView]{}, fault.InvalidRequest("搜索最多 120 个字符")
	}
	items, total, err := s.repository.ListProjectReviews(ctx, user, query, page.Offset, page.PageSize)
	if err != nil {
		return Page[ProjectReviewView]{}, err
	}
	views := make([]ProjectReviewView, 0, len(items))
	for _, item := range items {
		views = append(views, projectReviewView(item))
	}
	return Page[ProjectReviewView]{Items: views, Page: page.Page, PageSize: page.PageSize, Total: total}, nil
}

// GetReview 读取独立文档，来源策划删除不影响其作者访问。
func (s *ProjectWorkflowService) GetReview(ctx context.Context, user, id uuid.UUID) (ProjectReviewView, error) {
	item, err := s.repository.GetProjectReview(ctx, user, id)
	if err != nil {
		return ProjectReviewView{}, err
	}
	return projectReviewView(item), nil
}

// ProjectReviewInput 是独立文档保存快照，与原策划的 revision 无关。
type ProjectReviewInput struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	Revision int64  `json:"revision"`
}

// SaveReview 限制标题及正文长度，并通过独立修订号阻止并发覆盖。
func (s *ProjectWorkflowService) SaveReview(ctx context.Context, user, id uuid.UUID, input ProjectReviewInput) (ProjectReviewView, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > 600 || utf8.RuneCountInString(input.Content) > 100000 || input.Revision < 1 {
		return ProjectReviewView{}, fault.InvalidRequest("总结标题、正文长度或修订号无效")
	}
	item, err := s.repository.UpdateProjectReview(ctx, user, id, input.Revision, input.Title, input.Content)
	if err != nil {
		return ProjectReviewView{}, err
	}
	return projectReviewView(item), nil
}

// Portfolios 返回策划已关联的作品集，权限仍由复用的双端范围校验保证。
func (s *ProjectWorkflowService) Portfolios(ctx context.Context, user, project uuid.UUID) ([]PortfolioView, error) {
	items, err := s.repository.ListProjectPortfolios(ctx, user, project)
	if err != nil {
		return nil, err
	}
	views := make([]PortfolioView, 0, len(items))
	for _, item := range items {
		views = append(views, portfolioView(item))
	}
	return views, nil
}

// SetPortfolios 只接受有限数量、非空且不重复的作品集业务 UUID。
func (s *ProjectWorkflowService) SetPortfolios(ctx context.Context, user, project uuid.UUID, ids []uuid.UUID) error {
	if len(ids) > 100 {
		return fault.InvalidField("portfolio_ids", "最多关联 100 个作品集")
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			return fault.InvalidField("portfolio_ids", "作品集标识无效或重复")
		}
		seen[id] = true
	}
	return s.repository.SetProjectPortfolios(ctx, user, project, ids)
}
