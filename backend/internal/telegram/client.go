package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

func (u User) DisplayName() string { return strings.TrimSpace(u.FirstName + " " + u.LastName) }

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

func (u Update) Identity() (userID, chatID *int64, private bool) {
	if u.Message != nil && u.Message.From != nil {
		return &u.Message.From.ID, &u.Message.Chat.ID, u.Message.Chat.Type == "private"
	}
	if u.CallbackQuery != nil && u.CallbackQuery.Message != nil {
		return &u.CallbackQuery.From.ID, &u.CallbackQuery.Message.Chat.ID,
			u.CallbackQuery.Message.Chat.Type == "private"
	}
	return nil, nil, false
}

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}
type InlineKeyboard struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func (c *Client) GetMe(ctx context.Context) (User, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(c.baseURL, "/")+"/bot"+c.token+"/getMe", nil)
	if err != nil {
		return User{}, errors.New("create Telegram getMe request failed")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return User{}, errors.New("Telegram getMe request failed")
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var result struct {
		OK          bool   `json:"ok"`
		Result      User   `json:"result"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return User{}, err
	}
	if !result.OK || result.Result.Username == "" {
		return User{}, fmt.Errorf("Telegram getMe failed: %s", result.Description)
	}
	return result.Result, nil
}

func NewClient(token string) *Client {
	return &Client{baseURL: "https://api.telegram.org", token: strings.TrimSpace(token),
		httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) call(ctx context.Context, method string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.baseURL, "/")+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Telegram %s request failed", method)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("Telegram %s request failed", method)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram %s failed: %s", method, response.Status)
	}
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("Telegram %s rejected request: %s", method, result.Description)
	}
	return nil
}

func (c *Client) SetWebhook(ctx context.Context, url, secret string) error {
	return c.call(ctx, "setWebhook", map[string]any{
		"url": url, "secret_token": secret, "allowed_updates": []string{"message", "callback_query"},
		"drop_pending_updates": false, "max_connections": 4,
	})
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, keyboard *InlineKeyboard) error {
	if strings.TrimSpace(text) == "" {
		text = "I couldn't produce a response."
	}
	for len(text) > 4096 {
		cut := 4096
		if index := strings.LastIndex(text[:cut], "\n"); index > 2000 {
			cut = index
		}
		if err := c.SendMessage(ctx, chatID, text[:cut], nil); err != nil {
			return err
		}
		text = strings.TrimSpace(text[cut:])
	}
	payload := map[string]any{"chat_id": chatID, "text": text}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	return c.call(ctx, "sendMessage", payload)
}

func (c *Client) AnswerCallback(ctx context.Context, callbackID, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": callbackID, "text": text})
}

func (c *Client) SendDocument(ctx context.Context, chatID int64, filename string, data []byte, caption string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", fmt.Sprint(chatID))
	_ = writer.WriteField("caption", caption)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="document"; filename="%s"`, strings.ReplaceAll(filename, `"`, "")))
	header.Set("Content-Type", "text/csv")
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.baseURL, "/")+"/bot"+c.token+"/sendDocument", &body)
	if err != nil {
		return errors.New("create Telegram sendDocument request failed")
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := c.httpClient.Do(request)
	if err != nil {
		return errors.New("Telegram sendDocument request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram sendDocument failed: %s", response.Status)
	}
	return nil
}
