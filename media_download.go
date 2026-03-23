package weixin

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

const weixinMediaMaxBytes = 100 * 1024 * 1024

type SaveMediaFunc func(buffer []byte, contentType, subdir string, maxBytes int64, originalFilename string) (string, error)
type SilkToWAVFunc func(silk []byte) ([]byte, error)

func SaveMediaToDir(rootDir string) SaveMediaFunc {
	return func(buffer []byte, contentType, subdir string, maxBytes int64, originalFilename string) (string, error) {
		if maxBytes > 0 && int64(len(buffer)) > maxBytes {
			return "", fmt.Errorf("media too large: %d > %d", len(buffer), maxBytes)
		}
		dir := rootDir
		if subdir != "" {
			dir = filepath.Join(dir, subdir)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}

		name := originalFilename
		if name == "" {
			ext := ExtensionFromMIME(contentType)
			name = TempFileName("weixin-media", ext)
		}
		filePath := filepath.Join(dir, name)
		if err := os.WriteFile(filePath, buffer, 0o600); err != nil {
			return "", err
		}
		return filePath, nil
	}
}

func DownloadMediaFromItem(ctx context.Context, item MessageItem, cdnBaseURL string, httpClient *http.Client, saveMedia SaveMediaFunc, silkToWAV SilkToWAVFunc) (*InboundMediaOptions, error) {
	result := &InboundMediaOptions{}
	switch item.Type {
	case MessageItemTypeImage:
		if item.ImageItem == nil || item.ImageItem.Media == nil || item.ImageItem.Media.EncryptQueryParam == "" {
			return result, nil
		}
		aesKeyBase64 := item.ImageItem.Media.AESKey
		if item.ImageItem.AESKeyHex != "" {
			aesKeyBase64 = base64.StdEncoding.EncodeToString([]byte(item.ImageItem.AESKeyHex))
		}
		var data []byte
		var err error
		if aesKeyBase64 != "" {
			data, err = DownloadAndDecryptBuffer(ctx, httpClient, item.ImageItem.Media.EncryptQueryParam, aesKeyBase64, cdnBaseURL)
		} else {
			data, err = DownloadPlainCDNBuffer(ctx, httpClient, item.ImageItem.Media.EncryptQueryParam, cdnBaseURL)
		}
		if err != nil {
			return result, err
		}
		path, err := saveMedia(data, "", "inbound", weixinMediaMaxBytes, "")
		if err != nil {
			return result, err
		}
		result.DecryptedPicPath = path
	case MessageItemTypeVoice:
		if item.VoiceItem == nil || item.VoiceItem.Media == nil || item.VoiceItem.Media.EncryptQueryParam == "" || item.VoiceItem.Media.AESKey == "" {
			return result, nil
		}
		silkBuf, err := DownloadAndDecryptBuffer(ctx, httpClient, item.VoiceItem.Media.EncryptQueryParam, item.VoiceItem.Media.AESKey, cdnBaseURL)
		if err != nil {
			return result, err
		}
		if silkToWAV != nil {
			if wavBuf, err := silkToWAV(silkBuf); err == nil && len(wavBuf) > 0 {
				path, err := saveMedia(wavBuf, "audio/wav", "inbound", weixinMediaMaxBytes, "")
				if err != nil {
					return result, err
				}
				result.DecryptedVoicePath = path
				result.VoiceMediaType = "audio/wav"
				return result, nil
			}
		}
		path, err := saveMedia(silkBuf, "audio/silk", "inbound", weixinMediaMaxBytes, "")
		if err != nil {
			return result, err
		}
		result.DecryptedVoicePath = path
		result.VoiceMediaType = "audio/silk"
	case MessageItemTypeFile:
		if item.FileItem == nil || item.FileItem.Media == nil || item.FileItem.Media.EncryptQueryParam == "" || item.FileItem.Media.AESKey == "" {
			return result, nil
		}
		data, err := DownloadAndDecryptBuffer(ctx, httpClient, item.FileItem.Media.EncryptQueryParam, item.FileItem.Media.AESKey, cdnBaseURL)
		if err != nil {
			return result, err
		}
		mime := MIMEFromFilename(firstNonEmpty(item.FileItem.FileName, "file.bin"))
		path, err := saveMedia(data, mime, "inbound", weixinMediaMaxBytes, item.FileItem.FileName)
		if err != nil {
			return result, err
		}
		result.DecryptedFilePath = path
		result.FileMediaType = mime
	case MessageItemTypeVideo:
		if item.VideoItem == nil || item.VideoItem.Media == nil || item.VideoItem.Media.EncryptQueryParam == "" || item.VideoItem.Media.AESKey == "" {
			return result, nil
		}
		data, err := DownloadAndDecryptBuffer(ctx, httpClient, item.VideoItem.Media.EncryptQueryParam, item.VideoItem.Media.AESKey, cdnBaseURL)
		if err != nil {
			return result, err
		}
		path, err := saveMedia(data, "video/mp4", "inbound", weixinMediaMaxBytes, "")
		if err != nil {
			return result, err
		}
		result.DecryptedVideoPath = path
	}
	return result, nil
}
