package weixin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResolveStateDirAndHelpers(t *testing.T) {
	t.Setenv("OPENCLAW_STATE_DIR", "  /tmp/openclaw-state  ")
	t.Setenv("CLAWDBOT_STATE_DIR", "/tmp/clawdbot-state")
	if got := ResolveStateDir(); got != "/tmp/openclaw-state" {
		t.Fatalf("unexpected OPENCLAW_STATE_DIR: %q", got)
	}

	t.Setenv("OPENCLAW_STATE_DIR", " \n\t ")
	if got := ResolveStateDir(); got != "/tmp/clawdbot-state" {
		t.Fatalf("unexpected CLAWDBOT_STATE_DIR fallback: %q", got)
	}

	if got := SyncBufFilePath("/state-root", "bot@im.bot"); got != filepath.Join("/state-root", "openclaw-weixin", "accounts", "bot@im.bot.sync.json") {
		t.Fatalf("unexpected sync buf path: %q", got)
	}

	if got := stringsTrimSpace(" \n hello \t "); got != "hello" {
		t.Fatalf("unexpected trimmed string: %q", got)
	}
	if got := stringsTrimSpace(" \n\t "); got != "" {
		t.Fatalf("expected empty trimmed string, got %q", got)
	}
}

func TestLoadSyncBufferInvalidJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bad.sync.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatalf("write bad sync buffer: %v", err)
	}

	_, err := LoadSyncBuffer(path)
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
}

func TestContextTokenBodyAndContextHelpers(t *testing.T) {
	t.Parallel()

	SetContextToken("acc-1", "user-1", "ctx-1")
	if got := GetContextToken("acc-1", "user-1"); got != "ctx-1" {
		t.Fatalf("unexpected context token: %q", got)
	}

	body := BodyFromItemList([]MessageItem{
		{Type: MessageItemTypeVoice, VoiceItem: &VoiceItem{Text: "voice transcript"}},
	})
	if body != "voice transcript" {
		t.Fatalf("unexpected voice body: %q", body)
	}

	body = BodyFromItemList([]MessageItem{
		{
			Type:     MessageItemTypeText,
			TextItem: &TextItem{Text: "reply text"},
			RefMessage: &RefMessage{
				MessageItem: &MessageItem{
					Type: MessageItemTypeImage,
					ImageItem: &ImageItem{
						Media: &CDNMedia{EncryptQueryParam: "enc"},
					},
				},
			},
		},
	})
	if body != "reply text" {
		t.Fatalf("expected text-only reply body for media quote, got %q", body)
	}

	msg := WeixinMessage{
		FromUserID:   "user-1",
		CreateTimeMS: 12345,
		ContextToken: "ctx-9",
		ItemList: []MessageItem{
			{Type: MessageItemTypeText, TextItem: &TextItem{Text: "hello"}},
		},
	}

	cases := []struct {
		name     string
		opts     *InboundMediaOptions
		wantPath string
		wantType string
	}{
		{name: "none", opts: nil, wantPath: "", wantType: ""},
		{name: "image", opts: &InboundMediaOptions{DecryptedPicPath: "/tmp/pic.png"}, wantPath: "/tmp/pic.png", wantType: "image/*"},
		{name: "video", opts: &InboundMediaOptions{DecryptedVideoPath: "/tmp/video.mp4"}, wantPath: "/tmp/video.mp4", wantType: "video/mp4"},
		{name: "file", opts: &InboundMediaOptions{DecryptedFilePath: "/tmp/file.bin", FileMediaType: "application/pdf"}, wantPath: "/tmp/file.bin", wantType: "application/pdf"},
		{name: "voice", opts: &InboundMediaOptions{DecryptedVoicePath: "/tmp/voice.wav"}, wantPath: "/tmp/voice.wav", wantType: "audio/wav"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := WeixinMessageToContext(msg, "acc-1", tc.opts)
			if ctx.Body != "hello" || ctx.From != "user-1" || ctx.To != "user-1" || ctx.AccountID != "acc-1" || ctx.ContextToken != "ctx-9" {
				t.Fatalf("unexpected context core fields: %#v", ctx)
			}
			if ctx.MediaPath != tc.wantPath || ctx.MediaType != tc.wantType {
				t.Fatalf("unexpected media mapping: %#v", ctx)
			}
		})
	}
}

func TestAPIUploadURLAndSendTyping(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		paths    []string
		payloads []map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		paths = append(paths, r.URL.Path)
		payloads = append(payloads, payload)
		mu.Unlock()

		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			_ = json.NewEncoder(w).Encode(GetUploadURLResponse{UploadParam: "upload-param"})
		case "/ilink/bot/sendtyping":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	api := NewAPIClient(APIOptions{
		BaseURL:        server.URL,
		Token:          "bot-token",
		RouteTag:       "route-a",
		ChannelVersion: "test-port",
		HTTPClient:     server.Client(),
	})

	resp, err := api.GetUploadURL(context.Background(), GetUploadURLRequest{
		FileKey:     "file-key",
		MediaType:   UploadMediaTypeImage,
		ToUserID:    "user-1",
		RawSize:     10,
		RawFileMD5:  "abc",
		FileSize:    16,
		NoNeedThumb: true,
		AESKey:      "001122",
	}, time.Second)
	if err != nil {
		t.Fatalf("GetUploadURL returned error: %v", err)
	}
	if resp.UploadParam != "upload-param" {
		t.Fatalf("unexpected upload response: %#v", resp)
	}

	if err := api.SendTyping(context.Background(), SendTypingRequest{
		ILinkUserID:  "user-1",
		TypingTicket: "ticket-1",
		Status:       TypingStatusTyping,
	}, time.Second); err != nil {
		t.Fatalf("SendTyping returned error: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(paths))
	}
	if paths[0] != "/ilink/bot/getuploadurl" || paths[1] != "/ilink/bot/sendtyping" {
		t.Fatalf("unexpected API paths: %#v", paths)
	}
	if payloads[0]["filekey"] != "file-key" || payloads[0]["to_user_id"] != "user-1" {
		t.Fatalf("unexpected getuploadurl payload: %#v", payloads[0])
	}
	if payloads[1]["typing_ticket"] != "ticket-1" || int(payloads[1]["status"].(float64)) != TypingStatusTyping {
		t.Fatalf("unexpected sendtyping payload: %#v", payloads[1])
	}
}

func TestPackageVersionHelpersAndTimeoutChecks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if got := packageVersionFromPath(root); got != "unknown" {
		t.Fatalf("expected unknown without package.json, got %q", got)
	}

	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"version":"1.2.3"}`), 0o600); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if got := packageVersionFromPath(root); got != "1.2.3" {
		t.Fatalf("unexpected package version: %q", got)
	}

	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"version":`), 0o600); err != nil {
		t.Fatalf("write invalid package.json: %v", err)
	}
	if got := packageVersionFromPath(root); got != "unknown" {
		t.Fatalf("expected unknown for invalid JSON, got %q", got)
	}

	if !isContextTimeout(errors.New("context deadline exceeded")) {
		t.Fatalf("expected context timeout to match")
	}
	if !errorsIsTimeout(errors.New("context deadline exceeded")) {
		t.Fatalf("expected client timeout helper to match")
	}
	if errorsIsTimeout(errors.New("network reset")) {
		t.Fatalf("did not expect unrelated error to match timeout")
	}
}

func TestDoJSONErrorPaths(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad-status":
			http.Error(w, "boom", http.StatusBadGateway)
		case "/bad-json":
			_, _ = w.Write([]byte("{bad"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Options{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/bad-status", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if err := client.doJSON(req, &map[string]any{}); err == nil || !strings.Contains(err.Error(), "unexpected status 502") {
		t.Fatalf("unexpected bad-status error: %v", err)
	}

	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/bad-json", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if err := client.doJSON(req, &map[string]any{}); err == nil || !strings.Contains(err.Error(), "decode JSON") {
		t.Fatalf("unexpected bad-json error: %v", err)
	}
}

func TestAPIClientPostJSONErrorPaths(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/empty":
			w.WriteHeader(http.StatusOK)
		case "/bad-json":
			_, _ = w.Write([]byte("{bad"))
		case "/bad-status":
			http.Error(w, "nope", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	api := NewAPIClient(APIOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})

	var out map[string]any
	if err := api.postJSON(context.Background(), "empty", map[string]string{"x": "1"}, time.Second, &out); err != nil {
		t.Fatalf("postJSON empty response returned error: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil output map on empty body, got %#v", out)
	}

	if err := api.postJSON(context.Background(), "bad-json", map[string]string{"x": "1"}, time.Second, &out); err == nil || !strings.Contains(err.Error(), "decode bad-json JSON") {
		t.Fatalf("unexpected bad-json error: %v", err)
	}
	if err := api.postJSON(context.Background(), "bad-status", map[string]string{"x": "1"}, time.Second, nil); err == nil || !strings.Contains(err.Error(), "bad-status 400") {
		t.Fatalf("unexpected bad-status error: %v", err)
	}
}

func TestConversationSendVideoAndFile(t *testing.T) {
	t.Parallel()

	var requests []SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})

	videoConversation := sender.Conversation(Target{
		ToUserID:     "user-1",
		ContextToken: "ctx-1",
	})
	videoID, err := videoConversation.SendVideo(context.Background(), "", UploadedFileInfo{
		DownloadEncryptedQueryParam: "video-dl",
		AESKeyHex:                   "00112233445566778899aabbccddeeff",
		FileSizeCiphertext:          96,
	})
	if err != nil {
		t.Fatalf("SendVideo returned error: %v", err)
	}

	fileConversation := sender.Conversation(Target{
		ToUserID:     "user-1",
		ContextToken: "ctx-2",
	})
	fileID, err := fileConversation.SendFile(context.Background(), "file caption", "demo.pdf", UploadedFileInfo{
		DownloadEncryptedQueryParam: "file-dl",
		AESKeyHex:                   "00112233445566778899aabbccddeeff",
		FileSize:                    42,
	})
	if err != nil {
		t.Fatalf("SendFile returned error: %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(requests))
	}
	if requests[0].Message.ItemList[0].VideoItem == nil {
		t.Fatalf("expected first request to be video: %#v", requests[0])
	}
	if requests[1].Message.ItemList[0].TextItem == nil || requests[1].Message.ItemList[0].TextItem.Text != "file caption" {
		t.Fatalf("expected second request to be text: %#v", requests[1])
	}
	if requests[2].Message.ItemList[0].FileItem == nil || requests[2].Message.ItemList[0].FileItem.FileName != "demo.pdf" {
		t.Fatalf("expected third request to be file: %#v", requests[2])
	}
	if videoID == "" || fileID == "" {
		t.Fatalf("expected client IDs from sender")
	}
}

func TestConversationSendRequiresContextToken(t *testing.T) {
	t.Parallel()

	sender := NewSender(SenderOptions{})
	conversation := sender.Conversation(Target{ToUserID: "user-1"})

	if _, err := conversation.SendText(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "contextToken is required") {
		t.Fatalf("unexpected SendText error: %v", err)
	}
	if _, err := conversation.SendImage(context.Background(), "", UploadedFileInfo{}); err == nil || !strings.Contains(err.Error(), "contextToken is required") {
		t.Fatalf("unexpected SendImage error: %v", err)
	}
	if _, err := conversation.SendVideo(context.Background(), "", UploadedFileInfo{}); err == nil || !strings.Contains(err.Error(), "contextToken is required") {
		t.Fatalf("unexpected SendVideo error: %v", err)
	}
	if _, err := conversation.SendFile(context.Background(), "", "demo.txt", UploadedFileInfo{}); err == nil || !strings.Contains(err.Error(), "contextToken is required") {
		t.Fatalf("unexpected SendFile error: %v", err)
	}
	if _, err := sender.Conversation(Target{ContextToken: "ctx-1"}).SendText(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "toUserID is required") {
		t.Fatalf("unexpected missing toUserID error: %v", err)
	}
}

func TestCDNHelpersAndRemoteDownload(t *testing.T) {
	t.Parallel()

	if got := AESECBPaddedSize(17); got != 32 {
		t.Fatalf("unexpected padded size: %d", got)
	}
	if got := BuildCDNUploadURL("https://cdn.example.com/", "up 1", "file/1"); got != "https://cdn.example.com/upload?encrypted_query_param=up+1&filekey=file%2F1" {
		t.Fatalf("unexpected upload URL: %q", got)
	}
	if !isHexASCII([]byte("a1B2")) || isHexASCII([]byte("xyz")) {
		t.Fatalf("unexpected hex ASCII detection")
	}

	key := []byte("1234567890abcdef")
	ciphertext, err := EncryptAESECB([]byte("hello cdn"), key)
	if err != nil {
		t.Fatalf("EncryptAESECB returned error: %v", err)
	}

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			attempts++
			if attempts == 1 {
				http.Error(w, "retry", http.StatusBadGateway)
				return
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/octet-stream" {
				t.Fatalf("unexpected content type: %q", ct)
			}
			w.Header().Set("x-encrypted-param", "download-param")
			w.WriteHeader(http.StatusOK)
		case "/download":
			_, _ = w.Write(ciphertext)
		case "/remote":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("remote-file"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	gotParam, err := UploadBufferToCDN(context.Background(), server.Client(), []byte("hello cdn"), "up 1", "file/1", server.URL, key)
	if err != nil {
		t.Fatalf("UploadBufferToCDN returned error: %v", err)
	}
	if gotParam != "download-param" {
		t.Fatalf("unexpected download param: %q", gotParam)
	}

	plain, err := DownloadAndDecryptBuffer(context.Background(), server.Client(), "download-param", base64.StdEncoding.EncodeToString(key), server.URL)
	if err != nil {
		t.Fatalf("DownloadAndDecryptBuffer returned error: %v", err)
	}
	if string(plain) != "hello cdn" {
		t.Fatalf("unexpected decrypted payload: %q", plain)
	}

	hexKey := "31323334353637383930616263646566"
	plain, err = DownloadAndDecryptBuffer(context.Background(), server.Client(), "download-param", base64.StdEncoding.EncodeToString([]byte(hexKey)), server.URL)
	if err != nil {
		t.Fatalf("DownloadAndDecryptBuffer with hex-wrapped key returned error: %v", err)
	}
	if string(plain) != "hello cdn" {
		t.Fatalf("unexpected hex-key decrypted payload: %q", plain)
	}

	path, err := DownloadRemoteMediaToTemp(context.Background(), server.Client(), server.URL+"/remote?name=demo.jpg", t.TempDir())
	if err != nil {
		t.Fatalf("DownloadRemoteMediaToTemp returned error: %v", err)
	}
	if filepath.Ext(path) != ".pdf" {
		t.Fatalf("expected content-type-derived extension, got %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read remote file: %v", err)
	}
	if string(data) != "remote-file" {
		t.Fatalf("unexpected remote file payload: %q", data)
	}
}

func TestUploadBufferToCDNClientError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-error-message", "bad upload")
		http.Error(w, "ignored", http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := UploadBufferToCDN(context.Background(), server.Client(), []byte("hello"), "upload", "file", server.URL, []byte("1234567890abcdef"))
	if err == nil || !strings.Contains(err.Error(), "CDN upload client error 400: bad upload") {
		t.Fatalf("unexpected upload error: %v", err)
	}
}

func TestUploadWrappersAndConversationSendMediaFile(t *testing.T) {
	t.Parallel()

	type sendRecord struct {
		Path    string
		Payload map[string]any
	}
	var (
		mu      sync.Mutex
		records []sendRecord
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl", "/ilink/bot/sendmessage":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			mu.Lock()
			records = append(records, sendRecord{Path: r.URL.Path, Payload: payload})
			mu.Unlock()
			if r.URL.Path == "/ilink/bot/getuploadurl" {
				_ = json.NewEncoder(w).Encode(GetUploadURLResponse{UploadParam: "upload-param"})
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/upload":
			w.Header().Set("x-encrypted-param", "download-param")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	apiOpts := APIOptions{BaseURL: server.URL, HTTPClient: server.Client()}
	imageFile := filepath.Join(t.TempDir(), "image.png")
	videoFile := filepath.Join(t.TempDir(), "video.mp4")
	docFile := filepath.Join(t.TempDir(), "doc.pdf")
	for path, data := range map[string]string{
		imageFile: "image-data",
		videoFile: "video-data",
		docFile:   "doc-data",
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatalf("write temp file %s: %v", path, err)
		}
	}

	if _, err := UploadFileToWeixin(context.Background(), imageFile, "user-1", server.URL, apiOpts); err != nil {
		t.Fatalf("UploadFileToWeixin returned error: %v", err)
	}
	if _, err := UploadVideoToWeixin(context.Background(), videoFile, "user-1", server.URL, apiOpts); err != nil {
		t.Fatalf("UploadVideoToWeixin returned error: %v", err)
	}
	if _, err := UploadFileAttachmentToWeixin(context.Background(), docFile, "user-1", server.URL, apiOpts); err != nil {
		t.Fatalf("UploadFileAttachmentToWeixin returned error: %v", err)
	}

	sender := NewSender(SenderOptions{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		CDNBaseURL: server.URL,
	})
	conversation := sender.Conversation(Target{
		ToUserID:     "user-1",
		ContextToken: "ctx-1",
	})
	if _, err := conversation.SendMediaFile(context.Background(), imageFile, "img"); err != nil {
		t.Fatalf("SendMediaFile image returned error: %v", err)
	}
	if _, err := conversation.SendMediaFile(context.Background(), videoFile, ""); err != nil {
		t.Fatalf("SendMediaFile video returned error: %v", err)
	}
	if _, err := conversation.SendMediaFile(context.Background(), docFile, ""); err != nil {
		t.Fatalf("SendMediaFile file returned error: %v", err)
	}

	var (
		uploadURLCalls int
		sendCalls      int
	)
	for _, rec := range records {
		switch rec.Path {
		case "/ilink/bot/getuploadurl":
			uploadURLCalls++
		case "/ilink/bot/sendmessage":
			sendCalls++
		}
	}
	if uploadURLCalls != 6 {
		t.Fatalf("expected 6 getuploadurl calls, got %d", uploadURLCalls)
	}
	if sendCalls != 4 {
		t.Fatalf("expected 4 sendmessage calls, got %d", sendCalls)
	}
}

func TestDownloadMediaFromItemVoiceFileAndVideo(t *testing.T) {
	t.Parallel()

	key := []byte("1234567890abcdef")
	makeCiphertext := func(plain string) []byte {
		buf, err := EncryptAESECB([]byte(plain), key)
		if err != nil {
			t.Fatalf("EncryptAESECB returned error: %v", err)
		}
		return buf
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("encrypted_query_param") {
		case "voice":
			_, _ = w.Write(makeCiphertext("silk-bytes"))
		case "file":
			_, _ = w.Write(makeCiphertext("file-bytes"))
		case "video":
			_, _ = w.Write(makeCiphertext("video-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var saved []struct {
		contentType string
		subdir      string
		name        string
		data        string
	}
	saveMedia := func(buffer []byte, contentType, subdir string, maxBytes int64, originalFilename string) (string, error) {
		saved = append(saved, struct {
			contentType string
			subdir      string
			name        string
			data        string
		}{
			contentType: contentType,
			subdir:      subdir,
			name:        originalFilename,
			data:        string(buffer),
		})
		return "/tmp/" + firstNonEmpty(originalFilename, "generated.bin"), nil
	}
	aesKey := base64.StdEncoding.EncodeToString(key)

	voiceResult, err := DownloadMediaFromItem(context.Background(), MessageItem{
		Type: MessageItemTypeVoice,
		VoiceItem: &VoiceItem{
			Media: &CDNMedia{EncryptQueryParam: "voice", AESKey: aesKey},
		},
	}, server.URL, server.Client(), saveMedia, func(silk []byte) ([]byte, error) {
		if string(silk) != "silk-bytes" {
			t.Fatalf("unexpected silk bytes: %q", silk)
		}
		return []byte("wav-bytes"), nil
	})
	if err != nil {
		t.Fatalf("voice DownloadMediaFromItem returned error: %v", err)
	}
	if voiceResult.DecryptedVoicePath == "" || voiceResult.VoiceMediaType != "audio/wav" {
		t.Fatalf("unexpected voice result: %#v", voiceResult)
	}

	fileResult, err := DownloadMediaFromItem(context.Background(), MessageItem{
		Type: MessageItemTypeFile,
		FileItem: &FileItem{
			FileName: "demo.pdf",
			Media:    &CDNMedia{EncryptQueryParam: "file", AESKey: aesKey},
		},
	}, server.URL, server.Client(), saveMedia, nil)
	if err != nil {
		t.Fatalf("file DownloadMediaFromItem returned error: %v", err)
	}
	if fileResult.DecryptedFilePath != "/tmp/demo.pdf" || fileResult.FileMediaType != "application/pdf" {
		t.Fatalf("unexpected file result: %#v", fileResult)
	}

	videoResult, err := DownloadMediaFromItem(context.Background(), MessageItem{
		Type: MessageItemTypeVideo,
		VideoItem: &VideoItem{
			Media: &CDNMedia{EncryptQueryParam: "video", AESKey: aesKey},
		},
	}, server.URL, server.Client(), saveMedia, nil)
	if err != nil {
		t.Fatalf("video DownloadMediaFromItem returned error: %v", err)
	}
	if videoResult.DecryptedVideoPath == "" {
		t.Fatalf("unexpected video result: %#v", videoResult)
	}

	if len(saved) != 3 {
		t.Fatalf("expected 3 saved media records, got %d", len(saved))
	}
	if saved[0].contentType != "audio/wav" || saved[0].data != "wav-bytes" {
		t.Fatalf("unexpected voice save: %#v", saved[0])
	}
	if saved[1].contentType != "application/pdf" || saved[1].name != "demo.pdf" || saved[1].data != "file-bytes" {
		t.Fatalf("unexpected file save: %#v", saved[1])
	}
	if saved[2].contentType != "video/mp4" || saved[2].data != "video-bytes" {
		t.Fatalf("unexpected video save: %#v", saved[2])
	}
}

func TestDownloadMediaFromItemImageEncryptedAndVoiceFallback(t *testing.T) {
	t.Parallel()

	key := []byte("1234567890abcdef")
	hexKey := "31323334353637383930616263646566"
	makeCiphertext := func(plain string) []byte {
		buf, err := EncryptAESECB([]byte(plain), key)
		if err != nil {
			t.Fatalf("EncryptAESECB returned error: %v", err)
		}
		return buf
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("encrypted_query_param") {
		case "image":
			_, _ = w.Write(makeCiphertext("image-bytes"))
		case "voice":
			_, _ = w.Write(makeCiphertext("voice-silk"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var saved []struct {
		contentType string
		data        string
	}
	saveMedia := func(buffer []byte, contentType, subdir string, maxBytes int64, originalFilename string) (string, error) {
		saved = append(saved, struct {
			contentType string
			data        string
		}{contentType: contentType, data: string(buffer)})
		return "/tmp/result.bin", nil
	}

	imageResult, err := DownloadMediaFromItem(context.Background(), MessageItem{
		Type: MessageItemTypeImage,
		ImageItem: &ImageItem{
			AESKeyHex: hexKey,
			Media: &CDNMedia{
				EncryptQueryParam: "image",
			},
		},
	}, server.URL, server.Client(), saveMedia, nil)
	if err != nil {
		t.Fatalf("image DownloadMediaFromItem returned error: %v", err)
	}
	if imageResult.DecryptedPicPath != "/tmp/result.bin" {
		t.Fatalf("unexpected image result: %#v", imageResult)
	}

	voiceResult, err := DownloadMediaFromItem(context.Background(), MessageItem{
		Type: MessageItemTypeVoice,
		VoiceItem: &VoiceItem{
			Media: &CDNMedia{
				EncryptQueryParam: "voice",
				AESKey:            base64.StdEncoding.EncodeToString([]byte(hexKey)),
			},
		},
	}, server.URL, server.Client(), saveMedia, func([]byte) ([]byte, error) {
		return nil, errors.New("decode failed")
	})
	if err != nil {
		t.Fatalf("voice fallback DownloadMediaFromItem returned error: %v", err)
	}
	if voiceResult.DecryptedVoicePath != "/tmp/result.bin" || voiceResult.VoiceMediaType != "audio/silk" {
		t.Fatalf("unexpected voice fallback result: %#v", voiceResult)
	}

	if len(saved) != 2 {
		t.Fatalf("expected 2 saved media records, got %d", len(saved))
	}
	if saved[0].data != "image-bytes" {
		t.Fatalf("unexpected decrypted image save: %#v", saved[0])
	}
	if saved[1].contentType != "audio/silk" || saved[1].data != "voice-silk" {
		t.Fatalf("unexpected silk fallback save: %#v", saved[1])
	}
}

func TestMonitorValidationAndFlow(t *testing.T) {
	t.Parallel()

	if err := Monitor(context.Background(), MonitorOptions{}); err == nil || !strings.Contains(err.Error(), "monitor API client is nil") {
		t.Fatalf("unexpected nil API error: %v", err)
	}

	api := NewAPIClient(APIOptions{})
	if err := Monitor(context.Background(), MonitorOptions{API: api}); err == nil || !strings.Contains(err.Error(), "monitor OnMessages callback is nil") {
		t.Fatalf("unexpected nil callback error: %v", err)
	}

	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			_ = json.NewEncoder(w).Encode(GetUpdatesResponse{
				Ret:           0,
				GetUpdatesBuf: "buf-next",
				Messages: []WeixinMessage{
					{FromUserID: "allowed", ItemList: []MessageItem{{Type: MessageItemTypeText, TextItem: &TextItem{Text: "hello"}}}},
					{FromUserID: "blocked", ItemList: []MessageItem{{Type: MessageItemTypeText, TextItem: &TextItem{Text: "skip"}}}},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(GetUpdatesResponse{Ret: 0, Messages: []WeixinMessage{}})
		}
	}))
	defer server.Close()

	syncPath := filepath.Join(t.TempDir(), "monitor.sync.json")
	wantErr := errors.New("stop monitor")
	err := Monitor(context.Background(), MonitorOptions{
		API:         NewAPIClient(APIOptions{BaseURL: server.URL, HTTPClient: server.Client()}),
		AccountID:   "bot@im.bot",
		SyncBufPath: syncPath,
		AllowFrom:   []string{"allowed"},
		OnMessages: func(ctx context.Context, messages []WeixinMessage) error {
			if len(messages) != 1 || messages[0].FromUserID != "allowed" {
				t.Fatalf("unexpected filtered messages: %#v", messages)
			}
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("unexpected monitor error: %v", err)
	}

	buf, err := LoadSyncBuffer(syncPath)
	if err != nil {
		t.Fatalf("LoadSyncBuffer returned error: %v", err)
	}
	if buf != "buf-next" {
		t.Fatalf("unexpected persisted sync buf: %q", buf)
	}
}

func TestMonitorStatusPathCancelsCleanly(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(GetUpdatesResponse{Ret: 0, Messages: []WeixinMessage{}})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var statusCalls int
	err := Monitor(ctx, MonitorOptions{
		API: NewAPIClient(APIOptions{
			BaseURL:    server.URL,
			HTTPClient: server.Client(),
		}),
		OnMessages: func(context.Context, []WeixinMessage) error { return nil },
		OnStatus: func(time.Time) {
			statusCalls++
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected monitor cancel error: %v", err)
	}
	if statusCalls == 0 {
		t.Fatalf("expected status callback to run")
	}
}

func TestMIMEFromFilename(t *testing.T) {
	t.Parallel()

	if got := MIMEFromFilename("demo.PDF"); got != "application/pdf" {
		t.Fatalf("unexpected MIME type: %q", got)
	}
	if got := MIMEFromFilename("demo.unknown"); got != "application/octet-stream" {
		t.Fatalf("unexpected fallback MIME type: %q", got)
	}
}

func TestJoinURLAndPrintQRCodeErrors(t *testing.T) {
	t.Parallel()

	u, err := joinURL("https://example.com/base/", "/v1/path")
	if err != nil {
		t.Fatalf("joinURL returned error: %v", err)
	}
	if got := u.String(); got != "https://example.com/base/v1/path" {
		t.Fatalf("unexpected joined URL: %q", got)
	}

	if err := PrintQRCode(&bytes.Buffer{}, " "); err == nil || !strings.Contains(err.Error(), "QR content is empty") {
		t.Fatalf("unexpected PrintQRCode error: %v", err)
	}
}

func TestWaitLoginErrorBranches(t *testing.T) {
	t.Parallel()

	client := NewClient(Options{
		PollInterval: 5 * time.Millisecond,
	})
	if _, err := client.WaitLogin(context.Background(), nil, WaitOptions{}); err == nil || !strings.Contains(err.Error(), "login session is nil") {
		t.Fatalf("unexpected nil session error: %v", err)
	}

	expired := &LoginSession{QRCode: "qr", StartedAt: time.Now().Add(-2 * DefaultQRSessionTTL)}
	if _, err := client.WaitLogin(context.Background(), expired, WaitOptions{}); err == nil || !strings.Contains(err.Error(), "login session expired") {
		t.Fatalf("unexpected expired session error: %v", err)
	}

	cases := []struct {
		name       string
		statusResp map[string]string
		maxRefresh int
		wantErr    string
	}{
		{
			name: "missing account on confirmed",
			statusResp: map[string]string{
				"status":    "confirmed",
				"bot_token": "token",
			},
			maxRefresh: 3,
			wantErr:    "ilink_bot_id is missing",
		},
		{
			name: "unexpected status",
			statusResp: map[string]string{
				"status": "mystery",
			},
			maxRefresh: 3,
			wantErr:    `unexpected QR status "mystery"`,
		},
		{
			name: "expired too many times",
			statusResp: map[string]string{
				"status": "expired",
			},
			maxRefresh: 1,
			wantErr:    "expired too many times",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/ilink/bot/get_qrcode_status":
					_ = json.NewEncoder(w).Encode(tc.statusResp)
				case "/ilink/bot/get_bot_qrcode":
					_ = json.NewEncoder(w).Encode(map[string]string{
						"qrcode":             "qr-new",
						"qrcode_img_content": "weixin://new",
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			client := NewClient(Options{
				BaseURL:      server.URL,
				HTTPClient:   server.Client(),
				PollInterval: 5 * time.Millisecond,
				MaxQRRefresh: tc.maxRefresh,
			})
			session := &LoginSession{QRCode: "qr", StartedAt: time.Now()}
			_, err := client.WaitLogin(context.Background(), session, WaitOptions{Timeout: 50 * time.Millisecond})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected WaitLogin error: %v", err)
			}
		})
	}
}

func TestStartLoginAndFetchQRCodePaths(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ilink/bot/get_bot_qrcode" {
				t.Fatalf("unexpected path: %q", r.URL.Path)
			}
			if got := r.URL.Query().Get("bot_type"); got != "9" {
				t.Fatalf("unexpected bot_type: %q", got)
			}
			if got := r.Header.Get("SKRouteTag"); got != "route-a" {
				t.Fatalf("unexpected route tag header: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"qrcode":             "qr-123",
				"qrcode_img_content": "weixin://qr-123",
			})
		}))
		defer server.Close()

		client := NewClient(Options{
			BaseURL:    server.URL,
			BotType:    "9",
			RouteTag:   "route-a",
			HTTPClient: server.Client(),
		})
		session, err := client.StartLogin(context.Background(), "  hint-1  ")
		if err != nil {
			t.Fatalf("StartLogin returned error: %v", err)
		}
		if session.QRCode != "qr-123" || session.QRContent != "weixin://qr-123" || session.AccountHint != "hint-1" || session.SessionKey == "" {
			t.Fatalf("unexpected login session: %#v", session)
		}
	})

	t.Run("empty payload", func(t *testing.T) {
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
		_, err := client.fetchQRCode(context.Background())
		if err == nil || !strings.Contains(err.Error(), "empty QR payload") {
			t.Fatalf("unexpected fetchQRCode error: %v", err)
		}
	})
}

func TestPollQRStatusTimeoutReturnsWait(t *testing.T) {
	t.Parallel()

	client := NewClient(Options{
		BaseURL:           "https://example.com",
		QRLongPollTimeout: 10 * time.Millisecond,
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("context deadline exceeded")
			}),
		},
	})

	status, err := client.pollQRStatus(context.Background(), "qr-1")
	if err != nil {
		t.Fatalf("pollQRStatus returned error: %v", err)
	}
	if status.Status != "wait" {
		t.Fatalf("unexpected timeout fallback status: %#v", status)
	}
}

func TestCDNErrorAndPaddingPaths(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cdn failed", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := downloadCDNBytes(context.Background(), server.Client(), server.URL+"/download")
	if err == nil || !strings.Contains(err.Error(), "CDN download 502") {
		t.Fatalf("unexpected CDN download error: %v", err)
	}

	if _, err := pkcs7Unpad([]byte{1, 2, 3}, 16); err == nil || !strings.Contains(err.Error(), "invalid PKCS7 data length") {
		t.Fatalf("unexpected PKCS7 length error: %v", err)
	}
	if _, err := pkcs7Unpad(append(bytes.Repeat([]byte{'a'}, 15), 0), 16); err == nil || !strings.Contains(err.Error(), "invalid PKCS7 padding") {
		t.Fatalf("unexpected PKCS7 padding error: %v", err)
	}
}
