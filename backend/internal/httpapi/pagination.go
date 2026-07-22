package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"mserp/internal/repository"
)

func parsePagination(r *http.Request) (repository.Pagination, error) {
	page, err := parsePositiveQueryInt(r, "page", 1)
	if err != nil {
		return repository.Pagination{}, err
	}
	pageSize, err := parsePositiveQueryInt(r, "pageSize", 25)
	if err != nil {
		return repository.Pagination{}, err
	}
	if pageSize > 100 {
		return repository.Pagination{}, fmt.Errorf("pageSize must be 100 or less")
	}
	return repository.Pagination{Page: page, PageSize: pageSize}, nil
}

func parsePositiveQueryInt(r *http.Request, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}
