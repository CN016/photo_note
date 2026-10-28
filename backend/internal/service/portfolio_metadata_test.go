package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPortfolioMetadataForPublic 验证公开白名单过滤敏感与未来新增字段。
func TestPortfolioMetadataForPublic(t *testing.T) {
	raw := json.RawMessage(`{"focal_length_mm":35,"iso_speed":400,"gps_latitude":31,"camera_serial_number":"secret","extra":{"XMP:Secret":"secret"},"future_sensitive":"secret"}`)
	view, err := PortfolioMetadataForPublic(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"focal_length_mm":35`) || !strings.Contains(text, `"iso_speed":400`) {
		t.Fatalf("拍摄参数缺失: %s", text)
	}
	if strings.Contains(text, "secret") || strings.Contains(text, "gps") || strings.Contains(text, "extra") {
		t.Fatalf("公开属性泄露: %s", text)
	}
	if _, err := PortfolioMetadataForPublic(json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("无效数据必须返回错误")
	}
}
