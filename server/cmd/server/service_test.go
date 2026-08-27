package main

import (
	"os"
	"strings"
	"testing"
)

// sectionsOf 按行把单元文件切成各个段落，只保留配置行。
//
// 不能用 strings.Index 找 "[Service]" —— [Unit] 段的注释里就提到了这个词，
// 会被当成段落起点，把整段判错（第一版就是这么写的，测试反过来诬告了正确的代码）。
func sectionsOf(unit string) map[string]string {
	out := map[string]string{}
	cur := ""
	for _, line := range strings.Split(unit, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]"):
			cur = t
		case strings.HasPrefix(t, "#"), t == "":
			// 注释和空行里的字样不算配置
		default:
			out[cur] += t + "\n"
		}
	}
	return out
}

// scripts/moecard.service 是给想手工改的人看的参考，内容必须和
// -install-service 生成的一致。两份各改各的迟早会漂：手工装的人拿到的
// 是老配置，用命令装的人拿到的是新配置，排查时两边看起来都"没错"。
func TestUnitMatchesCheckedInFile(t *testing.T) {
	const path = "../../../scripts/moecard.service"
	want, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("读不到 %s，跳过（发布包里的源码树没有 scripts/）", path)
	}

	got := renderUnit("/opt/moecard/moecard", "/opt/moecard", "moecard")

	// 顶部注释两边不一样（一个说"由命令生成"，一个是给人读的说明），
	// 比的是配置项本身。
	gotSec, wantSec := sectionsOf(got), sectionsOf(string(want))
	for _, name := range []string{"[Unit]", "[Service]", "[Install]"} {
		if gotSec[name] != wantSec[name] {
			t.Errorf("%s 段不一致。改了其中一个就要同步另一个：\n"+
				"  cd server && go run ./cmd/server -print-service > ../scripts/moecard.service\n"+
				"\n--- 生成的 ---\n%s\n--- 仓库里的 ---\n%s",
				name, gotSec[name], wantSec[name])
		}
	}
}

// 沙箱那几项是"万一被 RCE 了对方能干什么"的边界，删掉任何一条都该被注意到。
func TestUnitKeepsSandbox(t *testing.T) {
	u := renderUnit("/opt/moecard/moecard", "/opt/moecard", "moecard")
	for _, key := range []string{
		"User=moecard",
		"NoNewPrivileges=true",
		"ProtectSystem=full",
		"ReadWritePaths=/opt/moecard",
		"ProtectHome=true",
		"PrivateTmp=true",
		"RestrictSUIDSGID=true",
		"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX",
	} {
		if !strings.Contains(u, key) {
			t.Errorf("单元文件里少了 %q", key)
		}
	}
}

// StartLimit* 属于 [Unit]。写进 [Service] 的话 systemd 只打一行
// "Unknown key ... ignoring" 就继续跑，服务照样 active，但限流完全没生效 ——
// 配上 Restart=always 就是每 5 秒无限重启。真踩过一次。
func TestStartLimitInUnitSection(t *testing.T) {
	sec := sectionsOf(renderUnit("/opt/moecard/moecard", "/opt/moecard", "moecard"))
	for _, key := range []string{"StartLimitIntervalSec", "StartLimitBurst"} {
		if !strings.Contains(sec["[Unit]"], key) {
			t.Errorf("%s 必须在 [Unit] 段里", key)
		}
		if strings.Contains(sec["[Service]"], key) {
			t.Errorf("%s 出现在 [Service] 段里，systemd 会直接忽略它", key)
		}
	}
}

// 路径要原样带进去，不能被拼错 —— 装到非默认目录的人全靠这个。
func TestUnitUsesGivenPaths(t *testing.T) {
	u := renderUnit("/srv/shop/moecard", "/srv/shop", "shopuser")
	for _, want := range []string{
		"ExecStart=/srv/shop/moecard",
		"WorkingDirectory=/srv/shop",
		"ReadWritePaths=/srv/shop",
		"User=shopuser",
		"Group=shopuser",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("单元文件里少了 %q", want)
		}
	}
	// 别把默认路径漏在里面
	if strings.Contains(u, "/opt/moecard") {
		t.Error("生成的单元文件里还残留着默认路径 /opt/moecard")
	}
}
