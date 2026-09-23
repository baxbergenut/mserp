package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxDownloadBytes = 20 << 20

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

type PhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MIMEType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type Message struct {
	MessageID    int64       `json:"message_id"`
	MediaGroupID string      `json:"media_group_id"`
	Date         int64       `json:"date"`
	Chat         Chat        `json:"chat"`
	Text         string      `json:"text"`
	Caption      string      `json:"caption"`
	Photo        []PhotoSize `json:"photo"`
	Document     *Document   `json:"document"`
}

type Update struct {
	UpdateID      int64    `json:"update_id"`
	Message       *Message `json:"message"`
	EditedMessage *Message `json:"edited_message"`
}

func (u Update) ExpenseMessage() *Message {
	if u.Message != nil {
		return u.Message
	}
	return u.EditedMessage
}

func (m Message) TextContent() string {
	if strings.TrimSpace(m.Caption) != "" {
		return strings.TrimSpace(m.Caption)
	}
	return strings.TrimSpace(m.Text)
}

func (m Message) LargestPhoto() *PhotoSize {
	if len(m.Photo) == 0 {
		return nil
	}
	largest := &m.Photo[0]
	for index := 1; index < len(m.Photo); index++ {
		candidate := &m.Photo[index]
		if candidate.FileSize > largest.FileSize ||
			(candidate.FileSize == largest.FileSize && candidate.Width*candidate.Height > largest.Width*largest.Height) {
			largest = candidate
		}
	}
	return largest
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		baseURL:    "https://api.telegram.org",
		token:      strings.TrimSpace(token),
		httpClient: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) GetMe(ctx context.Context) (User, error) {
	var user User
	if err := c.call(ctx, "getMe", nil, &user); err != nil {
		return User{}, err
	}
	if user.ID == 0 || user.Username == "" {
		return User{}, errors.New("Telegram getMe returned an invalid bot identity")
	}
	return user, nil
}

func (c *Client) SetWebhook(ctx context.Context, webhookURL, secret string) error {
	return c.call(ctx, "setWebhook", map[string]any{
		"url":                  webhookURL,
		"secret_token":         secret,
		"allowed_updates":      []string{"message"},
		"drop_pending_updates": false,
		"max_connections":      4,
	}, nil)
}

func (c *Client) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	var file struct {
		FilePath string `json:"file_path"`
		FileSize int64  `json:"file_size"`
	}
	if err := c.call(ctx, "getFile", map[string]string{"file_id": fileID}, &file); err != nil {
		return nil, err
	}
	if file.FilePath == "" {
		return nil, errors.New("Telegram getFile returned no file path")
	}
	if file.FileSize > maxDownloadBytes {
		return nil, fmt.Errorf("Telegram file is too large: %d bytes", file.FileSize)
	}
	requestURL := strings.TrimRight(c.baseURL, "/") + "/file/bot" + c.token + "/" +
		strings.TrimLeft(file.FilePath, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, errors.New("create Telegram file request failed")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, errors.New("Telegram file download failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Telegram file download failed: %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, errors.New("read Telegram file failed")
	}
	if len(data) > maxDownloadBytes {
		return nil, errors.New("Telegram file exceeds the 20 MB download limit")
	}
	return data, nil
}

func (c *Client) call(ctx context.Context, method string, payload any, result any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	endpoint := strings.TrimRight(c.baseURL, "/") + "/bot" + url.PathEscape(c.token) + "/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("create Telegram %s request failed", method)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("Telegram %s request failed", method)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram %s failed: %s", method, response.Status)
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode Telegram %s response: %w", method, err)
	}
	if !envelope.OK {
		return fmt.Errorf("Telegram %s rejected request: %s", method, envelope.Description)
	}
	if result != nil && len(envelope.Result) != 0 {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode Telegram %s result: %w", method, err)
		}
	}
	return nil
}
