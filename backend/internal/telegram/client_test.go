package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientGetsIdentityAndDownloadsFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botabc:def/getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"username":"expense_bot"}}`))
		case "/botabc:def/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"receipts/a.pdf","file_size":3}}`))
		case "/file/botabc:def/receipts/a.pdf":
			_, _ = w.Write([]byte("pdf"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient("abc:def")
	client.baseURL = server.URL
	bot, err := client.GetMe(context.Background())
	if err != nil || bot.Username != "expense_bot" {
		t.Fatalf("GetMe() = %#v, %v", bot, err)
	}
	data, err := client.DownloadFile(context.Background(), "file-id")
	if err != nil || string(data) != "pdf" {
		t.Fatalf("DownloadFile() = %q, %v", data, err)
	}
}
