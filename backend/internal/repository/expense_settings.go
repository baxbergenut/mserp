package repository

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrExpenseSettingConflict = errors.New("expense setting changed; reload and try again")
var ErrExpenseSettingInvalid = errors.New("invalid expense setting")

type ExpenseSetting struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	CategoryID *string `json:"categoryId"`
	Name       string  `json:"name"`
	Active     bool    `json:"active"`
	Version    int     `json:"version"`
}

func (r *ExpenseRepository) ListExpenseSettings(ctx context.Context) ([]ExpenseSetting, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,kind,category_id,name,active,version FROM expense_settings ORDER BY lower(name),id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExpenseSetting{}
	for rows.Next() {
		var v ExpenseSetting
		if err = rows.Scan(&v.ID, &v.Kind, &v.CategoryID, &v.Name, &v.Active, &v.Version); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (r *ExpenseRepository) SaveExpenseSetting(ctx context.Context, id string, input ExpenseSetting) (ExpenseSetting, error) {
	input.Name = strings.Join(strings.Fields(input.Name), " ")
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 100 {
		return ExpenseSetting{}, ErrExpenseSettingInvalid
	}
	switch input.Kind {
	case "category", "name", "payment_method", "payer":
	default:
		return ExpenseSetting{}, ErrExpenseSettingInvalid
	}
	if (input.Kind == "name") != (input.CategoryID != nil) {
		return ExpenseSetting{}, ErrExpenseSettingInvalid
	}
	var row pgx.Row
	if id == "" {
		row = r.pool.QueryRow(ctx, `INSERT INTO expense_settings(kind,category_id,name,active) VALUES($1,$2,$3,$4) RETURNING id,kind,category_id,name,active,version`, input.Kind, input.CategoryID, input.Name, input.Active)
	} else {
		row = r.pool.QueryRow(ctx, `UPDATE expense_settings SET category_id=$3,name=$4,active=$5,version=version+1 WHERE id=$1 AND kind=$2 AND version=$6 RETURNING id,kind,category_id,name,active,version`, id, input.Kind, input.CategoryID, input.Name, input.Active, input.Version)
	}
	var result ExpenseSetting
	err := row.Scan(&result.ID, &result.Kind, &result.CategoryID, &result.Name, &result.Active, &result.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrExpenseSettingConflict
	}
	return result, err
}
