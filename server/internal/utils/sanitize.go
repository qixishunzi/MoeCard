package utils

import (
	"strings"

	"golang.org/x/net/html"
)

// 富文本白名单。商品描述允许基础排版标签，但绝不允许任何可执行内容。
var (
	allowedTags = map[string]bool{
		"p": true, "br": true, "hr": true, "div": true, "span": true,
		"strong": true, "b": true, "em": true, "i": true, "u": true, "s": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
		"ul": true, "ol": true, "li": true, "blockquote": true,
		"code": true, "pre": true,
		"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
		"a": true, "img": true,
	}

	// 允许的属性（按标签）。style 一律禁止 —— CSS 表达式与 url() 都可能被滥用。
	allowedAttrs = map[string]map[string]bool{
		"a":   {"href": true, "title": true, "target": true, "rel": true},
		"img": {"src": true, "alt": true, "title": true, "width": true, "height": true},
		"td":  {"colspan": true, "rowspan": true},
		"th":  {"colspan": true, "rowspan": true},
	}

	// 整块丢弃（含内部文本）的危险标签。
	dropWithContent = map[string]bool{
		"script": true, "style": true, "iframe": true, "object": true,
		"embed": true, "applet": true, "form": true, "noscript": true,
		"template": true, "svg": true, "math": true,
	}

	// URL 协议白名单。
	safeURLSchemes = map[string]bool{"http": true, "https": true, "mailto": true}
)

// SanitizeHTML 用真正的 HTML 分词器做白名单过滤，返回可安全渲染的 HTML。
//
// 为什么不用正则：正则无法正确处理嵌套、畸形标签、编码绕过
// （如 <img src=x onerror=alert(1)> 的各种变体），历史上被绕过无数次。
// 这里基于 x/net/html 的分词器逐 token 处理，未在白名单内的标签一律丢弃，
// 文本内容全部重新转义输出。
func SanitizeHTML(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}

	var (
		sb        strings.Builder
		skipDepth int
		skipTag   string
		openStack []string
	)
	z := html.NewTokenizer(strings.NewReader(input))

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		tok := z.Token()
		name := strings.ToLower(tok.Data)

		switch tt {
		case html.StartTagToken:
			if skipDepth > 0 {
				if name == skipTag {
					skipDepth++
				}
				continue
			}
			if dropWithContent[name] {
				skipDepth, skipTag = 1, name
				continue
			}
			if !allowedTags[name] {
				continue // 丢弃标签但保留其内部文本
			}
			sb.WriteString(renderStartTag(name, tok.Attr, false))
			if !voidElements[name] {
				openStack = append(openStack, name)
			}

		case html.SelfClosingTagToken:
			if skipDepth > 0 || dropWithContent[name] || !allowedTags[name] {
				continue
			}
			sb.WriteString(renderStartTag(name, tok.Attr, true))

		case html.EndTagToken:
			if skipDepth > 0 {
				if name == skipTag {
					skipDepth--
					if skipDepth == 0 {
						skipTag = ""
					}
				}
				continue
			}
			if !allowedTags[name] || voidElements[name] {
				continue
			}
			// 只闭合确实被我们打开过的标签，避免产生游离的 </div>
			for i := len(openStack) - 1; i >= 0; i-- {
				if openStack[i] == name {
					sb.WriteString("</" + name + ">")
					openStack = openStack[:i]
					break
				}
			}

		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			sb.WriteString(html.EscapeString(tok.Data))

		case html.CommentToken, html.DoctypeToken:
			// 注释可能藏条件注释脚本，直接丢弃
			continue
		}
	}

	// 补齐未闭合标签，保证输出是结构完整的 HTML
	for i := len(openStack) - 1; i >= 0; i-- {
		sb.WriteString("</" + openStack[i] + ">")
	}
	return sb.String()
}

var voidElements = map[string]bool{
	"br": true, "hr": true, "img": true,
}

func renderStartTag(name string, attrs []html.Attribute, selfClose bool) string {
	var sb strings.Builder
	sb.WriteString("<")
	sb.WriteString(name)

	allowed := allowedAttrs[name]
	isLink := name == "a"
	hasTargetBlank := false

	for _, a := range attrs {
		key := strings.ToLower(a.Key)
		if allowed == nil || !allowed[key] {
			continue
		}
		val := a.Val
		if key == "href" || key == "src" {
			if !isSafeURL(val) {
				continue
			}
		}
		if key == "target" {
			if val != "_blank" {
				continue
			}
			hasTargetBlank = true
		}
		sb.WriteString(` `)
		sb.WriteString(key)
		sb.WriteString(`="`)
		sb.WriteString(html.EscapeString(val))
		sb.WriteString(`"`)
	}

	// target=_blank 必须配 rel=noopener noreferrer，否则新页面可通过
	// window.opener 篡改原页面（反向 tabnabbing）。
	if isLink && hasTargetBlank {
		sb.WriteString(` rel="noopener noreferrer"`)
	}
	if selfClose || voidElements[name] {
		sb.WriteString(" />")
	} else {
		sb.WriteString(">")
	}
	return sb.String()
}

// isSafeURL 只放行 http/https/mailto 与站内相对路径。
func isSafeURL(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	// 去掉可能用于绕过的不可见字符
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)

	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "//") {
		return true // 协议相对 URL
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "./") {
		return true // 站内相对路径
	}
	idx := strings.Index(lower, ":")
	if idx < 0 {
		return true // 无协议，视为相对路径
	}
	// 协议部分若含 / ? #，说明冒号出现在路径里而非协议里
	if strings.ContainsAny(lower[:idx], "/?#") {
		return true
	}
	return safeURLSchemes[lower[:idx]]
}

// StripHTML 移除全部标签，只保留纯文本。用于生成摘要与邮件纯文本副本。
func StripHTML(input string) string {
	var sb strings.Builder
	z := html.NewTokenizer(strings.NewReader(input))
	skip := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		tok := z.Token()
		name := strings.ToLower(tok.Data)
		switch tt {
		case html.StartTagToken:
			if dropWithContent[name] {
				skip++
			} else if name == "br" || name == "p" || name == "div" || name == "li" {
				sb.WriteString("\n")
			}
		case html.EndTagToken:
			if dropWithContent[name] && skip > 0 {
				skip--
			}
		case html.TextToken:
			if skip == 0 {
				sb.WriteString(tok.Data)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// EscapeHTML 是 html.EscapeString 的转发，用于把纯文本安全嵌入 HTML 邮件。
func EscapeHTML(s string) string { return html.EscapeString(s) }
