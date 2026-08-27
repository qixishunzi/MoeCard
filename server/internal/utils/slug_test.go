package utils

import "testing"

// TestSlugifyPinyin 验证中文标题转成按字分隔的拼音 slug。
func TestSlugifyPinyin(t *testing.T) {
	cases := []struct{ in, want string }{
		{"王者荣耀代充", "wang-zhe-rong-yao-dai-chong"},
		{"Netflix 高级会员", "netflix-gao-ji-hui-yuan"},
		{"Steam 充值卡 100 元", "steam-chong-zhi-ka-100-yuan"},
		{"流媒体会员", "liu-mei-ti-hui-yuan"},
		{"软件授权", "ruan-jian-shou-quan"},
		{"游戏点卡", "you-xi-dian-ka"},
		// 纯英文保持原样（小写化）
		{"Office 365 Family", "office-365-family"},
		{"already-a-slug", "already-a-slug"},
		// 标点全部当作分隔符，不会残留
		{"会员（一个月）", "hui-yuan-yi-ge-yue"},
		{"A/B——测试", "a-b-ce-shi"},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestSlugifyAlwaysValid 无论输入什么，产出都必须能通过 ValidateSlug。
func TestSlugifyAlwaysValid(t *testing.T) {
	inputs := []string{
		"", "   ", "！！！", "😀😀", "---", "。，、；",
		"超长的商品名称需要被截断超长的商品名称需要被截断超长的商品名称需要被截断超长的商品名称需要被截断超长的商品名称需要被截断",
		"Mixed 混合 123 内容!!!",
	}
	for _, in := range inputs {
		got := Slugify(in)
		if err := ValidateSlug(got); err != nil {
			t.Errorf("Slugify(%q) = %q，不是合法 slug: %v", in, got, err)
		}
	}
}

// TestSlugifyStable 同一输入必须每次得到相同结果（多音字取首选读音）。
func TestSlugifyStable(t *testing.T) {
	const in = "重庆银行的行长"
	first := Slugify(in)
	for i := 0; i < 5; i++ {
		if got := Slugify(in); got != first {
			t.Fatalf("Slugify 结果不稳定: %q vs %q", first, got)
		}
	}
	if first == "" {
		t.Fatal("不应为空")
	}
	t.Logf("多音字示例: %q -> %q", in, first)
}
