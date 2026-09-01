package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"mserp/internal/groq"
	"mserp/internal/repository"
	"mserp/internal/telegram"
)

const systemPrompt = `You are the private MSERP management assistant for MS Express Inc.
Use only the provided tools for company facts and actions. Never invent figures or claim an action succeeded without a successful tool result.
Reporting weeks are Monday through Sunday in America/New_York. "Last week" is the previous completed Monday-Sunday. "This week" is Monday through today. "Last 7 days" includes today. Always print exact resolved dates.
For every relative date phrase, call resolve_date_range and use its returned dates in reporting tools.
Fuel transaction dates use each merchant's timezone. Load reporting uses DataTruck's encoded UTC pickup calendar date. Tolls use stored dates.
For read-only questions, use a reasonable documented default and state it. For any write, ask a concise follow-up if the entity, intended fields, or values are ambiguous. Questions like "can you" or "what would happen" are not commands and must not call mutation tools.
Use IDs returned by list_fleet; never guess an entity ID. Be concise but include methodology/freshness when returned. English only.`

var errMutationCompleted = errors.New("mutation completed; do not retry update")

type ChatModel interface {
	CompleteWithTools(context.Context, []groq.AssistantMessage, []groq.AssistantTool) (groq.AssistantCompletion, error)
}

type TelegramClient interface {
	SendMessage(context.Context, int64, string, *telegram.InlineKeyboard) error
	SendDocument(context.Context, int64, string, []byte, string) error
	AnswerCallback(context.Context, string, string) error
}

type Service struct {
	repo      *repository.AssistantRepository
	tools     *ToolExecutor
	model     ChatModel
	telegram  TelegramClient
	logger    *slog.Logger
	now       func() time.Time
	rateMu    sync.Mutex
	rate      map[int64][]time.Time
	queueMu   sync.Mutex
	userLocks map[int64]*sync.Mutex
}

func NewService(repo *repository.AssistantRepository, tools *ToolExecutor, model ChatModel, client TelegramClient, logger *slog.Logger) *Service {
	return &Service{repo: repo, tools: tools, model: model, telegram: client,
		logger: logger, now: time.Now, rate: make(map[int64][]time.Time), userLocks: make(map[int64]*sync.Mutex)}
}

func (s *Service) AcceptUpdate(ctx context.Context, body []byte) (bool, error) {
	var update telegram.Update
	if err := json.Unmarshal(body, &update); err != nil {
		return false, fmt.Errorf("decode Telegram update: %w", err)
	}
	if update.UpdateID == 0 {
		return false, errors.New("Telegram update_id is required")
	}
	if exists, err := s.repo.HasUpdate(ctx, update.UpdateID); err != nil {
		return false, err
	} else if exists {
		return false, nil
	}
	userID, chatID, private := update.Identity()
	status := "queued"
	if !private || userID == nil || chatID == nil {
		status = "rejected"
	}
	if status == "queued" {
		if !s.allow(*userID) {
			return false, errors.New("Telegram rate limit exceeded")
		}
		count, err := s.repo.PendingCount(ctx, *userID)
		if err != nil {
			return false, err
		}
		if count >= 5 {
			return false, errors.New("Telegram request queue is full")
		}
	}
	return s.repo.EnqueueUpdate(ctx, update.UpdateID, userID, chatID, body, status)
}

func (s *Service) allow(userID int64) bool {
	now := s.now()
	cutoff := now.Add(-time.Minute)
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	values := s.rate[userID][:0]
	for _, value := range s.rate[userID] {
		if value.After(cutoff) {
			values = append(values, value)
		}
	}
	if len(values) >= 10 {
		s.rate[userID] = values
		return false
	}
	s.rate[userID] = append(values, now)
	return true
}

func (s *Service) Run(ctx context.Context, workers int) {
	if workers < 1 {
		workers = 1
	}
	if workers > 4 {
		workers = 4
	}
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() { defer group.Done(); s.runWorker(ctx) }()
	}
	group.Wait()
}

func (s *Service) runWorker(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		update, err := s.repo.ClaimNextUpdate(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			s.logger.Error("claim Telegram update", "error", err)
			continue
		}
		unlock := s.lockUpdate(update)
		processErr := s.processUpdate(ctx, update)
		unlock()
		status := "completed"
		if processErr != nil && update.Attempts < 5 && !errors.Is(processErr, errMutationCompleted) {
			status = "failed"
		}
		if processErr != nil && (update.Attempts >= 5 || errors.Is(processErr, errMutationCompleted)) {
			status = "rejected"
			if update.TelegramChatID != nil {
				message := "I couldn't complete that request after several attempts. No unconfirmed action was executed. Please try again later."
				if errors.Is(processErr, errMutationCompleted) {
					message = "The action completed, but I couldn't deliver the full response. Check the Telegram audit page before repeating it."
				}
				_ = s.telegram.SendMessage(ctx, *update.TelegramChatID, message, nil)
			}
		}
		if err := s.repo.FinishUpdate(ctx, update.UpdateID, status, processErr); err != nil {
			s.logger.Error("finish Telegram update", "update_id", update.UpdateID, "error", err)
		}
	}
}

func (s *Service) lockUpdate(update repository.TelegramUpdate) func() {
	if update.TelegramUserID == nil {
		return func() {}
	}
	s.queueMu.Lock()
	lock := s.userLocks[*update.TelegramUserID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.userLocks[*update.TelegramUserID] = lock
	}
	s.queueMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (s *Service) processUpdate(ctx context.Context, queued repository.TelegramUpdate) error {
	var update telegram.Update
	if err := json.Unmarshal(queued.Payload, &update); err != nil {
		return err
	}
	if update.CallbackQuery != nil {
		return s.processCallback(ctx, queued.UpdateID, update.CallbackQuery)
	}
	if update.Message == nil || update.Message.From == nil {
		return nil
	}
	message := update.Message
	text := strings.TrimSpace(message.Text)
	if strings.HasPrefix(text, "/start") {
		return s.processStart(ctx, message, text)
	}
	identity, err := s.repo.FindIdentity(ctx, message.From.ID, message.Chat.ID)
	if err != nil {
		_ = s.telegram.SendMessage(ctx, message.Chat.ID,
			"This Telegram account is not linked to an approved MSERP manager. Sign in to MSERP, open Telegram Settings, and generate a new link.", nil)
		return nil
	}
	switch strings.ToLower(strings.Fields(text)[0]) {
	case "/help":
		return s.telegram.SendMessage(ctx, message.Chat.ID, helpText, nil)
	case "/new", "/clear":
		if err := s.repo.ClearConversation(ctx, identity.AppUserID); err != nil {
			return err
		}
		return s.telegram.SendMessage(ctx, message.Chat.ID, "Conversation context cleared.", nil)
	case "/whoami":
		location, _ := time.LoadLocation("America/New_York")
		if location == nil {
			location = time.UTC
		}
		return s.telegram.SendMessage(ctx, message.Chat.ID,
			fmt.Sprintf("Linked MSERP user: %s\nAccess expires: %s", identity.Username, identity.LinkExpiresAt.In(location).Format(time.RFC1123)), nil)
	case "/unlink":
		if err := s.repo.RevokeIdentity(ctx, identity.AppUserID); err != nil {
			return err
		}
		return s.telegram.SendMessage(ctx, message.Chat.ID, "Telegram access has been unlinked. Generate a new link in MSERP to reconnect.", nil)
	}
	if text == "" {
		return s.telegram.SendMessage(ctx, message.Chat.ID, "Send a text question or command. Use /help for examples.", nil)
	}
	return s.runAssistant(ctx, queued.UpdateID, identity, text)
}

func (s *Service) processStart(ctx context.Context, message *telegram.Message, text string) error {
	parts := strings.Fields(text)
	if len(parts) != 2 {
		return s.telegram.SendMessage(ctx, message.Chat.ID, "Generate a fresh Telegram link from MSERP Settings, then open that link here.", nil)
	}
	digest := sha256.Sum256([]byte(parts[1]))
	identity, err := s.repo.LinkIdentity(ctx, hex.EncodeToString(digest[:]), message.From.ID,
		message.Chat.ID, message.From.Username, message.From.DisplayName(), s.now().AddDate(0, 0, 90))
	if err != nil {
		return s.telegram.SendMessage(ctx, message.Chat.ID, "That link is invalid, expired, or already used. Generate a new link in MSERP.", nil)
	}
	return s.telegram.SendMessage(ctx, message.Chat.ID,
		fmt.Sprintf("Linked securely to MSERP as %s. Access expires %s.\n\n%s",
			identity.Username, identity.LinkExpiresAt.Format("Jan 2, 2006"), helpText), nil)
}

func (s *Service) runAssistant(ctx context.Context, updateID int64, identity repository.TelegramIdentity, prompt string) (processErr error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stored, err := s.repo.LoadConversation(ctx, identity.AppUserID)
	if err != nil {
		return err
	}
	var history []groq.AssistantMessage
	_ = json.Unmarshal(stored, &history)
	if len(history) > 12 {
		history = history[len(history)-12:]
	}
	messages := []groq.AssistantMessage{{Role: "system", Content: systemPrompt + "\nCurrent company date: " + companyToday(s.now()).Format(time.DateOnly)}}
	messages = append(messages, history...)
	messages = append(messages, groq.AssistantMessage{Role: "user", Content: prompt})
	var calls []any
	var results []any
	var attachment *Attachment
	var pending *repository.AssistantActionRequest
	finalResponse := ""
	mutationExecuted := false
	defer func() {
		outcome := "success"
		if processErr != nil {
			outcome = "failed"
		}
		auditCtx, cancelAudit := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelAudit()
		_ = s.repo.WriteAudit(auditCtx, identity.AppUserID, updateID, "conversation", prompt, finalResponse, outcome, calls, results, processErr)
	}()
	defer func() {
		if processErr != nil && mutationExecuted {
			processErr = fmt.Errorf("%w: %v", errMutationCompleted, processErr)
		}
	}()
	for iteration := 0; iteration < 5; iteration++ {
		completion, err := s.model.CompleteWithTools(ctx, messages, ToolDefinitions())
		if err != nil {
			return err
		}
		messages = append(messages, completion.Message)
		if len(completion.Message.ToolCalls) == 0 {
			response := strings.TrimSpace(completion.Message.Content)
			finalResponse = response
			if pending != nil {
				keyboard := &telegram.InlineKeyboard{InlineKeyboard: [][]telegram.InlineButton{{
					{Text: "Confirm", CallbackData: "confirm:" + pending.ID},
					{Text: "Cancel", CallbackData: "cancel:" + pending.ID},
				}}}
				response = pending.Preview + "\n\nThis high-risk action expires in 5 minutes."
				if err := s.telegram.SendMessage(ctx, identity.TelegramChatID, response, keyboard); err != nil {
					return err
				}
			} else {
				if err := s.telegram.SendMessage(ctx, identity.TelegramChatID, response, nil); err != nil {
					return err
				}
			}
			if attachment != nil {
				if err := s.telegram.SendDocument(ctx, identity.TelegramChatID, attachment.Filename, attachment.Data, attachment.Caption); err != nil {
					return err
				}
			}
			history = append(history, groq.AssistantMessage{Role: "user", Content: prompt}, groq.AssistantMessage{Role: "assistant", Content: response})
			if len(history) > 12 {
				history = history[len(history)-12:]
			}
			encoded, _ := json.Marshal(history)
			return s.repo.SaveConversation(ctx, identity.AppUserID, encoded)
		}
		for _, call := range completion.Message.ToolCalls {
			calls = append(calls, call)
			arguments := json.RawMessage(call.Function.Arguments)
			result, err := s.tools.Execute(ctx, identity, call.Function.Name, arguments, false)
			if err != nil {
				result.Data = map[string]any{"error": err.Error()}
			}
			if result.Action && result.Pending == nil {
				outcome := "success"
				if err != nil {
					outcome = "failed"
				}
				_ = s.repo.WriteActionAudit(ctx, identity.AppUserID, call.Function.Name,
					arguments, result.Before, result.Data, outcome, err)
				if err == nil {
					mutationExecuted = true
				}
			}
			results = append(results, result.Data)
			if result.Attachment != nil {
				attachment = result.Attachment
			}
			if result.Pending != nil {
				pending = result.Pending
			}
			content, _ := json.Marshal(result.Data)
			messages = append(messages, groq.AssistantMessage{Role: "tool", ToolCallID: call.ID,
				Name: call.Function.Name, Content: string(content)})
			if result.Action && err == nil {
				formatted, _ := json.MarshalIndent(result.Data, "", "  ")
				finalResponse = "Action completed.\n" + string(formatted)
				if sendErr := s.telegram.SendMessage(ctx, identity.TelegramChatID, finalResponse, nil); sendErr != nil {
					return sendErr
				}
				history = append(history, groq.AssistantMessage{Role: "user", Content: prompt}, groq.AssistantMessage{Role: "assistant", Content: finalResponse})
				if len(history) > 12 {
					history = history[len(history)-12:]
				}
				encoded, _ := json.Marshal(history)
				return s.repo.SaveConversation(ctx, identity.AppUserID, encoded)
			}
		}
		if pending != nil {
			keyboard := &telegram.InlineKeyboard{InlineKeyboard: [][]telegram.InlineButton{{
				{Text: "Confirm", CallbackData: "confirm:" + pending.ID},
				{Text: "Cancel", CallbackData: "cancel:" + pending.ID},
			}}}
			response := pending.Preview + "\n\nThis high-risk action expires in 5 minutes."
			finalResponse = response
			if err := s.telegram.SendMessage(ctx, identity.TelegramChatID, response, keyboard); err != nil {
				return err
			}
			history = append(history, groq.AssistantMessage{Role: "user", Content: prompt}, groq.AssistantMessage{Role: "assistant", Content: response})
			if len(history) > 12 {
				history = history[len(history)-12:]
			}
			encoded, _ := json.Marshal(history)
			return s.repo.SaveConversation(ctx, identity.AppUserID, encoded)
		}
	}
	return errors.New("assistant exceeded the five-tool iteration limit")
}

func (s *Service) processCallback(ctx context.Context, updateID int64, callback *telegram.CallbackQuery) error {
	if callback.Message == nil {
		return nil
	}
	identity, err := s.repo.FindIdentity(ctx, callback.From.ID, callback.Message.Chat.ID)
	if err != nil {
		return s.telegram.AnswerCallback(ctx, callback.ID, "Access is not authorized.")
	}
	parts := strings.SplitN(callback.Data, ":", 2)
	if len(parts) != 2 {
		return s.telegram.AnswerCallback(ctx, callback.ID, "Invalid action.")
	}
	if parts[0] == "cancel" {
		if err := s.repo.CancelActionRequest(ctx, parts[1], callback.From.ID, callback.Message.Chat.ID); err != nil {
			return s.telegram.AnswerCallback(ctx, callback.ID, "This action is no longer pending.")
		}
		_ = s.telegram.AnswerCallback(ctx, callback.ID, "Cancelled.")
		return s.telegram.SendMessage(ctx, callback.Message.Chat.ID, "Action cancelled.", nil)
	}
	if parts[0] != "confirm" {
		return s.telegram.AnswerCallback(ctx, callback.ID, "Invalid action.")
	}
	request, err := s.repo.ClaimActionRequest(ctx, parts[1], callback.From.ID, callback.Message.Chat.ID)
	if err != nil {
		return s.telegram.AnswerCallback(ctx, callback.ID, "This action is expired or already handled.")
	}
	result, executeErr := s.tools.ExecuteConfirmed(ctx, identity, request)
	after, _ := json.Marshal(result.Data)
	status := "confirmed"
	if executeErr != nil {
		status = "failed"
	}
	_ = s.repo.FinishActionRequest(ctx, request, status, after, executeErr)
	if executeErr != nil {
		_ = s.telegram.AnswerCallback(ctx, callback.ID, "Action failed.")
		return s.telegram.SendMessage(ctx, callback.Message.Chat.ID, "Action failed: "+executeErr.Error(), nil)
	}
	_ = s.telegram.AnswerCallback(ctx, callback.ID, "Confirmed.")
	data, _ := json.MarshalIndent(result.Data, "", "  ")
	_ = s.repo.WriteAudit(ctx, identity.AppUserID, updateID, "confirmed_action", request.Preview,
		string(data), "success", []any{request.ActionName}, []any{result.Data}, nil)
	return s.telegram.SendMessage(ctx, callback.Message.Chat.ID, "Action completed.\n"+string(data), nil)
}

func companyToday(now time.Time) time.Time {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return now
	}
	return now.In(location)
}

const helpText = `Ask management questions naturally, for example:
• How much did John Smith spend on fuel last week?
• Show last week's company financial summary.
• List active trucks in maintenance.
• Sync fuel transactions.
• Assign driver John Smith to truck 142.

Reports use stored MSERP data and show exact dates and freshness. High-risk changes require a confirmation button.`
