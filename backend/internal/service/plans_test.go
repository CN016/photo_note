package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/photo-nini/backend/internal/domain/fault"
)

type planEmojiConfig struct{ values []PublicConfigValue }

func (config planEmojiConfig) ListPublicConfigValues(context.Context, []string) ([]PublicConfigValue, error) {
	return config.values, nil
}

type planImageFiles struct{ content []byte }

func (files planImageFiles) Open(string) (io.ReadCloser, int64, string, error) {
	return io.NopCloser(bytes.NewReader(files.content)), int64(len(files.content)), "image/png", nil
}

func TestNormalizePlanFieldsDefaultsAndLimitsReminders(t *testing.T) {
	fields := PlanFields{
		Title: "  预约体检  ", StartsAt: time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC),
		CoverEmoji: "🩺",
	}
	clean, err := normalizePlanFields(fields)
	if err != nil {
		t.Fatalf("valid plan fields were rejected: %v", err)
	}
	if clean.Title != "预约体检" || len(clean.ReminderOffsets) != 1 || clean.ReminderOffsets[0] != 60 {
		t.Fatalf("unexpected normalized plan defaults: %#v", clean)
	}

	fields.ReminderOffsets = []int{1440, 30, 5}
	clean, err = normalizePlanFields(fields)
	if err != nil {
		t.Fatalf("three distinct reminder offsets should be accepted: %v", err)
	}
	if !samePlanReminderOffsets(clean.ReminderOffsets, []int{5, 30, 1440}) {
		t.Fatalf("reminder offsets should have a stable order: %#v", clean.ReminderOffsets)
	}
	fields.ReminderOffsets = []int{5, 30, 60, 120}
	if _, err := normalizePlanFields(fields); err == nil {
		t.Fatal("more than three reminders should be rejected")
	}
	fields.ReminderOffsets = []int{30, 30}
	if _, err := normalizePlanFields(fields); err == nil {
		t.Fatal("duplicate reminder offsets should be rejected")
	}
}

func TestValidatePlanCoverUsesCurrentPublicEmojiConfiguration(t *testing.T) {
	ctx := context.Background()
	service := &PlanService{coverEmojis: planEmojiConfig{values: []PublicConfigValue{{Key: "plans.cover_emojis", Value: `["📌","🩺"]`}}}}
	if err := service.validatePlanCover(ctx, "🩺", ""); err != nil {
		t.Fatalf("configured emoji should be accepted: %v", err)
	}
	if err := service.validatePlanCover(ctx, "🎉", ""); err == nil {
		t.Fatal("emoji removed from public config should be rejected")
	}
	if err := service.validatePlanCover(ctx, "🩺", "local_20261019_00000000-0000-4000-8000-000000000000.jpg"); err == nil {
		t.Fatal("mutually exclusive cover values should be rejected")
	} else if !fault.Is(err, 400, "invalid_request") {
		t.Fatalf("unexpected invalid cover error: %v", err)
	}
}

func TestValidatePlanCoverChecksPrivateStoreKeyAndImageContents(t *testing.T) {
	var content bytes.Buffer
	testImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	testImage.Set(0, 0, color.RGBA{R: 40, G: 90, B: 150, A: 255})
	if err := png.Encode(&content, testImage); err != nil {
		t.Fatalf("encode test image: %v", err)
	}
	const imageKey = "local_20261019_00000000-0000-4000-8000-000000000000.png"
	service := &PlanService{files: planImageFiles{content: content.Bytes()}, maxBytes: 1024}
	if err := service.validatePlanCover(context.Background(), "", imageKey); err != nil {
		t.Fatalf("valid private PNG cover should be accepted: %v", err)
	}
	if err := service.validatePlanCover(context.Background(), "", "public_"+imageKey); err == nil {
		t.Fatal("public file storekey should be rejected")
	}
	service.files = planImageFiles{content: []byte("not an image")}
	if err := service.validatePlanCover(context.Background(), "", imageKey); err == nil {
		t.Fatal("invalid image contents should be rejected")
	}
}
