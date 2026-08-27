package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
		why  string
	}{
		{"1.0.1", "1.0.0", true, "补丁号更大"},
		{"1.1.0", "1.0.9", true, "次版本号更大"},
		{"2.0.0", "1.9.9", true, "主版本号更大"},
		{"1.0.0", "1.0.0", false, "相同不算更新"},
		{"1.0.0", "1.0.1", false, "更旧"},
		{"v1.2.0", "1.1.0", true, "带 v 前缀"},
		{"1.2", "1.1.9", true, "只写两段"},
		{"1.0.0", "1.0.0-beta.1", true, "正式版盖过预发布"},
		{"1.0.0-beta.1", "1.0.0", false, "预发布不该盖过正式版"},
		{"1.0.10", "1.0.9", true, "按数字比而不是字符串"},
		// 当前版本解析不出来（比如没注版本号的自建包）时，
		// 正式发布应该算更新 —— 提示一下总比让人以为自己是最新的好
		{"1.0.0", "乱写", true, "当前版本无法解析时按更旧处理"},
		{"乱写", "1.0.0", false, "发布标签无法解析时绝不触发更新"},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v，期望 %v（%s）", c.a, c.b, got, c.want, c.why)
		}
	}
}

// 校验和这一关是整套自更新唯一的安全屏障，每一种"绕过"都要挡住。
func TestVerifyChecksum(t *testing.T) {
	blob := []byte("这是一个假的安装包")
	sum := sha256.Sum256(blob)
	hexSum := hex.EncodeToString(sum[:])
	name := "moecard_1.0.0_linux_amd64.tar.gz"

	t.Run("正确的校验和通过", func(t *testing.T) {
		sums := "deadbeef  别的文件\n" + hexSum + "  " + name + "\n"
		if err := verifyChecksum(blob, name, sums); err != nil {
			t.Fatalf("应该通过，却报错: %v", err)
		}
	})

	t.Run("二进制模式的星号前缀也认", func(t *testing.T) {
		if err := verifyChecksum(blob, name, hexSum+" *"+name); err != nil {
			t.Fatalf("应该通过，却报错: %v", err)
		}
	})

	t.Run("大写校验和也认", func(t *testing.T) {
		if err := verifyChecksum(blob, name, strings.ToUpper(hexSum)+"  "+name); err != nil {
			t.Fatalf("应该通过，却报错: %v", err)
		}
	})

	t.Run("内容被改过就失败", func(t *testing.T) {
		if err := verifyChecksum([]byte("被掉包的内容"), name, hexSum+"  "+name); err == nil {
			t.Fatal("内容不匹配却通过了")
		}
	})

	t.Run("校验文件里没有这个文件名时失败", func(t *testing.T) {
		// 这条最重要：漏传校验和时绝不能退化成"那就不校验了"
		err := verifyChecksum(blob, name, hexSum+"  某个别的包.tar.gz")
		if err == nil {
			t.Fatal("找不到条目却通过了")
		}
		if !strings.Contains(err.Error(), "没有") {
			t.Errorf("错误信息该说清楚是缺条目，实际: %v", err)
		}
	})

	t.Run("空校验文件失败", func(t *testing.T) {
		if err := verifyChecksum(blob, name, ""); err == nil {
			t.Fatal("空校验文件却通过了")
		}
	})

	t.Run("同名前缀不会误匹配", func(t *testing.T) {
		// moecard_1.0.0_linux_amd64.tar.gz 不该匹配上 ..._linux_amd64.tar.gz.sig
		err := verifyChecksum(blob, name, hexSum+"  "+name+".sig")
		if err == nil {
			t.Fatal("前缀相同的别的文件被当成了本体")
		}
	})
}

func TestExtractBinary(t *testing.T) {
	payload := bytes.Repeat([]byte("MZ"), 64)

	t.Run("从 tar.gz 里取出 moecard", func(t *testing.T) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		writeTar(t, tw, "moecard-1.0.0/README.md", []byte("说明"))
		writeTar(t, tw, "moecard-1.0.0/moecard", payload)
		tw.Close()
		gz.Close()

		got, err := extractBinary(buf.Bytes(), "x.tar.gz")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Error("取出来的不是可执行文件")
		}
	})

	t.Run("从 zip 里取出 moecard.exe", func(t *testing.T) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		writeZip(t, zw, "使用说明.txt", []byte("说明"))
		writeZip(t, zw, "moecard.exe", payload)
		zw.Close()

		got, err := extractBinary(buf.Bytes(), "x.zip")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Error("取出来的不是可执行文件")
		}
	})

	t.Run("包里没有可执行文件时报错", func(t *testing.T) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		writeTar(t, tw, "README.md", []byte("只有说明"))
		tw.Close()
		gz.Close()

		if _, err := extractBinary(buf.Bytes(), "x.tar.gz"); err == nil {
			t.Fatal("包里没有可执行文件却没报错")
		}
	})

	t.Run("不认识的内容不会 panic", func(t *testing.T) {
		if _, err := extractBinary([]byte("这不是压缩包"), "x.tar.gz"); err == nil {
			t.Fatal("坏数据却没报错")
		}
	})
}

func TestAssetNameMatchesWorkflow(t *testing.T) {
	// 命名规则一旦和 release.yml 对不上，自更新就永远找不到包。
	// 这里只能钉住格式，真正的一致性由 README 里那张表和 workflow 保证。
	got := AssetName("1.2.3")
	if !strings.HasPrefix(got, "moecard_1.2.3_") {
		t.Errorf("文件名前缀不对: %s", got)
	}
	if !strings.HasSuffix(got, ".tar.gz") && !strings.HasSuffix(got, ".zip") {
		t.Errorf("扩展名不对: %s", got)
	}
}

func writeTar(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, zw *zip.Writer, name string, data []byte) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
}
