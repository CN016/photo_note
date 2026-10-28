package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPortfolioMetadataForPublic 验证作者授权后的完整属性及异常输入。
func TestPortfolioMetadataForPublic(t *testing.T) {
	raw := json.RawMessage(`{"focal_length_mm":35,"gps_latitude":31,"camera_serial_number":"serial","extra":{"maker":"value"}}`)
	view, err := PortfolioMetadataForPublic(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"focal_length_mm", "gps_latitude", "camera_serial_number", "maker"} {
		if !strings.Contains(string(view), fragment) {
			t.Fatalf("公开属性缺失 %s: %s", fragment, view)
		}
	}
	view[0] = 'X'
	if raw[0] != '{' {
		t.Fatal("不得修改原始快照")
	}
	for _, invalid := range []string{"{invalid", "null", "[]"} {
		if _, err := PortfolioMetadataForPublic(json.RawMessage(invalid)); err == nil {
			t.Fatalf("无效对象 %s 必须返回错误", invalid)
		}
	}
}
