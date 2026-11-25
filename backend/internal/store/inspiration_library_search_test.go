package store

import (
	"strings"
	"testing"
)

// TestInspirationColorDistanceExpression 确认匹配分数按代表色占比汇总，且接受全量五色画像。
func TestInspirationColorDistanceExpression(t *testing.T) {
	expression, ok := inspirationColorDistance("#3A7BD5")
	if !ok {
		t.Fatal("合法色值未生成排序表达式")
	}
	for _, fragment := range []string{"percentage", "SUM(", "585225.0", "- 58", "- 123", "- 213", "color_palette"} {
		if !strings.Contains(expression, fragment) {
			t.Fatalf("排序表达式缺少 %q: %s", fragment, expression)
		}
	}
	for _, invalid := range []string{"", "3A7BD5", "#3A7BD", "#3A7BDZ", "#3A7BD5 OR 1=1"} {
		if _, ok := inspirationColorDistance(invalid); ok {
			t.Fatalf("非法色值接受了排序: %q", invalid)
		}
	}
}
