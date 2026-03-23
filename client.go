package weixin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
)

const (
	DefaultBaseURL           = "https://ilinkai.weixin.qq.com"
	DefaultBotType           = "3"
	DefaultQRSessionTTL      = 5 * time.Minute
	DefaultQRLongPollTimeout = 35 * time.Second
	DefaultLoginTimeout      = 8 * time.Minute
	DefaultPollInterval      = time.Second
	DefaultMaxQRRefresh      = 3
)

type Options struct {
	BaseURL           string
	BotType           string
	RouteTag          string
	HTTPClient        *http.Client
	Output            io.Writer
	QRSessionTTL      time.Duration
	QRLongPollTimeout time.Duration
	PollInterval      time.Duration
	MaxQRRefresh      int
}

type Client struct {
	baseURL           string
	botType           string
	routeTag          string
	httpClient        *http.Client
	output            io.Writer
	qrSessionTTL      time.Duration
	qrLongPollTimeout time.Duration
	pollInterval      time.Duration
	maxQRRefresh      int
}

type LoginSession struct {
	SessionKey  string
	AccountHint string
	QRCode      string
	QRContent   string
	StartedAt   time.Time
}

type InteractiveLoginOptions struct {
	AccountHint string
	Timeout     time.Duration
	Output      io.Writer
	SaveDir     string
}

type WaitOptions struct {
	Timeout time.Duration
	Output  io.Writer
	SaveDir string
}

type Account struct {
	AccountID string `json:"account_id"`
	BotToken  string `json:"bot_token"`
	BaseURL   string `json:"base_url,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	SavedAt   string `json:"saved_at,omitempty"`
}

type qrCodeResponse struct {
	QRCode       string `json:"qrcode"`
	QRCodeImgRaw string `json:"qrcode_img_content"`
}

type qrStatusResponse struct {
	Status    string `json:"status"`
	BotToken  string `json:"bot_token"`
	AccountID string `json:"ilink_bot_id"`
	BaseURL   string `json:"baseurl"`
	UserID    string `json:"ilink_user_id"`
}

func NewClient(opts Options) *Client {
	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	botType := strings.TrimSpace(opts.BotType)
	if botType == "" {
		botType = DefaultBotType
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	output := opts.Output
	if output == nil {
		output = os.Stdout
	}

	qrSessionTTL := opts.QRSessionTTL
	if qrSessionTTL <= 0 {
		qrSessionTTL = DefaultQRSessionTTL
	}

	qrLongPollTimeout := opts.QRLongPollTimeout
	if qrLongPollTimeout <= 0 {
		qrLongPollTimeout = DefaultQRLongPollTimeout
	}

	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}

	maxQRRefresh := opts.MaxQRRefresh
	if maxQRRefresh <= 0 {
		maxQRRefresh = DefaultMaxQRRefresh
	}

	return &Client{
		baseURL:           baseURL,
		botType:           botType,
		routeTag:          strings.TrimSpace(opts.RouteTag),
		httpClient:        httpClient,
		output:            output,
		qrSessionTTL:      qrSessionTTL,
		qrLongPollTimeout: qrLongPollTimeout,
		pollInterval:      pollInterval,
		maxQRRefresh:      maxQRRefresh,
	}
}

func (c *Client) StartLogin(ctx context.Context, accountHint string) (*LoginSession, error) {
	resp, err := c.fetchQRCode(ctx)
	if err != nil {
		return nil, err
	}

	return &LoginSession{
		SessionKey:  randomSessionKey(),
		AccountHint: strings.TrimSpace(accountHint),
		QRCode:      resp.QRCode,
		QRContent:   resp.QRCodeImgRaw,
		StartedAt:   time.Now(),
	}, nil
}

func (c *Client) LoginInteractive(ctx context.Context, opts InteractiveLoginOptions) (*Account, error) {
	session, err := c.StartLogin(ctx, opts.AccountHint)
	if err != nil {
		return nil, err
	}

	out := opts.Output
	if out == nil {
		out = c.output
	}

	if out != nil {
		fmt.Fprintln(out, "使用微信扫描以下二维码，以完成连接：")
		if err := PrintQRCode(out, session.QRContent); err != nil {
			fmt.Fprintf(out, "二维码内容: %s\n", session.QRContent)
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "等待连接结果...")
	}

	return c.WaitLogin(ctx, session, WaitOptions{
		Timeout: opts.Timeout,
		Output:  out,
		SaveDir: opts.SaveDir,
	})
}

func (c *Client) WaitLogin(ctx context.Context, session *LoginSession, opts WaitOptions) (*Account, error) {
	if session == nil {
		return nil, fmt.Errorf("login session is nil")
	}
	if time.Since(session.StartedAt) > c.qrSessionTTL {
		return nil, fmt.Errorf("login session expired")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultLoginTimeout
	}
	deadline := time.Now().Add(timeout)
	output := opts.Output
	if output == nil {
		output = c.output
	}

	qrRefreshCount := 1
	scannedPrinted := false

	for time.Now().Before(deadline) {
		status, err := c.pollQRStatus(ctx, session.QRCode)
		if err != nil {
			return nil, err
		}

		switch status.Status {
		case "wait":
		case "scaned":
			if !scannedPrinted && output != nil {
				fmt.Fprintln(output, "已扫码，请在微信里确认登录。")
				scannedPrinted = true
			}
		case "expired":
			qrRefreshCount++
			if qrRefreshCount > c.maxQRRefresh {
				return nil, fmt.Errorf("login timeout: QR code expired too many times")
			}
			refreshed, err := c.fetchQRCode(ctx)
			if err != nil {
				return nil, fmt.Errorf("refresh QR code: %w", err)
			}
			session.QRCode = refreshed.QRCode
			session.QRContent = refreshed.QRCodeImgRaw
			session.StartedAt = time.Now()
			scannedPrinted = false
			if output != nil {
				fmt.Fprintf(output, "二维码已过期，正在刷新... (%d/%d)\n", qrRefreshCount, c.maxQRRefresh)
				if err := PrintQRCode(output, session.QRContent); err != nil {
					fmt.Fprintf(output, "二维码内容: %s\n", session.QRContent)
				}
				fmt.Fprintln(output)
			}
		case "confirmed":
			if status.AccountID == "" {
				return nil, fmt.Errorf("login confirmed but ilink_bot_id is missing")
			}
			account := &Account{
				AccountID: status.AccountID,
				BotToken:  status.BotToken,
				BaseURL:   firstNonEmpty(status.BaseURL, c.baseURL),
				UserID:    status.UserID,
				SavedAt:   time.Now().UTC().Format(time.RFC3339),
			}
			if opts.SaveDir != "" {
				if _, err := SaveAccount(opts.SaveDir, account); err != nil {
					return nil, fmt.Errorf("save account: %w", err)
				}
			}
			if output != nil {
				fmt.Fprintln(output, "与微信连接成功。")
			}
			return account, nil
		default:
			return nil, fmt.Errorf("unexpected QR status %q", status.Status)
		}

		if err := sleepContext(ctx, c.pollInterval); err != nil {
			return nil, err
		}
	}

	return nil, fmt.Errorf("login timeout after %s", timeout)
}

func PrintQRCode(w io.Writer, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("QR content is empty")
	}
	cfg := qrterminal.Config{
		HalfBlocks: true,
		Level:      qrterminal.M,
		Writer:     w,
		QuietZone:  1,
	}
	qrterminal.GenerateWithConfig(content, cfg)
	return nil
}

func (c *Client) fetchQRCode(ctx context.Context) (*qrCodeResponse, error) {
	endpoint, err := joinURL(c.baseURL, "/ilink/bot/get_bot_qrcode")
	if err != nil {
		return nil, err
	}

	query := endpoint.Query()
	query.Set("bot_type", c.botType)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	if c.routeTag != "" {
		req.Header.Set("SKRouteTag", c.routeTag)
	}

	var resp qrCodeResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, fmt.Errorf("fetch QR code: %w", err)
	}
	if resp.QRCode == "" || resp.QRCodeImgRaw == "" {
		return nil, fmt.Errorf("fetch QR code: empty QR payload")
	}
	return &resp, nil
}

func (c *Client) pollQRStatus(ctx context.Context, qrCode string) (*qrStatusResponse, error) {
	endpoint, err := joinURL(c.baseURL, "/ilink/bot/get_qrcode_status")
	if err != nil {
		return nil, err
	}

	query := endpoint.Query()
	query.Set("qrcode", qrCode)
	endpoint.RawQuery = query.Encode()

	pollCtx, cancel := context.WithTimeout(ctx, c.qrLongPollTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pollCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")
	if c.routeTag != "" {
		req.Header.Set("SKRouteTag", c.routeTag)
	}

	var resp qrStatusResponse
	if err := c.doJSON(req, &resp); err != nil {
		if errorsIsTimeout(err) {
			return &qrStatusResponse{Status: "wait"}, nil
		}
		return nil, fmt.Errorf("poll QR status: %w", err)
	}
	return &resp, nil
}

func (c *Client) doJSON(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}

func joinURL(base, pathPart string) (*url.URL, error) {
	baseURL, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	ref, err := url.Parse(strings.TrimLeft(pathPart, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse path: %w", err)
	}
	return baseURL.ResolveReference(ref), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func randomSessionKey() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func errorsIsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "context deadline exceeded") {
		return true
	}
	return false
}
