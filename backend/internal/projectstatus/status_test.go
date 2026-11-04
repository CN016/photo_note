package projectstatus

import "testing"

// TestCalculate 覆盖任务执行、空总结、普通格式及嵌套任务，防止状态误判。
func TestCalculate(t *testing.T) {
	tests := []struct{ name, document, summary, want string }{
		{"空文档", `{"type":"doc","content":[{"type":"paragraph"}]}`, "复盘", Unfinished},
		{"勾选但未总结", `{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true}}]}]}`, "  ", Executed},
		{"勾选并总结", `{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true}}]}]}`, "完成", Completed},
		{"正文删除线不计入", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"说明","marks":[{"type":"strike"}]}]}]}`, "完成", Unfinished},
		{"任务删除线", `{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":false},"content":[{"type":"paragraph","content":[{"type":"text","text":"动作","marks":[{"type":"strike"}]}]}]}]}]}`, "", Executed},
		{"空白删除线不计入", `{"type":"doc","content":[{"type":"taskItem","content":[{"type":"text","text":" ","marks":[{"type":"strike"}]}]}]}`, "", Unfinished},
		{"嵌套任务勾选", `{"type":"doc","content":[{"type":"taskItem","attrs":{"checked":false},"content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true}}]}]}]}`, "复盘", Completed},
		{"无效文档", `{`, "复盘", Unfinished},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Calculate([]byte(tt.document), tt.summary); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
