package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/mail"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// MailService 负责渲染并发送业务邮件，同时记录发送日志。
//
// **核心约束**：邮件发送与支付事务完全解耦。
// 所有业务邮件都通过 SendAsync 在独立 goroutine 中发送，
// 失败只写 email_logs，绝不会让已经收到的钱回滚。
type MailService struct {
	db       *database.DB
	logs     *repository.LogRepo
	settings *SettingService
	baseURL  string
}

// NewMailService 构造。
func NewMailService(db *database.DB, logs *repository.LogRepo, settings *SettingService, frontendURL string) *MailService {
	return &MailService{db: db, logs: logs, settings: settings, baseURL: strings.TrimRight(frontendURL, "/")}
}

// Enabled 是否已启用邮件通知。
func (s *MailService) Enabled() bool { return s.settings.MailEnabled() }

// SendTest 发送测试邮件（后台"发送测试邮件"按钮）。
//
// cfg 允许传入尚未保存的配置，让管理员先测通再保存。
func (s *MailService) SendTest(ctx context.Context, to string, cfg *mail.Config) error {
	c := s.settings.MailConfig()
	if cfg != nil {
		// 密码为空表示沿用已保存的值
		if strings.TrimSpace(cfg.Password) == "" || utils.IsSecretUnchanged(cfg.Password) {
			cfg.Password = c.Password
		}
		c = *cfg
	}
	if err := c.Validate(); err != nil {
		return err
	}

	site := s.settings.SiteName()
	body := fmt.Sprintf(
		`<p>这是一封来自 <strong>%s</strong> 的测试邮件。</p>
<p>如果你收到了它，说明 SMTP 配置正确，商城可以正常发送订单与发货通知。</p>
<table><tr><td>SMTP 主机</td><td>%s:%d</td></tr>
<tr><td>加密方式</td><td>%s</td></tr>
<tr><td>发送时间</td><td>%s</td></tr></table>`,
		utils.EscapeHTML(site), utils.EscapeHTML(c.Host), c.Port,
		utils.EscapeHTML(c.Encryption),
		utils.FormatInZone(utils.NowUTC(), s.settings.Timezone(), ""))

	msg := mail.Message{
		To:      to,
		Subject: "【" + site + "】SMTP 测试邮件",
		HTML:    mail.WrapHTML(site, "SMTP 测试邮件", body),
	}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err := mail.NewSMTPMailer(c).Send(sendCtx, msg)
	s.writeLog(ctx, 0, "", to, msg.Subject, model.MailTemplateTest, err)
	return err
}

// OrderMailData 是订单邮件的渲染数据。
type OrderMailData struct {
	Order    *model.Order
	Items    []model.OrderItem
	Template string
}

// SendOrderMail 异步发送订单相关邮件。
//
// 这个方法**永远不返回错误、永远不阻塞调用方** ——
// 它是支付事务提交后的副作用，不能反过来影响事务结果。
func (s *MailService) SendOrderMail(order *model.Order, items []model.OrderItem, template string) {
	if order == nil || order.Email == "" {
		return
	}
	if !s.Enabled() {
		logger.Mail().Debug("邮件通知未启用，跳过发送", "order_no", order.OrderNo)
		return
	}
	switch template {
	case model.MailTemplatePaid:
		if !s.settings.GetBool(model.SetMailNotifyOnPaid) {
			return
		}
	case model.MailTemplateDeliver, model.MailTemplateManual:
		if !s.settings.GetBool(model.SetMailNotifyOnDelivery) {
			return
		}
	}

	// 拷贝一份，避免调用方后续修改 order 影响到这里
	o := *order
	its := make([]model.OrderItem, len(items))
	copy(its, items)

	go func() {
		// 用独立 context：调用方的 HTTP 请求可能早已结束，
		// 不能让请求 context 的取消把邮件发送掐掉
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := s.sendOrderMail(ctx, &o, its, template); err != nil {
			logger.Mail().Error("订单邮件发送失败",
				"order_no", o.OrderNo, "template", template, "err", err)
		}
	}()
}

// SendOrderMailSync 同步发送（后台"重新发送邮件"按钮需要拿到结果）。
func (s *MailService) SendOrderMailSync(ctx context.Context, order *model.Order, items []model.OrderItem, template string) error {
	if !s.Enabled() {
		return fmt.Errorf("邮件通知未启用或 SMTP 未配置")
	}
	return s.sendOrderMail(ctx, order, items, template)
}

func (s *MailService) sendOrderMail(ctx context.Context, order *model.Order, items []model.OrderItem, template string) error {
	subject, body := s.render(order, items, template)
	cfg := s.settings.MailConfig()
	if err := cfg.Validate(); err != nil {
		s.writeLog(ctx, order.ID, order.OrderNo, order.Email, subject, template, err)
		return err
	}

	msg := mail.Message{
		To:      order.Email,
		Subject: subject,
		HTML:    mail.WrapHTML(s.settings.SiteName(), subject, body),
	}
	err := mail.NewSMTPMailer(cfg).Send(ctx, msg)
	s.writeLog(ctx, order.ID, order.OrderNo, order.Email, subject, template, err)
	return err
}

// render 渲染邮件主题与正文。
//
// 安全要点：所有变量值在放进模板前都做 HTML 转义。
// 卡密内容尤其重要 —— 它可能包含 <、>、& 等字符，
// 不转义会破坏邮件结构，甚至被用于 HTML 注入。
func (s *MailService) render(order *model.Order, items []model.OrderItem, template string) (string, string) {
	tz := s.settings.Timezone()
	symbol := s.settings.CurrencySymbol()

	var productNames []string
	var quantity int
	var delivery []string
	for _, it := range items {
		productNames = append(productNames, it.ProductName)
		quantity += it.Quantity
		if strings.TrimSpace(it.DeliveryContent) != "" {
			delivery = append(delivery, it.DeliveryContent)
		}
	}
	deliveryContent := strings.Join(delivery, "\n")
	if strings.TrimSpace(deliveryContent) == "" {
		deliveryContent = order.DeliveryContent
	}

	orderURL := s.baseURL + "/order/" + order.OrderNo
	if order.QueryToken != "" {
		orderURL += "?token=" + order.QueryToken
	}

	vars := map[string]string{
		"site_name":        utils.EscapeHTML(s.settings.SiteName()),
		"order_no":         utils.EscapeHTML(order.OrderNo),
		"email":            utils.EscapeHTML(order.Email),
		"product_name":     utils.EscapeHTML(strings.Join(productNames, "、")),
		"quantity":         fmt.Sprint(quantity),
		"original_amount":  utils.EscapeHTML(utils.FormatAmountWithSymbol(order.OriginalAmount, symbol)),
		"discount_amount":  utils.EscapeHTML(utils.FormatAmountWithSymbol(order.DiscountAmount, symbol)),
		"pay_amount":       utils.EscapeHTML(utils.FormatAmountWithSymbol(order.PayAmount, symbol)),
		"payment_method":   utils.EscapeHTML(order.PaymentMethod),
		"status":           utils.EscapeHTML(model.OrderStatusLabel(order.Status)),
		"delivery_content": utils.EscapeHTML(deliveryContent),
		"paid_at":          utils.FormatPtrInZone(order.PaidAt, tz, ""),
		"delivered_at":     utils.FormatPtrInZone(order.DeliveredAt, tz, ""),
		"created_at":       utils.FormatInZone(order.CreatedAt, tz, ""),
		"order_url":        utils.EscapeHTML(orderURL),
	}

	var subjectTpl, bodyTpl string
	switch template {
	case model.MailTemplateDeliver:
		subjectTpl = s.settings.Get(model.SetMailDeliverSubject)
		bodyTpl = s.settings.Get(model.SetMailDeliverBody)
	case model.MailTemplateManual:
		subjectTpl = s.settings.Get(model.SetMailManualSubject)
		bodyTpl = s.settings.Get(model.SetMailManualBody)
	default:
		subjectTpl = s.settings.Get(model.SetMailPaidSubject)
		bodyTpl = s.settings.Get(model.SetMailPaidBody)
	}

	subject := mail.Render(subjectTpl, vars)
	// 主题必须是纯文本；模板里若混入 HTML 标签会在客户端显示成源码
	subject = utils.StripHTML(subject)
	if strings.TrimSpace(subject) == "" {
		subject = fmt.Sprintf("【%s】订单 %s", s.settings.SiteName(), order.OrderNo)
	}
	return subject, mail.Render(bodyTpl, vars)
}

func (s *MailService) writeLog(ctx context.Context, orderID uint64, orderNo, to, subject, template string, sendErr error) {
	l := &model.EmailLog{
		OrderID:  orderID,
		OrderNo:  orderNo,
		ToEmail:  utils.NormalizeEmail(to),
		Subject:  utils.TrimAndLimit(subject, 250),
		Template: template,
		Status:   model.MailStatusSuccess,
	}
	if sendErr != nil {
		l.Status = model.MailStatusFailed
		l.Error = sendErr.Error()
	}
	// 写日志失败不能再抛出去（否则会掩盖真正的发送错误），只记应用日志
	if err := s.logs.CreateEmailLog(ctx, nil, l); err != nil {
		logger.Mail().Error("写入邮件日志失败", "err", err, "order_no", orderNo)
	}
}

// ListLogs 查询邮件日志。
func (s *MailService) ListLogs(ctx context.Context, q repository.EmailLogQuery) ([]model.EmailLog, int64, error) {
	list, total, err := s.logs.ListEmailLogs(ctx, nil, q)
	if err != nil {
		return nil, 0, wrapServiceErr(err)
	}
	return list, total, nil
}

// ---- notify.MailSender 实现 ----
//
// 让「商家通知」能复用商城已配好的 SMTP。
// 定义在 service 层而不是 notify 包里：SMTP 配置怎么读是这一层的事。

// Ready 表示 SMTP 已配置且邮件通知已启用。
func (s *MailService) Ready() bool { return s.Enabled() }

// SiteName 返回商城名称，用于邮件标题与页眉。
func (s *MailService) SiteName() string { return s.settings.SiteName() }

// SendNotify 发送一封商家通知邮件。
//
// 与订单邮件走同一套 SMTP 与 HTML 外框，但**不写 email_logs** ——
// 通知的投递结果记在 notify_logs 里，两边都记会让邮件日志被通知刷屏。
func (s *MailService) SendNotify(ctx context.Context, to, subject, htmlBody string) error {
	cfg := s.settings.MailConfig()
	if err := cfg.Validate(); err != nil {
		return err
	}
	site := s.settings.SiteName()
	return mail.NewSMTPMailer(cfg).Send(ctx, mail.Message{
		To:      to,
		Subject: subject,
		HTML:    mail.WrapHTML(site, subject, htmlBody),
	})
}
