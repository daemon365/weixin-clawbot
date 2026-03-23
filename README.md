<p align="center">
  <a href="https://pkg.go.dev/github.com/daemon365/weixin-clawbot"><img src="https://pkg.go.dev/badge/github.com/daemon365/weixin-clawbot" alt="GoDoc"></a>
  <a href="https://codecov.io/gh/daemon365/weixin-clawbot"><img src="https://codecov.io/gh/daemon365/weixin-clawbot/branch/main/graph/badge.svg" alt="Codecov"></a>
</p>


# weixin-clawbot

[中文文档 / Chinese](./README_CN.md)

Go package for the Weixin QR login flow and bot API used by OpenClaw.

## Install

```bash
go get github.com/daemon365/weixin-clawbot
```

```go
import weixin "github.com/daemon365/weixin-clawbot"
```

## Features

- Interactive Weixin QR login with local account persistence
- Long-poll message monitoring with sync buffer persistence
- Text, image, video, and file sending helpers
- CDN upload/download helpers with AES-ECB handling
- Utilities for inbound media download and message conversion

## Quick Start

### 1. Login

```go
package main

import (
	"context"
	"log"
	"os"

	weixin "github.com/daemon365/weixin-clawbot"
)

func main() {
	client := weixin.NewClient(weixin.Options{})

	account, err := client.LoginInteractive(context.Background(), weixin.InteractiveLoginOptions{
		Output:  os.Stdout,
		SaveDir: ".weixin-accounts",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("account=%s token=%s", account.AccountID, account.BotToken)
}
```

### 2. Send a text message

```go
ctx := context.Background()

clientID, err := weixin.SendMessageWeixin(ctx, "user@im.wechat", "hello from bot", weixin.SendOptions{
	BaseURL:      "https://ilinkai.weixin.qq.com",
	Token:        "YOUR_BOT_TOKEN",
	ContextToken: "YOUR_CONTEXT_TOKEN",
	Timeout:      15 * time.Second,
})
if err != nil {
	log.Fatal(err)
}

log.Println("sent:", clientID)
```

### 3. Monitor updates

```go
api := weixin.NewAPIClient(weixin.APIOptions{
	BaseURL: "https://ilinkai.weixin.qq.com",
	Token:   "YOUR_BOT_TOKEN",
})

err := weixin.Monitor(context.Background(), weixin.MonitorOptions{
	API:         api,
	AccountID:   "bot@im.bot",
	SyncBufPath: weixin.SyncBufFilePath(weixin.ResolveStateDir(), "bot@im.bot"),
	OnMessages: func(ctx context.Context, messages []weixin.WeixinMessage) error {
		for _, msg := range messages {
			log.Printf("from=%s body=%s", msg.FromUserID, weixin.BodyFromItemList(msg.ItemList))
		}
		return nil
	},
})
if err != nil {
	log.Fatal(err)
}
```

## Main Types

- `Client`: QR login flow
- `APIClient`: ilink bot API wrapper
- `MonitorOptions`: long-poll monitor configuration
- `SendOptions`: outbound message configuration
- `UploadedFileInfo`: CDN upload result

## Notes

- The package name is `weixin`, while the module import path is `github.com/daemon365/weixin-clawbot`.
- Account files are stored as base64url-encoded filenames to avoid unsafe path characters.
- `ContextToken` is required for outbound message helpers such as `SendMessageWeixin`.
