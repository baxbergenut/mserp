package repository

import (
	"context"
	"time"
)

type GrossBoardCell struct {
	DriverID  string `json:"driverId"`
	WeekStart string `json:"weekStart"`
	Date      string `json:"date"`
	Slot      int    `json:"slot"`
	Column    string `json:"column"`
}
type GrossBoardPresence struct {
	GrossBoardCell
	ClientID  string    `json:"clientId"`
	UserID    string    `json:"userId"`
	UserName  string    `json:"userName"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (r *GrossBoardRepository) Presence(ctx context.Context, week time.Time) ([]GrossBoardPresence, error) {
	result := []GrossBoardPresence{}
	rows, err := r.pool.Query(ctx, `SELECT p.client_id,p.user_id,u.username,p.driver_id,p.week_start::text,coalesce(p.cell_date::text,''),p.slot,p.column_key,p.expires_at
 FROM gross_board_presence p JOIN app_users u ON u.id=p.user_id JOIN auth_sessions s ON s.token_hash=p.session_hash
 JOIN app_roles role ON role.id=u.role_id WHERE p.week_start=$1 AND p.expires_at>now() AND s.expires_at>now() AND u.active
 AND (role.system_role OR 'board.read'=ANY(role.permissions)) ORDER BY u.username,p.client_id`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p GrossBoardPresence
		if err = rows.Scan(&p.ClientID, &p.UserID, &p.UserName, &p.DriverID, &p.WeekStart, &p.Date, &p.Slot, &p.Column, &p.ExpiresAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func (r *GrossBoardRepository) SetPresence(ctx context.Context, client, user, session string, cell GrossBoardCell) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM gross_board_presence WHERE expires_at<=now()`); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `INSERT INTO gross_board_presence(client_id,user_id,session_hash,driver_id,week_start,cell_date,slot,column_key,expires_at)
 SELECT $1,$2,$3,$4,$5::date,NULLIF($6,'')::date,$7,$8,now()+interval '30 seconds'
 WHERE EXISTS(SELECT 1 FROM drivers WHERE id=$4) AND EXISTS(SELECT 1 FROM auth_sessions WHERE token_hash=$3 AND user_id=$2 AND expires_at>now())
 ON CONFLICT(client_id) DO UPDATE SET driver_id=EXCLUDED.driver_id,week_start=EXCLUDED.week_start,cell_date=EXCLUDED.cell_date,slot=EXCLUDED.slot,column_key=EXCLUDED.column_key,expires_at=EXCLUDED.expires_at
 WHERE gross_board_presence.user_id=EXCLUDED.user_id AND gross_board_presence.session_hash=EXCLUDED.session_hash`, client, user, session, cell.DriverID, cell.WeekStart, cell.Date, cell.Slot, cell.Column)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrGrossBoardConflict
	}
	return tx.Commit(ctx)
}
func (r *GrossBoardRepository) ClearPresence(ctx context.Context, client, user, session string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM gross_board_presence WHERE client_id=$1 AND user_id=$2 AND session_hash=$3`, client, user, session)
	return err
}
