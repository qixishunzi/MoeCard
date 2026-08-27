// Package mail 提供 SMTP 邮件发送能力。
//
// 只支持 SMTP（按需求约定），但通过 Mailer 接口抽象，
// 未来接入 API 型服务（SendGrid / Resend / SES）只需实现同一接口。
//
// 关键约束：**邮件发送失败绝不能影响支付事务**。
// 因此发送始终在支付事务提交之后、以异步方式进行，失败只写 email_logs。
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/moecard/server/internal/utils"
)

// 加密方式。
const (
	EncryptionNone     = "none"
	EncryptionSSL      = "ssl"      // 隐式 TLS，通常是 465 端口
	EncryptionSTARTTLS = "starttls" // 显式 TLS，通常是 587 端口
)

// Config 是 SMTP 配置。
type Config struct {
	Host       string
	Port       int
	Username   string
	Password   string
	FromEmail  string
	FromName   string
	Encryption string
	Timeout    time.Duration
	SkipVerify bool // 仅用于自建证书的内网 SMTP，生产环境不建议开启
}

// Validate 校验配置完整性。
func (c Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.Host) == "" {
		missing = append(missing, "SMTP 主机")
	}
	if c.Port <= 0 || c.Port > 65535 {
		missing = append(missing, "SMTP 端口")
	}
	if strings.TrimSpace(c.FromEmail) == "" {
		missing = append(missing, "发件人邮箱")
	}
	if len(missing) > 0 {
		return fmt.Errorf("SMTP 配置不完整: %s", strings.Join(missing, "、"))
	}
	if err := utils.ValidateEmail(strings.TrimSpace(c.FromEmail)); err != nil {
		return fmt.Errorf("发件人邮箱不合法: %w", err)
	}
	return nil
}

// Message 是一封待发送的邮件。
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string // 纯文本副本；为空时自动从 HTML 生成
}

// Mailer 是邮件发送接口。
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPMailer 是基于标准库 net/smtp 的实现。
type SMTPMailer struct {
	cfg Config
}

// NewSMTPMailer 构造。
func NewSMTPMailer(cfg Config) *SMTPMailer {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.Encryption == "" {
		cfg.Encryption = EncryptionSSL
	}
	return &SMTPMailer{cfg: cfg}
}

// Send 发送邮件。
func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if err := m.cfg.Validate(); err != nil {
		return err
	}
	to := strings.TrimSpace(msg.To)
	if err := utils.ValidateEmail(to); err != nil {
		return fmt.Errorf("收件人邮箱不合法: %w", err)
	}
	// 防 SMTP 头注入：主题里的换行会让攻击者插入额外的邮件头（如 Bcc）
	if utils.ContainsControlChars(msg.Subject) || utils.ContainsControlChars(to) {
		return errors.New("邮件主题或收件人包含非法字符")
	}

	body := m.buildMIME(to, msg)

	// 用 context 控制整体超时：SMTP 服务器无响应时不能一直挂住 goroutine
	done := make(chan error, 1)
	go func() { done <- m.send(to, body) }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (m *SMTPMailer) send(to string, body []byte) error {
	addr := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))
	tlsCfg := &tls.Config{
		ServerName:         m.cfg.Host,
		InsecureSkipVerify: m.cfg.SkipVerify, //nolint:gosec // 仅在用户显式开启时生效
		MinVersion:         tls.VersionTLS12,
	}

	var (
		client *smtp.Client
		err    error
	)

	switch strings.ToLower(m.cfg.Encryption) {
	case EncryptionSSL:
		// 隐式 TLS：连接建立时就是加密的（465 端口）
		dialer := &net.Dialer{Timeout: m.cfg.Timeout}
		conn, e := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if e != nil {
			return fmt.Errorf("连接 SMTP 服务器失败(SSL): %w", e)
		}
		client, err = smtp.NewClient(conn, m.cfg.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("建立 SMTP 会话失败: %w", err)
		}
	default:
		conn, e := net.DialTimeout("tcp", addr, m.cfg.Timeout)
		if e != nil {
			return fmt.Errorf("连接 SMTP 服务器失败: %w", e)
		}
		_ = conn.SetDeadline(time.Now().Add(m.cfg.Timeout))
		client, err = smtp.NewClient(conn, m.cfg.Host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("建立 SMTP 会话失败: %w", err)
		}
		if strings.EqualFold(m.cfg.Encryption, EncryptionSTARTTLS) {
			if ok, _ := client.Extension("STARTTLS"); !ok {
				client.Close()
				return errors.New("SMTP 服务器不支持 STARTTLS，请改用 SSL 或关闭加密")
			}
			if err := client.StartTLS(tlsCfg); err != nil {
				client.Close()
				return fmt.Errorf("STARTTLS 握手失败: %w", err)
			}
		}
	}
	defer client.Close()

	if m.cfg.Username != "" {
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			// 部分服务器只支持 LOGIN 认证
			if err2 := client.Auth(&loginAuth{m.cfg.Username, m.cfg.Password}); err2 != nil {
				return fmt.Errorf("SMTP 认证失败: %w", err)
			}
		}
	}

	from := strings.TrimSpace(m.cfg.FromEmail)
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("设置收件人失败: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("开始写入邮件正文失败: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		_ = w.Close()
		return fmt.Errorf("写入邮件正文失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("提交邮件失败: %w", err)
	}
	return client.Quit()
}

// buildMIME 构造 multipart/alternative 邮件（同时提供纯文本与 HTML）。
func (m *SMTPMailer) buildMIME(to string, msg Message) []byte {
	text := msg.Text
	if strings.TrimSpace(text) == "" {
		text = utils.StripHTML(msg.HTML)
	}
	boundary := "moecard-" + utils.RandomHex(12)

	fromName := m.cfg.FromName
	if fromName == "" {
		fromName = "MoeCard"
	}
	// 中文发件人名与主题必须做 RFC 2047 编码，否则会显示成乱码
	encodedFrom := mime.QEncoding.Encode("utf-8", fromName)
	encodedSubject := mime.QEncoding.Encode("utf-8", msg.Subject)

	var sb strings.Builder
	sb.WriteString("From: " + encodedFrom + " <" + m.cfg.FromEmail + ">\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + encodedSubject + "\r\n")
	sb.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	sb.WriteString("Message-ID: <" + utils.RandomHex(16) + "@moecard>\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	sb.WriteString("\r\n")

	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	sb.WriteString(base64Wrap(text))
	sb.WriteString("\r\n")

	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	sb.WriteString(base64Wrap(msg.HTML))
	sb.WriteString("\r\n")

	sb.WriteString("--" + boundary + "--\r\n")
	return []byte(sb.String())
}

// loginAuth 实现 AUTH LOGIN（部分国内 SMTP 服务器不支持 PLAIN）。
type loginAuth struct{ username, password string }

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("unexpected server challenge: %s", fromServer)
	}
}
