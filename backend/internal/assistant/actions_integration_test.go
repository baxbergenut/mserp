package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mserp/internal/repository"
)

func TestFleetActionsAndHighRiskConfirmationIntegration(t *testing.T) {
	databaseURL := os.Getenv("ASSISTANT_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("assistant integration database is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO app_users(username,password_hash)VALUES('telegram-manager','$2a$validation')RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_managers(app_user_id)VALUES($1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO telegram_identities(app_user_id,telegram_user_id,telegram_chat_id,expires_at)VALUES($1,42,99,now()+interval '90 days')`, userID); err != nil {
		t.Fatal(err)
	}
	identity := repository.TelegramIdentity{AppUserID: userID, Username: "telegram-manager", TelegramUserID: 42, TelegramChatID: 99, LinkExpiresAt: time.Now().AddDate(0, 0, 90)}
	assistantRepo := repository.NewAssistantRepository(pool)
	fleetRepo := repository.NewFleetRepository(pool)
	executor := &ToolExecutor{repo: assistantRepo, fleet: fleetRepo}
	createArgs := json.RawMessage(`{"entity":"driver","record":{"fullName":"Action Test Driver","payType":"cpm","payRate":0.65,"active":true}}`)
	created, err := executor.Execute(ctx, identity, "create_fleet_record", createArgs, false)
	if err != nil {
		t.Fatal(err)
	}
	driver := created.Data.(repository.Driver)
	if err := assistantRepo.WriteActionAudit(ctx, userID, "create_fleet_record", createArgs, nil, driver, "success", nil); err != nil {
		t.Fatal(err)
	}
	entries, err := assistantRepo.ListAudit(ctx, 10)
	if err != nil || len(entries) == 0 {
		t.Fatalf("action audit was not listed: entries=%d err=%v", len(entries), err)
	}
	ordinaryArgs := json.RawMessage(`{"entity":"driver","id":"` + driver.ID + `","changes":{"phone":"555-0100"}}`)
	ordinary, err := executor.Execute(ctx, identity, "update_fleet_record", ordinaryArgs, false)
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.Pending != nil || ordinary.Data.(repository.Driver).Phone == nil {
		t.Fatal("ordinary patch did not execute immediately")
	}
	highArgs := json.RawMessage(`{"entity":"driver","id":"` + driver.ID + `","changes":{"payRate":0.75}}`)
	pending, err := executor.Execute(ctx, identity, "update_fleet_record", highArgs, false)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Pending == nil {
		t.Fatal("pay rate change did not require confirmation")
	}
	request, err := assistantRepo.ClaimActionRequest(ctx, pending.Pending.ID, 42, 99)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := executor.ExecuteConfirmed(ctx, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	after := confirmed.Data.(repository.Driver)
	if after.PayRate != 0.75 {
		t.Fatalf("pay rate=%v", after.PayRate)
	}
	encoded, _ := json.Marshal(after)
	if err := assistantRepo.FinishActionRequest(ctx, request, "confirmed", encoded, nil); err != nil {
		t.Fatal(err)
	}
	staleArgs := json.RawMessage(`{"entity":"driver","id":"` + driver.ID + `","changes":{"active":false}}`)
	stale, err := executor.Execute(ctx, identity, "update_fleet_record", staleArgs, false)
	if err != nil {
		t.Fatal(err)
	}
	current, err := fleetRepo.GetDriver(ctx, driver.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := driverInput(current)
	note := "changed elsewhere"
	input.Notes = &note
	if _, err := fleetRepo.UpdateDriver(ctx, driver.ID, input); err != nil {
		t.Fatal(err)
	}
	staleRequest, err := assistantRepo.ClaimActionRequest(ctx, stale.Pending.ID, 42, 99)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecuteConfirmed(ctx, identity, staleRequest); err == nil {
		t.Fatal("stale confirmation unexpectedly executed")
	}
	if err := assistantRepo.FinishActionRequest(ctx, staleRequest, "failed", []byte(`null`), errors.New("stale")); err != nil {
		t.Fatal(err)
	}
}
