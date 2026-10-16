package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONB 在 Go 中保留原始 JSON 对象，在 PostgreSQL 中以 jsonb 类型读写而不转成 base64 字符串。
type JSONB []byte

// Value 将 JSONB 作为文本参数交给 PostgreSQL，由列类型负责解析成 jsonb。
func (value JSONB) Value() (driver.Value, error) {
	if !json.Valid(value) {
		return nil, fmt.Errorf("JSONB 内容不是有效 JSON")
	}
	return string(value), nil
}

// Scan 复制数据库返回的 JSON 字节，避免复用底层缓冲区导致模型数据被覆盖。
func (value *JSONB) Scan(source any) error {
	switch typed := source.(type) {
	case []byte:
		if !json.Valid(typed) {
			return fmt.Errorf("PostgreSQL 返回了无效 JSONB")
		}
		*value = append((*value)[:0], typed...)
	case string:
		if !json.Valid([]byte(typed)) {
			return fmt.Errorf("PostgreSQL 返回了无效 JSONB")
		}
		*value = append((*value)[:0], typed...)
	case nil:
		*value = nil
	default:
		return fmt.Errorf("不支持将 %T 扫描为 JSONB", source)
	}
	return nil
}

// MarshalJSON 输出底层 JSON 对象本身，避免 []byte 默认编码为 base64 字符串。
func (value JSONB) MarshalJSON() ([]byte, error) {
	if !json.Valid(value) {
		return nil, fmt.Errorf("JSONB 内容不是有效 JSON")
	}
	return value, nil
}

// UnmarshalJSON 保留 API 中嵌套 JSON 对象的原始字节，避免 []byte 默认要求 Base64 字符串。
func (value *JSONB) UnmarshalJSON(input []byte) error {
	if !json.Valid(input) {
		return fmt.Errorf("JSONB 内容不是有效 JSON")
	}
	*value = append((*value)[:0], input...)
	return nil
}
