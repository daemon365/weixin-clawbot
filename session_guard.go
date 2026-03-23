package weixin

import (
	"fmt"
	"sync"
	"time"
)

const (
	SessionExpiredErrCode = -14
	sessionPauseDuration  = time.Hour
)

var pauseState struct {
	sync.Mutex
	until map[string]time.Time
}

func init() {
	pauseState.until = make(map[string]time.Time)
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
