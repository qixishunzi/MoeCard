package mail

import (
	"encoding/base64"
	"strings"
)

// Render 用 {{key}} 占位符渲染模板。
//
// 不使用 html/template：邮件模板由管理员在后台编辑，
// 我们需要允许他们写 HTML，但**所有变量值必须转义**（防止卡密内容里的
// < > 破坏邮件结构）。这里的实现正好满足：模板 HTML 原样保留，
// 变量值由调用方在放入 vars 前完成转义。
func Render(tpl string, vars map[string]string) string {
	if tpl == "" {
		return ""
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(tpl)
}

// WrapHTML 给邮件正文套上一层基础样式，让各邮件客户端显示一致。
func WrapHTML(siteName, title, body string) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	sb.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	sb.WriteString(`<title>` + title + `</title></head>`)
	sb.WriteString(`<body style="margin:0;padding:0;background:#f5f6f8;">`)
	sb.WriteString(`<table width="100%" cellpadding="0" cellspacing="0" style="background:#f5f6f8;padding:24px 12px;">`)
	sb.WriteString(`<tr><td align="center">`)
	sb.WriteString(`<table width="100%" cellpadding="0" cellspacing="0" style="max-width:600px;background:#ffffff;border-radius:10px;overflow:hidden;box-shadow:0 1px 3px rgba(0,0,0,.08);">`)

	sb.WriteString(`<tr><td style="background:#5b6ef5;padding:18px 24px;color:#fff;font:600 17px/1.4 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;">`)
	sb.WriteString(siteName)
	sb.WriteString(`</td></tr>`)

	sb.WriteString(`<tr><td style="padding:24px;color:#333;font:400 14px/1.75 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;">`)
	sb.WriteString(bodyStyleFix(body))
	sb.WriteString(`</td></tr>`)

	sb.WriteString(`<tr><td style="padding:16px 24px;border-top:1px solid #eee;color:#999;font:400 12px/1.6 -apple-system,'Segoe UI',Arial,sans-serif;">`)
	sb.WriteString(`本邮件由系统自动发送，请勿直接回复。`)
	sb.WriteString(`</td></tr>`)

	sb.WriteString(`</table></td></tr></table></body></html>`)
	return sb.String()
}

// bodyStyleFix 给模板里的 table / pre 补上内联样式。
// 邮件客户端普遍不支持 <style> 标签，只能内联。
func bodyStyleFix(body string) string {
	r := strings.NewReplacer(
		"<table>", `<table cellpadding="6" cellspacing="0" style="width:100%;border-collapse:collapse;margin:12px 0;font-size:14px;">`,
		"<td>", `<td style="padding:6px 8px;border-bottom:1px solid #f0f0f0;">`,
		"<pre>", `<pre style="background:#f6f7f9;border:1px solid #e6e8eb;border-radius:6px;padding:12px;white-space:pre-wrap;word-break:break-all;font:13px/1.7 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:#222;">`,
		"<a ", `<a style="color:#5b6ef5;" `,
	)
	return r.Replace(body)
}

// base64Wrap 按 RFC 2045 每 76 字符折行。
func base64Wrap(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	const width = 76
	var sb strings.Builder
	for i := 0; i < len(enc); i += width {
		end := min(i+width, len(enc))
		sb.WriteString(enc[i:end])
		sb.WriteString("\r\n")
	}
	return sb.String()
}
