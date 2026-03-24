<p align="center">
  <a href="https://pkg.go.dev/github.com/daemon365/weixin-clawbot"><img src="https://pkg.go.dev/badge/github.com/daemon365/weixin-clawbot" alt="GoDoc"></a>
  <a href="https://codecov.io/gh/daemon365/weixin-clawbot"><img src="https://codecov.io/gh/daemon365/weixin-clawbot/branch/main/graph/badge.svg" alt="Codecov"></a>
</p>

# weixin-clawbot

[English](./README.md)

用于 OpenClaw 微信接入的 Go 包，提供微信扫码登录、消息收发、长轮询监听以及媒体上传下载能力。

## 项目简介

**weixin-clawbot** 是一个 Go 语言库，专为通过 [iLink](https://ilinkai.weixin.qq.com) Bot 平台接入微信（Weixin/WeChat）而设计，是 [OpenClaw](https://github.com/daemon365) 微信插件的底层客户端。

它解决的核心问题是：如何用 Go 代码以机器人身份登录微信、接收消息并自动回复。整个流程包括：

1. **扫码登录**：在终端展示二维码，用户用手机扫码授权后，库会持久化登录凭证，后续无需重复登录。
2. **消息监听**：通过长轮询（Long Polling）实时接收来自微信好友或群组的消息。
3. **消息发送**：向指定用户或群组发送文本、图片、视频、文件等多种类型的消息。
4. **媒体处理**：支持将媒体文件上传至 CDN（含 AES-ECB 加密），以及下载入站媒体文件到本地。

如果你正在构建一个基于微信的聊天机器人、消息自动化系统或客服机器人，这个库提供了所需的底层能力。

## 安装

```bash
go get github.com/daemon365/weixin-clawbot
```

```go
import weixin "github.com/daemon365/weixin-clawbot"
```

## 功能

- 交互式微信扫码登录，并可将账号信息保存到本地
- 支持长轮询监听与 `get_updates_buf` 持久化
- 提供文本、图片、视频、文件发送辅助函数
- 提供带 AES-ECB 处理的 CDN 上传下载能力
- 提供入站媒体落盘与消息转换工具

## 快速开始

### 1. 登录

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

### 2. 发送文本消息

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

### 3. 监听消息

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

## 主要类型

- `Client`：扫码登录流程
- `APIClient`：ilink bot API 封装
- `Sender`：可复用的消息发送器
- `Conversation`：绑定单个 `ToUserID` + `ContextToken` 的会话发送器
- `Target`：发送目标
- `MonitorOptions`：长轮询监听配置
- `UploadedFileInfo`：CDN 上传结果

## 说明

- 包名仍为 `weixin`，模块导入路径为 `github.com/daemon365/weixin-clawbot`。
- 账号文件名会做 base64url 编码，避免路径字符不安全。
- `Target.ContextToken` 是发送消息时的必填字段。
