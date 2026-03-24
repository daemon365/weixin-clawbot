package weixin

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultLongPollTimeout = 35 * time.Second
	defaultAPITimeout      = 15 * time.Second
	defaultConfigTimeout   = 10 * time.Second

	SessionExpiredErrCode = -14
	sessionPauseDuration  = time.Hour

	configCacheTTL          = 24 * time.Hour
	configCacheInitialRetry = 2 * time.Second
	configCacheMaxRetry     = time.Hour
)

var pauseState struct {
	sync.Mutex
	until map[string]time.Time
}

func init() {
	pauseState.until = make(map[string]time.Time)
}

type APIOptions struct {
	BaseURL        string
	Token          string
	RouteTag       string
	ChannelVersion string
	HTTPClient     *http.Client
	AccountID      string
}

type APIClient struct {
	baseURL        string
	token          string
	routeTag       string
	channelVersion string
	httpClient     *http.Client
	accountID      string
}

type CachedConfig struct {
	TypingTicket string
}

type configCacheEntry struct {
	config        CachedConfig
	everSucceeded bool
	nextFetchAt   time.Time
	retryDelay    time.Duration
}

type ConfigManager struct {
	api   *APIClient
	now   func() time.Time
	rand  *mathrand.Rand
	mu    sync.Mutex
	cache map[string]configCacheEntry
}

func NewAPIClient(opts APIOptions) *APIClient {
	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	channelVersion := strings.TrimSpace(opts.ChannelVersion)
	if channelVersion == "" {
		channelVersion = "go-port"
	}

	return &APIClient{
		baseURL:        baseURL,
		token:          strings.TrimSpace(opts.Token),
		routeTag:       strings.TrimSpace(opts.RouteTag),
		channelVersion: channelVersion,
		httpClient:     httpClient,
		accountID:      strings.TrimSpace(opts.AccountID),
	}
}

func NewConfigManager(api *APIClient) *ConfigManager {
	return &ConfigManager{
		api:   api,
		now:   time.Now,
		rand:  mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
		cache: make(map[string]configCacheEntry),
	}
}

func (c *APIClient) BuildBaseInfo() BaseInfo {
	return BaseInfo{ChannelVersion: c.channelVersion}
}

func (c *APIClient) GetUpdates(ctx context.Context, req GetUpdatesRequest, timeout time.Duration) (*GetUpdatesResponse, error) {
	if timeout <= 0 {
		timeout = defaultLongPollTimeout
	}

	var resp GetUpdatesResponse
	err := c.postJSON(ctx, "ilink/bot/getupdates", map[string]any{
		"get_updates_buf": req.GetUpdatesBuf,
		"base_info":       c.BuildBaseInfo(),
	}, timeout, &resp)
	if isContextTimeout(err) {
		return &GetUpdatesResponse{
			Ret:           0,
			Messages:      []WeixinMessage{},
			GetUpdatesBuf: req.GetUpdatesBuf,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *APIClient) GetUploadURL(ctx context.Context, req GetUploadURLRequest, timeout time.Duration) (*GetUploadURLResponse, error) {
	if err := c.assertSession(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultAPITimeout
	}

	payload := map[string]any{
		"filekey":          req.FileKey,
		"media_type":       req.MediaType,
		"to_user_id":       req.ToUserID,
		"rawsize":          req.RawSize,
		"rawfilemd5":       req.RawFileMD5,
		"filesize":         req.FileSize,
		"thumb_rawsize":    req.ThumbRawSize,
		"thumb_rawfilemd5": req.ThumbRawFileMD5,
		"thumb_filesize":   req.ThumbFileSize,
		"no_need_thumb":    req.NoNeedThumb,
		"aeskey":           req.AESKey,
		"base_info":        c.BuildBaseInfo(),
	}

	var resp GetUploadURLResponse
	if err := c.postJSON(ctx, "ilink/bot/getuploadurl", payload, timeout, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *APIClient) SendMessage(ctx context.Context, req SendMessageRequest, timeout time.Duration) error {
	if err := c.assertSession(); err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = defaultAPITimeout
	}
	return c.postJSON(ctx, "ilink/bot/sendmessage", map[string]any{
		"msg":       req.Message,
		"base_info": c.BuildBaseInfo(),
	}, timeout, nil)
}

func (c *APIClient) GetConfig(ctx context.Context, ilinkUserID, contextToken string, timeout time.Duration) (*GetConfigResponse, error) {
	if err := c.assertSession(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultConfigTimeout
	}
	var resp GetConfigResponse
	if err := c.postJSON(ctx, "ilink/bot/getconfig", map[string]any{
		"ilink_user_id": ilinkUserID,
		"context_token": contextToken,
		"base_info":     c.BuildBaseInfo(),
	}, timeout, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *APIClient) SendTyping(ctx context.Context, req SendTypingRequest, timeout time.Duration) error {
	if err := c.assertSession(); err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = defaultConfigTimeout
	}
	return c.postJSON(ctx, "ilink/bot/sendtyping", map[string]any{
		"ilink_user_id": req.ILinkUserID,
		"typing_ticket": req.TypingTicket,
		"status":        req.Status,
		"base_info":     c.BuildBaseInfo(),
	}, timeout, nil)
}

func (c *APIClient) postJSON(ctx context.Context, endpoint string, payload any, timeout time.Duration, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpointURL, err := joinURL(c.baseURL, endpoint)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpointURL.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}

	for key, value := range c.buildHeaders(body) {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s JSON: %w", endpoint, err)
	}
	return nil
}

func (c *APIClient) buildHeaders(body []byte) map[string]string {
	headers := map[string]string{
		"Content-Type":      "application/json",
		"AuthorizationType": "ilink_bot_token",
		"Content-Length":    fmt.Sprintf("%d", len(body)),
		"X-WECHAT-UIN":      randomWechatUIN(),
	}
	if c.token != "" {
		headers["Authorization"] = "Bearer " + c.token
	}
	if c.routeTag != "" {
		headers["SKRouteTag"] = c.routeTag
	}
	return headers
}

func (c *APIClient) assertSession() error {
	if c.accountID == "" {
		return nil
	}
	return AssertSessionActive(c.accountID)
}

func randomWechatUIN() string {
	buf := make([]byte, 4)
	if _, err := cryptoRand.Read(buf); err != nil {
		return base64.StdEncoding.EncodeToString([]byte("0"))
	}
	n := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", n)))
}

func PauseSession(accountID string) {
	pauseState.Lock()
	defer pauseState.Unlock()
	pauseState.until[accountID] = time.Now().Add(sessionPauseDuration)
}

func IsSessionPaused(accountID string) bool {
	return RemainingPause(accountID) > 0
}

func RemainingPause(accountID string) time.Duration {
	pauseState.Lock()
	defer pauseState.Unlock()

	until, ok := pauseState.until[accountID]
	if !ok {
		return 0
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		delete(pauseState.until, accountID)
		return 0
	}
	return remaining
}

func AssertSessionActive(accountID string) error {
	if remaining := RemainingPause(accountID); remaining > 0 {
		return fmt.Errorf("session paused for account_id=%s, %d min remaining (errcode %d)", accountID, int(remaining.Minutes()+0.999), SessionExpiredErrCode)
	}
	return nil
}

func resetSessionGuardForTest() {
	pauseState.Lock()
	defer pauseState.Unlock()
	pauseState.until = make(map[string]time.Time)
}

func (m *ConfigManager) GetForUser(ctx context.Context, userID, contextToken string) (CachedConfig, error) {
	m.mu.Lock()
	entry, ok := m.cache[userID]
	now := m.now()
	shouldFetch := !ok || !now.Before(entry.nextFetchAt)
	m.mu.Unlock()

	if shouldFetch {
		resp, err := m.api.GetConfig(ctx, userID, contextToken, 0)
		if err == nil && resp.Ret == 0 {
			next := configCacheEntry{
				config:        CachedConfig{TypingTicket: resp.TypingTicket},
				everSucceeded: true,
				nextFetchAt:   now.Add(time.Duration(m.rand.Float64() * float64(configCacheTTL))),
				retryDelay:    configCacheInitialRetry,
			}
			m.mu.Lock()
			m.cache[userID] = next
			m.mu.Unlock()
			return next.config, nil
		}

		m.mu.Lock()
		defer m.mu.Unlock()
		if ok {
			entry.retryDelay *= 2
			if entry.retryDelay > configCacheMaxRetry {
				entry.retryDelay = configCacheMaxRetry
			}
			entry.nextFetchAt = now.Add(entry.retryDelay)
			m.cache[userID] = entry
			return entry.config, err
		}

		m.cache[userID] = configCacheEntry{
			config:      CachedConfig{},
			nextFetchAt: now.Add(configCacheInitialRetry),
			retryDelay:  configCacheInitialRetry,
		}
		return CachedConfig{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cache[userID].config, nil
}

func packageVersionFromPath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "unknown"
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "unknown"
	}
	if payload.Version == "" {
		return "unknown"
	}
	return payload.Version
}

func isContextTimeout(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "Client.Timeout exceeded"))
}
