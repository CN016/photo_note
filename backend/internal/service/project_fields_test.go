package service

import (
	"strconv"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/photo-nini/backend/internal/model"
)

// TestValidateProjectShootingKeywords 验证策划关键词去空白、去重及数量上限。
func TestValidateProjectShootingKeywords(t *testing.T) {
	shooting := model.ProjectShooting{Keywords: pq.StringArray{" 侧逆光 ", "侧逆光", "树林"}}
	if err := validateProjectShooting(&shooting); err != nil {
		t.Fatalf("valid keywords rejected: %v", err)
	}
	if len(shooting.Keywords) != 2 || shooting.Keywords[0] != "侧逆光" || shooting.Keywords[1] != "树林" {
		t.Fatalf("normalized keywords = %#v", shooting.Keywords)
	}
	tooMany := make(pq.StringArray, 21)
	for index := range tooMany {
		tooMany[index] = "关键词" + strconv.Itoa(index)
	}
	if err := validateProjectShooting(&model.ProjectShooting{Keywords: tooMany}); err == nil {
		t.Fatal("more than 20 distinct keywords should be rejected")
	}
	if err := validateProjectShooting(&model.ProjectShooting{Keywords: pq.StringArray{strings.Repeat("长", 65)}}); err == nil {
		t.Fatal("overlong keyword should be rejected")
	}
}
