package weixin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func SaveAccount(dir string, account *Account) (string, error) {
	if account == nil {
		return "", fmt.Errorf("account is nil")
	}
	if account.AccountID == "" {
		return "", fmt.Errorf("account_id is empty")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	filePath := accountFilePath(dir, account.AccountID)
	data, err := json.MarshalIndent(account, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return "", err
	}
	return filePath, nil
}

func LoadAccount(dir, accountID string) (*Account, error) {
	data, err := os.ReadFile(accountFilePath(dir, accountID))
	if err != nil {
		return nil, err
	}

	var account Account
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

func ListAccounts(dir string) ([]Account, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	accounts := make([]Account, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}

		var account Account
		if err := json.Unmarshal(data, &account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].AccountID < accounts[j].AccountID
	})
	return accounts, nil
}

func accountFilePath(dir, accountID string) string {
	safeName := base64.RawURLEncoding.EncodeToString([]byte(accountID))
	return filepath.Join(dir, safeName+".json")
}
