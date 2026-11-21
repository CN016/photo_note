package store

import (
	"strings"
	"testing"
)

// TestInspirationColorDistanceExpression 确保任意色值只进入数值参数，并明确限制前三种代表色。
func TestInspirationColorDistanceExpression(t *testing.T) {
	expression, ok := inspirationColorDistance("#3A7BD5")
	if !ok {
		t.Fatal("合法色值未生成排序表达式")
	}
	for _, fragment := range []string{"position <= 3", "- 58", "- 123", "- 213", "color_palette"} {
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
