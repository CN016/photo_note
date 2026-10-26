package service

import (
	"testing"
	"time"

	"github.com/photo-nini/backend/internal/model"
)

// TestLibraryFiltersValidation 确保查询白名单拒绝伪日期、SQL 片段和不支持的分类。
func TestLibraryFiltersValidation(t *testing.T) {
	for _, f := range []model.InspirationFilters{{DateFrom: "2026-02-30"}, {DateFrom: "2026-10-28", DateTo: "2026-10-01"}, {People: "0 OR 1=1"}, {Color: "h12"}, {Project: "other-user"}, {EmptyShot: "unknown"}, {Status: "failed|succeeded"}} {
		if ValidateInspirationFilters(f) == nil {
			t.Errorf("应拒绝: %+v", f)
		}
	}
	for _, f := range []model.InspirationFilters{{}, {People: "3+", EmptyShot: "false", Color: "h11", Project: "unassigned", Status: "succeeded"}, {DateFrom: "2026-10-01", DateTo: "2026-10-31", Color: "gray"}} {
		if e := ValidateInspirationFilters(f); e != nil {
			t.Errorf("合法条件被拒绝: %v", e)
		}
	}
}

// TestDailyScanBoundary 覆盖启动、北京时间三点和次日边界。
func TestDailyScanBoundary(t *testing.T) {
	at := func(value string) time.Time {
		d, e := time.ParseInLocation("2006-01-02 15:04", value, inspirationChina)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	cases := []struct {
		initial   bool
		last, now string
		due       bool
	}{{true, "", "2026-10-27 01:00", true}, {false, "", "2026-10-27 02:59", false}, {false, "", "2026-10-27 03:00", true}, {false, "2026-10-27", "2026-10-27 03:01", false}, {false, "2026-10-27", "2026-10-28 03:00", true}}
	for _, c := range cases {
		if inspirationScanDue(c.initial, c.last, at(c.now), "03:00") != c.due {
			t.Errorf("扫描边界错误: %+v", c)
		}
	}
}
