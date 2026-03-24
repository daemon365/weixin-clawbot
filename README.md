<p align="center">
  <a href="https://pkg.go.dev/github.com/daemon365/weixin-clawbot"><img src="https://pkg.go.dev/badge/github.com/daemon365/weixin-clawbot" alt="GoDoc"></a>
  <a href="https://codecov.io/gh/daemon365/weixin-clawbot"><img src="https://codecov.io/gh/daemon365/weixin-clawbot/branch/main/graph/badge.svg" alt="Codecov"></a>
</p>


# weixin-clawbot

[中文文档 / Chinese](./README_CN.md)

Go package for the Weixin QR login flow and bot API used by OpenClaw.

## About

**weixin-clawbot** is a Go library for integrating with WeChat (Weixin) as a bot via the [iLink](https://ilinkai.weixin.qq.com) Bot platform. It serves as the underlying client for the [OpenClaw](https://github.com/daemon365) WeChat plugin.

It addresses the core problem of how to log into WeChat as a bot, receive messages, and reply automatically from Go code. The main capabilities are:

1. **QR-code login**: Renders a QR code in the terminal; after the user scans it with their phone, the library persists the login credentials so subsequent runs don't require re-scanning.
2. **Message monitoring**: Receives messages from WeChat contacts and groups in real time using long polling.
3. **Message sending**: Sends text, image, video, and file messages to individual users or groups.
4. **Media handling**: Uploads media files to the CDN (with AES-ECB encryption) and downloads inbound media to local storage.

If you are building a WeChat chatbot, automated messaging system, or customer-service bot, this library provides the low-level building blocks you need.

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

sender := weixin.NewSender(weixin.SenderOptions{
	BaseURL:      "https://ilinkai.weixin.qq.com",
	Token:        "YOUR_BOT_TOKEN",
	Timeout:      15 * time.Second,
})
conversation := sender.Conversation(weixin.Target{
	ToUserID:     "user@im.wechat",
	ContextToken: "YOUR_CONTEXT_TOKEN",
})

clientID, err := conversation.SendText(ctx, "hello from bot")
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
- `Sender`: reusable outbound sender
- `Conversation`: bound sender for one `ToUserID` + `ContextToken`
- `Target`: outbound conversation target
- `MonitorOptions`: long-poll monitor configuration
- `UploadedFileInfo`: CDN upload result

## Notes

- The package name is `weixin`, while the module import path is `github.com/daemon365/weixin-clawbot`.
- Account files are stored as base64url-encoded filenames to avoid unsafe path characters.
- `Target.ContextToken` is required for outbound messaging.
