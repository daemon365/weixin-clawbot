package weixin

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func ResolveStateDir() string {
	if value := stringsTrimSpace(os.Getenv("OPENCLAW_STATE_DIR")); value != "" {
		return value
	}
	if value := stringsTrimSpace(os.Getenv("CLAWDBOT_STATE_DIR")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".openclaw"
	}
	return filepath.Join(home, ".openclaw")
}

func SyncBufFilePath(stateDir, accountID string) string {
	return filepath.Join(stateDir, "openclaw-weixin", "accounts", accountID+".sync.json")
}

func LoadSyncBuffer(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var payload struct {
		GetUpdatesBuf string `json:"get_updates_buf"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	return payload.GetUpdatesBuf, nil
}

func SaveSyncBuffer(filePath, getUpdatesBuf string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		GetUpdatesBuf string `json:"get_updates_buf"`
	}{GetUpdatesBuf: getUpdatesBuf})
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0o600)
}

func stringsTrimSpace(s string) string {
	start := 0
	for start < len(s) {
		switch s[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			goto leftDone
		}
	}
leftDone:
	end := len(s)
	for end > start {
		switch s[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			return s[start:end]
		}
	}
	return s[start:end]
}
