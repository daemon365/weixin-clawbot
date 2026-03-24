package weixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type stubMessageAPI struct {
	sendCalls   int
	lastTimeout time.Duration
	lastReq     SendMessageRequest
}

func (s *stubMessageAPI) SendMessage(_ context.Context, req SendMessageRequest, timeout time.Duration) error {
	s.sendCalls++
	s.lastReq = req
	s.lastTimeout = timeout
	return nil
}

func (s *stubMessageAPI) GetUploadURL(context.Context, GetUploadURLRequest, time.Duration) (*GetUploadURLResponse, error) {
	return &GetUploadURLResponse{UploadParam: "stub-upload"}, nil
}

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()

	client := NewClient(Options{})
	if client.baseURL != DefaultBaseURL {
		t.Fatalf("unexpected baseURL: %q", client.baseURL)
	}
	if client.botType != DefaultBotType {
		t.Fatalf("unexpected botType: %q", client.botType)
	}
	if client.httpClient == nil {
		t.Fatalf("expected default HTTP client")
	}
	if client.output == nil {
		t.Fatalf("expected default output")
	}
	if client.qrSessionTTL != DefaultQRSessionTTL {
		t.Fatalf("unexpected QR session TTL: %v", client.qrSessionTTL)
	}
	if client.qrLongPollTimeout != DefaultQRLongPollTimeout {
		t.Fatalf("unexpected long-poll timeout: %v", client.qrLongPollTimeout)
	}
	if client.pollInterval != DefaultPollInterval {
		t.Fatalf("unexpected poll interval: %v", client.pollInterval)
	}
	if client.maxQRRefresh != DefaultMaxQRRefresh {
		t.Fatalf("unexpected max QR refresh: %d", client.maxQRRefresh)
	}
}

func TestNewAPIClientDefaultsAndHeaders(t *testing.T) {
	t.Parallel()

	api := NewAPIClient(APIOptions{
		Token:    " bot-token ",
		RouteTag: " route-a ",
	})

	if api.baseURL != DefaultBaseURL {
		t.Fatalf("unexpected baseURL: %q", api.baseURL)
	}
	if api.channelVersion != "go-port" {
		t.Fatalf("unexpected channel version: %q", api.channelVersion)
	}
	if api.httpClient == nil {
		t.Fatalf("expected default HTTP client")
	}

	body := []byte(`{"hello":"world"}`)
	headers := api.buildHeaders(body)
	if headers["Content-Type"] != "application/json" {
		t.Fatalf("unexpected content-type: %q", headers["Content-Type"])
	}
	if headers["AuthorizationType"] != "ilink_bot_token" {
		t.Fatalf("unexpected authorization type: %q", headers["AuthorizationType"])
	}
	if headers["Authorization"] != "Bearer bot-token" {
		t.Fatalf("unexpected authorization header: %q", headers["Authorization"])
	}
	if headers["SKRouteTag"] != "route-a" {
		t.Fatalf("unexpected route tag: %q", headers["SKRouteTag"])
	}
	if headers["Content-Length"] != strconv.Itoa(len(body)) {
		t.Fatalf("unexpected content length: %q", headers["Content-Length"])
	}

	decoded, err := base64.StdEncoding.DecodeString(headers["X-WECHAT-UIN"])
	if err != nil {
		t.Fatalf("expected decodable X-WECHAT-UIN: %v", err)
	}
	if len(decoded) == 0 {
		t.Fatalf("expected X-WECHAT-UIN payload")
	}
	if api.BuildBaseInfo().ChannelVersion != "go-port" {
		t.Fatalf("unexpected base info: %#v", api.BuildBaseInfo())
	}
}

func TestNewSenderDefaults(t *testing.T) {
	t.Parallel()

	sender := NewSender(SenderOptions{})
	if sender.api == nil {
		t.Fatalf("expected sender API")
	}
	api, ok := sender.api.(*APIClient)
	if !ok {
		t.Fatalf("expected *APIClient, got %T", sender.api)
	}
	if api.baseURL != DefaultBaseURL {
		t.Fatalf("unexpected sender API baseURL: %q", api.baseURL)
	}
	if sender.httpClient == nil {
		t.Fatalf("expected sender HTTP client")
	}
	if sender.cdnBaseURL != DefaultBaseURL {
		t.Fatalf("unexpected sender CDN baseURL: %q", sender.cdnBaseURL)
	}
}

func TestNewSenderUsesInjectedAPI(t *testing.T) {
	t.Parallel()

	api := &stubMessageAPI{}
	sender := NewSender(SenderOptions{
		API:     api,
		Timeout: 3 * time.Second,
	})

	clientID, err := sender.Conversation(Target{
		ToUserID:     "user@im.wechat",
		ContextToken: "ctx-1",
	}).SendText(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if clientID == "" {
		t.Fatalf("expected client ID")
	}
	if api.sendCalls != 1 {
		t.Fatalf("expected one send call, got %d", api.sendCalls)
	}
	if api.lastTimeout != 3*time.Second {
		t.Fatalf("unexpected timeout: %v", api.lastTimeout)
	}
	if api.lastReq.Message == nil || api.lastReq.Message.ToUserID != "user@im.wechat" {
		t.Fatalf("unexpected request payload: %#v", api.lastReq)
	}
}

func TestConversationSendTextBuildsExpectedRequest(t *testing.T) {
	t.Parallel()

	var (
		gotPath    string
		gotHeaders http.Header
		gotReq     SendMessageRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender(SenderOptions{
		BaseURL:        server.URL,
		Token:          "bot-token",
		RouteTag:       "route-a",
		ChannelVersion: "test-port",
		HTTPClient:     server.Client(),
		AccountID:      "demo@im.bot",
		Timeout:        time.Second,
	})
	conversation := sender.Conversation(Target{
		ToUserID:     "user@im.wechat",
		ContextToken: "ctx-1",
	})
	clientID, err := conversation.SendText(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}

	if gotPath != "/ilink/bot/sendmessage" {
		t.Fatalf("unexpected request path: %q", gotPath)
	}
	if gotHeaders.Get("Authorization") != "Bearer bot-token" {
		t.Fatalf("unexpected Authorization header: %q", gotHeaders.Get("Authorization"))
	}
	if gotHeaders.Get("SKRouteTag") != "route-a" {
		t.Fatalf("unexpected SKRouteTag: %q", gotHeaders.Get("SKRouteTag"))
	}
	if gotReq.Message == nil {
		t.Fatalf("expected message payload")
	}
	if gotReq.Message.ToUserID != "user@im.wechat" {
		t.Fatalf("unexpected to_user_id: %q", gotReq.Message.ToUserID)
	}
	if gotReq.Message.ContextToken != "ctx-1" {
		t.Fatalf("unexpected context token: %q", gotReq.Message.ContextToken)
	}
	if gotReq.Message.MessageType != MessageTypeBot || gotReq.Message.MessageState != MessageStateFinish {
		t.Fatalf("unexpected message metadata: %#v", gotReq.Message)
	}
	if len(gotReq.Message.ItemList) != 1 {
		t.Fatalf("expected one item, got %d", len(gotReq.Message.ItemList))
	}
	if gotReq.Message.ItemList[0].Type != MessageItemTypeText || gotReq.Message.ItemList[0].TextItem == nil {
		t.Fatalf("unexpected item payload: %#v", gotReq.Message.ItemList[0])
	}
	if gotReq.Message.ItemList[0].TextItem.Text != "hello" {
		t.Fatalf("unexpected text payload: %q", gotReq.Message.ItemList[0].TextItem.Text)
	}
	if clientID == "" || clientID != gotReq.Message.ClientID {
		t.Fatalf("unexpected clientID: %q vs %#v", clientID, gotReq.Message)
	}
}

func TestConversationSendImageSendsTextThenMedia(t *testing.T) {
	t.Parallel()

	var requests []SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Fatalf("unexpected request path: %q", r.URL.Path)
		}
		var req SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, req)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender(SenderOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Timeout:    time.Second,
	})
	conversation := sender.Conversation(Target{
		ToUserID:     "user@im.wechat",
		ContextToken: "ctx-2",
	})

	lastID, err := conversation.SendImage(context.Background(), "caption", UploadedFileInfo{
		DownloadEncryptedQueryParam: "enc-param",
		AESKeyHex:                   "00112233445566778899aabbccddeeff",
		FileSizeCiphertext:          128,
	})
	if err != nil {
		t.Fatalf("SendImage returned error: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("expected two send requests, got %d", len(requests))
	}

	first := requests[0].Message
	if first == nil || len(first.ItemList) != 1 || first.ItemList[0].TextItem == nil {
		t.Fatalf("unexpected first request: %#v", requests[0])
	}
	if first.ItemList[0].TextItem.Text != "caption" {
		t.Fatalf("unexpected caption: %q", first.ItemList[0].TextItem.Text)
	}

	second := requests[1].Message
	if second == nil || len(second.ItemList) != 1 || second.ItemList[0].ImageItem == nil {
		t.Fatalf("unexpected second request: %#v", requests[1])
	}
	if second.ItemList[0].ImageItem.Media == nil {
		t.Fatalf("expected image media payload")
	}
	if second.ItemList[0].ImageItem.Media.EncryptQueryParam != "enc-param" {
		t.Fatalf("unexpected download param: %q", second.ItemList[0].ImageItem.Media.EncryptQueryParam)
	}
	if second.ItemList[0].ImageItem.Media.AESKey != base64.StdEncoding.EncodeToString([]byte("00112233445566778899aabbccddeeff")) {
		t.Fatalf("unexpected AES key: %q", second.ItemList[0].ImageItem.Media.AESKey)
	}
	if second.ItemList[0].ImageItem.MidSize != 128 {
		t.Fatalf("unexpected image size: %d", second.ItemList[0].ImageItem.MidSize)
	}
	if lastID == "" || lastID != second.ClientID {
		t.Fatalf("unexpected last client ID: %q", lastID)
	}
}

func TestConfigManagerCachesSuccessfulResponse(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/getconfig" {
			t.Fatalf("unexpected request path: %q", r.URL.Path)
		}
		calls++
		_ = json.NewEncoder(w).Encode(GetConfigResponse{
			Ret:          0,
			TypingTicket: "ticket-1",
		})
	}))
	defer server.Close()

	manager := NewConfigManager(NewAPIClient(APIOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Token:      "bot-token",
	}))
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	manager.rand = rand.New(rand.NewSource(1))

	cfg1, err := manager.GetForUser(context.Background(), "user-1", "ctx-1")
	if err != nil {
		t.Fatalf("first GetForUser returned error: %v", err)
	}
	cfg2, err := manager.GetForUser(context.Background(), "user-1", "ctx-1")
	if err != nil {
		t.Fatalf("second GetForUser returned error: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected one upstream request, got %d", calls)
	}
	if cfg1.TypingTicket != "ticket-1" || cfg2.TypingTicket != "ticket-1" {
		t.Fatalf("unexpected cached config: %#v %#v", cfg1, cfg2)
	}
}

func TestConfigManagerBacksOffAndReturnsCachedConfigOnFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	manager := NewConfigManager(NewAPIClient(APIOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Token:      "bot-token",
	}))
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	manager.cache["user-1"] = configCacheEntry{
		config:      CachedConfig{TypingTicket: "cached-ticket"},
		nextFetchAt: now.Add(-time.Second),
		retryDelay:  configCacheInitialRetry,
	}

	cfg, err := manager.GetForUser(context.Background(), "user-1", "ctx-1")
	if err == nil {
		t.Fatalf("expected fetch error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TypingTicket != "cached-ticket" {
		t.Fatalf("expected cached config, got %#v", cfg)
	}

	entry := manager.cache["user-1"]
	if entry.retryDelay != 2*configCacheInitialRetry {
		t.Fatalf("unexpected retry delay: %v", entry.retryDelay)
	}
	if !entry.nextFetchAt.Equal(now.Add(2 * configCacheInitialRetry)) {
		t.Fatalf("unexpected next fetch time: %v", entry.nextFetchAt)
	}
}

func TestSaveMediaToDirUsesSubdirAndExtension(t *testing.T) {
	t.Parallel()

	save := SaveMediaToDir(t.TempDir())
	path, err := save([]byte("png-bytes"), "image/png", "inbound", 32, "")
	if err != nil {
		t.Fatalf("SaveMediaToDir returned error: %v", err)
	}
	if filepath.Base(filepath.Dir(path)) != "inbound" {
		t.Fatalf("expected inbound subdir, got %q", path)
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("expected .png extension, got %q", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "png-bytes" {
		t.Fatalf("unexpected saved file content: %q", data)
	}
}

func TestSaveMediaToDirRejectsOversizeBuffer(t *testing.T) {
	t.Parallel()

	save := SaveMediaToDir(t.TempDir())
	_, err := save([]byte("too-large"), "text/plain", "", 3, "payload.txt")
	if err == nil {
		t.Fatalf("expected size limit error")
	}
	if !strings.Contains(err.Error(), "media too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtensionFromContentTypeOrURLPrefersContentType(t *testing.T) {
	t.Parallel()

	if ext := ExtensionFromContentTypeOrURL("image/png; charset=utf-8", "https://example.com/file.jpg"); ext != ".png" {
		t.Fatalf("unexpected extension from content type: %q", ext)
	}
	if ext := ExtensionFromContentTypeOrURL("", "https://example.com/archive.tar"); ext != ".tar" {
		t.Fatalf("unexpected extension from URL: %q", ext)
	}
	if ext := ExtensionFromContentTypeOrURL("", "://bad-url"); ext != ".bin" {
		t.Fatalf("unexpected fallback extension: %q", ext)
	}
}

func TestBodyFromItemListFormatsQuotedText(t *testing.T) {
	t.Parallel()

	body := BodyFromItemList([]MessageItem{
		{
			Type: MessageItemTypeText,
			TextItem: &TextItem{
				Text: "reply body",
			},
			RefMessage: &RefMessage{
				Title: "quoted title",
				MessageItem: &MessageItem{
					Type:     MessageItemTypeText,
					TextItem: &TextItem{Text: "quoted body"},
				},
			},
		},
	})

	if body != "[引用: quoted title | quoted body]\nreply body" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestDownloadMediaFromItemUsesPlainImageDownloadWhenAESKeyAbsent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/download") {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer server.Close()

	var (
		savedData []byte
		savedSub  string
	)
	saveMedia := func(buffer []byte, contentType, subdir string, maxBytes int64, originalFilename string) (string, error) {
		savedData = append([]byte(nil), buffer...)
		savedSub = subdir
		return "/tmp/image.png", nil
	}

	result, err := DownloadMediaFromItem(context.Background(), MessageItem{
		Type: MessageItemTypeImage,
		ImageItem: &ImageItem{
			Media: &CDNMedia{
				EncryptQueryParam: "enc-image",
			},
		},
	}, server.URL, server.Client(), saveMedia, nil)
	if err != nil {
		t.Fatalf("DownloadMediaFromItem returned error: %v", err)
	}
	if result.DecryptedPicPath != "/tmp/image.png" {
		t.Fatalf("unexpected image path: %q", result.DecryptedPicPath)
	}
	if string(savedData) != "image-bytes" {
		t.Fatalf("unexpected saved image bytes: %q", savedData)
	}
	if savedSub != "inbound" {
		t.Fatalf("unexpected save subdir: %q", savedSub)
	}
}

func TestParseAESKeyRejectsInvalidLength(t *testing.T) {
	t.Parallel()

	_, err := parseAESKey(base64.StdEncoding.EncodeToString([]byte("short")))
	if err == nil {
		t.Fatalf("expected invalid aes key error")
	}
	if !strings.Contains(err.Error(), "aes_key must decode") {
		t.Fatalf("unexpected error: %v", err)
	}
}
