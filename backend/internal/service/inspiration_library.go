package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// InspirationProcessingConfig 集中来自私有配置；进程路径为部署者控制，不接受 API 请求覆盖。
type InspirationProcessingConfig struct {
	Enabled                   bool   `json:"enabled"`
	Algorithm                 string `json:"algorithm"`
	ScanTime                  string `json:"scan_time"`
	CompressionWorkers        int    `json:"compression_workers"`
	AnalysisWorkers           int    `json:"analysis_workers"`
	PollMS                    int    `json:"poll_ms"`
	CompressionTimeoutSeconds int    `json:"compression_timeout_seconds"`
	TimeoutSeconds            int    `json:"timeout_seconds"`
	LeaseSeconds              int    `json:"lease_seconds"`
	RetrySeconds              []int  `json:"retry_seconds"`
	PythonPath                string `json:"python_path"`
	ScriptPath                string `json:"script_path"`
	ModelPath                 string `json:"model_path"`
	SocketPath                string `json:"socket_path"`
	MaxImagePixels            int    `json:"max_image_pixels"`
}

var inspirationClock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
var inspirationChina = time.FixedZone("Asia/Shanghai", 8*60*60)

// InspirationProcessing 通过唯一配置入口读取并拒绝不安全或无法恢复的并发、租约参数。
func (s *RuntimeConfigService) InspirationProcessing(ctx context.Context) (InspirationProcessingConfig, error) {
	var c InspirationProcessingConfig
	values, err := s.reader.ReadConfigValues(ctx, []string{"inspirations.processing"})
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal([]byte(values["inspirations.processing"]), &c); err != nil {
		return c, errors.New("灵感图处理配置格式无效")
	}
	if c.Algorithm == "" || !inspirationClock.MatchString(c.ScanTime) || c.CompressionWorkers < 1 || c.CompressionWorkers > 2 || c.AnalysisWorkers < 1 || c.AnalysisWorkers > 10 || c.PollMS < 20 || c.PollMS > 60000 || c.CompressionTimeoutSeconds < 1 || c.CompressionTimeoutSeconds > 300 || c.TimeoutSeconds < 1 || c.TimeoutSeconds > 300 || c.LeaseSeconds < c.TimeoutSeconds*2 || c.LeaseSeconds < 360 || len(c.RetrySeconds) == 0 || len(c.RetrySeconds) > 10 || c.MaxImagePixels < 1 {
		return c, errors.New("灵感图处理配置的版本、并发、租约或时间无效")
	}
	for _, n := range c.RetrySeconds {
		if n < 1 || n > 86400 {
			return c, errors.New("灵感图重试间隔无效")
		}
	}
	for _, p := range []string{c.PythonPath, c.ScriptPath, c.ModelPath, c.SocketPath} {
		if !filepath.IsAbs(p) {
			return c, errors.New("灵感图处理路径必须是部署绝对路径")
		}
	}
	return c, nil
}

// InspirationLibraryRepository 收敛图库、目标和队列存储能力；普通方法必须由调用者传入已鉴权范围。
type InspirationLibraryRepository interface {
	GetInspirationGoal(context.Context, uuid.UUID) (model.InspirationGoal, error)
	SaveInspirationGoal(context.Context, uuid.UUID, bool, int, string) error
	ListInspirationActivities(context.Context, uuid.UUID, string, string) ([]model.InspirationActivity, error)
	AcknowledgeInspirationReminder(context.Context, uuid.UUID, string) (bool, error)
	InspirationBrowseSummary(context.Context, uuid.UUID, string, string) (map[string][]model.InspirationFacet, error)
	ScanInspirationProcessingPage(context.Context, int, string) (int, int, error)
	ClaimInspirationProcessing(context.Context, string, time.Duration) (*model.InspirationProcessingJob, error)
	FinishInspirationProcessing(context.Context, model.InspirationProcessingJob, string, string, time.Time, *model.InspirationAnalysis) error
	RetryInspirationProcessing(context.Context, uuid.UUID, uuid.UUID) error
	DueInspirationGoalsPage(context.Context, int) ([]model.InspirationGoal, error)
}

// InspirationLibraryService 管理图库查询、后台工作生命周期和同容器识别子进程，不给子进程数据库凭据。
type InspirationLibraryService struct {
	repository   InspirationLibraryRepository
	inspirations *InspirationService
	config       *RuntimeConfigService
	notices      *NoticeService
	errors       *ErrorLogService
	wg           sync.WaitGroup
	client       *http.Client
}

// NewInspirationLibraryService 将 socket 访问限制在配置的本机 Unix Socket，传输真实图片字节而不是任意路径。
func NewInspirationLibraryService(repo InspirationLibraryRepository, images *InspirationService, config *RuntimeConfigService, notices *NoticeService, errors *ErrorLogService) *InspirationLibraryService {
	return &InspirationLibraryService{repository: repo, inspirations: images, config: config, notices: notices, errors: errors}
}

// InspirationGoalStatus 返回当前日及可选年度活动；不输出提醒通知 UUID 或数据库主键。
type InspirationGoalStatus struct {
	Goal            model.InspirationGoal       `json:"goal"`
	Today           model.InspirationActivity   `json:"today"`
	Days            []model.InspirationActivity `json:"days,omitempty"`
	ReminderPending bool                        `json:"reminder_pending"`
}

// GoalStatus 对当前空间读取；没有当天记录时只构造零值，不改变历史记录。
func (s *InspirationLibraryService) GoalStatus(ctx context.Context, user uuid.UUID, year int) (InspirationGoalStatus, error) {
	goal, err := s.repository.GetInspirationGoal(ctx, user)
	if err != nil {
		return InspirationGoalStatus{}, err
	}
	today := time.Now().In(inspirationChina).Format("2006-01-02")
	from, to := today, today
	if year != 0 {
		from = fmt.Sprintf("%04d-01-01", year)
		to = fmt.Sprintf("%04d-12-31", year)
	}
	rows, err := s.repository.ListInspirationActivities(ctx, user, from, to)
	if err != nil {
		return InspirationGoalStatus{}, err
	}
	status := InspirationGoalStatus{Goal: goal, Today: model.InspirationActivity{Day: today}}
	if goal.Enabled {
		status.Today.Target = &goal.Target
	}
	for _, row := range rows {
		if row.Day == today {
			status.Today = row
		}
	}
	if year != 0 {
		status.Days = rows
		if !strings.HasPrefix(today, fmt.Sprintf("%04d-", year)) {
			current, err := s.repository.ListInspirationActivities(ctx, user, today, today)
			if err != nil {
				return status, err
			}
			if len(current) > 0 {
				status.Today = current[0]
			}
		}
	}
	status.ReminderPending = goal.Enabled && status.Today.SavedCount < goal.Target && status.Today.ReminderNotificationID != nil && status.Today.ReminderAcknowledgedAt == nil
	return status, nil
}

// SaveGoal 验证用户输入，仅当天目标快照可变；关闭目标不会清除既有存图次数。
func (s *InspirationLibraryService) SaveGoal(ctx context.Context, user uuid.UUID, enabled bool, target int, clock string) error {
	if target < 0 || target > 100000 || enabled && target == 0 || !inspirationClock.MatchString(clock) {
		return fault.InvalidRequest("每日目标及提醒时间无效")
	}
	return s.repository.SaveInspirationGoal(ctx, user, enabled, target, clock)
}

// Acknowledge 校验日期必须为当天，避免旧标签页替其他日期确认弹窗。
func (s *InspirationLibraryService) Acknowledge(ctx context.Context, user uuid.UUID, day string) (bool, error) {
	if day != time.Now().In(inspirationChina).Format("2006-01-02") {
		return false, fault.InvalidRequest("只能确认当天提醒")
	}
	return s.repository.AcknowledgeInspirationReminder(ctx, user, day)
}

// BrowseSummary 限制日历查询最多一年，避免用任意区间请求扫描全库。
func (s *InspirationLibraryService) BrowseSummary(ctx context.Context, user uuid.UUID, from, to string) (map[string][]model.InspirationFacet, error) {
	first, e1 := time.Parse("2006-01-02", from)
	last, e2 := time.Parse("2006-01-02", to)
	if e1 != nil || e2 != nil || last.Before(first) || last.Sub(first) > 366*24*time.Hour {
		return nil, fault.InvalidRequest("日历日期区间无效")
	}
	return s.repository.InspirationBrowseSummary(ctx, user, from, to)
}

// Retry 仅提交当前身份下的失败任务重试，HTTP 接受不代表异步任务已完成。
func (s *InspirationLibraryService) Retry(ctx context.Context, user, id uuid.UUID) error {
	return s.repository.RetryInspirationProcessing(ctx, user, id)
}

// ValidateInspirationFilters 统一验证新增筛选参数，数据库只接受白名单条件。
func ValidateInspirationFilters(f model.InspirationFilters) error {
	for _, d := range []string{f.DateFrom, f.DateTo} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return fault.InvalidRequest("日期格式应为 YYYY-MM-DD")
			}
		}
	}
	if f.DateFrom != "" && f.DateTo != "" && f.DateFrom > f.DateTo {
		return fault.InvalidRequest("日期区间无效")
	}
	if f.Project != "" && f.Project != "unassigned" {
		if _, err := uuid.Parse(f.Project); err != nil {
			return fault.InvalidRequest("项目标识无效")
		}
	}
	if f.People != "" && f.People != "0" && f.People != "1" && f.People != "2" && f.People != "3+" {
		return fault.InvalidRequest("人数筛选无效")
	}
	if f.EmptyShot != "" && f.EmptyShot != "true" && f.EmptyShot != "false" {
		return fault.InvalidRequest("空镜筛选无效")
	}
	if f.Color != "" && !regexp.MustCompile(`^(h0[0-9]|h1[01]|black|white|gray)$`).MatchString(f.Color) {
		return fault.InvalidRequest("颜色分类无效")
	}
	if f.Status != "" && !strings.Contains("|pending_compression|compressing|pending_analysis|analyzing|succeeded|failed|unsupported|canceled|", "|"+f.Status+"|") {
		return fault.InvalidRequest("处理状态无效")
	}
	return nil
}

// Start 启动受 context 管理的工作线程；配置来自统一服务，调度器每分钟刷新扫描时刻。
func (s *InspirationLibraryService) Start(ctx context.Context, c InspirationProcessingConfig) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.SocketPath)
	}, MaxConnsPerHost: 10, MaxIdleConnsPerHost: 10}
	s.client = &http.Client{Transport: transport, Timeout: time.Duration(c.TimeoutSeconds) * time.Second}
	s.spawn(func() { s.supervise(ctx, c) })
	for i := 0; i < 2; i++ {
		s.spawn(func() { s.worker(ctx, "compression", i) })
	}
	for i := 0; i < 10; i++ {
		s.spawn(func() { s.worker(ctx, "analysis", i) })
	}
	s.spawn(func() { s.schedule(ctx) })
}

// spawn 将后台协程统一纳入退出等待，防止子进程或worker脱离生命周期。
func (s *InspirationLibraryService) spawn(work func()) {
	s.wg.Add(1)
	go func() { defer s.wg.Done(); work() }()
}

// Wait 等待所有任务和子进程收尾，避免退出时遗留识别进程；超时任务由数据库租约接管。
func (s *InspirationLibraryService) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// capture 只记录安全错误类别；不记录图片字节、storekey、套接字或底层响应正文。
func (s *InspirationLibraryService) capture(code string, err error) {
	if s.errors != nil && err != nil {
		s.errors.CaptureBackground(code, errors.New(code))
	}
}

// supervise 常驻子进程共享一个模型；环境只继承进程运行必需值，不把 DB/Redis 密码传给 Python。
func (s *InspirationLibraryService) supervise(ctx context.Context, c InspirationProcessingConfig) {
	for ctx.Err() == nil {
		if err := os.MkdirAll(filepath.Dir(c.SocketPath), 0700); err != nil {
			s.capture("inspiration_ai_start_failed", err)
			return
		}
		cmd := exec.CommandContext(ctx, c.PythonPath, c.ScriptPath, "--socket", c.SocketPath, "--model", c.ModelPath, "--workers", "10", "--max-pixels", fmt.Sprint(c.MaxImagePixels), "--max-bytes", fmt.Sprint(s.inspirations.maxImageBytes))
		cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "OMP_NUM_THREADS=1", "OPENBLAS_NUM_THREADS=1", "PYTHONUNBUFFERED=1"}
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		err := cmd.Run()
		if ctx.Err() != nil {
			return
		}
		s.capture("inspiration_ai_process_exited", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// worker 根据阶段单条领取；配置暂停时保留任务，不把模型不可用误记为图片内容错误。
func (s *InspirationLibraryService) worker(ctx context.Context, kind string, index int) {
	for ctx.Err() == nil {
		c, err := s.config.InspirationProcessing(ctx)
		if err != nil || !c.Enabled || (kind == "compression" && index >= c.CompressionWorkers) || (kind == "analysis" && index >= c.AnalysisWorkers) {
			s.capture("inspiration_processing_config", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		job, err := s.repository.ClaimInspirationProcessing(tenant.System(ctx), kind, time.Duration(c.LeaseSeconds)*time.Second)
		if err != nil {
			s.capture("inspiration_processing_claim", err)
		}
		if job == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(c.PollMS) * time.Millisecond):
			}
			continue
		}
		scoped := tenant.WithScope(ctx, tenant.Scope{OrgID: job.OrgID, UserID: job.UserID})
		stage := "succeeded"
		var analysis *model.InspirationAnalysis
		code := ""
		key, keyErr := filestore.ParseKey(job.ImageKey)
		if keyErr != nil || key.Public {
			stage = "unsupported"
			code = "unsupported_image"
		} else if kind == "compression" {
			stage = "pending_analysis"
			compressionCtx, cancelCompression := context.WithTimeout(ctx, time.Duration(c.CompressionTimeoutSeconds)*time.Second)
			err = s.inspirations.processCompressionJobContext(compressionCtx, inspirationCompressionJob{scope: tenant.Scope{OrgID: job.OrgID, UserID: job.UserID}, scoped: true, userID: job.UserID, inspirationID: job.InspirationID, imageKey: job.ImageKey, asyncVersion: job.AsyncVersion})
			cancelCompression()
		} else {
			analysis, err = s.analyze(scoped, *job, c)
		}
		next := time.Now().UTC()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			stage = "failed"
			code = "image_processing_failed"
			if job.Attempts <= len(c.RetrySeconds) {
				stage = "pending_analysis"
				if kind == "compression" {
					stage = "pending_compression"
				}
				next = next.Add(time.Duration(c.RetrySeconds[job.Attempts-1]) * time.Second)
			}
			s.capture("inspiration_processing_failed", err)
		}
		if err := s.repository.FinishInspirationProcessing(scoped, *job, stage, code, next, analysis); err != nil {
			s.capture("inspiration_processing_save", err)
		}
	}
}

// analyze 经过 FileService 当前归属验证后流式发送受限图片，严格校验子进程的最小响应。
func (s *InspirationLibraryService) analyze(ctx context.Context, job model.InspirationProcessingJob, c InspirationProcessingConfig) (*model.InspirationAnalysis, error) {
	if err := authorizeFileReference(ctx, s.inspirations.files, job.ImageKey); err != nil {
		return nil, err
	}
	reader, size, _, err := OpenFile(ctx, s.inspirations.files, job.ImageKey)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if size < 1 || size > s.inspirations.maxImageBytes {
		return nil, errors.New("invalid_image_size")
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "http://ai/infer", io.LimitReader(reader, size))
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	response, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("ai_inference_unavailable")
	}
	var result model.InspirationAnalysis
	if err := json.NewDecoder(io.LimitReader(response.Body, 32768)).Decode(&result); err != nil {
		return nil, err
	}
	if result.PeopleCount == nil || *result.PeopleCount < 0 || result.IsEmptyShot == nil || *result.IsEmptyShot != (*result.PeopleCount == 0) || result.DominantColor == nil || !regexp.MustCompile(`^#[0-9a-f]{6}$`).MatchString(*result.DominantColor) || result.DominantHue == nil || *result.DominantHue < 0 || *result.DominantHue >= 360 || result.ColorBucket == nil || ValidateInspirationFilters(model.InspirationFilters{Color: *result.ColorBucket}) != nil || result.ColorPalette == nil || !json.Valid(*result.ColorPalette) {
		return nil, errors.New("invalid_ai_result")
	}
	return &result, nil
}

// schedule 启动补扫及每日到点扫描；每天可重复触发，但任务唯一约束保证不会重复处理成功代次。
func (s *InspirationLibraryService) schedule(ctx context.Context) {
	scanned := ""
	initialScan := true
	for ctx.Err() == nil {
		c, err := s.config.InspirationProcessing(ctx)
		local := time.Now().In(inspirationChina)
		day := local.Format("2006-01-02")
		if err == nil && c.Enabled && inspirationScanDue(initialScan, scanned, local, c.ScanTime) {
			after := 0
			for ctx.Err() == nil {
				next, n, e := s.repository.ScanInspirationProcessingPage(tenant.System(ctx), after, c.Algorithm)
				if e != nil {
					s.capture("inspiration_processing_scan", e)
					break
				}
				after = next
				if n < 500 {
					initialScan = false
					if local.Format("15:04") >= c.ScanTime {
						scanned = day
					}
					break
				}
			}
		}
		after := 0
		for ctx.Err() == nil {
			goals, e := s.repository.DueInspirationGoalsPage(tenant.System(ctx), after)
			if e != nil {
				s.capture("inspiration_goal_scan", e)
				break
			}
			for _, goal := range goals {
				after = goal.ID
				scope := tenant.WithScope(ctx, tenant.Scope{OrgID: goal.OrgID, UserID: goal.UserID})
				if e := s.notices.PublishInspirationGoalReminder(scope, goal); e != nil {
					s.capture("inspiration_goal_publish", e)
				}
			}
			if len(goals) < 500 {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

// inspirationScanDue 将启动补扫与每日到点分开，凌晨启动不能吞掉当天三点的扫描。
func inspirationScanDue(initial bool, last string, local time.Time, clock string) bool {
	return initial || (last != local.Format("2006-01-02") && local.Format("15:04") >= clock)
}
