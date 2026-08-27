package notify

import (
	"context"
	"fmt"
	"html"
	"strings"
)

// 邮件通知渠道。
//
// 复用商城已经配好的 SMTP —— 大多数店主本来就得配它给买家发卡密，
// 因此这是唯一一个"零额外配置成本"的渠道：填个收件邮箱就能用，
// 不用装 App、不用申请机器人。
//
// 代价是时效性不如推送：邮件可能延迟几十秒，也可能被归到垃圾箱。
// 因此它适合做兜底或补充，紧急事件建议同时配一个推送类渠道。

func init() {
	Register(Descriptor{
		Key:  "email",
		Name: "邮件通知",
		Note: "直接复用「邮件配置」里已有的 SMTP，填个收件邮箱就能用。" +
			"注意邮件可能有延迟或被判为垃圾邮件，紧急事件建议再配一个推送类渠道。",
		Fields: []ConfigField{
			{
				Key: "to", Label: "接收邮箱", Type: "text", Required: true,
				Placeholder: "you@example.com",
				Help:        "商家自己的邮箱，不是买家的。多个用逗号分隔",
			},
		},
	}, newEmail)
}

// maxNotifyRecipients 限制收件人数量，避免把通知渠道当群发工具用。
const maxNotifyRecipients = 5

type emailProvider struct {
	to     []string
	sender MailSender
}

func newEmail(cfg map[string]string, deps Deps) (Provider, error) {
	if deps.Mail == nil {
		return nil, fmt.Errorf("邮件渠道不可用：邮件服务未就绪")
	}
	if !deps.Mail.Ready() {
		return nil, fmt.Errorf("请先在「邮件配置」里配好 SMTP 并启用邮件通知")
	}

	var to []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(cfg["to"], ",") {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		// 只做最基本的形状校验：真正的投递失败会由 SMTP 报出来并记进通知日志
		if !strings.Contains(addr, "@") || strings.ContainsAny(addr, " \t\r\n") {
			return nil, fmt.Errorf("收件邮箱格式不正确: %s", addr)
		}
		if seen[strings.ToLower(addr)] {
			continue
		}
		seen[strings.ToLower(addr)] = true
		to = append(to, addr)
	}
	if len(to) == 0 {
		return nil, fmt.Errorf("邮件渠道需要配置接收邮箱")
	}
	if len(to) > maxNotifyRecipients {
		return nil, fmt.Errorf("接收邮箱最多 %d 个", maxNotifyRecipients)
	}
	return &emailProvider{to: to, sender: deps.Mail}, nil
}

func (e *emailProvider) Key() string { return "email" }

func (e *emailProvider) Send(ctx context.Context, msg *Message) error {
	subject := msg.Title
	if msg.Priority == PriorityUrgent {
		// 收件箱里一眼能挑出来。紧急事件往往埋在几十封订单邮件中间。
		subject = "【需处理】" + subject
	}

	body := e.renderHTML(msg)

	// 逐个发送而不是一封多收件人：一个地址写错不该让其他人也收不到，
	// 也避免商家之间互相看到对方邮箱。
	var errs []string
	for _, addr := range e.to {
		if err := e.sender.SendNotify(ctx, addr, subject, body); err != nil {
			errs = append(errs, addr+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// renderHTML 把消息渲染成邮件正文。
//
// 所有动态内容都要转义：商品名、买家填写的信息都可能含 <、& 之类的字符，
// 不转义会把邮件结构撑坏（在部分客户端里还可能被当成标签解析）。
func (e *emailProvider) renderHTML(msg *Message) string {
	var sb strings.Builder

	sb.WriteString(`<p style="margin:0 0 12px;font-size:16px;font-weight:600;">`)
	sb.WriteString(html.EscapeString(msg.Title))
	sb.WriteString(`</p>`)

	if msg.Body != "" {
		sb.WriteString(`<p style="margin:0 0 16px;color:#555;">`)
		sb.WriteString(html.EscapeString(msg.Body))
		sb.WriteString(`</p>`)
	}

	if len(msg.Fields) > 0 {
		sb.WriteString(`<table cellpadding="0" cellspacing="0" style="width:100%;border-collapse:collapse;">`)
		for _, f := range msg.Fields {
			sb.WriteString(`<tr>`)
			sb.WriteString(`<td style="padding:6px 12px 6px 0;color:#888;white-space:nowrap;vertical-align:top;">`)
			sb.WriteString(html.EscapeString(f.Key))
			sb.WriteString(`</td><td style="padding:6px 0;color:#222;word-break:break-all;">`)
			sb.WriteString(html.EscapeString(f.Value))
			sb.WriteString(`</td></tr>`)
		}
		sb.WriteString(`</table>`)
	}

	if msg.URL != "" {
		esc := html.EscapeString(msg.URL)
		sb.WriteString(`<p style="margin:20px 0 0;">`)
		sb.WriteString(`<a href="` + esc + `" style="display:inline-block;padding:10px 20px;`)
		sb.WriteString(`background:#4a9d9a;color:#fff;text-decoration:none;border-radius:6px;">`)
		sb.WriteString(`前往后台处理</a></p>`)
		sb.WriteString(`<p style="margin:10px 0 0;color:#999;font-size:12px;word-break:break-all;">`)
		sb.WriteString(esc)
		sb.WriteString(`</p>`)
	}

	sb.WriteString(`<p style="margin:20px 0 0;color:#999;font-size:12px;">`)
	sb.WriteString(`这是一封商家通知邮件，可在后台「商家通知」中调整推送哪些事件。`)
	sb.WriteString(`</p>`)
	return sb.String()
}
