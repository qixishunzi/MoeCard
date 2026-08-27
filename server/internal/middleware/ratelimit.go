package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/moecard/server/internal/api"
	"github.com/moecard/server/internal/logger"
)

// 限流采用内存令牌桶。
//
// 为什么不用 Redis：本项目的核心目标是"轻量、低依赖、易部署"。
// 单实例部署时内存限流完全够用；多实例部署时应在网关层（Nginx / Cloudflare）
// 做统一限流 —— 那本来就是更合适的位置。README 中有明确说明。

type bucket struct {
	tokens   float64
	last     time.Time
	lastSeen time.Time
}

// Limiter 是令牌桶限流器。
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket

	rate     float64 // 每秒补充的令牌数
	capacity float64 // 桶容量（允许的突发量）

	stop chan struct{}
	once sync.Once
}

// NewLimiter 创建限流器：每 window 时间内最多 limit 次请求。
func NewLimiter(limit int, window time.Duration) *Limiter {
	if limit <= 0 {
		limit = 60
	}
	if window <= 0 {
		window = time.Minute
	}
	l := &Limiter{
		buckets:  make(map[string]*bucket),
		rate:     float64(limit) / window.Seconds(),
		capacity: float64(limit),
		stop:     make(chan struct{}),
	}
	go l.gc()
	return l
}

// Allow 判断某个 key 是否允许通过，并返回需要等待的秒数。
func (l *Limiter) Allow(key string) (bool, int) {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.capacity - 1, last: now, lastSeen: now}
		return true, 0
	}

	// 按经过的时间补充令牌
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min64(l.capacity, b.tokens+elapsed*l.rate)
	b.last = now
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	// 还差多少秒才能攒够 1 个令牌
	wait := int((1 - b.tokens) / l.rate)
	if wait < 1 {
		wait = 1
	}
	return false, wait
}

// gc 定期清理长时间未使用的桶，防止内存无限增长。
func (l *Limiter) gc() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case now := <-ticker.C:
			l.mu.Lock()
			for k, b := range l.buckets {
				if now.Sub(b.lastSeen) > 15*time.Minute {
					delete(l.buckets, k)
				}
			}
			l.mu.Unlock()
		}
	}
}

// Close 停止后台清理。
func (l *Limiter) Close() {
	l.once.Do(func() { close(l.stop) })
}

// KeyFunc 决定限流的维度。
type KeyFunc func(c *gin.Context) string

// ByIP 按客户端 IP 限流。
func ByIP(prefix string) KeyFunc {
	return func(c *gin.Context) string { return prefix + "|" + c.ClientIP() }
}

// ByIPAndField 按 IP + 请求体某字段限流（如登录用户名）。
//
// 双维度很重要：只按 IP 限流的话，共用出口 IP 的正常用户会被少数人连累；
// 只按用户名限流的话，攻击者能用一个 IP 遍历所有用户名，
// 还能通过反复输错锁定他人账号造成拒绝服务。
//
// 登录接口收的是 JSON，而 c.PostForm 只解析表单编码的 body，
// 对 JSON 请求永远返回空 —— 之前的实现因此静默退化成了单纯的按 IP 限流。
// 这里显式读一次 JSON body 并原样放回，保证后续 BindJSON 仍能正常读取。
func ByIPAndField(prefix, field string) KeyFunc {
	return func(c *gin.Context) string {
		v := c.Query(field)
		if v == "" {
			v = c.PostForm(field)
		}
		if v == "" {
			v = fieldFromJSONBody(c, field)
		}
		return prefix + "|" + c.ClientIP() + "|" + strings.ToLower(strings.TrimSpace(v))
	}
}

// maxPeekBody 是为取限流维度而读取的 body 上限。
// 登录请求只有几十字节，给 64KB 足够；超出的部分不参与取值。
const maxPeekBody = 64 << 10

// fieldFromJSONBody 从 JSON body 中取出一个顶层字符串字段，并把 body 还原回去。
func fieldFromJSONBody(c *gin.Context, field string) string {
	if !strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		return ""
	}
	if c.Request.Body == nil {
		return ""
	}
	buf, err := io.ReadAll(io.LimitReader(c.Request.Body, maxPeekBody))
	if err != nil {
		// 读失败时把已读到的部分放回，交给后续 handler 去报错
		c.Request.Body = io.NopCloser(bytes.NewReader(buf))
		return ""
	}
	// 必须还原：body 是一次性的流，读完不放回后面就绑定不到任何数据
	c.Request.Body = io.NopCloser(bytes.NewReader(buf))

	var m map[string]any
	if json.Unmarshal(buf, &m) != nil {
		return ""
	}
	s, _ := m[field].(string)
	return s
}

// RateLimit 返回限流中间件。
func RateLimit(l *Limiter, keyFn KeyFunc) gin.HandlerFunc {
	if keyFn == nil {
		keyFn = ByIP("default")
	}
	return func(c *gin.Context) {
		key := keyFn(c)
		ok, wait := l.Allow(key)
		if !ok {
			c.Header("Retry-After", strconv.Itoa(wait))
			logger.L().Warn("请求被限流",
				"path", c.Request.URL.Path, "ip", c.ClientIP(), "retry_after", wait)
			api.Fail(c, api.NewErrorf(api.CodeTooManyReqs,
				"请求过于频繁，请 %d 秒后重试", wait))
			return
		}
		c.Next()
	}
}

// Limiters 集中管理各接口的限流器。
type Limiters struct {
	Login       *Limiter
	LoginIP     *Limiter
	Setup       *Limiter
	CreateOrder *Limiter
	QueryOrder  *Limiter
	Coupon      *Limiter
	Pay         *Limiter
	Public      *Limiter
}

// NewLimiters 按各接口的风险特征配置限流强度。
func NewLimiters() *Limiters {
	return &Limiters{
		// 登录：按 IP + 用户名，防对单个账号爆破
		Login: NewLimiter(5, time.Minute),
		// 登录：再按 IP 兜一层，防止换着用户名喷。
		// 比单账号限制宽，正常人不会一分钟内试 30 次登录
		LoginIP: NewLimiter(30, time.Minute),
		// 初始化：只会用一次，但要防止被反复探测
		Setup: NewLimiter(5, 10*time.Minute),
		// 下单：防止刷单占用库存
		CreateOrder: NewLimiter(10, time.Minute),
		// 订单查询：防止遍历订单号做 IDOR 探测
		QueryOrder: NewLimiter(20, time.Minute),
		// 优惠券校验：防止爆破券码
		Coupon: NewLimiter(20, time.Minute),
		// 发起支付
		Pay: NewLimiter(20, time.Minute),
		// 前台浏览接口的兜底限流
		Public: NewLimiter(300, time.Minute),
	}
}

// Close 关闭全部限流器。
func (l *Limiters) Close() {
	for _, x := range []*Limiter{l.Login, l.LoginIP, l.Setup, l.CreateOrder, l.QueryOrder, l.Coupon, l.Pay, l.Public} {
		if x != nil {
			x.Close()
		}
	}
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
