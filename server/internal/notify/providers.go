package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func init() {
	Register(Descriptor{
		Key:  "telegram",
		Name: "Telegram 机器人",
		Note: "向 @BotFather 申请机器人拿到 Token；把机器人拉进群或私聊后，" +
			"访问 https://api.telegram.org/bot<Token>/getUpdates 可以看到 chat_id。",
		Fields: []ConfigField{
			{Key: "token", Label: "Bot Token", Type: "password", Required: true, Secret: true,
				Placeholder: "123456789:AAxxxxxxxxxxxxxxxxxxxxxxxxxxx"},
			{Key: "chat_id", Label: "Chat ID", Type: "text", Required: true,
				Placeholder: "私聊为正数，群组为负数", Help: "群组 ID 通常以 - 开头"},
		},
	}, newTelegram)

	Register(Descriptor{
		Key:  "bark",
		Name: "Bark（iOS 推送）",
		Note: "在 iPhone 上安装 Bark App，复制 App 里显示的推送地址填到下面。",
		Fields: []ConfigField{
			{Key: "url", Label: "推送地址", Type: "password", Required: true, Secret: true,
				Placeholder: "https://api.day.app/你的Key",
				Help:        "Bark App 首页展示的那串地址，末尾不要带消息内容"},
		},
	}, newBark)

	Register(Descriptor{
		Key:  "wecom",
		Name: "企业微信群机器人",
		Note: "在企业微信群里「添加群机器人」，复制 Webhook 地址填到下面。",
		Fields: []ConfigField{
			{Key: "webhook", Label: "Webhook 地址", Type: "password", Required: true, Secret: true,
				Placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."},
		},
	}, newWecom)

	Register(Descriptor{
		Key:  "webhook",
		Name: "自定义 Webhook",
		Note: "以 POST + JSON 推送到你自己的地址。配置密钥后会带 X-MoeCard-Signature 头" +
			"（HMAC-SHA256，签名对象为原始请求体），便于接收方校验来源。",
		Fields: []ConfigField{
			{Key: "url", Label: "接收地址", Type: "text", Required: true,
				Placeholder: "https://example.com/moecard-hook"},
			{Key: "secret", Label: "签名密钥", Type: "password", Secret: true,
				Help: "可选。填写后请求会带 HMAC-SHA256 签名头"},
		},
	}, newWebhook)
}

// postJSON 是各渠道共用的 POST JSON helper。
func postJSON(ctx context.Context, endpoint string, payload any, headers map[string]string) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化通知内容失败: %w", err)
	}
	return postRaw(ctx, endpoint, buf, "application/json", headers)
}

func postRaw(ctx context.Context, endpoint string, body []byte, contentType string, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := Client().Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 只读前 2KB：通知接口的响应没必要全读，出错时够定位就行
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// ---------------------------------------------------------------- Telegram

type telegram struct{ token, chatID string }

func newTelegram(cfg map[string]string, _ Deps) (Provider, error) {
	t := strings.TrimSpace(cfg["token"])
	c := strings.TrimSpace(cfg["chat_id"])
	if t == "" || c == "" {
		return nil, fmt.Errorf("Telegram 需要配置 Bot Token 与 Chat ID")
	}
	return &telegram{token: t, chatID: c}, nil
}

func (t *telegram) Key() string { return "telegram" }

func (t *telegram) Send(ctx context.Context, msg *Message) error {
	var sb strings.Builder
	sb.WriteString("*")
	sb.WriteString(escapeMarkdownV2(msg.Title))
	sb.WriteString("*")
	if msg.Body != "" {
		sb.WriteString("\n")
		sb.WriteString(escapeMarkdownV2(msg.Body))
	}
	for _, f := range msg.Fields {
		sb.WriteString("\n")
		sb.WriteString(escapeMarkdownV2(f.Key))
		sb.WriteString("：`")
		sb.WriteString(escapeMarkdownV2(f.Value))
		sb.WriteString("`")
	}
	if msg.URL != "" {
		sb.WriteString("\n")
		sb.WriteString(escapeMarkdownV2(msg.URL))
	}

	return postJSON(ctx, "https://api.telegram.org/bot"+t.token+"/sendMessage", map[string]any{
		"chat_id":                  t.chatID,
		"text":                     sb.String(),
		"parse_mode":               "MarkdownV2",
		"disable_web_page_preview": true,
	}, nil)
}

// escapeMarkdownV2 转义 Telegram MarkdownV2 的全部保留字符。
// 漏转义会让整条消息被 API 拒绝（400），而不是排版错乱。
func escapeMarkdownV2(s string) string {
	const special = `_*[]()~` + "`" + `>#+-=|{}.!\`
	var sb strings.Builder
	sb.Grow(len(s) * 2)
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// ---------------------------------------------------------------- Bark

type bark struct{ base string }

func newBark(cfg map[string]string, _ Deps) (Provider, error) {
	u := strings.TrimRight(strings.TrimSpace(cfg["url"]), "/")
	if u == "" {
		return nil, fmt.Errorf("Bark 需要配置推送地址")
	}
	return &bark{base: u}, nil
}

func (b *bark) Key() string { return "bark" }

func (b *bark) Send(ctx context.Context, msg *Message) error {
	payload := map[string]any{
		"title": msg.Title,
		"body":  strings.TrimPrefix(msg.Text(), msg.Title+"\n"),
		"group": "MoeCard",
	}
	if msg.URL != "" {
		payload["url"] = msg.URL
	}
	if msg.Priority == PriorityUrgent {
		// 时效性通知：绕过 iOS 专注模式，并让提示音重复响
		payload["level"] = "timeSensitive"
		payload["sound"] = "alarm"
	}
	return postJSON(ctx, b.base+"/push", payload, nil)
}

// ---------------------------------------------------------------- 企业微信

type wecom struct{ webhook string }

func newWecom(cfg map[string]string, _ Deps) (Provider, error) {
	u := strings.TrimSpace(cfg["webhook"])
	if u == "" {
		return nil, fmt.Errorf("企业微信需要配置 Webhook 地址")
	}
	if _, err := url.Parse(u); err != nil {
		return nil, fmt.Errorf("企业微信 Webhook 地址不合法: %w", err)
	}
	return &wecom{webhook: u}, nil
}

func (w *wecom) Key() string { return "wecom" }

func (w *wecom) Send(ctx context.Context, msg *Message) error {
	var sb strings.Builder
	sb.WriteString("**")
	sb.WriteString(msg.Title)
	sb.WriteString("**")
	if msg.Body != "" {
		sb.WriteString("\n")
		sb.WriteString(msg.Body)
	}
	for _, f := range msg.Fields {
		sb.WriteString("\n> ")
		sb.WriteString(f.Key)
		sb.WriteString("：")
		sb.WriteString(f.Value)
	}
	if msg.URL != "" {
		sb.WriteString("\n[查看详情](")
		sb.WriteString(msg.URL)
		sb.WriteString(")")
	}

	// 企业微信即使业务失败也返回 HTTP 200，必须解析 errcode
	buf, _ := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": sb.String()},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.webhook, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := Client().Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if json.Unmarshal(raw, &r) == nil && r.ErrCode != 0 {
		return fmt.Errorf("企业微信返回错误 %d: %s", r.ErrCode, r.ErrMsg)
	}
	return nil
}

// ---------------------------------------------------------------- 自定义 Webhook

type webhook struct{ url, secret string }

func newWebhook(cfg map[string]string, _ Deps) (Provider, error) {
	u := strings.TrimSpace(cfg["url"])
	if u == "" {
		return nil, fmt.Errorf("自定义 Webhook 需要配置接收地址")
	}
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("接收地址必须是 http/https 开头的完整 URL")
	}
	return &webhook{url: u, secret: strings.TrimSpace(cfg["secret"])}, nil
}

func (w *webhook) Key() string { return "webhook" }

func (w *webhook) Send(ctx context.Context, msg *Message) error {
	fields := make(map[string]string, len(msg.Fields))
	for _, f := range msg.Fields {
		fields[f.Key] = f.Value
	}
	body, err := json.Marshal(map[string]any{
		"event":     msg.Event,
		"title":     msg.Title,
		"body":      msg.Body,
		"priority":  string(msg.Priority),
		"url":       msg.URL,
		"fields":    fields,
		"timestamp": time.Now().Unix(),
	})
	if err != nil {
		return err
	}

	headers := map[string]string{"User-Agent": "MoeCard-Notify/1.0"}
	if w.secret != "" {
		mac := hmac.New(sha256.New, []byte(w.secret))
		mac.Write(body)
		headers["X-MoeCard-Signature"] = hex.EncodeToString(mac.Sum(nil))
		headers["X-MoeCard-Timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	}
	return postRaw(ctx, w.url, body, "application/json", headers)
}
