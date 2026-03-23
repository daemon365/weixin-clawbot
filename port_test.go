package weixin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMarkdownToPlainText(t *testing.T) {
	t.Parallel()

	in := "Hello **world**\n\n[link](https://example.com)\n\n```go\nfmt.Println(1)\n```"
	out := MarkdownToPlainText(in)
	if !strings.Contains(out, "Hello world") {
		t.Fatalf("unexpected markdown conversion: %q", out)
	}
	if !strings.Contains(out, "link") {
		t.Fatalf("expected link text to remain: %q", out)
	}
	if !strings.Contains(out, "fmt.Println(1)") {
		t.Fatalf("expected code content to remain: %q", out)
	}
}

func TestAESECBRoundTrip(t *testing.T) {
	t.Parallel()

	key := []byte("1234567890abcdef")
	plain := []byte("hello weixin")
	ciphertext, err := EncryptAESECB(plain, key)
	if err != nil {
		t.Fatalf("EncryptAESECB returned error: %v", err)
	}
	got, err := DecryptAESECB(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptAESECB returned error: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("unexpected decrypted plaintext: %q", got)
	}
}

func TestSessionGuard(t *testing.T) {
	t.Parallel()
	resetSessionGuardForTest()

	if err := AssertSessionActive("acc"); err != nil {
		t.Fatalf("expected active session, got %v", err)
	}
	PauseSession("acc")
	if !IsSessionPaused("acc") {
		t.Fatalf("expected session to be paused")
	}
	if err := AssertSessionActive("acc"); err == nil {
		t.Fatalf("expected paused session error")
	}
}

func TestSyncBufferPersistence(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "acc.sync.json")
	if err := SaveSyncBuffer(path, "cursor-1"); err != nil {
		t.Fatalf("SaveSyncBuffer returned error: %v", err)
	}
	got, err := LoadSyncBuffer(path)
	if err != nil {
		t.Fatalf("LoadSyncBuffer returned error: %v", err)
	}
	if got != "cursor-1" {
		t.Fatalf("unexpected sync buffer: %q", got)
	}
}

func TestGetUpdatesTimeoutReturnsEmptyResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret":             0,
			"msgs":            []any{},
			"get_updates_buf": "cursor-next",
		})
	}))
	defer server.Close()

	api := NewAPIClient(APIOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	resp, err := api.GetUpdates(context.Background(), GetUpdatesRequest{GetUpdatesBuf: "cursor-prev"}, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("GetUpdates returned error: %v", err)
	}
	if resp.Ret != 0 {
		t.Fatalf("expected ret=0, got %d", resp.Ret)
	}
	if resp.GetUpdatesBuf != "cursor-prev" {
		t.Fatalf("expected previous cursor on timeout, got %q", resp.GetUpdatesBuf)
	}
}
