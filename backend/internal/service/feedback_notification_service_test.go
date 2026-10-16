package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
)

type feedbackNotificationRepositoryTest struct {
	userID     uuid.UUID
	lookupErr  error
	lookedUp   string
	lookupHits int
}

func (*feedbackNotificationRepositoryTest) ClaimFeedbackNotificationJob(context.Context) (model.FeedbackNotificationJob, model.Feedback, bool, error) {
	return model.FeedbackNotificationJob{}, model.Feedback{}, false, nil
}

func (*feedbackNotificationRepositoryTest) CompleteFeedbackNotificationJob(context.Context, uuid.UUID, int, string, string) error {
	return nil
}

func (repository *feedbackNotificationRepositoryTest) FindUserBusinessIDByPhone(_ context.Context, phone string) (uuid.UUID, error) {
	repository.lookedUp = phone
	repository.lookupHits++
	return repository.userID, repository.lookupErr
}

type feedbackNotificationSettingsTest struct {
	enabled bool
	phone   string
	baseURL string
	err     error
}

func (settings feedbackNotificationSettingsTest) FeedbackNotificationSettings(context.Context) (bool, string, string, error) {
	return settings.enabled, settings.phone, settings.baseURL, settings.err
}

type feedbackNotificationPublisherTest struct {
	userID    uuid.UUID
	feedback  uuid.UUID
	content   string
	imageURLs []string
	err       error
	calls     int
}

func (publisher *feedbackNotificationPublisherTest) PublishFeedbackSubmitted(_ context.Context, userID, feedbackID uuid.UUID, content string, imageURLs []string) error {
	publisher.calls++
	publisher.userID = userID
	publisher.feedback = feedbackID
	publisher.content = content
	publisher.imageURLs = append([]string(nil), imageURLs...)
	return publisher.err
}

// TestFeedbackNotificationProcessHandlesDisabledMissingAndSuccessfulRecipients 验证开关、收件人和图片 URL 的任务状态。
func TestFeedbackNotificationProcessHandlesDisabledMissingAndSuccessfulRecipients(t *testing.T) {
	feedbackID := uuid.New()
	userID := uuid.New()
	publicKey := "public_local_20261016_123e4567-e89b-12d3-a456-426614174000.png"
	feedback := model.Feedback{
		BaseModel: model.BaseModel{BusinessID: feedbackID},
		Content:   "完整反馈正文",
		ImageKeys: pq.StringArray{publicKey},
	}

	t.Run("disabled", func(t *testing.T) {
		repository := &feedbackNotificationRepositoryTest{}
		publisher := &feedbackNotificationPublisherTest{}
		worker := NewFeedbackNotificationService(repository, feedbackNotificationSettingsTest{}, publisher)
		outcome, code := worker.process(context.Background(), feedback)
		if outcome != "skipped" || code != "notifications_disabled" || repository.lookupHits != 0 || publisher.calls != 0 {
			t.Fatalf("disabled notification result = %s/%s lookup=%d publish=%d", outcome, code, repository.lookupHits, publisher.calls)
		}
	})

	t.Run("recipient missing", func(t *testing.T) {
		repository := &feedbackNotificationRepositoryTest{lookupErr: fault.NotFound()}
		publisher := &feedbackNotificationPublisherTest{}
		worker := NewFeedbackNotificationService(repository, feedbackNotificationSettingsTest{enabled: true, phone: "13800138000", baseURL: "https://photos.example"}, publisher)
		outcome, code := worker.process(context.Background(), feedback)
		if outcome != "failed" || code != "recipient_not_found" || publisher.calls != 0 {
			t.Fatalf("missing recipient result = %s/%s publish=%d", outcome, code, publisher.calls)
		}
	})

	t.Run("published", func(t *testing.T) {
		repository := &feedbackNotificationRepositoryTest{userID: userID}
		publisher := &feedbackNotificationPublisherTest{}
		worker := NewFeedbackNotificationService(repository, feedbackNotificationSettingsTest{enabled: true, phone: "13800138000", baseURL: "https://photo.016imcn.love/back/"}, publisher)
		outcome, code := worker.process(context.Background(), feedback)
		if outcome != "succeeded" || code != "" || publisher.calls != 1 || publisher.userID != userID || publisher.feedback != feedbackID || publisher.content != feedback.Content {
			t.Fatalf("published feedback result = %s/%s publisher=%+v", outcome, code, publisher)
		}
		wantURL := "https://photo.016imcn.love/back/api/v1/public-feedback-images/" + publicKey
		if len(publisher.imageURLs) != 1 || publisher.imageURLs[0] != wantURL {
			t.Fatalf("feedback image URL = %v, want %s", publisher.imageURLs, wantURL)
		}
		if repository.lookedUp != "13800138000" {
			t.Fatalf("dedicated feedback phone lookup = %q", repository.lookedUp)
		}
	})
}

// TestFeedbackNotificationProcessRejectsPrivateImagesAndRetriesTransientFailures 验证私有图片 key 拒绝发布且临时错误进入重试。
func TestFeedbackNotificationProcessRejectsPrivateImagesAndRetriesTransientFailures(t *testing.T) {
	settings := feedbackNotificationSettingsTest{enabled: true, phone: "13800138000", baseURL: "https://photos.example"}
	userID := uuid.New()
	privateFeedback := model.Feedback{BaseModel: model.BaseModel{BusinessID: uuid.New()}, ImageKeys: pq.StringArray{"local_20261016_123e4567-e89b-12d3-a456-426614174000.png"}}
	privatePublisher := &feedbackNotificationPublisherTest{}
	privateWorker := NewFeedbackNotificationService(&feedbackNotificationRepositoryTest{userID: userID}, settings, privatePublisher)
	if outcome, code := privateWorker.process(context.Background(), privateFeedback); outcome != "failed" || code != "public_image_key_invalid" || privatePublisher.calls != 0 {
		t.Fatalf("private feedback image result = %s/%s publish=%d", outcome, code, privatePublisher.calls)
	}

	feedback := model.Feedback{BaseModel: model.BaseModel{BusinessID: uuid.New()}, Content: "retry me"}
	transientPublisher := &feedbackNotificationPublisherTest{err: errors.New("database unavailable")}
	transientWorker := NewFeedbackNotificationService(&feedbackNotificationRepositoryTest{userID: userID}, settings, transientPublisher)
	outcome, code := transientWorker.process(context.Background(), feedback)
	if outcome != "retry" || code != "notification_publish_failed" {
		t.Fatalf("transient publish result = %s/%s", outcome, code)
	}
	if strings.Contains(code, "database unavailable") {
		t.Fatal("notification task error code exposed a low-level error")
	}
}
