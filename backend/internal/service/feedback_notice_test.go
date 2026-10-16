package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
)

type feedbackNoticeRepositoryTest struct {
	NoticeRepository
	eventKey string
	payload  model.JSONB
	parts    []string
	calls    int
}

func (repository *feedbackNoticeRepositoryTest) CreateFeedbackNotification(_ context.Context, _ uuid.UUID, _ uuid.UUID, eventKey string, payload model.JSONB, parts []string) error {
	repository.eventKey = eventKey
	repository.payload = append(model.JSONB(nil), payload...)
	repository.parts = append([]string(nil), parts...)
	repository.calls++
	return nil
}

// TestPublishFeedbackSubmittedKeepsFullPayloadAndStableIdempotencyKey 验证通知载荷完整且重试使用固定事件键。
func TestPublishFeedbackSubmittedKeepsFullPayloadAndStableIdempotencyKey(t *testing.T) {
	repository := &feedbackNoticeRepositoryTest{}
	notices := NewNoticeService(repository, nil)
	feedbackID := uuid.New()
	userID := uuid.New()
	reporter := FeedbackReporter{Nickname: "反馈用户", Phone: "13800138000"}
	content := "完整反馈正文\n包含第二行"
	imageURL := "https://photos.example/api/v1/public-feedback-images/public_local_20261016_123e4567-e89b-12d3-a456-426614174000.png"
	for attempt := 0; attempt < 2; attempt++ {
		if err := notices.PublishFeedbackSubmitted(context.Background(), userID, feedbackID, reporter, content, []string{imageURL}); err != nil {
			t.Fatal(err)
		}
	}
	if repository.calls != 2 || repository.eventKey != "feedback.submitted:"+feedbackID.String() {
		t.Fatalf("feedback event key was not stable: calls=%d key=%q", repository.calls, repository.eventKey)
	}
	var payload struct {
		Text      string   `json:"text"`
		Markdown  string   `json:"markdown"`
		ImageURLs []string `json:"image_urls"`
	}
	if err := json.Unmarshal(repository.payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Text != content || !strings.Contains(payload.Markdown, "![反馈图片1]("+imageURL+")") || len(payload.ImageURLs) != 1 || payload.ImageURLs[0] != imageURL {
		t.Fatalf("feedback notification payload lost content or image URL: %+v", payload)
	}
	if strings.Join(repository.parts, "") != payload.Markdown {
		t.Fatal("webhook parts do not reconstruct the complete feedback Markdown")
	}
}
