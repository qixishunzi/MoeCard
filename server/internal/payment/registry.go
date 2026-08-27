package payment

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry 是 provider 注册表。
//
// 新增支付平台只需在 init() 里调用 Register，
// 后台的"支付管理"表单、可选 provider 列表都会自动出现新项。
type Registry struct {
	mu    sync.RWMutex
	items map[string]Descriptor
}

var defaultRegistry = &Registry{items: map[string]Descriptor{}}

// Register 注册一个 provider。重复注册会 panic —— 这是编码错误，应在启动时立刻暴露。
func Register(d Descriptor, f Factory) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	if _, exists := defaultRegistry.items[d.Key]; exists {
		panic("payment: duplicate provider registration: " + d.Key)
	}
	d.factory = f
	defaultRegistry.items[d.Key] = d
}

// Build 按 provider key 与渠道配置构造实例。
func Build(providerKey string, cfg map[string]string) (Provider, error) {
	defaultRegistry.mu.RLock()
	d, ok := defaultRegistry.items[providerKey]
	defaultRegistry.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("未知的支付渠道类型: %s", providerKey)
	}
	if cfg == nil {
		cfg = map[string]string{}
	}
	// 构造前先做必填校验，避免把"配置漏填"变成运行时的诡异错误
	if err := ValidateConfig(providerKey, cfg); err != nil {
		return nil, err
	}
	return d.factory(cfg)
}

// Descriptors 返回全部已注册 provider（按 key 排序，保证前端顺序稳定）。
func Descriptors() []Descriptor {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	out := make([]Descriptor, 0, len(defaultRegistry.items))
	for _, d := range defaultRegistry.items {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Lookup 返回指定 provider 的描述。
func Lookup(key string) (Descriptor, bool) {
	defaultRegistry.mu.RLock()
	defer defaultRegistry.mu.RUnlock()
	d, ok := defaultRegistry.items[key]
	return d, ok
}

// IsRegistered 判断 provider 是否存在。
func IsRegistered(key string) bool {
	_, ok := Lookup(key)
	return ok
}

// SecretFields 返回该 provider 中需要脱敏的配置项 key 集合。
func SecretFields(providerKey string) map[string]bool {
	out := map[string]bool{}
	d, ok := Lookup(providerKey)
	if !ok {
		return out
	}
	for _, f := range d.Fields {
		if f.Secret {
			out[f.Key] = true
		}
	}
	return out
}

// ValidateConfig 校验必填项是否齐全。
func ValidateConfig(providerKey string, cfg map[string]string) error {
	d, ok := Lookup(providerKey)
	if !ok {
		return fmt.Errorf("未知的支付渠道类型: %s", providerKey)
	}
	var missing []string
	for _, f := range d.Fields {
		if f.Required && strings.TrimSpace(cfg[f.Key]) == "" {
			missing = append(missing, f.Label)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: 缺少必填项 %s", ErrInvalidConfig, strings.Join(missing, "、"))
	}
	return nil
}

// ApplyDefaults 用 descriptor 中的默认值补齐缺失的配置项。
func ApplyDefaults(providerKey string, cfg map[string]string) map[string]string {
	out := make(map[string]string, len(cfg)+4)
	for k, v := range cfg {
		out[k] = v
	}
	if d, ok := Lookup(providerKey); ok {
		for _, f := range d.Fields {
			if strings.TrimSpace(out[f.Key]) == "" && f.Default != "" {
				out[f.Key] = f.Default
			}
		}
	}
	return out
}
