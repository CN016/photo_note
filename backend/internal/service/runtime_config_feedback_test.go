package service

import (
	"context"
	"errors"
	"testing"
)

type feedbackSettingsConfigReaderTest struct {
	values map[string]string
	err    error
}

func (reader feedbackSettingsConfigReaderTest) ReadConfigValues(context.Context, []string) (map[string]string, error) {
	return reader.values, reader.err
}

func (feedbackSettingsConfigReaderTest) ReadPublicConfigValues(context.Context, []string) (map[string]string, error) {
	return nil, nil
}

// TestFeedbackNotificationSettingsValidatesPrivateConfiguration 验证默认关闭、专用手机号和允许路径前缀的 HTTPS 公共地址校验。
func TestFeedbackNotificationSettingsValidatesPrivateConfiguration(t *testing.T) {
	t.Run("missing setting defaults disabled", func(t *testing.T) {
		service := NewRuntimeConfigService(feedbackSettingsConfigReaderTest{})
		enabled, phone, baseURL, err := service.FeedbackNotificationSettings(context.Background())
		if err != nil || enabled || phone != "" || baseURL != "" {
			t.Fatalf("missing feedback notification settings = %t/%q/%q error=%v", enabled, phone, baseURL, err)
		}
	})

	t.Run("valid enabled settings", func(t *testing.T) {
		service := NewRuntimeConfigService(feedbackSettingsConfigReaderTest{values: map[string]string{
			"feedback.notify_admin": "true", "feedback.admin_phone": "13800138000", "http.public_base_url": "https://photo.016imcn.love/back",
		}})
		enabled, phone, baseURL, err := service.FeedbackNotificationSettings(context.Background())
		if err != nil || !enabled || phone != "13800138000" || baseURL != "https://photo.016imcn.love/back" {
			t.Fatalf("valid feedback notification settings = %t/%q/%q error=%v", enabled, phone, baseURL, err)
		}
	})

	for name, values := range map[string]map[string]string{
		"invalid phone":     {"feedback.notify_admin": "true", "feedback.admin_phone": "138-0013-8000", "http.public_base_url": "https://photos.example"},
		"base URL query":    {"feedback.notify_admin": "true", "feedback.admin_phone": "13800138000", "http.public_base_url": "https://photos.example/back?host=bad"},
		"base URL fragment": {"feedback.notify_admin": "true", "feedback.admin_phone": "13800138000", "http.public_base_url": "https://photos.example/back#fragment"},
	} {
		t.Run(name, func(t *testing.T) {
			service := NewRuntimeConfigService(feedbackSettingsConfigReaderTest{values: values})
			if _, _, _, err := service.FeedbackNotificationSettings(context.Background()); !errors.Is(err, ErrFeedbackNotificationConfigInvalid) {
				t.Fatalf("invalid setting error = %v, want configuration classification", err)
			}
		})
	}

	t.Run("reader error remains transient", func(t *testing.T) {
		service := NewRuntimeConfigService(feedbackSettingsConfigReaderTest{err: errors.New("database unavailable")})
		if _, _, _, err := service.FeedbackNotificationSettings(context.Background()); err == nil || errors.Is(err, ErrFeedbackNotificationConfigInvalid) {
			t.Fatalf("reader error was not kept distinct from invalid values: %v", err)
		}
	})
}

// TestFeedbackReplyPublicBaseURLDoesNotDependOnFeedbackSMSSettings 验证回复图片只依赖私有 HTTPS 公共地址配置。
func TestFeedbackReplyPublicBaseURLDoesNotDependOnFeedbackSMSSettings(t *testing.T) {
	service := NewRuntimeConfigService(feedbackSettingsConfigReaderTest{values: map[string]string{
		"http.public_base_url": "https://photos.example/proxy/",
	}})
	baseURL, err := service.FeedbackReplyPublicBaseURL(context.Background())
	if err != nil || baseURL != "https://photos.example/proxy" {
		t.Fatalf("reply public base URL = %q, error=%v", baseURL, err)
	}

	for _, invalid := range []string{"http://photos.example", "https://photos.example/path?token=secret", "https://user:pass@photos.example"} {
		service := NewRuntimeConfigService(feedbackSettingsConfigReaderTest{values: map[string]string{"http.public_base_url": invalid}})
		if _, err := service.FeedbackReplyPublicBaseURL(context.Background()); !errors.Is(err, ErrFeedbackNotificationConfigInvalid) {
			t.Fatalf("invalid public base URL %q was not rejected safely: %v", invalid, err)
		}
	}
}
