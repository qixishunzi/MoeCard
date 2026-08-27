package turnstile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeCF 起一个假的 Cloudflare 端点，把收到的表单原样交给断言。
func fakeCF(t *testing.T, reply string, capture *url.Values) *Verifier {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err == nil && capture != nil {
			*capture = r.PostForm
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return New().WithEndpoint(srv.URL)
}

func TestVerifySendsOfficialFields(t *testing.T) {
	var got url.Values
	v := fakeCF(t, `{"success":true,"hostname":"shop.example.com"}`, &got)

	res, err := v.Verify(context.Background(), "sec-key", "tok-123", "203.0.113.9")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Success {
		t.Fatal("应当成功")
	}
	if got.Get("secret") != "sec-key" {
		t.Errorf("secret 字段错: %q", got.Get("secret"))
	}
	if got.Get("response") != "tok-123" {
		t.Errorf("response 字段错: %q", got.Get("response"))
	}
	if got.Get("remoteip") != "203.0.113.9" {
		t.Errorf("remoteip 字段错: %q", got.Get("remoteip"))
	}
	if res.Hostname != "shop.example.com" {
		t.Errorf("hostname 没解析出来: %q", res.Hostname)
	}
}

// TestVerifyOmitsPrivateIP 内网/回环地址不该带给 Cloudflare。
// 反代后面很容易取到 127.0.0.1，带上去只会让判定失真。
func TestVerifyOmitsPrivateIP(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "192.168.1.7", "10.0.0.3", "", "not-an-ip"} {
		var got url.Values
		v := fakeCF(t, `{"success":true}`, &got)
		if _, err := v.Verify(context.Background(), "s", "t", ip); err != nil {
			t.Fatalf("Verify(%s): %v", ip, err)
		}
		if _, ok := got["remoteip"]; ok {
			t.Errorf("IP %q 不该被发送，实际发了 %q", ip, got.Get("remoteip"))
		}
	}
}

// TestVerifyParsesErrorCodes 官方的字段名是 error-codes（带连字符），
// 结构体 tag 写错的话这里会是空数组，而调用方会把「密钥配错」当成「用户没过」。
func TestVerifyParsesErrorCodes(t *testing.T) {
	v := fakeCF(t, `{"success":false,"error-codes":["invalid-input-secret"]}`, nil)
	res, err := v.Verify(context.Background(), "s", "t", "")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Success {
		t.Fatal("应当失败")
	}
	if len(res.ErrorCodes) != 1 || res.ErrorCodes[0] != ErrInvalidSecret {
		t.Fatalf("error-codes 没解析出来: %#v", res.ErrorCodes)
	}
	if !IsConfigError(res.ErrorCodes) {
		t.Error("invalid-input-secret 应被判定为配置错误")
	}
}

func TestVerifyRejectsBadInputLocally(t *testing.T) {
	v := fakeCF(t, `{"success":true}`, nil)
	ctx := context.Background()

	if _, err := v.Verify(ctx, "s", "   ", ""); err != ErrNoToken {
		t.Errorf("空令牌应返回 ErrNoToken，实际 %v", err)
	}
	if _, err := v.Verify(ctx, "s", strings.Repeat("x", MaxTokenLen+1), ""); err == nil {
		t.Error("超长令牌应被本地拒绝，不该白跑一次外部请求")
	}
	if _, err := v.Verify(ctx, "  ", "tok", ""); err == nil {
		t.Error("没配密钥时应报错")
	}
}

func TestVerifyHandlesTransportFailure(t *testing.T) {
	// 指向一个不存在的端口，模拟 Cloudflare 不可达
	v := New().WithEndpoint("http://127.0.0.1:1/siteverify")
	if _, err := v.Verify(context.Background(), "s", "t", ""); err == nil {
		t.Error("连不上时必须返回错误 —— 绝不能当成验证通过")
	}
}

func TestVerifyHandlesNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	v := New().WithEndpoint(srv.URL)
	if _, err := v.Verify(context.Background(), "s", "t", ""); err == nil {
		t.Error("HTTP 502 必须当作失败")
	}
}

func TestFriendlyErrorSeparatesConfigFromUser(t *testing.T) {
	cases := []struct {
		codes []string
		want  string
	}{
		{[]string{ErrInvalidSecret}, "人机验证配置有误（密钥不正确），请联系管理员"},
		{[]string{ErrTimeoutOrDup}, "人机验证已过期或被重复使用，请重新验证"},
		{[]string{ErrInvalidResponse}, "人机验证未通过，请重试"},
		{[]string{ErrInternal}, "Cloudflare 暂时不可用，请稍后重试"},
		{nil, "人机验证未通过，请重试"},
		{[]string{"某个还没见过的码"}, "人机验证未通过，请重试"},
	}
	for _, c := range cases {
		if got := FriendlyError(c.codes); got != c.want {
			t.Errorf("FriendlyError(%v) = %q, want %q", c.codes, got, c.want)
		}
	}

	if IsConfigError([]string{ErrTimeoutOrDup}) {
		t.Error("令牌重放是访客侧问题，不该被当成配置错误")
	}
}
