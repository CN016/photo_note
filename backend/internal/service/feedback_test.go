package service

import (
	"strings"
	"testing"
)

// TestFeedbackLimits 在不依赖数据库和 Redis 的情况下验证反馈正文及图片数量边界。
func TestFeedbackLimits(t *testing.T) {
	service := NewFeedbackService(nil, nil, 2000, 5, 5)
	if err := service.Validate(strings.Repeat("你", 2000), 0); err != nil {
		t.Fatalf("2000-character feedback rejected: %v", err)
	}
	if err := service.Validate(strings.Repeat("你", 2001), 0); err == nil {
		t.Fatal("2001-character feedback should be rejected")
	}
	if err := service.Validate("", 5); err != nil {
		t.Fatalf("five-image feedback rejected: %v", err)
	}
	if err := service.Validate("", 6); err == nil {
		t.Fatal("six-image feedback should be rejected")
	}
	if err := service.Validate("  ", 0); err == nil {
		t.Fatal("empty text and image feedback should be rejected")
	}
}
