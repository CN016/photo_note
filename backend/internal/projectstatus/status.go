// Package projectstatus 计算拍摄策划的执行状态，不依赖客户端提供的状态值。
package projectstatus

import (
	"encoding/json"
	"strings"
)

const (
	// Unfinished 表示未出现任何执行标记。
	Unfinished = "unfinished"
	// Executed 表示已有执行标记但尚无人工总结。
	Executed = "executed"
	// Completed 表示已有执行标记且人工总结包含非空文字。
	Completed = "completed"
)

// Calculate 根据已保存的 Tiptap 文档和人工总结计算状态；非法文档不产生执行标记。
// 只有 taskItem 的勾选或其正文文字的删除线计入，普通正文的格式不影响状态。
func Calculate(document []byte, summary string) string {
	var root any
	if json.Unmarshal(document, &root) != nil || !hasExecution(root, false) {
		return Unfinished
	}
	if strings.TrimSpace(summary) == "" {
		return Executed
	}
	return Completed
}

// hasExecution 递归遍历内容树。任务标记只由任务项本身的 checked 或其后代文本的 strike 判定。
func hasExecution(value any, inTask bool) bool {
	node, ok := value.(map[string]any)
	if !ok {
		return false
	}
	kind, _ := node["type"].(string)
	if kind == "taskItem" {
		inTask = true
		if attrs, ok := node["attrs"].(map[string]any); ok && attrs["checked"] == true {
			return true
		}
	}
	if inTask && kind == "text" {
		content, _ := node["text"].(string)
		if strings.TrimSpace(content) != "" {
			if marks, ok := node["marks"].([]any); ok {
				for _, item := range marks {
					if mark, ok := item.(map[string]any); ok && mark["type"] == "strike" {
						return true
					}
				}
			}
		}
	}
	if children, ok := node["content"].([]any); ok {
		for _, child := range children {
			if hasExecution(child, inTask) {
				return true
			}
		}
	}
	return false
}
