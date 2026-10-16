package repository

import (
	"context"
	"time"
)

type BoardPresence struct {
	ClientID  string    `json:"clientId"`
	UserID    string    `json:"userId"`
	UserName  string    `json:"userName"`
	DriverID  string    `json:"driverId"`
	Column    string    `json:"column"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (r *DriverBoardRepository) Presence(ctx context.Context) ([]BoardPresence, error) {
	result := []BoardPresence{}
	rows, err := r.pool.Query(ctx, `SELECT p.client_id,p.user_id,u.username,p.driver_id,p.column_key,p.expires_at
 FROM driver_board_presence p JOIN app_users u ON u.id=p.user_id JOIN auth_sessions s ON s.token_hash=p.session_hash
 JOIN app_roles role ON role.id=u.role_id JOIN drivers d ON d.id=p.driver_id
 WHERE p.expires_at>now() AND s.expires_at>now() AND u.active AND d.active
 AND (role.system_role OR 'driver_board.read'=ANY(role.permissions)) ORDER BY u.username,p.client_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p BoardPresence
		if err = rows.Scan(&p.ClientID, &p.UserID, &p.UserName, &p.DriverID, &p.Column, &p.ExpiresAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *DriverBoardRepository) SetPresence(ctx context.Context, client, user, session, driver, column string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM driver_board_presence WHERE expires_at<=now()`); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `INSERT INTO driver_board_presence(client_id,user_id,session_hash,driver_id,column_key,expires_at)
 SELECT $1,$2,$3,$4,$5,now()+interval '30 seconds' WHERE EXISTS(SELECT 1 FROM drivers WHERE id=$4 AND active)
 AND EXISTS(SELECT 1 FROM auth_sessions WHERE token_hash=$3 AND user_id=$2 AND expires_at>now())
 ON CONFLICT(client_id) DO UPDATE SET driver_id=EXCLUDED.driver_id,column_key=EXCLUDED.column_key,expires_at=EXCLUDED.expires_at
 WHERE driver_board_presence.user_id=EXCLUDED.user_id AND driver_board_presence.session_hash=EXCLUDED.session_hash`, client, user, session, driver, column)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrDriverBoardConflict
	}
	return tx.Commit(ctx)
}

func (r *DriverBoardRepository) ClearPresence(ctx context.Context, client, user, session string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM driver_board_presence WHERE client_id=$1 AND user_id=$2 AND session_hash=$3`, client, user, session)
	return err
}
