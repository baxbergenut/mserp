package repository

type Pagination struct {
	Page     int
	PageSize int
}

func (pagination Pagination) Normalize(total int) Pagination {
	if pagination.PageSize < 1 {
		pagination.PageSize = 25
	}
	if pagination.Page < 1 {
		pagination.Page = 1
	}
	totalPages := max(1, (total+pagination.PageSize-1)/pagination.PageSize)
	if pagination.Page > totalPages {
		pagination.Page = totalPages
	}
	return pagination
}

func (pagination Pagination) Offset() int {
	return (pagination.Page - 1) * pagination.PageSize
}

type Page[T any] struct {
	Items      []T `json:"items"`
	Total      int `json:"total"`
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalPages int `json:"totalPages"`
}

func NewPage[T any](items []T, total int, pagination Pagination) Page[T] {
	pagination = pagination.Normalize(total)
	return Page[T]{
		Items:      items,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: max(1, (total+pagination.PageSize-1)/pagination.PageSize),
	}
}
