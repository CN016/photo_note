package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"
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

// TestNormalizeTagIDsAndLimit 验证标签 UUID 去重后最多允许 8 个。
func TestNormalizeTagIDsAndLimit(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	ids := normalizeTagIDs([]uuid.UUID{first, first, uuid.Nil, second})
	if len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatalf("normalized ids = %#v", ids)
	}
	if err := validateInspirationTagLimit(8); err != nil {
		t.Fatalf("8 labels should be accepted: %v", err)
	}
	if err := validateInspirationTagLimit(9); err == nil {
		t.Fatal("9 labels should be rejected")
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
