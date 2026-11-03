package repository

import "context"

// ThemeUsage counts accounts, not sessions, and exposes no user identities.
type ThemeUsage struct {
	TotalUsers int            `json:"totalUsers"`
	Counts     map[string]int `json:"counts"`
}

func (r *AuthRepository) ThemeUsage(ctx context.Context) (ThemeUsage, error) {
	usage := ThemeUsage{Counts: map[string]int{}}
	rows, err := r.pool.Query(ctx, `SELECT theme,count(*) FROM app_users WHERE active AND role_id IS NOT NULL GROUP BY theme`)
	if err != nil {
		return usage, err
	}
	defer rows.Close()
	for rows.Next() {
		var theme string
		var count int
		if err := rows.Scan(&theme, &count); err != nil {
			return usage, err
		}
		usage.Counts[theme] = count
		usage.TotalUsers += count
	}
	return usage, rows.Err()
}
