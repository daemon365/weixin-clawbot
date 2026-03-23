package weixin

import (
	"context"
	"fmt"
	"time"
)

const (
	defaultMonitorLongPoll = 35 * time.Second
	maxConsecutiveFailures = 3
	backoffDelay           = 30 * time.Second
	retryDelay             = 2 * time.Second
)

type MonitorOptions struct {
	API             *APIClient
	AccountID       string
	SyncBufPath     string
	LongPollTimeout time.Duration
	AllowFrom       []string
	OnMessages      func(context.Context, []WeixinMessage) error
	OnError         func(error)
	OnStatus        func(lastEventAt time.Time)
}

func Monitor(ctx context.Context, opts MonitorOptions) error {
	if opts.API == nil {
		return fmt.Errorf("monitor API client is nil")
	}
	if opts.OnMessages == nil {
		return fmt.Errorf("monitor OnMessages callback is nil")
	}

	getUpdatesBuf, err := LoadSyncBuffer(opts.SyncBufPath)
	if err != nil {
		return err
	}
	nextTimeout := opts.LongPollTimeout
	if nextTimeout <= 0 {
		nextTimeout = defaultMonitorLongPoll
	}

	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := opts.API.GetUpdates(ctx, GetUpdatesRequest{GetUpdatesBuf: getUpdatesBuf}, nextTimeout)
		if err != nil {
			consecutiveFailures++
			if opts.OnError != nil {
				opts.OnError(err)
			}
			if consecutiveFailures >= maxConsecutiveFailures {
				consecutiveFailures = 0
				if err := sleepContext(ctx, backoffDelay); err != nil {
					return err
				}
			} else if err := sleepContext(ctx, retryDelay); err != nil {
				return err
			}
			continue
		}

		if resp.LongPollingTimeoutMS > 0 {
			nextTimeout = time.Duration(resp.LongPollingTimeoutMS) * time.Millisecond
		}
		if resp.Ret != 0 || resp.ErrCode != 0 {
			if resp.ErrCode == SessionExpiredErrCode || resp.Ret == SessionExpiredErrCode {
				PauseSession(opts.AccountID)
				if err := sleepContext(ctx, RemainingPause(opts.AccountID)); err != nil {
					return err
				}
				continue
			}
			consecutiveFailures++
			if opts.OnError != nil {
				opts.OnError(fmt.Errorf("getUpdates failed: ret=%d errcode=%d errmsg=%s", resp.Ret, resp.ErrCode, resp.ErrMsg))
			}
			if consecutiveFailures >= maxConsecutiveFailures {
				consecutiveFailures = 0
				if err := sleepContext(ctx, backoffDelay); err != nil {
					return err
				}
			} else if err := sleepContext(ctx, retryDelay); err != nil {
				return err
			}
			continue
		}

		consecutiveFailures = 0
		if resp.GetUpdatesBuf != "" && resp.GetUpdatesBuf != getUpdatesBuf {
			if err := SaveSyncBuffer(opts.SyncBufPath, resp.GetUpdatesBuf); err != nil && opts.OnError != nil {
				opts.OnError(err)
			}
			getUpdatesBuf = resp.GetUpdatesBuf
		}
		if len(resp.Messages) == 0 {
			if opts.OnStatus != nil {
				opts.OnStatus(time.Now())
			}
			continue
		}

		filtered := filterMessagesBySender(resp.Messages, opts.AllowFrom)
		if len(filtered) == 0 {
			if opts.OnStatus != nil {
				opts.OnStatus(time.Now())
			}
			continue
		}

		if err := opts.OnMessages(ctx, filtered); err != nil {
			return err
		}
		if opts.OnStatus != nil {
			opts.OnStatus(time.Now())
		}
	}
}

func filterMessagesBySender(messages []WeixinMessage, allowFrom []string) []WeixinMessage {
	if len(allowFrom) == 0 {
		return messages
	}
	allow := make(map[string]struct{}, len(allowFrom))
	for _, v := range allowFrom {
		allow[v] = struct{}{}
	}
	filtered := make([]WeixinMessage, 0, len(messages))
	for _, msg := range messages {
		if _, ok := allow[msg.FromUserID]; ok {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}
