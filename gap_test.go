package weixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type uploadStubAPI struct {
	resp GetUploadURLResponse
	err  error
}

func (s *uploadStubAPI) SendMessage(context.Context, SendMessageRequest, time.Duration) error {
	return nil
}

func (s *uploadStubAPI) GetUploadURL(context.Context, GetUploadURLRequest, time.Duration) (*GetUploadURLResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &s.resp, nil
}

func TestResolveStateDirHomeFallback(t *testing.T) {
	t.Setenv("OPENCLAW_STATE_DIR", "")
	t.Setenv("CLAWDBOT_STATE_DIR", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir returned error: %v", err)
	}
	if got := ResolveStateDir(); got != filepath.Join(home, ".openclaw") {
		t.Fatalf("unexpected home fallback: %q", got)
	}
}

func TestStorageErrorPaths(t *testing.T) {
	t.Parallel()

	if _, err := SaveAccount(t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "account is nil") {
		t.Fatalf("unexpected nil account error: %v", err)
	}
	if _, err := SaveAccount(t.TempDir(), &Account{}); err == nil || !strings.Contains(err.Error(), "account_id is empty") {
		t.Fatalf("unexpected empty account_id error: %v", err)
	}

	base := t.TempDir()
	notDir := filepath.Join(base, "not-dir")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write non-dir marker: %v", err)
	}
	if _, err := SaveAccount(filepath.Join(notDir, "child"), &Account{AccountID: "demo@im.bot"}); err == nil {
		t.Fatalf("expected SaveAccount mkdir error")
	}

	badDir := t.TempDir()
	if err := os.WriteFile(accountFilePath(badDir, "broken@im.bot"), []byte("{bad"), 0o600); err != nil {
		t.Fatalf("write broken account: %v", err)
	}
	if _, err := LoadAccount(badDir, "broken@im.bot"); err == nil {
		t.Fatalf("expected LoadAccount JSON error")
	}

	missingDir := filepath.Join(t.TempDir(), "missing")
	accounts, err := ListAccounts(missingDir)
	if err != nil {
		t.Fatalf("ListAccounts missing dir returned error: %v", err)
	}
	if accounts != nil {
		t.Fatalf("expected nil account list for missing dir, got %#v", accounts)
	}

	listDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(listDir, "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write broken list entry: %v", err)
	}
	if _, err := ListAccounts(listDir); err == nil {
		t.Fatalf("expected ListAccounts JSON error")
	}

	if err := SaveSyncBuffer(filepath.Join(notDir, "sync.json"), "buf"); err == nil {
		t.Fatalf("expected SaveSyncBuffer path error")
	}
}

func TestMarkdownToPlainTextMoreCases(t *testing.T) {
	t.Parallel()

	in := "![img](https://example.com/x)\n| a | b |\n|---|---|\n| 1 | 2 |\n~~bold~~"
	out := MarkdownToPlainText(in)
	if strings.Contains(out, "![img]") {
		t.Fatalf("expected markdown image to be removed: %q", out)
	}
	if !strings.Contains(out, "a  b") || !strings.Contains(out, "1  2") {
		t.Fatalf("expected markdown table rows to flatten: %q", out)
	}
	if strings.Contains(out, "~") {
		t.Fatalf("expected emphasis markers removed: %q", out)
	}
}

func TestSendTypingPausedSession(t *testing.T) {
	resetSessionGuardForTest()
	defer resetSessionGuardForTest()

	PauseSession("bot@im.bot")
	api := NewAPIClient(APIOptions{AccountID: "bot@im.bot"})
	err := api.SendTyping(context.Background(), SendTypingRequest{
		ILinkUserID:  "user-1",
		TypingTicket: "ticket-1",
		Status:       TypingStatusTyping,
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "session paused") {
		t.Fatalf("unexpected SendTyping paused error: %v", err)
	}
}

func TestJoinURLInvalidBase(t *testing.T) {
	t.Parallel()

	if _, err := joinURL("://bad-base", "/v1/path"); err == nil || !strings.Contains(err.Error(), "parse base URL") {
		t.Fatalf("unexpected joinURL error: %v", err)
	}
}

func TestMonitorTransportErrorCallsOnError(t *testing.T) {
	syncPath := filepath.Join(t.TempDir(), "monitor.sync.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var gotErr error
	api := NewAPIClient(APIOptions{
		BaseURL: "https://example.com",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network boom")
			}),
		},
	})

	err := Monitor(ctx, MonitorOptions{
		API:         api,
		SyncBufPath: syncPath,
		OnMessages:  func(context.Context, []WeixinMessage) error { return nil },
		OnError: func(err error) {
			gotErr = err
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected monitor error: %v", err)
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "network boom") {
		t.Fatalf("unexpected monitor transport error callback: %v", gotErr)
	}
}

func TestMonitorResponseErrorCallsOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(GetUpdatesResponse{
			Ret:                  1,
			ErrCode:              2,
			ErrMsg:               "bad update",
			LongPollingTimeoutMS: 7,
		})
	}))
	defer server.Close()

	syncPath := filepath.Join(t.TempDir(), "monitor.sync.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var gotErr error
	err := Monitor(ctx, MonitorOptions{
		API:         NewAPIClient(APIOptions{BaseURL: server.URL, HTTPClient: server.Client()}),
		SyncBufPath: syncPath,
		OnMessages:  func(context.Context, []WeixinMessage) error { return nil },
		OnError: func(err error) {
			gotErr = err
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected monitor error: %v", err)
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "getUpdates failed: ret=1 errcode=2 errmsg=bad update") {
		t.Fatalf("unexpected monitor response error callback: %v", gotErr)
	}
}

func TestMonitorSessionExpiredPausesAccount(t *testing.T) {
	resetSessionGuardForTest()
	defer resetSessionGuardForTest()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(GetUpdatesResponse{Ret: SessionExpiredErrCode})
	}))
	defer server.Close()

	syncPath := filepath.Join(t.TempDir(), "monitor.sync.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := Monitor(ctx, MonitorOptions{
		API:         NewAPIClient(APIOptions{BaseURL: server.URL, HTTPClient: server.Client()}),
		AccountID:   "bot@im.bot",
		SyncBufPath: syncPath,
		OnMessages:  func(context.Context, []WeixinMessage) error { return nil },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected monitor session-expired error: %v", err)
	}
	if !IsSessionPaused("bot@im.bot") {
		t.Fatalf("expected session to be paused")
	}
}

func TestMonitorFilteredStatusAndSaveSyncError(t *testing.T) {
	syncPath := filepath.Join(t.TempDir(), "monitor.sync.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := os.Mkdir(syncPath, 0o755); err != nil && !os.IsExist(err) {
			t.Fatalf("mkdir syncPath: %v", err)
		}
		_ = json.NewEncoder(w).Encode(GetUpdatesResponse{
			Ret:           0,
			GetUpdatesBuf: "buf-next",
			Messages: []WeixinMessage{
				{FromUserID: "blocked", ItemList: []MessageItem{{Type: MessageItemTypeText, TextItem: &TextItem{Text: "skip"}}}},
			},
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		statusCalls int
		gotErr      error
	)
	err := Monitor(ctx, MonitorOptions{
		API:         NewAPIClient(APIOptions{BaseURL: server.URL, HTTPClient: server.Client()}),
		SyncBufPath: syncPath,
		AllowFrom:   []string{"allowed"},
		OnMessages:  func(context.Context, []WeixinMessage) error { return nil },
		OnError: func(err error) {
			gotErr = err
		},
		OnStatus: func(time.Time) {
			statusCalls++
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected monitor status-loop error: %v", err)
	}
	if statusCalls == 0 {
		t.Fatalf("expected OnStatus to run for filtered-empty path")
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "directory") {
		t.Fatalf("expected sync buffer save error, got %v", gotErr)
	}
}

func TestMonitorSyncBufferLoadErrorAndStatusAfterMessages(t *testing.T) {
	t.Run("invalid sync buffer returns error", func(t *testing.T) {
		syncPath := filepath.Join(t.TempDir(), "monitor.sync.json")
		if err := os.WriteFile(syncPath, []byte("{bad"), 0o600); err != nil {
			t.Fatalf("write bad sync buffer: %v", err)
		}

		err := Monitor(context.Background(), MonitorOptions{
			API:         NewAPIClient(APIOptions{}),
			SyncBufPath: syncPath,
			OnMessages:  func(context.Context, []WeixinMessage) error { return nil },
		})
		if err == nil {
			t.Fatalf("expected sync buffer load error")
		}
	})

	t.Run("message path triggers status callback", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(GetUpdatesResponse{
				Ret: 0,
				Messages: []WeixinMessage{
					{FromUserID: "allowed", ItemList: []MessageItem{{Type: MessageItemTypeText, TextItem: &TextItem{Text: "hello"}}}},
				},
			})
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var (
			statusCalls    int
			onMessageCalls int
		)
		err := Monitor(ctx, MonitorOptions{
			API:         NewAPIClient(APIOptions{BaseURL: server.URL, HTTPClient: server.Client()}),
			SyncBufPath: filepath.Join(t.TempDir(), "monitor.sync.json"),
			AllowFrom:   []string{"allowed"},
			OnMessages: func(context.Context, []WeixinMessage) error {
				onMessageCalls++
				return nil
			},
			OnStatus: func(time.Time) {
				statusCalls++
				cancel()
			},
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected monitor status-after-message error: %v", err)
		}
		if onMessageCalls != 1 || statusCalls == 0 {
			t.Fatalf("unexpected callbacks: onMessage=%d status=%d", onMessageCalls, statusCalls)
		}
	})
}

func TestMonitorBackoffAfterConsecutiveFailures(t *testing.T) {
	syncPath := filepath.Join(t.TempDir(), "monitor.sync.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls int
	api := NewAPIClient(APIOptions{
		BaseURL: "https://example.com",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("still failing")
			}),
		},
	})

	start := time.Now()
	err := Monitor(ctx, MonitorOptions{
		API:         api,
		SyncBufPath: syncPath,
		OnMessages:  func(context.Context, []WeixinMessage) error { return nil },
		OnError: func(err error) {
			if calls >= maxConsecutiveFailures {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected monitor backoff error: %v", err)
	}
	if calls != maxConsecutiveFailures {
		t.Fatalf("expected %d failures before cancel, got %d", maxConsecutiveFailures, calls)
	}
	if time.Since(start) < 3800*time.Millisecond {
		t.Fatalf("expected retry sleeps before backoff branch, got %v", time.Since(start))
	}
}

func TestUploadBufferToCDNRetryAndMissingHeader(t *testing.T) {
	t.Parallel()

	t.Run("retry succeeds after server errors", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls < 3 {
				http.Error(w, "please retry", http.StatusBadGateway)
				return
			}
			w.Header().Set("x-encrypted-param", "download-param")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		got, err := UploadBufferToCDN(context.Background(), nil, []byte("hello"), "upload", "file", server.URL, []byte("1234567890abcdef"))
		if err != nil {
			t.Fatalf("UploadBufferToCDN returned error: %v", err)
		}
		if got != "download-param" {
			t.Fatalf("unexpected download param: %q", got)
		}
		if calls != 3 {
			t.Fatalf("expected 3 attempts, got %d", calls)
		}
	})

	t.Run("missing header exhausts retries", func(t *testing.T) {
		var calls int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		_, err := UploadBufferToCDN(context.Background(), nil, []byte("hello"), "upload", "file", server.URL, []byte("1234567890abcdef"))
		if err == nil || !strings.Contains(err.Error(), "missing x-encrypted-param header") {
			t.Fatalf("unexpected missing-header error: %v", err)
		}
		if calls != uploadMaxRetries {
			t.Fatalf("expected %d retries, got %d", uploadMaxRetries, calls)
		}
	})
}

func TestUploadBufferToCDNTransportError(t *testing.T) {
	t.Parallel()

	var calls int
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("upload transport failed")
		}),
	}

	_, err := UploadBufferToCDN(context.Background(), client, []byte("hello"), "upload", "file", "https://cdn.example.com", []byte("1234567890abcdef"))
	if err == nil || !strings.Contains(err.Error(), "upload transport failed") {
		t.Fatalf("unexpected UploadBufferToCDN transport error: %v", err)
	}
	if calls != uploadMaxRetries {
		t.Fatalf("expected %d transport retries, got %d", uploadMaxRetries, calls)
	}
}

func TestDownloadRemoteMediaAndDecryptErrorPaths(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := DownloadRemoteMediaToTemp(context.Background(), nil, server.URL, t.TempDir()); err == nil || !strings.Contains(err.Error(), "remote media download failed") {
		t.Fatalf("unexpected remote media error: %v", err)
	}
	if _, err := DownloadAndDecryptBuffer(context.Background(), server.Client(), "download-param", base64.StdEncoding.EncodeToString([]byte("short")), server.URL); err == nil || !strings.Contains(err.Error(), "aes_key must decode") {
		t.Fatalf("unexpected decrypt key error: %v", err)
	}
}

func TestDownloadRemoteMediaAndCDNTransportErrors(t *testing.T) {
	t.Parallel()

	destFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(destFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write dest file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("remote-file"))
	}))
	defer server.Close()

	if _, err := DownloadRemoteMediaToTemp(context.Background(), nil, server.URL, destFile); err == nil {
		t.Fatalf("expected destination mkdir/write error")
	}

	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("download transport failed")
		}),
	}
	if _, err := downloadCDNBytes(context.Background(), client, "https://cdn.example.com/download"); err == nil || !strings.Contains(err.Error(), "download transport failed") {
		t.Fatalf("unexpected downloadCDNBytes transport error: %v", err)
	}
}

func TestUploadMediaToCDNWithAPIErrors(t *testing.T) {
	t.Parallel()

	if _, err := uploadMediaToCDNWithAPI(context.Background(), filepath.Join(t.TempDir(), "missing.bin"), "user-1", "https://cdn.example.com", UploadMediaTypeFile, &uploadStubAPI{}, nil, 0); err == nil {
		t.Fatalf("expected file read error")
	}

	filePath := filepath.Join(t.TempDir(), "demo.bin")
	if err := os.WriteFile(filePath, []byte("demo"), 0o600); err != nil {
		t.Fatalf("write temp media file: %v", err)
	}
	if _, err := uploadMediaToCDNWithAPI(context.Background(), filePath, "user-1", "https://cdn.example.com", UploadMediaTypeFile, &uploadStubAPI{}, nil, 0); err == nil || !strings.Contains(err.Error(), "no upload_param") {
		t.Fatalf("unexpected uploadURL error: %v", err)
	}

	if _, err := uploadMediaToCDNWithAPI(context.Background(), filePath, "user-1", "https://cdn.example.com", UploadMediaTypeFile, &uploadStubAPI{err: errors.New("upload-url failed")}, nil, 0); err == nil || !strings.Contains(err.Error(), "upload-url failed") {
		t.Fatalf("unexpected uploadURL transport error: %v", err)
	}
}

func TestUploadMediaToCDNWithAPIUsesAPIClientHTTPClient(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			_ = json.NewEncoder(w).Encode(GetUploadURLResponse{UploadParam: "upload-param"})
		case "/upload":
			w.Header().Set("x-encrypted-param", "download-param")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	filePath := filepath.Join(t.TempDir(), "demo.bin")
	if err := os.WriteFile(filePath, []byte("demo"), 0o600); err != nil {
		t.Fatalf("write temp media file: %v", err)
	}

	api := NewAPIClient(APIOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	uploaded, err := uploadMediaToCDNWithAPI(context.Background(), filePath, "user-1", server.URL, UploadMediaTypeFile, api, nil, 0)
	if err != nil {
		t.Fatalf("uploadMediaToCDNWithAPI returned error: %v", err)
	}
	if uploaded.DownloadEncryptedQueryParam != "download-param" || uploaded.FileSize == 0 {
		t.Fatalf("unexpected uploaded file info: %#v", uploaded)
	}
}

func TestDownloadMediaFromItemAdditionalPaths(t *testing.T) {
	t.Parallel()

	result, err := DownloadMediaFromItem(context.Background(), MessageItem{Type: MessageItemTypeText}, "https://cdn.example.com", nil, func([]byte, string, string, int64, string) (string, error) {
		return "", nil
	}, nil)
	if err != nil {
		t.Fatalf("unexpected no-op DownloadMediaFromItem error: %v", err)
	}
	if result == nil || *result != (InboundMediaOptions{}) {
		t.Fatalf("unexpected no-op result: %#v", result)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain-image"))
	}))
	defer server.Close()

	_, err = DownloadMediaFromItem(context.Background(), MessageItem{
		Type: MessageItemTypeImage,
		ImageItem: &ImageItem{
			Media: &CDNMedia{EncryptQueryParam: "image"},
		},
	}, server.URL, server.Client(), func([]byte, string, string, int64, string) (string, error) {
		return "", errors.New("save failed")
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "save failed") {
		t.Fatalf("unexpected image save error: %v", err)
	}

	result, err = DownloadMediaFromItem(context.Background(), MessageItem{
		Type:     MessageItemTypeFile,
		FileItem: &FileItem{},
	}, server.URL, server.Client(), func([]byte, string, string, int64, string) (string, error) {
		return "", nil
	}, nil)
	if err != nil {
		t.Fatalf("unexpected empty-file DownloadMediaFromItem error: %v", err)
	}
	if result == nil || *result != (InboundMediaOptions{}) {
		t.Fatalf("unexpected empty-file result: %#v", result)
	}
}

func TestAPIClientSessionGuardBranches(t *testing.T) {
	resetSessionGuardForTest()
	defer resetSessionGuardForTest()

	PauseSession("bot@im.bot")
	api := NewAPIClient(APIOptions{AccountID: "bot@im.bot"})

	if _, err := api.GetUploadURL(context.Background(), GetUploadURLRequest{}, 0); err == nil || !strings.Contains(err.Error(), "session paused") {
		t.Fatalf("unexpected GetUploadURL paused error: %v", err)
	}
	if err := api.SendMessage(context.Background(), SendMessageRequest{}, 0); err == nil || !strings.Contains(err.Error(), "session paused") {
		t.Fatalf("unexpected SendMessage paused error: %v", err)
	}
	if _, err := api.GetConfig(context.Background(), "user-1", "ctx-1", 0); err == nil || !strings.Contains(err.Error(), "session paused") {
		t.Fatalf("unexpected GetConfig paused error: %v", err)
	}
}

func TestSendMediaFileValidationAndPaddingBranches(t *testing.T) {
	t.Parallel()

	sender := NewSender(SenderOptions{})
	if _, err := sender.Conversation(Target{ToUserID: "user-1"}).SendMediaFile(context.Background(), "demo.txt", "hello"); err == nil || !strings.Contains(err.Error(), "contextToken is required") {
		t.Fatalf("unexpected SendMediaFile validation error: %v", err)
	}

	padded := pkcs7Pad([]byte("1234567890abcdef"), 16)
	if len(padded) != 32 {
		t.Fatalf("unexpected PKCS7 padded length: %d", len(padded))
	}
	if _, err := DecryptAESECB([]byte("short"), []byte("1234567890abcdef")); err == nil || !strings.Contains(err.Error(), "not multiple of block size") {
		t.Fatalf("unexpected DecryptAESECB error: %v", err)
	}
}

func TestClientSmallBranchHelpers(t *testing.T) {
	t.Parallel()

	if got := firstNonEmpty(" ", "\n", "value", "later"); got != "value" {
		t.Fatalf("unexpected firstNonEmpty value: %q", got)
	}
	if got := firstNonEmpty(" ", "\n"); got != "" {
		t.Fatalf("expected empty firstNonEmpty result, got %q", got)
	}
	if ext := ExtensionFromMIME("image/png; charset=utf-8"); ext != ".png" {
		t.Fatalf("unexpected ExtensionFromMIME result: %q", ext)
	}
	if ext := ExtensionFromMIME("application/x-unknown"); ext != ".bin" {
		t.Fatalf("unexpected unknown ExtensionFromMIME result: %q", ext)
	}
}

func TestStartLoginErrorAndLoginInteractiveFallbackOutput(t *testing.T) {
	t.Run("StartLogin propagates fetch error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"qrcode": "",
			})
		}))
		defer server.Close()

		client := NewClient(Options{
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		})
		if _, err := client.StartLogin(context.Background(), "hint"); err == nil || !strings.Contains(err.Error(), "empty QR payload") {
			t.Fatalf("unexpected StartLogin error: %v", err)
		}
	})

	t.Run("LoginInteractive falls back to QR content output", func(t *testing.T) {
		var pollCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/ilink/bot/get_bot_qrcode":
				_ = json.NewEncoder(w).Encode(map[string]string{
					"qrcode":             "qr-1",
					"qrcode_img_content": " ",
				})
			case "/ilink/bot/get_qrcode_status":
				pollCount++
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status":       "confirmed",
					"bot_token":    "bot-token",
					"ilink_bot_id": "demo@im.bot",
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		var output strings.Builder
		client := NewClient(Options{
			BaseURL:           server.URL,
			HTTPClient:        server.Client(),
			Output:            &output,
			PollInterval:      5 * time.Millisecond,
			QRLongPollTimeout: 100 * time.Millisecond,
		})
		account, err := client.LoginInteractive(context.Background(), InteractiveLoginOptions{
			Timeout: time.Second,
		})
		if err != nil {
			t.Fatalf("LoginInteractive returned error: %v", err)
		}
		if account.AccountID != "demo@im.bot" || pollCount != 1 {
			t.Fatalf("unexpected LoginInteractive result: %#v pollCount=%d", account, pollCount)
		}
		if !strings.Contains(output.String(), "二维码内容:") {
			t.Fatalf("expected QR fallback output, got %q", output.String())
		}
	})
}

func TestPollQRStatusNonTimeoutErrorAndSendTypingDefaultTimeout(t *testing.T) {
	t.Run("pollQRStatus wraps non-timeout error", func(t *testing.T) {
		client := NewClient(Options{
			BaseURL: "https://example.com",
			HTTPClient: &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return nil, errors.New("network reset")
				}),
			},
		})
		if _, err := client.pollQRStatus(context.Background(), "qr-1"); err == nil || !strings.Contains(err.Error(), "poll QR status:") || !strings.Contains(err.Error(), "network reset") {
			t.Fatalf("unexpected pollQRStatus error: %v", err)
		}
	})

	t.Run("SendTyping uses default timeout path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ilink/bot/sendtyping" {
				t.Fatalf("unexpected path: %q", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		api := NewAPIClient(APIOptions{
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		})
		if err := api.SendTyping(context.Background(), SendTypingRequest{
			ILinkUserID:  "user-1",
			TypingTicket: "ticket-1",
			Status:       TypingStatusTyping,
		}, 0); err != nil {
			t.Fatalf("SendTyping default timeout returned error: %v", err)
		}
	})
}

func TestRemainingPauseExpiresEntries(t *testing.T) {
	resetSessionGuardForTest()
	defer resetSessionGuardForTest()

	pauseState.Lock()
	pauseState.until["expired@im.bot"] = time.Now().Add(-time.Second)
	pauseState.Unlock()

	if got := RemainingPause("expired@im.bot"); got != 0 {
		t.Fatalf("expected expired pause to be cleared, got %v", got)
	}

	pauseState.Lock()
	_, exists := pauseState.until["expired@im.bot"]
	pauseState.Unlock()
	if exists {
		t.Fatalf("expected expired pause entry to be deleted")
	}
}
