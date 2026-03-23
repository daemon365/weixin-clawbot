package weixin

import (
	"strings"
	"sync"
)

var contextTokenStore struct {
	sync.RWMutex
	values map[string]string
}

func init() {
	contextTokenStore.values = make(map[string]string)
}

type MessageContext struct {
	Body              string
	From              string
	To                string
	AccountID         string
	OriginatingTo     string
	MessageSID        string
	Timestamp         int64
	Provider          string
	ChatType          string
	SessionKey        string
	ContextToken      string
	MediaURL          string
	MediaPath         string
	MediaType         string
	CommandBody       string
	CommandAuthorized bool
}

type InboundMediaOptions struct {
	DecryptedPicPath   string
	DecryptedVoicePath string
	VoiceMediaType     string
	DecryptedFilePath  string
	FileMediaType      string
	DecryptedVideoPath string
}

func SetContextToken(accountID, userID, token string) {
	contextTokenStore.Lock()
	defer contextTokenStore.Unlock()
	contextTokenStore.values[accountID+":"+userID] = token
}

func GetContextToken(accountID, userID string) string {
	contextTokenStore.RLock()
	defer contextTokenStore.RUnlock()
	return contextTokenStore.values[accountID+":"+userID]
}

func IsMediaItem(item MessageItem) bool {
	return item.Type == MessageItemTypeImage ||
		item.Type == MessageItemTypeVideo ||
		item.Type == MessageItemTypeFile ||
		item.Type == MessageItemTypeVoice
}

func BodyFromItemList(items []MessageItem) string {
	for _, item := range items {
		if item.Type == MessageItemTypeText && item.TextItem != nil {
			text := item.TextItem.Text
			if item.RefMessage == nil {
				return text
			}
			if item.RefMessage.MessageItem != nil && IsMediaItem(*item.RefMessage.MessageItem) {
				return text
			}
			parts := make([]string, 0, 2)
			if item.RefMessage.Title != "" {
				parts = append(parts, item.RefMessage.Title)
			}
			if item.RefMessage.MessageItem != nil {
				if refBody := BodyFromItemList([]MessageItem{*item.RefMessage.MessageItem}); refBody != "" {
					parts = append(parts, refBody)
				}
			}
			if len(parts) == 0 {
				return text
			}
			return "[引用: " + strings.Join(parts, " | ") + "]\n" + text
		}
		if item.Type == MessageItemTypeVoice && item.VoiceItem != nil && item.VoiceItem.Text != "" {
			return item.VoiceItem.Text
		}
	}
	return ""
}

func WeixinMessageToContext(msg WeixinMessage, accountID string, opts *InboundMediaOptions) MessageContext {
	fromUserID := msg.FromUserID
	ctx := MessageContext{
		Body:          BodyFromItemList(msg.ItemList),
		From:          fromUserID,
		To:            fromUserID,
		AccountID:     accountID,
		OriginatingTo: fromUserID,
		MessageSID:    GenerateID("openclaw-weixin"),
		Timestamp:     msg.CreateTimeMS,
		Provider:      "openclaw-weixin",
		ChatType:      "direct",
		ContextToken:  msg.ContextToken,
	}
	if opts != nil {
		switch {
		case opts.DecryptedPicPath != "":
			ctx.MediaPath = opts.DecryptedPicPath
			ctx.MediaType = "image/*"
		case opts.DecryptedVideoPath != "":
			ctx.MediaPath = opts.DecryptedVideoPath
			ctx.MediaType = "video/mp4"
		case opts.DecryptedFilePath != "":
			ctx.MediaPath = opts.DecryptedFilePath
			ctx.MediaType = firstNonEmpty(opts.FileMediaType, "application/octet-stream")
		case opts.DecryptedVoicePath != "":
			ctx.MediaPath = opts.DecryptedVoicePath
			ctx.MediaType = firstNonEmpty(opts.VoiceMediaType, "audio/wav")
		}
	}
	return ctx
}
