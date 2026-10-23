package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

var chinaLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// ScheduleRepository 定义档期、重复例外、公开分享和预约审批所需的持久化边界。
type ScheduleRepository interface {
	ListScheduleData(context.Context, uuid.UUID) ([]model.ScheduleItem, []model.ScheduleException, error)
	CreateScheduleItem(context.Context, *model.ScheduleItem, func([]model.ScheduleItem, []model.ScheduleException) error) error
	UpdateScheduleItem(context.Context, uuid.UUID, *model.ScheduleItem, func([]model.ScheduleItem, []model.ScheduleException) error) error
	DeleteScheduleItem(context.Context, uuid.UUID, uuid.UUID) error
	SaveScheduleException(context.Context, *model.ScheduleException, func([]model.ScheduleItem, []model.ScheduleException) error) error
	DeleteScheduleException(context.Context, uuid.UUID, uuid.UUID, time.Time) error
	ListScheduleBookings(context.Context, uuid.UUID, string) ([]model.ScheduleBooking, error)
	DecideScheduleBooking(context.Context, uuid.UUID, uuid.UUID, string) error
	CancelScheduleBooking(context.Context, uuid.UUID, uuid.UUID) error
	GetScheduleShare(context.Context, uuid.UUID) (model.ScheduleShare, error)
	SaveScheduleShare(context.Context, *model.ScheduleShare) error
	RevokeScheduleShare(context.Context, uuid.UUID) error
	FindScheduleShareByHash(context.Context, string) (model.ScheduleShare, error)
	CreateScheduleBooking(context.Context, *model.ScheduleBooking, string, func(model.ScheduleItem, []model.ScheduleException) (int, bool)) error
}

// ScheduleRateLimiter 限制公开预约申请频率；客户端 IP 必须已由可信代理链解析。
type ScheduleRateLimiter interface {
	CheckScheduleBookingLimit(context.Context, string, int) (bool, error)
}

// ScheduleService 实现中国标准时间下的档期展开、冲突校验、公开申请与拥有者审批。
type ScheduleService struct {
	repository ScheduleRepository
	limiter    ScheduleRateLimiter
	perHour    int
}

// NewScheduleService 注入数据库、Redis 和 app_configs 中的公开预约每 IP 限额。
func NewScheduleService(repository ScheduleRepository, limiter ScheduleRateLimiter, perHour int) *ScheduleService {
	return &ScheduleService{repository: repository, limiter: limiter, perHour: perHour}
}

// ScheduleFields 是创建或整条更新档期时可写入的业务字段集合。
type ScheduleFields struct {
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	StartAt       time.Time `json:"start_at"`
	EndAt         time.Time `json:"end_at"`
	Recurrence    string    `json:"recurrence"`
	Weekdays      []int     `json:"weekdays"`
	RecurrenceEnd string    `json:"recurrence_end"`
	Capacity      int       `json:"capacity"`
}

// ScheduleOccurrence 是按查询范围展开后的单次日历卡片，不含用户内部标识。
type ScheduleOccurrence struct {
	BusinessID    uuid.UUID `json:"business_id"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	StartAt       time.Time `json:"start_at"`
	EndAt         time.Time `json:"end_at"`
	Capacity      int       `json:"capacity"`
	Recurrence    string    `json:"recurrence"`
	Weekdays      []int64   `json:"weekdays"`
	RecurrenceEnd string    `json:"recurrence_end"`
	SeriesStartAt time.Time `json:"series_start_at"`
	SeriesEndAt   time.Time `json:"series_end_at"`
	SeriesDate    string    `json:"series_date"`
}

// SchedulePublicSlot 是分享页可见的最小开放时段字段，不包含拥有者或申请人资料。
type SchedulePublicSlot struct {
	ScheduleOccurrence
	Remaining int `json:"remaining"`
}

// ScheduleBookingView 是仅向档期拥有者返回的访客申请信息。
type ScheduleBookingView struct {
	BusinessID  uuid.UUID  `json:"business_id"`
	ScheduleID  uuid.UUID  `json:"schedule_id"`
	StartAt     time.Time  `json:"start_at"`
	EndAt       time.Time  `json:"end_at"`
	GuestName   string     `json:"guest_name"`
	GuestPhone  string     `json:"guest_phone"`
	Note        string     `json:"note"`
	Status      string     `json:"status"`
	RequestCode string     `json:"request_code"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ScheduleShareView 表示分享状态；明文随机令牌仅在刚创建时返回给拥有者。
type ScheduleShareView struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
}

// List 展开当前用户在中国标准时间日期区间内的档期和单次例外。
func (service *ScheduleService) List(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]ScheduleOccurrence, error) {
	if !from.Before(to) || to.Sub(from) > 90*24*time.Hour {
		return nil, fault.InvalidRequest("档期查询范围必须为 1 至 90 天")
	}
	items, exceptions, err := service.repository.ListScheduleData(ctx, userID)
	if err != nil {
		return nil, err
	}
	occurrences := expandSchedules(items, exceptions, from.In(chinaLocation), to.In(chinaLocation))
	sort.Slice(occurrences, func(i, j int) bool { return occurrences[i].StartAt.Before(occurrences[j].StartAt) })
	return occurrences, nil
}

// Create 校验档期字段和开放时段冲突，并在 repository 持有用户事务锁期间重复执行冲突检查。
func (service *ScheduleService) Create(ctx context.Context, userID uuid.UUID, fields ScheduleFields) (model.ScheduleItem, error) {
	item, err := scheduleModel(userID, fields)
	if err != nil {
		return model.ScheduleItem{}, err
	}
	item.BusinessID = uuid.New()
	err = service.repository.CreateScheduleItem(ctx, &item, func(existing []model.ScheduleItem, exceptions []model.ScheduleException) error {
		return validateScheduleConflicts(item, existing, exceptions)
	})
	return item, err
}

// Update 替换整条档期，并在单个 owner 写事务中检查其余开放时段及工作事项。
func (service *ScheduleService) Update(ctx context.Context, userID, itemID uuid.UUID, fields ScheduleFields) (model.ScheduleItem, error) {
	item, err := scheduleModel(userID, fields)
	if err != nil {
		return model.ScheduleItem{}, err
	}
	item.BusinessID = itemID
	err = service.repository.UpdateScheduleItem(ctx, userID, &item, func(existing []model.ScheduleItem, exceptions []model.ScheduleException) error {
		return validateScheduleConflicts(item, existing, exceptions)
	})
	return item, err
}

// Delete 删除整条档期；预约历史保留并由 service 层在 store 中显式清理关联记录。
func (service *ScheduleService) Delete(ctx context.Context, userID, itemID uuid.UUID) error {
	return service.repository.DeleteScheduleItem(ctx, userID, itemID)
}

// SetOccurrence 创建某次覆盖或取消例外；只允许目标日期确实属于重复系列。
func (service *ScheduleService) SetOccurrence(ctx context.Context, userID, itemID uuid.UUID, date time.Time, fields *ScheduleFields) error {
	items, _, err := service.repository.ListScheduleData(ctx, userID)
	if err != nil {
		return err
	}
	var base *model.ScheduleItem
	for index := range items {
		if items[index].BusinessID == itemID {
			base = &items[index]
			break
		}
	}
	if base == nil || base.Recurrence == "none" {
		return fault.NotFound()
	}
	date = localDate(date)
	if len(expandSchedules([]model.ScheduleItem{*base}, nil, date, date.AddDate(0, 0, 1))) == 0 {
		return fault.InvalidField("date", "所选日期不属于该重复档期")
	}
	exception := model.ScheduleException{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID, ScheduleBusinessID: itemID, OccurrenceDate: date}
	if fields == nil {
		exception.Action = "cancelled"
	} else {
		override, validationErr := scheduleModel(userID, *fields)
		if validationErr != nil {
			return validationErr
		}
		if override.Kind != base.Kind || override.Recurrence != "none" {
			return fault.InvalidField("kind", "单次例外必须保持原档期类型且不能继续重复")
		}
		exception.Action = "override"
		exception.Title, exception.Content = &override.Title, &override.Content
		exception.StartAt, exception.EndAt, exception.Capacity = &override.StartAt, &override.EndAt, &override.Capacity
	}
	return service.repository.SaveScheduleException(ctx, &exception, func(existing []model.ScheduleItem, currentExceptions []model.ScheduleException) error {
		var current *model.ScheduleItem
		for index := range existing {
			if existing[index].BusinessID == itemID {
				current = &existing[index]
				break
			}
		}
		if current == nil || current.Recurrence == "none" {
			return fault.NotFound()
		}
		if len(expandSchedules([]model.ScheduleItem{*current}, nil, date, date.AddDate(0, 0, 1))) == 0 {
			return fault.InvalidField("date", "所选日期已不属于该重复档期")
		}
		if fields == nil {
			return nil
		}
		if current.Kind != fields.Kind || fields.Recurrence != "none" {
			return fault.InvalidField("kind", "单次例外必须保持原档期类型")
		}
		candidate := model.ScheduleItem{BaseModel: model.BaseModel{BusinessID: itemID}, UserID: userID, Kind: current.Kind, Title: *exception.Title, Content: *exception.Content, StartAt: *exception.StartAt, EndAt: *exception.EndAt, Recurrence: "none", Capacity: *exception.Capacity}
		return validateScheduleOccurrenceConflicts(candidate, itemID, date, existing, currentExceptions)
	})
}

// PublicSlots 返回分享用户在日期范围内的开放时段及实时剩余名额。
func (service *ScheduleService) PublicSlots(ctx context.Context, token string, from, to time.Time) ([]SchedulePublicSlot, error) {
	share, err := service.repository.FindScheduleShareByHash(ctx, scheduleTokenHash(token))
	if err != nil || !share.Enabled {
		return nil, fault.NotFound()
	}
	// 公开能力的组织只能来自已验证分享记录，不能采信调用方的组织参数。
	ctx = tenant.WithScope(ctx, tenant.Scope{OrgID: share.OrgID, UserID: share.UserID})
	items, exceptions, err := service.repository.ListScheduleData(ctx, share.UserID)
	if err != nil {
		return nil, err
	}
	all := expandSchedules(items, exceptions, from.In(chinaLocation), to.In(chinaLocation))
	bookings, err := service.repository.ListScheduleBookings(ctx, share.UserID, "active")
	if err != nil {
		return nil, err
	}
	used := make(map[string]int)
	now := time.Now().UTC()
	for _, booking := range bookings {
		if booking.Status == "confirmed" || booking.Status == "pending" && booking.ExpiresAt != nil && booking.ExpiresAt.After(now) {
			used[slotKey(booking.ScheduleBusinessID, booking.OccurrenceStart)]++
		}
	}
	result := make([]SchedulePublicSlot, 0)
	for _, occurrence := range all {
		if occurrence.Kind != "availability" {
			continue
		}
		remaining := occurrence.Capacity - used[slotKey(occurrence.BusinessID, occurrence.StartAt)]
		if remaining > 0 {
			result = append(result, SchedulePublicSlot{ScheduleOccurrence: occurrence, Remaining: remaining})
		}
	}
	return result, nil
}

// Request 先验证分享链接、申请字段和滚动限流，再由 repository 锁定开放时段并原子占用名额。
func (service *ScheduleService) Request(ctx context.Context, clientIP, token string, scheduleID uuid.UUID, startAt time.Time, name, phone, note string) (model.ScheduleBooking, error) {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 80 || strings.TrimSpace(phone) == "" || utf8.RuneCountInString(phone) > 32 || utf8.RuneCountInString(note) > 1000 {
		return model.ScheduleBooking{}, fault.InvalidField("guest", "姓名、手机号或备注不符合长度要求")
	}
	if strings.TrimSpace(clientIP) == "" {
		return model.ScheduleBooking{}, fault.InvalidRequest("无法确认客户端网络地址")
	}
	allowed, err := service.limiter.CheckScheduleBookingLimit(ctx, clientIP, service.perHour)
	if err != nil {
		return model.ScheduleBooking{}, err
	}
	if !allowed {
		return model.ScheduleBooking{}, fault.New(http.StatusTooManyRequests, "rate_limited", "预约申请次数过多，请稍后重试", nil, nil)
	}
	share, err := service.repository.FindScheduleShareByHash(ctx, scheduleTokenHash(token))
	if err != nil || !share.Enabled {
		return model.ScheduleBooking{}, fault.NotFound()
	}
	ctx = tenant.WithScope(ctx, tenant.Scope{OrgID: share.OrgID, UserID: share.UserID})
	startAt = startAt.In(chinaLocation)
	booking := model.ScheduleBooking{
		BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: share.UserID,
		ScheduleBusinessID: scheduleID, OccurrenceStart: startAt,
		GuestName: strings.TrimSpace(name), GuestPhone: strings.TrimSpace(phone), Note: strings.TrimSpace(note),
		Status: "pending", RequestCode: strings.ToUpper(uuid.NewString()[:8]),
	}
	err = service.repository.CreateScheduleBooking(ctx, &booking, scheduleTokenHash(token), func(item model.ScheduleItem, exceptions []model.ScheduleException) (int, bool) {
		occurrences := expandSchedules([]model.ScheduleItem{item}, exceptions, startAt, startAt.Add(time.Second))
		for _, occurrence := range occurrences {
			if occurrence.Kind == "availability" && occurrence.BusinessID == scheduleID && occurrence.StartAt.Equal(startAt) {
				booking.OccurrenceEnd = occurrence.EndAt
				booking.ExpiresAt = timePtr(time.Now().UTC().Add(24 * time.Hour))
				return occurrence.Capacity, true
			}
		}
		return 0, false
	})
	return booking, err
}

// Bookings 返回当前用户的申请队列；失效待确认申请在读取时由 store 懒更新为 expired。
func (service *ScheduleService) Bookings(ctx context.Context, userID uuid.UUID, status string) ([]ScheduleBookingView, error) {
	bookings, err := service.repository.ListScheduleBookings(ctx, userID, status)
	if err != nil {
		return nil, err
	}
	result := make([]ScheduleBookingView, 0, len(bookings))
	for _, booking := range bookings {
		result = append(result, bookingView(booking))
	}
	return result, nil
}

// Decide 更新本用戶所有申请中指定业务 UUID 的状态，确认或拒绝均不可重复处理。
func (service *ScheduleService) Decide(ctx context.Context, userID, bookingID uuid.UUID, status string) error {
	if status != "confirmed" && status != "rejected" {
		return fault.InvalidRequest("预约状态不受支持")
	}
	return service.repository.DecideScheduleBooking(ctx, userID, bookingID, status)
}

// Cancel 允许档期拥有者取消待确认或已确认申请，释放其占用的开放时段名额。
func (service *ScheduleService) Cancel(ctx context.Context, userID, bookingID uuid.UUID) error {
	return service.repository.CancelScheduleBooking(ctx, userID, bookingID)
}

// ShareStatus 只返回当前用户的分享链接启用状态，不会尝试还原已哈希的令牌。
func (service *ScheduleService) ShareStatus(ctx context.Context, userID uuid.UUID) (ScheduleShareView, error) {
	share, err := service.repository.GetScheduleShare(ctx, userID)
	if err != nil {
		return ScheduleShareView{}, err
	}
	return ScheduleShareView{Enabled: share.Enabled}, nil
}

// CreateShare 生成高熵随机链接，只将摘要写入数据库，明文仅在本次响应中返回。
func (service *ScheduleService) CreateShare(ctx context.Context, userID uuid.UUID, baseURL string) (ScheduleShareView, error) {
	token := uuid.NewString() + uuid.NewString()
	share := model.ScheduleShare{BaseModel: model.BaseModel{BusinessID: uuid.New()}, UserID: userID, TokenHash: scheduleTokenHash(token), Enabled: true}
	if err := service.repository.SaveScheduleShare(ctx, &share); err != nil {
		return ScheduleShareView{}, err
	}
	return ScheduleShareView{Enabled: true, URL: strings.TrimRight(baseURL, "/") + "/book/" + token}, nil
}

// RevokeShare 立即撤销当前用户的公开预约链接。
func (service *ScheduleService) RevokeShare(ctx context.Context, userID uuid.UUID) error {
	return service.repository.RevokeScheduleShare(ctx, userID)
}

// scheduleModel 校验可写字段并将所有开始/结束时间规范到 Asia/Shanghai 解释下的真实时刻。
func scheduleModel(userID uuid.UUID, fields ScheduleFields) (model.ScheduleItem, error) {
	fields.Title = strings.TrimSpace(fields.Title)
	fields.Content = strings.TrimSpace(fields.Content)
	if fields.Kind != "work" && fields.Kind != "availability" {
		return model.ScheduleItem{}, fault.InvalidField("kind", "档期类型无效")
	}
	if fields.Title == "" || utf8.RuneCountInString(fields.Title) > 120 || utf8.RuneCountInString(fields.Content) > 4000 {
		return model.ScheduleItem{}, fault.InvalidField("title", "标题或内容长度不符合要求")
	}
	if fields.StartAt.IsZero() || fields.EndAt.IsZero() || !fields.StartAt.Before(fields.EndAt) || fields.EndAt.Sub(fields.StartAt) > 24*time.Hour {
		return model.ScheduleItem{}, fault.InvalidField("start_at", "结束时间必须晚于开始时间且单次时长不超过 24 小时")
	}
	if fields.Recurrence == "" {
		fields.Recurrence = "none"
	}
	if fields.Recurrence != "none" && fields.Recurrence != "daily" && fields.Recurrence != "weekly" && fields.Recurrence != "monthly" {
		return model.ScheduleItem{}, fault.InvalidField("recurrence", "重复规则无效")
	}
	if fields.Capacity == 0 {
		fields.Capacity = 1
	}
	if fields.Capacity < 1 || fields.Capacity > 100 || fields.Kind == "work" && fields.Capacity != 1 {
		return model.ScheduleItem{}, fault.InvalidField("capacity", "开放时段容量必须为 1 至 100，工作事项容量为 1")
	}
	weekdays := make(map[int]bool)
	for _, day := range fields.Weekdays {
		if day < 1 || day > 7 {
			return model.ScheduleItem{}, fault.InvalidField("weekdays", "星期编号必须为 1 至 7")
		}
		weekdays[day] = true
	}
	if fields.Recurrence == "weekly" && len(weekdays) == 0 {
		return model.ScheduleItem{}, fault.InvalidField("weekdays", "每周重复至少选择一个星期")
	}
	weekdayValues := make(pq.Int64Array, 0, len(weekdays))
	for day := 1; day <= 7; day++ {
		if weekdays[day] {
			weekdayValues = append(weekdayValues, int64(day))
		}
	}
	var recurrenceEnd *time.Time
	if fields.RecurrenceEnd != "" {
		parsed, err := time.ParseInLocation("2006-01-02", fields.RecurrenceEnd, chinaLocation)
		if err != nil || parsed.Before(localDate(fields.StartAt)) || parsed.After(localDate(fields.StartAt).AddDate(10, 0, 0)) {
			return model.ScheduleItem{}, fault.InvalidField("recurrence_end", "重复结束日期必须在首次日期至十年内")
		}
		recurrenceEnd = &parsed
	}
	if fields.Recurrence == "none" && fields.RecurrenceEnd != "" {
		return model.ScheduleItem{}, fault.InvalidField("recurrence_end", "单次档期不能设置重复结束日期")
	}
	if fields.Kind == "availability" && fields.Recurrence != "none" && recurrenceEnd == nil {
		return model.ScheduleItem{}, fault.InvalidField("recurrence_end", "重复开放时段请设置结束日期，以便避免与未来档期冲突")
	}
	return model.ScheduleItem{
		UserID: userID, Kind: fields.Kind, Title: fields.Title, Content: fields.Content,
		StartAt: fields.StartAt.In(chinaLocation), EndAt: fields.EndAt.In(chinaLocation),
		Recurrence: fields.Recurrence, RecurrenceWeekdays: weekdayValues, RecurrenceEnd: recurrenceEnd, Capacity: fields.Capacity,
	}, nil
}

// expandSchedules 将系列按中国标准时间展开，再应用单次取消或覆盖例外。
func expandSchedules(items []model.ScheduleItem, exceptions []model.ScheduleException, from, to time.Time) []ScheduleOccurrence {
	exceptionMap := make(map[string]model.ScheduleException, len(exceptions))
	itemMap := make(map[uuid.UUID]model.ScheduleItem, len(items))
	for _, exception := range exceptions {
		exceptionMap[exceptionKey(exception.ScheduleBusinessID, exception.OccurrenceDate)] = exception
	}
	for _, item := range items {
		itemMap[item.BusinessID] = item
	}
	emitted := make(map[string]bool)
	result := make([]ScheduleOccurrence, 0)
	from, to = from.In(chinaLocation), to.In(chinaLocation)
	for _, item := range items {
		baseStart := item.StartAt.In(chinaLocation)
		baseEnd := item.EndAt.In(chinaLocation)
		duration := baseEnd.Sub(baseStart)
		beginDate := localDate(from).AddDate(0, 0, -1)
		if beginDate.Before(localDate(baseStart)) {
			beginDate = localDate(baseStart)
		}
		lastDate := localDate(to).AddDate(0, 0, 1)
		if item.RecurrenceEnd != nil && localDate(*item.RecurrenceEnd).Before(lastDate) {
			lastDate = localDate(*item.RecurrenceEnd)
		}
		if item.Recurrence == "none" {
			beginDate, lastDate = localDate(baseStart), localDate(baseStart)
		}
		weekdaySet := make(map[int]bool, len(item.RecurrenceWeekdays))
		for _, day := range item.RecurrenceWeekdays {
			weekdaySet[int(day)] = true
		}
		for date := beginDate; !date.After(lastDate); date = date.AddDate(0, 0, 1) {
			if !matchesRecurrence(item, baseStart, date, weekdaySet) {
				continue
			}
			start := time.Date(date.Year(), date.Month(), date.Day(), baseStart.Hour(), baseStart.Minute(), baseStart.Second(), baseStart.Nanosecond(), chinaLocation)
			end := start.Add(duration)
			key := exceptionKey(item.BusinessID, date)
			if exception, exists := exceptionMap[key]; exists && item.Recurrence != "none" {
				if exception.Action == "cancelled" {
					continue
				}
				if exception.Action == "override" && exception.StartAt != nil && exception.EndAt != nil {
					start, end = exception.StartAt.In(chinaLocation), exception.EndAt.In(chinaLocation)
				}
			}
			if !start.Before(to) || !end.After(from) {
				continue
			}
			title, content, capacity := item.Title, item.Content, item.Capacity
			if exception, exists := exceptionMap[key]; exists && exception.Action == "override" && item.Recurrence != "none" {
				if exception.Title != nil {
					title = *exception.Title
				}
				if exception.Content != nil {
					content = *exception.Content
				}
				if exception.Capacity != nil {
					capacity = *exception.Capacity
				}
			}
			weekdays := append([]int64{}, item.RecurrenceWeekdays...)
			recurrenceEnd := ""
			if item.RecurrenceEnd != nil {
				recurrenceEnd = item.RecurrenceEnd.Format("2006-01-02")
			}
			occurrenceKey := exceptionKey(item.BusinessID, date)
			result = append(result, ScheduleOccurrence{BusinessID: item.BusinessID, Kind: item.Kind, Title: title, Content: content, StartAt: start, EndAt: end, Capacity: capacity, Recurrence: item.Recurrence, Weekdays: weekdays, RecurrenceEnd: recurrenceEnd, SeriesStartAt: baseStart, SeriesEndAt: baseEnd, SeriesDate: date.Format("2006-01-02")})
			emitted[occurrenceKey] = true
		}
	}
	// 若某次覆盖移动到了查询区间，原始发生日期可能在窗口之外，需按覆盖后的实际时间额外纳入结果。
	for _, exception := range exceptions {
		key := exceptionKey(exception.ScheduleBusinessID, exception.OccurrenceDate)
		if exception.Action != "override" || emitted[key] || exception.StartAt == nil || exception.EndAt == nil || !exception.StartAt.Before(to) || !exception.EndAt.After(from) {
			continue
		}
		item, exists := itemMap[exception.ScheduleBusinessID]
		if !exists || item.Recurrence == "none" {
			continue
		}
		weekdaySet := make(map[int]bool, len(item.RecurrenceWeekdays))
		for _, day := range item.RecurrenceWeekdays {
			weekdaySet[int(day)] = true
		}
		originalDate := localDate(exception.OccurrenceDate)
		if !matchesRecurrence(item, item.StartAt, originalDate, weekdaySet) || item.RecurrenceEnd != nil && originalDate.After(localDate(*item.RecurrenceEnd)) {
			continue
		}
		title, content, capacity := item.Title, item.Content, item.Capacity
		if exception.Title != nil {
			title = *exception.Title
		}
		if exception.Content != nil {
			content = *exception.Content
		}
		if exception.Capacity != nil {
			capacity = *exception.Capacity
		}
		weekdays := append([]int64{}, item.RecurrenceWeekdays...)
		recurrenceEnd := ""
		if item.RecurrenceEnd != nil {
			recurrenceEnd = item.RecurrenceEnd.Format("2006-01-02")
		}
		result = append(result, ScheduleOccurrence{BusinessID: item.BusinessID, Kind: item.Kind, Title: title, Content: content, StartAt: exception.StartAt.In(chinaLocation), EndAt: exception.EndAt.In(chinaLocation), Capacity: capacity, Recurrence: item.Recurrence, Weekdays: weekdays, RecurrenceEnd: recurrenceEnd, SeriesStartAt: item.StartAt.In(chinaLocation), SeriesEndAt: item.EndAt.In(chinaLocation), SeriesDate: exception.OccurrenceDate.In(chinaLocation).Format("2006-01-02")})
	}
	return result
}

// matchesRecurrence 判断一个本地日历日期是否属于指定档期重复规则。
func matchesRecurrence(item model.ScheduleItem, base time.Time, date time.Time, weekdays map[int]bool) bool {
	switch item.Recurrence {
	case "none":
		return localDate(base).Equal(date)
	case "daily":
		return !date.Before(localDate(base))
	case "weekly":
		return !date.Before(localDate(base)) && weekdays[int(date.Weekday()+6)%7+1]
	case "monthly":
		return !date.Before(localDate(base)) && date.Day() == base.Day()
	default:
		return false
	}
}

// validateScheduleConflicts 只禁止开放时段与工作事项或其他开放时段重叠，工作事项之间可重叠。
func validateScheduleConflicts(candidate model.ScheduleItem, existing []model.ScheduleItem, exceptions []model.ScheduleException) error {
	if candidate.Kind != "availability" {
		for _, item := range existing {
			if item.BusinessID == candidate.BusinessID && candidate.BusinessID != uuid.Nil {
				continue
			}
			if item.Kind == "availability" {
				if hasScheduleOverlap(candidate, item, exceptions) {
					return fault.Conflict("工作事项与现有开放时段重叠")
				}
			}
		}
		return nil
	}
	for _, item := range existing {
		if item.BusinessID == candidate.BusinessID && candidate.BusinessID != uuid.Nil {
			continue
		}
		if hasScheduleOverlap(candidate, item, exceptions) {
			return fault.Conflict("开放时段不能与工作事项或其他开放时段重叠")
		}
	}
	return nil
}

// validateScheduleOccurrenceConflicts 校验单次覆盖与其他档期及同系列相邻发生项之间的预约冲突。
func validateScheduleOccurrenceConflicts(candidate model.ScheduleItem, seriesID uuid.UUID, occurrenceDate time.Time, existing []model.ScheduleItem, exceptions []model.ScheduleException) error {
	if err := validateScheduleConflicts(candidate, existing, exceptions); err != nil {
		return err
	}
	if candidate.Kind != "availability" {
		return nil
	}
	for _, item := range existing {
		if item.BusinessID != seriesID {
			continue
		}
		occurrences := expandSchedules([]model.ScheduleItem{item}, exceptions, candidate.StartAt, candidate.EndAt)
		for _, occurrence := range occurrences {
			if occurrence.SeriesDate == localDate(occurrenceDate).Format("2006-01-02") {
				continue
			}
			if candidate.StartAt.Before(occurrence.EndAt) && occurrence.StartAt.Before(candidate.EndAt) {
				return fault.Conflict("單次開放時段不能與同一系列其他發生項重疊")
			}
		}
	}
	return nil
}

// hasScheduleOverlap 在有限而完整的候选重复日期范围内比较已展开发生项。
func hasScheduleOverlap(left, right model.ScheduleItem, exceptions []model.ScheduleException) bool {
	leftStart := localDate(left.StartAt.In(chinaLocation))
	leftEnd := left.RecurrenceEnd
	if leftEnd == nil {
		fallback := leftStart.AddDate(10, 0, 0)
		leftEnd = &fallback
	}
	rightStart := localDate(right.StartAt.In(chinaLocation))
	if rightStart.After(leftStart) {
		leftStart = rightStart
	}
	if right.RecurrenceEnd != nil && right.RecurrenceEnd.Before(*leftEnd) {
		leftEnd = right.RecurrenceEnd
	}
	if leftStart.After(*leftEnd) {
		return false
	}
	windowEnd := localDate(*leftEnd).AddDate(0, 0, 1)
	leftItems := expandSchedules([]model.ScheduleItem{left}, exceptions, leftStart, windowEnd)
	rightItems := expandSchedules([]model.ScheduleItem{right}, exceptions, leftStart, windowEnd)
	for _, a := range leftItems {
		for _, b := range rightItems {
			if a.StartAt.Before(b.EndAt) && b.StartAt.Before(a.EndAt) {
				return true
			}
		}
	}
	return false
}

// scheduleTokenHash 仅以 SHA-256 摘要查询随机分享令牌。
func scheduleTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// localDate 取得中国标准时间下的本地日期零点。
func localDate(value time.Time) time.Time {
	local := value.In(chinaLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, chinaLocation)
}

// exceptionKey 将单次例外定位到业务 UUID 与其原始本地发生日期。
func exceptionKey(scheduleID uuid.UUID, date time.Time) string {
	return scheduleID.String() + ":" + date.In(chinaLocation).Format("2006-01-02")
}

// slotKey 将预约容量按档期业务 UUID 和准确开始时刻分组。
func slotKey(scheduleID uuid.UUID, start time.Time) string {
	return fmt.Sprintf("%s:%d", scheduleID, start.UTC().UnixNano())
}

// bookingView 隐去预约模型中的用户 UUID、数据库内部 ID 和存储字段细节。
func bookingView(booking model.ScheduleBooking) ScheduleBookingView {
	return ScheduleBookingView{BusinessID: booking.BusinessID, ScheduleID: booking.ScheduleBusinessID, StartAt: booking.OccurrenceStart, EndAt: booking.OccurrenceEnd, GuestName: booking.GuestName, GuestPhone: booking.GuestPhone, Note: booking.Note, Status: booking.Status, RequestCode: booking.RequestCode, ExpiresAt: booking.ExpiresAt, CreatedAt: booking.CreatedAt}
}

// timePtr 创建供数据库 DTO 使用的 UTC 时刻指针。
func timePtr(value time.Time) *time.Time { return &value }
