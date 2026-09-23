package telegramexpense

import (
	"context"
	"testing"
	"time"

	"mserp/internal/gemini"
	"mserp/internal/repository"
)

type testStore struct {
	enqueued int
	chatID   int64
}

func (s *testStore) EnqueueTelegramUpdate(_ context.Context, _, chatID, _ int64, _ string, _ []byte) (bool, error) {
	s.enqueued++
	s.chatID = chatID
	return true, nil
}
func (*testStore) ClaimTelegramUpdate(context.Context) (*repository.TelegramExpenseUpdate, error) {
	return nil, nil
}
func (*testStore) RetryTelegramUpdate(context.Context, int64, error) error   { return nil }
func (*testStore) IgnoreTelegramUpdate(context.Context, int64, string) error { return nil }
func (*testStore) CompleteTelegramExpense(context.Context, int64, repository.TelegramExpenseDraft) (string, error) {
	return "", nil
}

type testDownloader struct{}

func (testDownloader) DownloadFile(context.Context, string) ([]byte, error) { return nil, nil }

type testExtractor struct{}

func (testExtractor) ExtractExpense(context.Context, gemini.ExpenseInput) (gemini.ExpenseExtraction, error) {
	return gemini.ExpenseExtraction{}, nil
}

func TestAcceptUpdateOnlyQueuesAllowedGroupMessages(t *testing.T) {
	store := &testStore{}
	service := NewService(store, testDownloader{}, testExtractor{}, []int64{-1007}, time.UTC, nil)
	payload := []byte(`{"update_id":11,"message":{"message_id":22,"date":1,"chat":{"id":-1007,"type":"supergroup"},"text":"Scale $15 truck 101"}}`)

	accepted, err := service.AcceptUpdate(context.Background(), payload)
	if err != nil || !accepted {
		t.Fatalf("AcceptUpdate() = %v, %v; want true, nil", accepted, err)
	}
	if store.enqueued != 1 || store.chatID != -1007 {
		t.Fatalf("queued = %d chat = %d", store.enqueued, store.chatID)
	}

	private := []byte(`{"update_id":12,"message":{"message_id":23,"chat":{"id":9,"type":"private"},"text":"$10"}}`)
	accepted, err = service.AcceptUpdate(context.Background(), private)
	if err != nil || accepted || store.enqueued != 1 {
		t.Fatalf("private AcceptUpdate() = %v, %v; queued=%d", accepted, err, store.enqueued)
	}
}

func TestAcceptUpdateSkipsCaptionlessAlbumSiblings(t *testing.T) {
	store := &testStore{}
	service := NewService(store, testDownloader{}, testExtractor{}, nil, time.UTC, nil)
	payload := []byte(`{"update_id":11,"message":{"message_id":22,"media_group_id":"album","chat":{"id":-7,"type":"group"},"photo":[{"file_id":"x","file_size":5}]}}`)

	accepted, err := service.AcceptUpdate(context.Background(), payload)
	if err != nil || accepted || store.enqueued != 0 {
		t.Fatalf("AcceptUpdate() = %v, %v; queued=%d", accepted, err, store.enqueued)
	}
}

func TestBuildDraftDefaultsAndNormalizes(t *testing.T) {
	amount := "$1,234.5"
	badDate := "not-a-date"
	category := "Maintenance"
	draft, reason := buildDraft(gemini.ExpenseExtraction{
		IsExpense: true, Confidence: 0.9, Amount: &amount, ExpenseDate: &badDate, Category: &category,
	}, gemini.ExpenseInput{Text: "oil change", MessageDate: time.Date(2026, 9, 23, 4, 0, 0, 0, time.UTC)})
	if reason != "" {
		t.Fatal(reason)
	}
	if draft.Amount != "1234.50" || draft.Company != "MS Express" || draft.Category != "Maintenance" {
		t.Fatalf("draft = %#v", draft)
	}
	if draft.ExpenseDate.Format(time.DateOnly) != "2026-09-23" || draft.CoveredBy == nil || *draft.CoveredBy != "Company" {
		t.Fatalf("draft defaults = %#v", draft)
	}
}
