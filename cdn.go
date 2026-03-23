package weixin

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const uploadMaxRetries = 3

type UploadedFileInfo struct {
	FileKey                     string
	DownloadEncryptedQueryParam string
	AESKeyHex                   string
	FileSize                    int64
	FileSizeCiphertext          int64
}

func EncryptAESECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plaintext, block.BlockSize())
	out := make([]byte, len(padded))
	for bs := 0; bs < len(padded); bs += block.BlockSize() {
		block.Encrypt(out[bs:bs+block.BlockSize()], padded[bs:bs+block.BlockSize()])
	}
	return out, nil
}

func DecryptAESECB(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("ciphertext size %d is not multiple of block size", len(ciphertext))
	}
	out := make([]byte, len(ciphertext))
	for bs := 0; bs < len(ciphertext); bs += block.BlockSize() {
		block.Decrypt(out[bs:bs+block.BlockSize()], ciphertext[bs:bs+block.BlockSize()])
	}
	return pkcs7Unpad(out, block.BlockSize())
}

func AESECBPaddedSize(plaintextSize int64) int64 {
	return ((plaintextSize / 16) + 1) * 16
}

func BuildCDNDownloadURL(encryptedQueryParam, cdnBaseURL string) string {
	return strings.TrimRight(cdnBaseURL, "/") + "/download?encrypted_query_param=" + url.QueryEscape(encryptedQueryParam)
}

func BuildCDNUploadURL(cdnBaseURL, uploadParam, fileKey string) string {
	return strings.TrimRight(cdnBaseURL, "/") + "/upload?encrypted_query_param=" + url.QueryEscape(uploadParam) + "&filekey=" + url.QueryEscape(fileKey)
}

func UploadBufferToCDN(ctx context.Context, httpClient *http.Client, buf []byte, uploadParam, fileKey, cdnBaseURL string, aesKey []byte) (string, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	ciphertext, err := EncryptAESECB(buf, aesKey)
	if err != nil {
		return "", err
	}
	uploadURL := BuildCDNUploadURL(cdnBaseURL, uploadParam, fileKey)

	var lastErr error
	for attempt := 1; attempt <= uploadMaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(ciphertext))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			msg := firstNonEmpty(resp.Header.Get("x-error-message"), strings.TrimSpace(string(body)))
			return "", fmt.Errorf("CDN upload client error %d: %s", resp.StatusCode, msg)
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("CDN upload server error %d: %s", resp.StatusCode, firstNonEmpty(resp.Header.Get("x-error-message"), strings.TrimSpace(string(body))))
			continue
		}
		downloadParam := strings.TrimSpace(resp.Header.Get("x-encrypted-param"))
		if downloadParam == "" {
			lastErr = fmt.Errorf("CDN upload missing x-encrypted-param header")
			continue
		}
		return downloadParam, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("CDN upload failed after %d attempts", uploadMaxRetries)
	}
	return "", lastErr
}

func DownloadAndDecryptBuffer(ctx context.Context, httpClient *http.Client, encryptedQueryParam, aesKeyBase64, cdnBaseURL string) ([]byte, error) {
	key, err := parseAESKey(aesKeyBase64)
	if err != nil {
		return nil, err
	}
	raw, err := downloadCDNBytes(ctx, httpClient, BuildCDNDownloadURL(encryptedQueryParam, cdnBaseURL))
	if err != nil {
		return nil, err
	}
	return DecryptAESECB(raw, key)
}

func DownloadPlainCDNBuffer(ctx context.Context, httpClient *http.Client, encryptedQueryParam, cdnBaseURL string) ([]byte, error) {
	return downloadCDNBytes(ctx, httpClient, BuildCDNDownloadURL(encryptedQueryParam, cdnBaseURL))
}

func DownloadRemoteMediaToTemp(ctx context.Context, httpClient *http.Client, rawURL, destDir string) (string, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("remote media download failed: %d %s", resp.StatusCode, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	ext := ExtensionFromContentTypeOrURL(resp.Header.Get("Content-Type"), rawURL)
	filePath := filepath.Join(destDir, TempFileName("weixin-remote", ext))
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return "", err
	}
	return filePath, nil
}

func UploadFileToWeixin(ctx context.Context, filePath, toUserID, cdnBaseURL string, apiOpts APIOptions) (*UploadedFileInfo, error) {
	return uploadMediaToCDN(ctx, filePath, toUserID, cdnBaseURL, UploadMediaTypeImage, apiOpts)
}

func UploadVideoToWeixin(ctx context.Context, filePath, toUserID, cdnBaseURL string, apiOpts APIOptions) (*UploadedFileInfo, error) {
	return uploadMediaToCDN(ctx, filePath, toUserID, cdnBaseURL, UploadMediaTypeVideo, apiOpts)
}

func UploadFileAttachmentToWeixin(ctx context.Context, filePath, toUserID, cdnBaseURL string, apiOpts APIOptions) (*UploadedFileInfo, error) {
	return uploadMediaToCDN(ctx, filePath, toUserID, cdnBaseURL, UploadMediaTypeFile, apiOpts)
}

func SendWeixinMediaFile(ctx context.Context, filePath, to, text, cdnBaseURL string, opts SendOptions) (string, error) {
	apiOpts := APIOptions{
		BaseURL:        opts.BaseURL,
		Token:          opts.Token,
		RouteTag:       opts.RouteTag,
		ChannelVersion: opts.ChannelVersion,
		HTTPClient:     opts.HTTPClient,
		AccountID:      opts.AccountID,
	}
	mime := MIMEFromFilename(filePath)
	switch {
	case strings.HasPrefix(mime, "video/"):
		uploaded, err := UploadVideoToWeixin(ctx, filePath, to, cdnBaseURL, apiOpts)
		if err != nil {
			return "", err
		}
		return SendVideoMessageWeixin(ctx, to, text, *uploaded, opts)
	case strings.HasPrefix(mime, "image/"):
		uploaded, err := UploadFileToWeixin(ctx, filePath, to, cdnBaseURL, apiOpts)
		if err != nil {
			return "", err
		}
		return SendImageMessageWeixin(ctx, to, text, *uploaded, opts)
	default:
		uploaded, err := UploadFileAttachmentToWeixin(ctx, filePath, to, cdnBaseURL, apiOpts)
		if err != nil {
			return "", err
		}
		return SendFileMessageWeixin(ctx, to, text, filepath.Base(filePath), *uploaded, opts)
	}
}

func uploadMediaToCDN(ctx context.Context, filePath, toUserID, cdnBaseURL string, mediaType int, apiOpts APIOptions) (*UploadedFileInfo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	md5sum := md5.Sum(data)
	fileSize := int64(len(data))
	fileSizeCiphertext := AESECBPaddedSize(fileSize)
	fileKey := randomHex(16)
	aesKey := make([]byte, 16)
	if _, err := rand.Read(aesKey); err != nil {
		return nil, err
	}

	api := NewAPIClient(apiOpts)
	uploadURLResp, err := api.GetUploadURL(ctx, GetUploadURLRequest{
		FileKey:     fileKey,
		MediaType:   mediaType,
		ToUserID:    toUserID,
		RawSize:     fileSize,
		RawFileMD5:  hex.EncodeToString(md5sum[:]),
		FileSize:    fileSizeCiphertext,
		NoNeedThumb: true,
		AESKey:      hex.EncodeToString(aesKey),
	}, 0)
	if err != nil {
		return nil, err
	}
	if uploadURLResp.UploadParam == "" {
		return nil, fmt.Errorf("getUploadURL returned no upload_param")
	}

	downloadParam, err := UploadBufferToCDN(ctx, apiOpts.HTTPClient, data, uploadURLResp.UploadParam, fileKey, cdnBaseURL, aesKey)
	if err != nil {
		return nil, err
	}

	return &UploadedFileInfo{
		FileKey:                     fileKey,
		DownloadEncryptedQueryParam: downloadParam,
		AESKeyHex:                   hex.EncodeToString(aesKey),
		FileSize:                    fileSize,
		FileSizeCiphertext:          fileSizeCiphertext,
	}, nil
}

func downloadCDNBytes(ctx context.Context, httpClient *http.Client, rawURL string) ([]byte, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("CDN download %d %s: %s", resp.StatusCode, resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func parseAESKey(aesKeyBase64 string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(aesKeyBase64)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 16 {
		return decoded, nil
	}
	if len(decoded) == 32 && isHexASCII(decoded) {
		return hex.DecodeString(string(decoded))
	}
	return nil, fmt.Errorf("aes_key must decode to 16 raw bytes or 32-char hex string, got %d bytes", len(decoded))
}

func isHexASCII(buf []byte) bool {
	for _, b := range buf {
		switch {
		case b >= '0' && b <= '9':
		case b >= 'a' && b <= 'f':
		case b >= 'A' && b <= 'F':
		default:
			return false
		}
	}
	return true
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - (len(data) % blockSize)
	if padLen == 0 {
		padLen = blockSize
	}
	return append(data, bytes.Repeat([]byte{byte(padLen)}, padLen)...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid PKCS7 data length")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > blockSize || padLen > len(data) {
		return nil, fmt.Errorf("invalid PKCS7 padding")
	}
	for _, b := range data[len(data)-padLen:] {
		if int(b) != padLen {
			return nil, fmt.Errorf("invalid PKCS7 padding")
		}
	}
	return data[:len(data)-padLen], nil
}
