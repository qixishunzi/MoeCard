package utils

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

var (
	slugPattern  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)
)

// NormalizeEmail 统一邮箱格式（去空格 + 小写）。
// 小写化很重要：否则 A@x.com 与 a@x.com 会被 per_user_limit 当成两个用户。
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ValidateEmail 校验邮箱格式。
func ValidateEmail(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("邮箱不能为空")
	}
	if len(s) > 190 {
		return errors.New("邮箱过长")
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return errors.New("邮箱格式不正确")
	}
	if !strings.Contains(strings.SplitN(s, "@", 2)[1], ".") {
		return errors.New("邮箱格式不正确")
	}
	return nil
}

// ValidateSlug 校验 URL 别名。
func ValidateSlug(s string) error {
	if s == "" {
		return errors.New("别名不能为空")
	}
	if len(s) > 150 {
		return errors.New("别名过长")
	}
	if !slugPattern.MatchString(s) {
		return errors.New("别名只能包含小写字母、数字和连字符，且不能以连字符开头或结尾")
	}
	return nil
}

// pinyinArgs 是转拼音的参数：不要声调、每个字单独一个音节。
var pinyinArgs = func() pinyin.Args {
	a := pinyin.NewArgs()
	a.Style = pinyin.Normal // 不带声调，声调符号不是合法 slug 字符
	a.Heteronym = false     // 多音字只取第一个读音，保证结果稳定可复现
	return a
}()

// Slugify 把任意标题转为合法 slug。
//
// 中文按字转成拼音，每个字之间用 - 分隔："王者荣耀" → "wang-zhe-rong-yao"。
// 之前的做法是直接把非 ASCII 字符剔除，结果全中文的商品名会得到一个随机串，
// 链接既不可读也不利于搜索引擎。
//
// 中英混排按段落切分："Netflix 高级会员" → "netflix-gao-ji-hui-yuan"。
func Slugify(s string) string {
	s = strings.TrimSpace(s)

	var (
		segs []string
		buf  strings.Builder
	)
	flush := func() {
		if buf.Len() > 0 {
			segs = append(segs, buf.String())
			buf.Reset()
		}
	}

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			buf.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			buf.WriteRune(r + 32) // 转小写
		case unicode.Is(unicode.Han, r):
			// 汉字自成一段，与相邻的字母数字分开
			flush()
			if py := pinyin.SinglePinyin(r, pinyinArgs); len(py) > 0 && py[0] != "" {
				segs = append(segs, py[0])
			}
		default:
			// 其余字符（空格、标点、其他语言）一律当作分隔符
			flush()
		}
	}
	flush()

	out := strings.Join(segs, "-")
	out = strings.Trim(out, "-")
	if out == "" {
		return "item-" + RandomHex(4)
	}
	// 超长时按段截断，避免把某个拼音音节切成半个
	if len(out) > 120 {
		out = ""
		for _, seg := range segs {
			if out != "" && len(out)+1+len(seg) > 120 {
				break
			}
			if out != "" {
				out += "-"
			}
			out += seg
		}
		out = strings.Trim(out, "-")
		if out == "" {
			return "item-" + RandomHex(4)
		}
	}
	return out
}

// TrimAndLimit 去空白并限制长度（按 rune 计），防止超长输入撑爆数据库列。
func TrimAndLimit(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// SplitLines 把多行文本切成去空行、去首尾空白的切片。用于卡密批量导入。
func SplitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// DedupeStrings 保序去重。
func DedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// DedupeUint64 保序去重。
func DedupeUint64(in []uint64) []uint64 {
	seen := make(map[uint64]bool, len(in))
	out := make([]uint64, 0, len(in))
	for _, v := range in {
		if v == 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// ContainsControlChars 判断字符串是否含有控制字符（换行/制表符除外）。
// 用于拦截 SMTP 头注入等攻击。
func ContainsControlChars(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' {
			return true
		}
		if unicode.IsControl(r) && r != '\t' {
			return true
		}
	}
	return false
}

// SortWhitelist 是排序字段白名单校验。
//
// 排序字段会被拼进 SQL 的 ORDER BY，绝不能直接使用用户输入。
// 调用方提供 允许值→真实列名 的映射，未命中时返回默认值。
func SortWhitelist(input string, allowed map[string]string, fallback string) string {
	if col, ok := allowed[input]; ok {
		return col
	}
	return fallback
}
