package selfupdate

import (
	"os"
	"testing"
)

// 用真实打出来的发布包验一遍解包路径。
//
// 单测里手工造的 tar 只能证明"我写的解包能读我写的 tar"。
// 包内目录结构（moecard_<版本>_<os>_<arch>/moecard）是 release.yml 定的，
// 那才是真正会出错的地方 —— 改错一层目录，自更新在发布当天才会暴露。
//
// 没有现成的包就跳过：CI 里跑单测时还没打包。
// 本地想验：设 MOECARD_TEST_PKG 指向一个 .tar.gz / .zip。
func TestExtractRealPackage(t *testing.T) {
	path := os.Getenv("MOECARD_TEST_PKG")
	if path == "" {
		t.Skip("未设置 MOECARD_TEST_PKG，跳过真实包解包验证")
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := extractBinary(blob, path)
	if err != nil {
		t.Fatalf("解包失败: %v", err)
	}
	if len(bin) < 1<<20 {
		t.Fatalf("解出来只有 %d 字节，不像是可执行文件", len(bin))
	}
	// ELF / PE / Mach-O 的魔数，确认拿到的确实是二进制而不是 README
	switch {
	case len(bin) > 4 && string(bin[:4]) == "\x7fELF":
	case len(bin) > 2 && string(bin[:2]) == "MZ":
	case len(bin) > 4 && (bin[0] == 0xcf || bin[0] == 0xce || bin[0] == 0xca):
	default:
		t.Errorf("解出来的开头是 % x，不像可执行文件", bin[:8])
	}
	t.Logf("解出 %d 字节", len(bin))
}
