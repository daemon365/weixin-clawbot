package weixin

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	reCodeBlock = regexp.MustCompile("(?s)```[^\\n]*\\n?(.*?)```")
	reImageMD   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLinkMD    = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reTableSep  = regexp.MustCompile(`(?m)^\|[\s:|-]+\|$`)
	reTableRow  = regexp.MustCompile(`(?m)^\|(.+)\|$`)
	reEmphasis  = regexp.MustCompile(`[*_~` + "`" + `]+`)
)

func MarkdownToPlainText(text string) string {
	result := reCodeBlock.ReplaceAllString(text, "$1")
	result = reImageMD.ReplaceAllString(result, "")
	result = reLinkMD.ReplaceAllString(result, "$1")
	result = reTableSep.ReplaceAllString(result, "")
	result = reTableRow.ReplaceAllStringFunc(result, func(row string) string {
		trimmed := strings.TrimPrefix(strings.TrimSuffix(row, "|"), "|")
		cells := strings.Split(trimmed, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		return strings.Join(cells, "  ")
	})
	result = reEmphasis.ReplaceAllString(result, "")
	return strings.TrimSpace(result)
}

type SendOptions struct {
	BaseURL        string
	Token          string
	RouteTag       string
	ChannelVersion string
	HTTPClient     *http.Client
	Timeout        time.Duration
	ContextToken   string
	AccountID      string
}

func SendMessageWeixin(ctx context.Context, to, text string, opts SendOptions) (string, error) {
	if opts.ContextToken == "" {
		return "", fmt.Errorf("sendMessageWeixin: contextToken is required")
	}

	clientID := GenerateID("openclaw-weixin")
	api := NewAPIClient(APIOptions{
		BaseURL:        opts.BaseURL,
		Token:          opts.Token,
		RouteTag:       opts.RouteTag,
		ChannelVersion: opts.ChannelVersion,
		HTTPClient:     opts.HTTPClient,
		AccountID:      opts.AccountID,
	})
	req := buildTextMessageRequest(to, text, opts.ContextToken, clientID)
	if err := api.SendMessage(ctx, req, opts.Timeout); err != nil {
		return "", err
	}
	return clientID, nil
}

func SendImageMessageWeixin(ctx context.Context, to, text string, uploaded UploadedFileInfo, opts SendOptions) (string, error) {
	if opts.ContextToken == "" {
		return "", fmt.Errorf("sendImageMessageWeixin: contextToken is required")
	}
	item := MessageItem{
		Type: MessageItemTypeImage,
		ImageItem: &ImageItem{
			Media: &CDNMedia{
				EncryptQueryParam: uploaded.DownloadEncryptedQueryParam,
				AESKey:            base64.StdEncoding.EncodeToString([]byte(uploaded.AESKeyHex)),
				EncryptType:       1,
			},
			MidSize: uploaded.FileSizeCiphertext,
		},
	}
	return sendMediaItems(ctx, to, text, item, opts)
}

func SendVideoMessageWeixin(ctx context.Context, to, text string, uploaded UploadedFileInfo, opts SendOptions) (string, error) {
	if opts.ContextToken == "" {
		return "", fmt.Errorf("sendVideoMessageWeixin: contextToken is required")
	}
	item := MessageItem{
		Type: MessageItemTypeVideo,
		VideoItem: &VideoItem{
			Media: &CDNMedia{
				EncryptQueryParam: uploaded.DownloadEncryptedQueryParam,
				AESKey:            base64.StdEncoding.EncodeToString([]byte(uploaded.AESKeyHex)),
				EncryptType:       1,
			},
			VideoSize: uploaded.FileSizeCiphertext,
		},
	}
	return sendMediaItems(ctx, to, text, item, opts)
}

func SendFileMessageWeixin(ctx context.Context, to, text, fileName string, uploaded UploadedFileInfo, opts SendOptions) (string, error) {
	if opts.ContextToken == "" {
		return "", fmt.Errorf("sendFileMessageWeixin: contextToken is required")
	}
	item := MessageItem{
		Type: MessageItemTypeFile,
		FileItem: &FileItem{
			Media: &CDNMedia{
				EncryptQueryParam: uploaded.DownloadEncryptedQueryParam,
				AESKey:            base64.StdEncoding.EncodeToString([]byte(uploaded.AESKeyHex)),
				EncryptType:       1,
			},
			FileName: fileName,
			Length:   fmt.Sprintf("%d", uploaded.FileSize),
		},
	}
	return sendMediaItems(ctx, to, text, item, opts)
}

func buildTextMessageRequest(to, text, contextToken, clientID string) SendMessageRequest {
	items := make([]MessageItem, 0, 1)
	if text != "" {
		items = append(items, MessageItem{
			Type:     MessageItemTypeText,
			TextItem: &TextItem{Text: text},
		})
	}
	return SendMessageRequest{
		Message: &WeixinMessage{
			ToUserID:     to,
			ClientID:     clientID,
			MessageType:  MessageTypeBot,
			MessageState: MessageStateFinish,
			ItemList:     items,
			ContextToken: contextToken,
		},
	}
}

func sendMediaItems(ctx context.Context, to, text string, mediaItem MessageItem, opts SendOptions) (string, error) {
	api := NewAPIClient(APIOptions{
		BaseURL:        opts.BaseURL,
		Token:          opts.Token,
		RouteTag:       opts.RouteTag,
		ChannelVersion: opts.ChannelVersion,
		HTTPClient:     opts.HTTPClient,
		AccountID:      opts.AccountID,
	})

	items := make([]MessageItem, 0, 2)
	if text != "" {
		items = append(items, MessageItem{Type: MessageItemTypeText, TextItem: &TextItem{Text: text}})
	}
	items = append(items, mediaItem)

	var lastID string
	for _, item := range items {
		lastID = GenerateID("openclaw-weixin")
		err := api.SendMessage(ctx, SendMessageRequest{
			Message: &WeixinMessage{
				ToUserID:     to,
				ClientID:     lastID,
				MessageType:  MessageTypeBot,
				MessageState: MessageStateFinish,
				ItemList:     []MessageItem{item},
				ContextToken: opts.ContextToken,
			},
		}, opts.Timeout)
		if err != nil {
			return "", err
		}
	}
	return lastID, nil
}
