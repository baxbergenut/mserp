package telegramexpense

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"mserp/internal/gemini"
	"mserp/internal/repository"
)

type testStore struct {
	enqueued      int
	chatID        int64
	reviewReason  string
	ignoredReason string
	stored        json.RawMessage
	completed     int
}

func (s *testStore) EnqueueTelegramUpdate(_ context.Context, _, chatID, _ int64, _ string, _ []byte) (bool, error) {
	s.enqueued++
	s.chatID = chatID
	return true, nil
}
func (*testStore) ClaimTelegramUpdate(context.Context) (*repository.TelegramExpenseUpdate, error) {
	return nil, nil
}
func (*testStore) RetryTelegramUpdate(context.Context, int64, error) error { return nil }
func (s *testStore) IgnoreTelegramUpdate(_ context.Context, _ int64, reason string) error {
	s.ignoredReason = reason
	return nil
}
func (s *testStore) ReviewTelegramUpdate(_ context.Context, _ int64, reason string) error {
	s.reviewReason = reason
	return nil
}
func (s *testStore) StoreTelegramExtraction(_ context.Context, _ int64, value json.RawMessage) error {
	s.stored = value
	return nil
}
func (s *testStore) CompleteTelegramExpense(context.Context, int64, repository.TelegramExpenseDraft) (string, error) {
	s.completed++
	return "", nil
}

type testDownloader struct{}

func (testDownloader) DownloadFile(context.Context, string) ([]byte, error) { return nil, nil }

type testExtractor struct{ value gemini.ExpenseExtraction }

func (extractor testExtractor) ExtractExpense(context.Context, gemini.ExpenseInput) (gemini.ExpenseExtraction, error) {
	return extractor.value, nil
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

func TestAcceptUpdateQueuesCaptionlessAlbumSiblingsForReview(t *testing.T) {
	store := &testStore{}
	service := NewService(store, testDownloader{}, testExtractor{}, nil, time.UTC, nil)
	payload := []byte(`{"update_id":11,"message":{"message_id":22,"media_group_id":"album","chat":{"id":-7,"type":"group"},"photo":[{"file_id":"x","file_size":5}]}}`)

	accepted, err := service.AcceptUpdate(context.Background(), payload)
	if err != nil || !accepted || store.enqueued != 1 {
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

func TestProcessRoutesMultipleExpensesToReview(t *testing.T) {
	store := &testStore{}
	service := NewService(store, testDownloader{}, testExtractor{value: gemini.ExpenseExtraction{
		IsExpense: true, ContainsMultipleExpenses: true, Confidence: 0.95,
	}}, nil, time.UTC, nil)
	queued := &repository.TelegramExpenseUpdate{
		UpdateID: 11,
		Payload:  json.RawMessage(`{"update_id":11,"message":{"message_id":22,"date":1,"chat":{"id":-7,"type":"group"},"text":"scale $15 and permit $30"}}`),
	}

	if err := service.process(context.Background(), queued); err != nil {
		t.Fatal(err)
	}
	if store.reviewReason == "" || store.completed != 0 || len(store.stored) == 0 {
		t.Fatalf("review=%q completed=%d stored=%s", store.reviewReason, store.completed, store.stored)
	}
}

func TestProcessIgnoresNonExpenseAfterSavingExtraction(t *testing.T) {
	store := &testStore{}
	service := NewService(store, testDownloader{}, testExtractor{}, nil, time.UTC, nil)
	queued := &repository.TelegramExpenseUpdate{
		UpdateID: 12,
		Payload:  json.RawMessage(`{"update_id":12,"message":{"message_id":23,"date":1,"chat":{"id":-7,"type":"group"},"text":"good morning"}}`),
	}

	if err := service.process(context.Background(), queued); err != nil {
		t.Fatal(err)
	}
	if store.ignoredReason == "" || store.reviewReason != "" || len(store.stored) == 0 {
		t.Fatalf("ignored=%q review=%q stored=%s", store.ignoredReason, store.reviewReason, store.stored)
	}
}
