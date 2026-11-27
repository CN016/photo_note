package store

import (
	"strings"
	"testing"
)

// TestInspirationColorScoreExpression 确认匹配分数按色相、饱和度与明度综合计算。
func TestInspirationColorScoreExpression(t *testing.T) {
	expression, ok := inspirationColorScore("#3A7BD5")
	if !ok {
		t.Fatal("合法色值未生成排序表达式")
	}
	for _, fragment := range []string{
		"percentage", "SUM(", "0.727700", "0.835294", "214.838710",
		"60.0", "item.hue", "item.saturation", "item.value", "color_palette",
	} {
		if !strings.Contains(expression, fragment) {
			t.Fatalf("排序表达式缺少 %q: %s", fragment, expression)
		}
	}
	for _, invalid := range []string{"", "3A7BD5", "#3A7BD", "#3A7BDZ", "#3A7BD5 OR 1=1"} {
		if _, ok := inspirationColorScore(invalid); ok {
			t.Fatalf("非法色值接受了排序: %q", invalid)
		}
	}
}

// TestParseColorHSV 确认查询色转换正确；绿色和蓝色必须落在明显不同的色相区间。
func TestParseColorHSV(t *testing.T) {
	green, ok := parseColorHSV("#00a651")
	if !ok || green.hue < 140 || green.hue > 155 || green.saturation <= 0 || green.value <= 0 {
		t.Fatalf("绿色 HSV 解析错误: %+v", green)
	}
	blue, ok := parseColorHSV("#2255ee")
	if !ok || blue.hue < 215 || blue.hue > 230 || blue.saturation <= 0 || blue.value <= 0 {
		t.Fatalf("蓝色 HSV 解析错误: %+v", blue)
	}
	if green.hue == blue.hue || green.saturation <= 0.12 || blue.saturation <= 0.12 {
		t.Fatalf("绿色与蓝色色相未形成有效区分: green=%+v blue=%+v", green, blue)
	}
	gray, ok := parseColorHSV("#888888")
	if !ok || gray.saturation != 0 || gray.hue != 0 {
		t.Fatalf("中性色 HSV 解析错误: %+v", gray)
	}
}
