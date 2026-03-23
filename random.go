package weixin

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func GenerateID(prefix string) string {
	return fmt.Sprintf("%s:%d-%s", prefix, time.Now().UnixMilli(), randomHex(4))
}

func TempFileName(prefix, ext string) string {
	return fmt.Sprintf("%s-%d-%s%s", prefix, time.Now().UnixMilli(), randomHex(4), ext)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
