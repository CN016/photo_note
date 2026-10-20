package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/photo-nini/backend/internal/service"
)

// TestProjectListPageResponseUsesLowercaseFields 验证项目列表分页 JSON 与 Web API 字段约定一致。
func TestProjectListPageResponseUsesLowercaseFields(t *testing.T) {
	response := projectListPageResponse(service.Page[service.ProjectSummary]{
		Items: []service.ProjectSummary{}, Page: 1, PageSize: 20, Total: 0,
	})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("序列化项目分页响应失败：%v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("解析项目分页响应失败：%v", err)
	}
	for _, name := range []string{"items", "page", "page_size", "total"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("项目分页响应缺少字段 %q：%s", name, encoded)
		}
	}
	if _, ok := fields["Items"]; ok {
		t.Errorf("项目分页响应不应暴露 Go 字段名：%s", encoded)
	}
}
