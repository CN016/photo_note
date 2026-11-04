package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
)

// projectModeRepository 捕获更新字段，验证旧客户端不会覆盖已有默认模式。
type projectModeRepository struct {
	ProjectRepository
	updates map[string]any
}

func (r *projectModeRepository) UpdateProject(_ context.Context, _, _ uuid.UUID, _ int64, updates map[string]any, _ []uuid.UUID, _ []model.ProjectAttachment) (model.Project, error) {
	r.updates = updates
	return model.Project{DefaultReadOnly: true}, nil
}
func TestProjectDefaultReadOnlyUpdateCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    *bool
		expected any
		present  bool
	}{
		{name: "旧客户端未传字段", present: false},
		{name: "切换阅读模式", value: boolPointer(true), expected: true, present: true},
		{name: "切换编辑模式", value: boolPointer(false), expected: false, present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &projectModeRepository{}
			service := NewProjectService(repo, nil, 1)
			input := ProjectSave{Title: "策划", CoverEmoji: "封面", DocumentSchemaVersion: 1, Document: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`), Revision: 1, DefaultReadOnly: tc.value}
			view, err := service.Update(context.Background(), uuid.New(), uuid.New(), input)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := repo.updates["default_read_only"]
			if ok != tc.present || (ok && got != tc.expected) {
				t.Fatalf("默认模式更新不符: got=%v present=%v", got, ok)
			}
			if !view.DefaultReadOnly {
				t.Fatal("详情未返回数据库中的默认模式")
			}
		})
	}
}
func boolPointer(value bool) *bool { return &value }

// TestProjectManualSummaryCompatibility 确保旧客户端省略复盘时不会将已保存内容清空。
func TestProjectManualSummaryCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary *string
		present bool
	}{
		{name: "旧客户端省略", present: false},
		{name: "明确清空", summary: summaryStringPointer(""), present: true},
		{name: "填写总结", summary: summaryStringPointer("复盘"), present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &projectModeRepository{}
			service := NewProjectService(repo, nil, 1)
			input := ProjectSave{Title: "策划", CoverEmoji: "封面", DocumentSchemaVersion: 1, Document: json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`), Revision: 1, ManualSummary: tc.summary}
			if _, err := service.Update(context.Background(), uuid.New(), uuid.New(), input); err != nil {
				t.Fatal(err)
			}
			got, exists := repo.updates["manual_summary"]
			if exists != tc.present || (exists && got != *tc.summary) {
				t.Fatalf("总结更新不符: %v %v", got, exists)
			}
		})
	}
}
func summaryStringPointer(value string) *string { return &value }
