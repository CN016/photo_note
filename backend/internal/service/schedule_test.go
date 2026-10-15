package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/model"
)

func scheduleTime(value string) time.Time {
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, chinaLocation)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestExpandSchedulesDailyWeeklyAndSingleOccurrenceExceptions(t *testing.T) {
	userID, itemID := uuid.New(), uuid.New()
	base := model.ScheduleItem{
		BaseModel: model.BaseModel{BusinessID: itemID}, UserID: userID, Kind: "work", Title: "拍摄", Content: "棚内",
		StartAt: scheduleTime("2026-10-12 09:00"), EndAt: scheduleTime("2026-10-12 10:00"),
		Recurrence: "weekly", RecurrenceWeekdays: pq.Int64Array{1, 3, 5}, Capacity: 1,
	}
	exceptions := []model.ScheduleException{
		{ScheduleBusinessID: itemID, OccurrenceDate: scheduleTime("2026-10-14 00:00"), Action: "cancelled"},
		{ScheduleBusinessID: itemID, OccurrenceDate: scheduleTime("2026-10-16 00:00"), Action: "override", Title: stringPtr("外景"), Content: stringPtr("公园"), StartAt: timePtr(scheduleTime("2026-10-16 11:00")), EndAt: timePtr(scheduleTime("2026-10-16 12:00"))},
	}
	items := expandSchedules([]model.ScheduleItem{base}, exceptions, scheduleTime("2026-10-12 00:00"), scheduleTime("2026-10-19 00:00"))
	if len(items) != 2 {
		t.Fatalf("expected Monday and overridden Friday, got %d", len(items))
	}
	if items[0].SeriesDate != "2026-10-12" || items[0].Title != "拍摄" {
		t.Fatalf("unexpected first occurrence: %#v", items[0])
	}
	if items[1].SeriesDate != "2026-10-16" || items[1].Title != "外景" || items[1].StartAt.Hour() != 11 {
		t.Fatalf("override was not applied: %#v", items[1])
	}
}

func TestExpandMonthlySkipsDatesMissingInMonth(t *testing.T) {
	start := scheduleTime("2026-01-31 13:30")
	endDate := scheduleTime("2026-05-31 00:00")
	item := model.ScheduleItem{
		BaseModel: model.BaseModel{BusinessID: uuid.New()}, Kind: "work", Title: "每月复盘",
		StartAt: start, EndAt: start.Add(time.Hour), Recurrence: "monthly", RecurrenceEnd: &endDate,
	}
	items := expandSchedules([]model.ScheduleItem{item}, nil, scheduleTime("2026-01-01 00:00"), scheduleTime("2026-06-01 00:00"))
	if len(items) != 3 {
		t.Fatalf("expected Jan, Mar, and May; got %d", len(items))
	}
	if items[1].SeriesDate != "2026-03-31" {
		t.Fatalf("February should be skipped, got %s", items[1].SeriesDate)
	}
	if items[2].SeriesDate != "2026-05-31" {
		t.Fatalf("April should be skipped, got %s", items[2].SeriesDate)
	}
}

func TestScheduleOverlapRules(t *testing.T) {
	start := scheduleTime("2026-10-15 09:00")
	work := model.ScheduleItem{BaseModel: model.BaseModel{BusinessID: uuid.New()}, Kind: "work", Title: "工作", StartAt: start, EndAt: start.Add(time.Hour), Recurrence: "none"}
	otherWork := work
	otherWork.BaseModel.BusinessID = uuid.New()
	if err := validateScheduleConflicts(work, []model.ScheduleItem{otherWork}, nil); err != nil {
		t.Fatalf("work items may overlap: %v", err)
	}
	open := work
	open.BaseModel.BusinessID = uuid.New()
	open.Kind = "availability"
	open.Capacity = 2
	if err := validateScheduleConflicts(open, []model.ScheduleItem{work}, nil); err == nil {
		t.Fatal("availability overlapping a work item should fail")
	}
}

func TestOccurrenceOverrideCannotOverlapAnotherAvailabilityInSameSeries(t *testing.T) {
	seriesID := uuid.New()
	start := scheduleTime("2026-10-12 09:00")
	series := model.ScheduleItem{
		BaseModel: model.BaseModel{BusinessID: seriesID}, Kind: "availability", Title: "每日时段",
		StartAt: start, EndAt: start.Add(time.Hour), Recurrence: "daily", Capacity: 1,
	}
	date := scheduleTime("2026-10-15 00:00")
	candidate := model.ScheduleItem{
		BaseModel: model.BaseModel{BusinessID: seriesID}, Kind: "availability", Title: "调整后的时段",
		StartAt: scheduleTime("2026-10-15 09:30"), EndAt: scheduleTime("2026-10-16 09:30"), Recurrence: "none", Capacity: 1,
	}
	if err := validateScheduleOccurrenceConflicts(candidate, seriesID, date, []model.ScheduleItem{series}, nil); err == nil {
		t.Fatal("single override overlapping the next occurrence of its own availability series should fail")
	}
}

func TestExpandIgnoresExceptionDateOutsideUpdatedRecurrence(t *testing.T) {
	start := scheduleTime("2026-10-12 09:00")
	item := model.ScheduleItem{
		BaseModel: model.BaseModel{BusinessID: uuid.New()}, Kind: "work", Title: "周一工作",
		StartAt: start, EndAt: start.Add(time.Hour), Recurrence: "weekly", RecurrenceWeekdays: pq.Int64Array{1},
	}
	exception := model.ScheduleException{
		ScheduleBusinessID: item.BusinessID, OccurrenceDate: scheduleTime("2026-10-13 00:00"), Action: "override",
		Title: stringPtr("已失效的周二覆盖"), StartAt: timePtr(scheduleTime("2026-10-13 11:00")), EndAt: timePtr(scheduleTime("2026-10-13 12:00")),
	}
	items := expandSchedules([]model.ScheduleItem{item}, []model.ScheduleException{exception}, scheduleTime("2026-10-13 00:00"), scheduleTime("2026-10-14 00:00"))
	if len(items) != 0 {
		t.Fatalf("exception should not survive after its original date leaves the recurrence rule: %#v", items)
	}
}

func TestScheduleModelRequiresBoundedRepeatingAvailability(t *testing.T) {
	fields := ScheduleFields{
		Kind: "availability", Title: "预约", StartAt: scheduleTime("2026-10-15 09:00"), EndAt: scheduleTime("2026-10-15 10:00"),
		Recurrence: "weekly", Weekdays: []int{4}, Capacity: 1,
	}
	if _, err := scheduleModel(uuid.New(), fields); err == nil {
		t.Fatal("repeating public availability must have an end date")
	}
	fields.RecurrenceEnd = "2026-10-29"
	if _, err := scheduleModel(uuid.New(), fields); err != nil {
		t.Fatalf("valid bounded weekly availability rejected: %v", err)
	}
}

func stringPtr(value string) *string { return &value }
