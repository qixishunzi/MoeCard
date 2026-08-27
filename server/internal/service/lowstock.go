package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/utils"
)

// defaultLowStockThreshold 是全局阈值缺省值。
const defaultLowStockThreshold = 5

// ScanLowStock 扫描所有在售商品，对库存跌破阈值的推送告警。
//
// 为什么用扫描而不是在下单时实时判断：
// 实时判断要在下单热路径上多做一次统计查询，而"库存告急"晚知道十几分钟
// 完全不影响处置。扫描还能覆盖"管理员手动删卡密"这类不经过下单流程的场景。
//
// 重复告警抑制：告警后写 low_stock_notified_at；库存回升到阈值以上时清空。
// 没有这一步，每 15 分钟就会重复轰炸一次，最后的结果一定是商家把通知关掉。
func (s *ProductService) ScanLowStock(ctx context.Context) error {
	if s.notifier == nil || !s.notifier.Enabled() {
		return nil
	}

	globalThreshold := defaultLowStockThreshold
	if v, err := strconv.Atoi(strings.TrimSpace(s.settings.Get(model.SetLowStockThreshold))); err == nil && v >= 0 {
		globalThreshold = v
	}

	// 只看在售商品：下架商品缺货不影响生意，没必要打扰商家
	list, _, err := s.repo.List(ctx, nil, repository.ProductQuery{
		Status: model.ProductStatusOn,
		Limit:  1000,
	})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}

	autoIDs := make([]uint64, 0, len(list))
	for _, p := range list {
		if p.IsAuto() {
			autoIDs = append(autoIDs, p.ID)
		}
	}
	counts := map[uint64]int64{}
	if len(autoIDs) > 0 {
		counts, err = s.codes.CountAvailableBatch(ctx, nil, autoIDs)
		if err != nil {
			return err
		}
	}

	for i := range list {
		p := &list[i]

		threshold := p.LowStockThreshold
		if threshold <= 0 {
			threshold = globalThreshold
		}
		if threshold <= 0 {
			continue // 阈值为 0 表示该商品不告警
		}

		var remain int64
		if p.IsAuto() {
			remain = counts[p.ID]
		} else {
			if p.IsUnlimitedStock() {
				continue // 无限库存永远不会告急
			}
			remain = p.Stock
		}

		low := remain <= int64(threshold)
		switch {
		case low && p.LowStockNotifiedAt == nil:
			s.notifier.LowStock(p, remain, threshold)
			now := utils.NowUTC()
			if err := s.repo.UpdateFields(ctx, nil, p.ID,
				map[string]any{"low_stock_notified_at": now}); err != nil {
				logger.L().Warn("记录库存告警时间失败", "product_id", p.ID, "err", err)
			}

		case !low && p.LowStockNotifiedAt != nil:
			// 已补货，清空标记，下次跌破时才会重新提醒
			if err := s.repo.UpdateFields(ctx, nil, p.ID,
				map[string]any{"low_stock_notified_at": nil}); err != nil {
				logger.L().Warn("清空库存告警标记失败", "product_id", p.ID, "err", err)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 买家自定义字段
// ---------------------------------------------------------------------------

// maxCustomFields 限制单个商品的自定义字段数量，避免下单页被塞成问卷。
const maxCustomFields = 5

// defaultCustomFieldMaxLen 是单个字段值的默认长度上限。
const defaultCustomFieldMaxLen = 200

var customFieldKeyRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,31}$`)

// ParseCustomFields 解析商品的自定义字段定义。解析失败时返回空列表而不是报错 ——
// 一条坏数据不应该让整个商品页打不开。
func ParseCustomFields(raw string) []model.CustomField {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []model.CustomField
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		logger.L().Warn("商品自定义字段定义解析失败", "err", err)
		return nil
	}
	return out
}

// ValidateCustomFields 校验并规范化字段定义（后台保存商品时调用）。
func ValidateCustomFields(fields []model.CustomField) ([]model.CustomField, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	if len(fields) > maxCustomFields {
		return nil, api.NewErrorf(api.CodeValidation, "自定义字段最多 %d 个", maxCustomFields)
	}

	seen := map[string]bool{}
	out := make([]model.CustomField, 0, len(fields))
	for _, f := range fields {
		f.Key = strings.TrimSpace(f.Key)
		f.Label = utils.TrimAndLimit(f.Label, 40)
		if f.Label == "" {
			return nil, api.NewErrorf(api.CodeValidation, "自定义字段的名称不能为空")
		}
		if !customFieldKeyRe.MatchString(f.Key) {
			return nil, api.NewErrorf(api.CodeValidation,
				"字段标识 %q 不合法：需以字母开头，只能包含字母、数字与下划线", f.Key)
		}
		if seen[f.Key] {
			return nil, api.NewErrorf(api.CodeValidation, "字段标识 %q 重复", f.Key)
		}
		seen[f.Key] = true

		switch f.Type {
		case "text", "textarea", "select":
		default:
			f.Type = "text"
		}
		if f.Type == "select" {
			cleaned := make([]string, 0, len(f.Options))
			for _, o := range f.Options {
				if o = utils.TrimAndLimit(o, 60); o != "" {
					cleaned = append(cleaned, o)
				}
			}
			if len(cleaned) == 0 {
				return nil, api.NewErrorf(api.CodeValidation, "下拉字段 %q 至少需要一个选项", f.Label)
			}
			f.Options = cleaned
		} else {
			f.Options = nil
		}

		if f.MaxLen <= 0 || f.MaxLen > 2000 {
			f.MaxLen = defaultCustomFieldMaxLen
		}
		f.Placeholder = utils.TrimAndLimit(f.Placeholder, 100)

		if f.Pattern != "" {
			if len(f.Pattern) > 200 {
				return nil, api.NewErrorf(api.CodeValidation, "字段 %q 的校验规则过长", f.Label)
			}
			if _, err := regexp.Compile(f.Pattern); err != nil {
				return nil, api.NewErrorf(api.CodeValidation,
					"字段 %q 的校验规则不是合法的正则表达式", f.Label)
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// EncodeCustomFields 把字段定义序列化成落库字符串。
func EncodeCustomFields(fields []model.CustomField) (string, error) {
	if len(fields) == 0 {
		return "", nil
	}
	b, err := json.Marshal(fields)
	if err != nil {
		return "", api.WrapError(api.CodeInternal, err)
	}
	return string(b), nil
}

// ValidateCustomData 校验买家提交的自定义信息，返回可落库的 JSON。
//
// 这是用户输入，必须逐项按定义校验：类型、必填、长度、可选值、正则。
// 未在定义中出现的键一律丢弃 —— 否则买家可以往订单里塞任意数据。
func ValidateCustomData(fields []model.CustomField, data map[string]string) (string, error) {
	if len(fields) == 0 {
		return "", nil
	}

	out := make(map[string]string, len(fields))
	for _, f := range fields {
		v := strings.TrimSpace(data[f.Key])

		if v == "" {
			if f.Required {
				return "", api.NewErrorf(api.CodeValidation, "请填写「%s」", f.Label)
			}
			continue
		}

		maxLen := f.MaxLen
		if maxLen <= 0 {
			maxLen = defaultCustomFieldMaxLen
		}
		if len([]rune(v)) > maxLen {
			return "", api.NewErrorf(api.CodeValidation, "「%s」长度不能超过 %d 个字符", f.Label, maxLen)
		}

		if f.Type == "select" {
			ok := false
			for _, o := range f.Options {
				if o == v {
					ok = true
					break
				}
			}
			if !ok {
				return "", api.NewErrorf(api.CodeValidation, "「%s」的取值不在可选范围内", f.Label)
			}
		}

		if f.Pattern != "" {
			re, err := regexp.Compile(f.Pattern)
			if err == nil && !re.MatchString(v) {
				return "", api.NewErrorf(api.CodeValidation, "「%s」格式不正确", f.Label)
			}
		}
		out[f.Key] = v
	}

	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", api.WrapError(api.CodeInternal, err)
	}
	return string(b), nil
}

// DecodeCustomData 解析订单里保存的买家自定义信息。
func DecodeCustomData(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
