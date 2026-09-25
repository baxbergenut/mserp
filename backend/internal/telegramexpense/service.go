package telegramexpense

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"mime"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mserp/internal/gemini"
	"mserp/internal/repository"
	"mserp/internal/telegram"
)

var decimalPattern = regexp.MustCompile(`^[0-9]{1,12}(?:\.[0-9]{1,2})?$`)

type Store interface {
	EnqueueTelegramUpdate(context.Context, int64, int64, int64, string, []byte) (bool, error)
	ClaimTelegramUpdate(context.Context) (*repository.TelegramExpenseUpdate, error)
	RetryTelegramUpdate(context.Context, int64, error) error
	IgnoreTelegramUpdate(context.Context, int64, string) error
	ReviewTelegramUpdate(context.Context, int64, string) error
	StoreTelegramExtraction(context.Context, int64, json.RawMessage) error
	CompleteTelegramExpenses(context.Context, int64, []repository.TelegramExpenseDraft) ([]string, error)
}

type FileDownloader interface {
	DownloadFile(context.Context, string) ([]byte, error)
}

type Service struct {
	store        Store
	telegram     FileDownloader
	extractor    gemini.ExpenseExtractor
	allowedChats map[int64]struct{}
	location     *time.Location
	logger       *slog.Logger
}

func NewService(
	store Store,
	telegramClient FileDownloader,
	extractor gemini.ExpenseExtractor,
	allowedChatIDs []int64,
	location *time.Location,
	logger *slog.Logger,
) *Service {
	allowed := make(map[int64]struct{}, len(allowedChatIDs))
	for _, chatID := range allowedChatIDs {
		allowed[chatID] = struct{}{}
	}
	if location == nil {
		location = time.UTC
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store: store, telegram: telegramClient, extractor: extractor,
		allowedChats: allowed, location: location, logger: logger,
	}
}

func (s *Service) AcceptUpdate(ctx context.Context, payload []byte) (bool, error) {
	var update telegram.Update
	if err := json.Unmarshal(payload, &update); err != nil {
		return false, errors.New("invalid Telegram update JSON")
	}
	message := update.Message
	if update.UpdateID == 0 || message == nil || message.MessageID == 0 {
		return false, nil
	}
	if message.Chat.Type != "group" && message.Chat.Type != "supergroup" {
		return false, nil
	}
	if len(s.allowedChats) > 0 {
		if _, ok := s.allowedChats[message.Chat.ID]; !ok {
			return false, nil
		}
	}
	if message.TextContent() == "" && message.Document == nil && message.LargestPhoto() == nil {
		return false, nil
	}
	return s.store.EnqueueTelegramUpdate(
		ctx, update.UpdateID, message.Chat.ID, message.MessageID, message.Chat.Type, payload,
	)
}

func (s *Service) Run(ctx context.Context, workers int) {
	if workers < 1 {
		workers = 1
	}
	for worker := 0; worker < workers; worker++ {
		go s.runWorker(ctx)
	}
}

func (s *Service) runWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		processed, err := s.processNext(ctx)
		if err != nil {
			s.logger.Error("process Telegram expense update", "error", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) processNext(ctx context.Context) (bool, error) {
	update, err := s.store.ClaimTelegramUpdate(ctx)
	if err != nil || update == nil {
		return false, err
	}
	processCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := s.process(processCtx, update); err != nil {
		if retryErr := s.store.RetryTelegramUpdate(context.WithoutCancel(ctx), update.UpdateID, err); retryErr != nil {
			return true, fmt.Errorf("%v; record retry: %w", err, retryErr)
		}
		return true, err
	}
	return true, nil
}

func (s *Service) process(ctx context.Context, queued *repository.TelegramExpenseUpdate) error {
	var update telegram.Update
	if err := json.Unmarshal(queued.Payload, &update); err != nil {
		return err
	}
	message := update.Message
	if message == nil {
		return s.store.IgnoreTelegramUpdate(ctx, queued.UpdateID, "update has no message")
	}
	if message.MediaGroupID != "" && strings.TrimSpace(message.Caption) == "" {
		return s.store.ReviewTelegramUpdate(
			ctx,
			queued.UpdateID,
			"Captionless media-album item needs review so a multi-image receipt is not duplicated or partially imported",
		)
	}

	input := gemini.ExpenseInput{Text: message.TextContent()}
	if message.Date > 0 {
		input.MessageDate = time.Unix(message.Date, 0).In(s.location)
	} else {
		input.MessageDate = time.Now().In(s.location)
	}
	if message.Document != nil {
		mimeType := supportedMIME(message.Document.MIMEType, message.Document.FileName)
		if mimeType == "" {
			return s.store.ReviewTelegramUpdate(ctx, queued.UpdateID, "Unsupported document type needs manual review")
		}
		data, err := s.telegram.DownloadFile(ctx, message.Document.FileID)
		if err != nil {
			return err
		}
		input.MIMEType, input.FileName, input.FileData = mimeType, message.Document.FileName, data
	} else if photo := message.LargestPhoto(); photo != nil {
		data, err := s.telegram.DownloadFile(ctx, photo.FileID)
		if err != nil {
			return err
		}
		input.MIMEType, input.FileName, input.FileData = "image/jpeg", "telegram-photo.jpg", data
	}

	extraction, err := s.extractor.ExtractExpense(ctx, input)
	if err != nil {
		return err
	}
	extractedData, err := json.Marshal(extraction)
	if err != nil {
		return err
	}
	if err := s.store.StoreTelegramExtraction(ctx, queued.UpdateID, extractedData); err != nil {
		return err
	}
	if !extraction.IsExpense {
		return s.store.IgnoreTelegramUpdate(ctx, queued.UpdateID, "Gemini did not identify an expense")
	}
	if len(extraction.Expenses) == 0 {
		return s.store.ReviewTelegramUpdate(ctx, queued.UpdateID, "Gemini identified an expense but returned no expense records")
	}
	if len(extraction.Expenses) > 25 {
		return s.store.ReviewTelegramUpdate(ctx, queued.UpdateID, "Gemini returned more than 25 expenses for one message")
	}
	drafts := make([]repository.TelegramExpenseDraft, 0, len(extraction.Expenses))
	for index, extractedExpense := range extraction.Expenses {
		if extractedExpense.Confidence < 0.40 || extractedExpense.Confidence > 1 {
			return s.store.ReviewTelegramUpdate(ctx, queued.UpdateID, fmt.Sprintf("Expense %d has low extraction confidence", index+1))
		}
		draft, reason := buildDraft(extractedExpense, input)
		if reason != "" {
			return s.store.ReviewTelegramUpdate(ctx, queued.UpdateID, fmt.Sprintf("Expense %d: %s", index+1, reason))
		}
		drafts = append(drafts, draft)
	}
	expenseIDs, err := s.store.CompleteTelegramExpenses(ctx, queued.UpdateID, drafts)
	if err != nil {
		return err
	}
	s.logger.Info("Telegram expenses created", "update_id", queued.UpdateID, "expense_count", len(expenseIDs), "expense_ids", expenseIDs)
	return nil
}

func buildDraft(extraction gemini.ExpenseItem, input gemini.ExpenseInput) (repository.TelegramExpenseDraft, string) {
	amount, ok := normalizeAmount(extraction.Amount)
	if !ok {
		return repository.TelegramExpenseDraft{}, "expense amount was missing or invalid"
	}
	expenseDate := input.MessageDate
	if value := clean(extraction.ExpenseDate); value != nil {
		parsed, err := time.Parse(time.DateOnly, *value)
		if err == nil {
			expenseDate = parsed
		}
	}
	company := "MS Express"
	if value := clean(extraction.Company); value != nil && (*value == "MS Express" || *value == "Flinn Corp") {
		company = *value
	}
	category := "Other"
	if value := clean(extraction.Category); value != nil {
		switch *value {
		case "Maintenance", "Other", "Safety", "HR", "Administrative":
			category = *value
		}
	}
	description := clean(extraction.Description)
	if description == nil {
		description = truncated(input.Text, 500)
	}
	coveredBy := clean(extraction.CoveredBy)
	if coveredBy == nil {
		coveredBy = stringPointer("Company")
	}
	return repository.TelegramExpenseDraft{
		Company: company, Category: category, ExpenseDate: expenseDate,
		UnitNumber: clean(extraction.UnitNumber), DriverName: clean(extraction.DriverName), Amount: amount,
		PaymentType: clean(extraction.PaymentType), ExpenseType: clean(extraction.ExpenseType),
		ReferenceNumber: clean(extraction.ReferenceNumber), Description: description,
		CoveredBy: coveredBy, PaidBy: clean(extraction.PaidBy),
	}, ""
}

func normalizeAmount(value *string) (string, bool) {
	value = clean(value)
	if value == nil {
		return "", false
	}
	normalized := strings.NewReplacer("$", "", ",", "", " ", "").Replace(*value)
	if !decimalPattern.MatchString(normalized) {
		return "", false
	}
	rational, ok := new(big.Rat).SetString(normalized)
	if !ok || rational.Sign() < 0 {
		return "", false
	}
	return rational.FloatString(2), true
}

func supportedMIME(mimeType, fileName string) string {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = strings.ToLower(mime.TypeByExtension(filepath.Ext(fileName)))
	}
	switch mimeType {
	case "application/pdf", "image/jpeg", "image/png", "image/webp", "text/plain":
		return mimeType
	default:
		return ""
	}
}

func clean(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func stringPointer(value string) *string { return &value }

func truncated(value string, limit int) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return &value
}
