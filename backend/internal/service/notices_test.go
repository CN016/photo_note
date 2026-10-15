package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/model"
)

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
		{provider: "feishu", address: "https://open.feishu.cn/open-apis/bot/v2/hook/feishu-secret", secret: "feishu-signing", wantKeys: []string{"msg_type", "content", "timestamp", "sign"}},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			config := model.NoticeConfig{Provider: test.provider, WebhookURL: test.address, SigningSecret: test.secret}
			delivery := model.NoticeDelivery{EventType: "inspiration.compression.succeeded", Payload: model.JSONB(`{"message":"灵感图图片处理已完成。"}`)}
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
