package service

import (
	"strings"
	"testing"
)

// TestNormalizeTagNamesTrimsAndDeduplicatesExactNames 验证标签名按首尾空格清理后精确去重。
func TestNormalizeTagNamesTrimsAndDeduplicatesExactNames(t *testing.T) {
	names, err := normalizeTagNames([]string{" 旅行 ", "旅行", "Photo", "photo"})
	if err != nil {
		t.Fatalf("normalizeTagNames returned error: %v", err)
	}
	want := []string{"旅行", "Photo", "photo"}
	if len(names) != len(want) {
		t.Fatalf("normalized names = %#v, want %#v", names, want)
	}
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("normalized names = %#v, want %#v", names, want)
		}
	}
}

// TestNormalizeTagNamesRejectsEmptyAndOverlongNames 验证标签名不能为空且最多包含 64 个 Unicode 字符。
func TestNormalizeTagNamesRejectsEmptyAndOverlongNames(t *testing.T) {
	for _, name := range []string{"   ", strings.Repeat("标", 65)} {
		if _, err := normalizeTagNames([]string{name}); err == nil {
			t.Fatalf("normalizeTagNames(%q) should reject the name", name)
		}
	}
}
