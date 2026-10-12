package service

// Page 表示 repository 查询结果的统一分页结构，API handler 映射 DTO 时只暴露业务字段。
type Page[T any] struct {
	Items    []T
	Page     int
	PageSize int
	Total    int64
}

// PageRequest 携带经过 HTTP 层范围校验的页号、偏移和每页数量，service 不接受原始查询字符串。
type PageRequest struct {
	Page     int
	PageSize int
	Offset   int
}
