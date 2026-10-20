package service

import (
	"reflect"
	"testing"
)

// TestProjectSearchNormalization 验证全角字符、小写和空白折叠后短中文仍保持连续可检索。
func TestProjectSearchNormalization(t *testing.T) {
	got := normalizeProjectText("  ＡＢＣ　项目  ")
	if got != "abc 项目" {
		t.Fatalf("规范化结果不符：got %q", got)
	}
	grams := projectNGrams("项目")
	want := []string{"项", "项目", "目"}
	if !reflect.DeepEqual(grams, want) && !(containsAll(grams, want) && len(grams) == len(want)) {
		t.Fatalf("中文一元/二元 n-gram 不符：got %v", grams)
	}
}

// TestProjectAttachmentHeaderValidation 拒绝仅靠文件扩展名伪装的 PDF/Office 附件。
func TestProjectAttachmentHeaderValidation(t *testing.T) {
	valid := []struct {
		name   string
		header []byte
		media  string
	}{
		{"guide.pdf", []byte("%PDF-1.7"), "application/pdf"},
		{"guide.docx", []byte("PK\x03\x04office"), "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"sheet.xlsx", []byte("PK\x03\x04office"), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	}
	for _, test := range valid {
		media, ok := identifyProjectAttachment(test.name, test.header)
		if !ok || media != test.media {
			t.Errorf("%s 类型识别失败：media=%q ok=%v", test.name, media, ok)
		}
	}
	if _, ok := identifyProjectAttachment("fake.pdf", []byte("<html>")); ok {
		t.Fatal("HTML 内容不应通过 PDF 文件头校验")
	}
	if _, ok := identifyProjectAttachment("fake.docx", []byte("not a zip")); ok {
		t.Fatal("伪装为 DOCX 的普通文本不应通过 Office 容器校验")
	}
}

func containsAll(values, expected []string) bool {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	for _, value := range expected {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}
