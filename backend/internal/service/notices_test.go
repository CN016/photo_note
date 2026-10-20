package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
)

// notificationListTestRepository 只提供消息列表序列化回归检查所需的两项读取能力。
type notificationListTestRepository struct {
	NoticeRepository
	items      []model.Notification
	deliveries []model.NoticeDelivery
}

func (repository notificationListTestRepository) ListNotifications(context.Context, uuid.UUID, bool, int, int) ([]model.Notification, int64, int64, error) {
	return repository.items, int64(len(repository.items)), 0, nil
}

func (repository notificationListTestRepository) ListNotificationDeliveries(context.Context, uuid.UUID, []uuid.UUID) ([]model.NoticeDelivery, error) {
	return repository.deliveries, nil
}

// TestListNotificationsReturnsEmptyDeliveryArray 防止无 webhook 投递的消息再次编码成 JSON null。
func TestListNotificationsReturnsEmptyDeliveryArray(t *testing.T) {
	repository := notificationListTestRepository{
		items: []model.Notification{{
			BaseModel: model.BaseModel{BusinessID: uuid.New(), CreatedAt: time.Now().UTC()},
			EventType: "inspiration.compression.succeeded", Payload: model.JSONB(`{"async_version":1}`),
		}},
	}
	service := NewNoticeService(repository, nil)

	page, err := service.ListNotifications(context.Background(), uuid.New(), false, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"deliveries":[]`) {
		t.Fatalf("notifications without webhook deliveries must serialize deliveries as an empty array: %s", encoded)
	}
}

func TestValidateNoticeConfigRequiresOfficialHTTPSWebhook(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		address  string
		secret   string
		wantErr  bool
	}{
		{name: "wecom", provider: "wecom", address: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=token"},
		{name: "dingtalk", provider: "dingtalk", address: "https://oapi.dingtalk.com/robot/send?access_token=token", secret: "signing"},
		{name: "feishu", provider: "feishu", address: "https://open.feishu.cn/open-apis/bot/v2/hook/token", secret: "signing"},
		{name: "reject plain http", provider: "wecom", address: "http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=token", wantErr: true},
		{name: "reject lookalike host", provider: "wecom", address: "https://qyapi.weixin.qq.com.attacker.example/cgi-bin/webhook/send?key=token", wantErr: true},
		{name: "reject nonstandard port", provider: "dingtalk", address: "https://oapi.dingtalk.com:8443/robot/send?access_token=token", wantErr: true},
		{name: "reject unexpected path", provider: "feishu", address: "https://open.feishu.cn/other/token", wantErr: true},
		{name: "reject wecom secret", provider: "wecom", address: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=token", secret: "unsupported", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateNoticeConfig(test.provider, "test", test.address, test.secret)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateNoticeConfig() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestProviderRequestUsesPlatformSpecificPayloadsAndSignatures(t *testing.T) {
	tests := []struct {
		provider string
		address  string
		secret   string
		wantKeys []string
	}{
		{provider: "wecom", address: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=wecom-secret", wantKeys: []string{"msgtype", "markdown"}},
		{provider: "dingtalk", address: "https://oapi.dingtalk.com/robot/send?access_token=ding-secret", secret: "ding-signing", wantKeys: []string{"msgtype", "markdown"}},
		{provider: "feishu", address: "https://open.feishu.cn/open-apis/bot/v2/hook/feishu-secret", secret: "feishu-signing", wantKeys: []string{"msg_type", "card", "timestamp", "sign"}},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			config := model.NoticeConfig{Provider: test.provider, WebhookURL: test.address, SigningSecret: test.secret}
			delivery := model.NoticeDelivery{EventType: "inspiration.compression.succeeded", Payload: model.JSONB(`{"message":"灵感库图片处理已完成。"}`)}
			address, body, err := providerRequest(config, delivery)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(address)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			for _, key := range test.wantKeys {
				if _, ok := payload[key]; !ok {
					t.Errorf("payload is missing provider field %q", key)
				}
			}
			if test.provider == "dingtalk" {
				timestamp := parsed.Query().Get("timestamp")
				signature := parsed.Query().Get("sign")
				if timestamp == "" || signature == "" || signature != signHMAC(test.secret, timestamp+"\n"+test.secret) {
					t.Fatal("DingTalk request signature does not match the configured key")
				}
			}
			if test.provider == "feishu" {
				var timestamp string
				if err := json.Unmarshal(payload["timestamp"], &timestamp); err != nil || timestamp == "" {
					t.Fatal("Feishu signature timestamp is missing")
				}
				var signature string
				if err := json.Unmarshal(payload["sign"], &signature); err != nil || signature == "" {
					t.Fatal("Feishu signature is missing")
				}
				mac := hmac.New(sha256.New, []byte(timestamp+"\n"+test.secret))
				if expected := base64.StdEncoding.EncodeToString(mac.Sum(nil)); signature != expected {
					t.Fatal("Feishu signature does not match the configured key")
				}
			}
			if strings.Contains(string(body), "wecom-secret") || strings.Contains(string(body), "ding-secret") || strings.Contains(string(body), "feishu-secret") {
				t.Fatal("provider payload leaked webhook credentials")
			}
		})
	}
}

// TestFeedbackWebhookMarkdownEscapingAndSplitting 保证输入不触发 Markdown 提及，且分段不拆 UTF-8 或图片链接。
func TestFeedbackWebhookMarkdownEscapingAndSplitting(t *testing.T) {
	imageURL := "https://images.example/api/v1/public-feedback-images/public_local_20261016_123e4567-e89b-12d3-a456-426614174000.png"
	markdown, err := feedbackMarkdown(FeedbackReporter{Nickname: "测试用户", Phone: "13800138000"}, "![伪造](https://attacker.example) <at user_id=\"all\">\n"+strings.Repeat("反馈内容", 900), []string{imageURL})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markdown, "![伪造]") || strings.Contains(markdown, "<at user_id=\"all\">") {
		t.Fatal("user feedback must remain plain text in webhook Markdown")
	}
	parts, err := splitFeedbackMarkdown(markdown, feedbackWebhookPartLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 || strings.Join(parts, "") != markdown {
		t.Fatalf("feedback Markdown was not fully preserved across parts: parts=%d", len(parts))
	}
	for _, part := range parts {
		if len(part) > feedbackWebhookPartLimit || !utf8.ValidString(part) {
			t.Fatalf("webhook part exceeds byte limit or has invalid UTF-8: bytes=%d", len(part))
		}
	}
	imageMarkdown := "![反馈图片1](" + imageURL + ")"
	imagePartCount := 0
	for _, part := range parts {
		if strings.Contains(part, imageMarkdown) {
			imagePartCount++
		}
		if strings.Contains(part, "![反馈图片1](") && !strings.Contains(part, imageMarkdown) {
			t.Fatal("generated image Markdown link was cut across webhook parts")
		}
	}
	if imagePartCount != 1 {
		t.Fatalf("generated image reference appears in %d parts, want 1", imagePartCount)
	}

	boundaryContent := strings.Repeat("x", feedbackWebhookPartLimit-1) + "\\*tail"
	boundaryMarkdown := escapeFeedbackMarkdown(boundaryContent)
	boundaryParts, err := splitFeedbackMarkdown(boundaryMarkdown, feedbackWebhookPartLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaryParts) != 2 || boundaryParts[0] != strings.Repeat("x", feedbackWebhookPartLimit-1) || !strings.HasPrefix(boundaryParts[1], strings.Repeat("\\", 3)+"*tail") {
		t.Fatalf("Markdown escape pair was split at a chunk boundary: %#v", boundaryParts)
	}
}

// TestFeedbackFeishuRequestUsesInteractiveMarkdownCard 检查反馈提交与管理员回复都保留飞书卡片中的图片链接。
func TestFeedbackFeishuRequestUsesInteractiveMarkdownCard(t *testing.T) {
	config := model.NoticeConfig{Provider: "feishu", WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/token"}
	for _, eventType := range []string{"feedback.submitted", "feedback.replied"} {
		delivery := model.NoticeDelivery{
			EventType: eventType,
			Payload:   model.JSONB(`{"title":"反馈中心通知（1/1）","markdown":"反馈正文\\n\\n![反馈图片1](https://images.example/image.png)\\n[查看反馈图片1](https://images.example/image.png)"}`),
		}
		_, body, err := providerRequest(config, delivery)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			MessageType string `json:"msg_type"`
			Card        struct {
				Schema string `json:"schema"`
				Body   struct {
					Elements []struct {
						Tag     string `json:"tag"`
						Content string `json:"content"`
					} `json:"elements"`
				} `json:"body"`
			} `json:"card"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.MessageType != "interactive" || payload.Card.Schema != "2.0" || len(payload.Card.Body.Elements) != 1 || payload.Card.Body.Elements[0].Tag != "markdown" || strings.Contains(payload.Card.Body.Elements[0].Content, "![反馈图片1]") || !strings.Contains(payload.Card.Body.Elements[0].Content, "[查看反馈图片1]") {
			t.Fatalf("Feishu card for %s did not preserve a clickable feedback image link: %+v", eventType, payload)
		}
	}
}

func TestNoticeConfigViewMasksWebhookCredentials(t *testing.T) {
	config := model.NoticeConfig{
		BaseModel:     model.BaseModel{BusinessID: uuid.New()},
		Provider:      "feishu",
		Name:          "primary",
		WebhookURL:    "https://open.feishu.cn/open-apis/bot/v2/hook/private-token",
		SigningSecret: "private-signing-key",
	}
	encoded, err := json.Marshal(noticeConfigView(config))
	if err != nil {
		t.Fatal(err)
	}
	for _, privateValue := range []string{"private-token", "private-signing-key", "/open-apis"} {
		if strings.Contains(string(encoded), privateValue) {
			t.Fatalf("config response leaked private value %q", privateValue)
		}
	}
}

func TestNoticeHTTPClientDoesNotFollowRedirects(t *testing.T) {
	followed := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	service := NewNoticeService(nil, nil)
	response, err := service.client.Get(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound || followed {
		t.Fatalf("redirect was followed: status=%d followed=%t", response.StatusCode, followed)
	}
}
