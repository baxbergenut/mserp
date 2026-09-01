package telegram

import (
	"context"
	"strings"
	"testing"
)

func TestUpdateIdentityPrivateMessage(t *testing.T) {
	update := Update{UpdateID: 1, Message: &Message{From: &User{ID: 42}, Chat: Chat{ID: 99, Type: "private"}}}
	user, chat, private := update.Identity()
	if !private || user == nil || chat == nil || *user != 42 || *chat != 99 {
		t.Fatalf("unexpected identity: %v %v %v", user, chat, private)
	}
	update.Message.Chat.Type = "group"
	_, _, private = update.Identity()
	if private {
		t.Fatal("group must not be private")
	}
}

func TestCallbackIdentityRequiresPrivateMessage(t *testing.T) {
	update := Update{CallbackQuery: &CallbackQuery{From: User{ID: 42}, Message: &Message{Chat: Chat{ID: 99, Type: "private"}}}}
	user, chat, private := update.Identity()
	if !private || *user != 42 || *chat != 99 {
		t.Fatal("callback identity was not resolved")
	}
}

func TestNetworkErrorsDoNotExposeBotToken(t *testing.T) {
	client := NewClient("super-secret-token")
	client.baseURL = "http://127.0.0.1:1"
	err := client.SendMessage(context.Background(), 1, "hello", nil)
	if err == nil {
		t.Fatal("expected network error")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("bot token leaked in error: %v", err)
	}
}
