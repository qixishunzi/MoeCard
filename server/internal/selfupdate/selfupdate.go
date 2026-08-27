// Package selfupdate 从 GitHub Release 检查并安装新版本。
//
// # 这套机制保护什么、不保护什么
//
// 保护：传输被篡改、下载损坏、装错平台的包。全程 HTTPS，下载完的压缩包
// 必须和同一个 Release 里的 SHA256SUMS.txt 对得上，对不上直接放弃，
// 不会有"校验文件缺失就跳过校验"这种退路。
//
// 不保护：GitHub 账号本身被攻陷、或者发布者主动发一个恶意版本。校验和与
// 二进制来自同一个 Release，攻陷了发布流程的人可以同时改掉两者。要挡住
// 这种情况需要离线私钥签名（minisign / cosign），那意味着密钥管理，
// 得由仓库所有者决定要不要上。verifyChecksum 这一层是按"以后能插进签名校验"
// 的形状写的。
//
// 更新永远是显式动作：没有后台轮询，没有自动安装。
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo 是发布仓库。改成 fork 时只需要动这里。
const Repo = "qixishunzi/MoeCard"

// maxAssetSize 限制下载体积，防止被一个超大文件把磁盘撑满。
// 当前的包大概 15MB，留足余量。
const maxAssetSize = 200 << 20

// Release 是一次发布的摘要。
type Release struct {
	Version     string    `json:"version"` // 去掉 v 前缀的版本号
	Tag         string    `json:"tag"`
	Name        string    `json:"name"`
	Notes       string    `json:"notes"`
	URL         string    `json:"url"`
	Published   time.Time `json:"published_at"`
	AssetName   string    `json:"asset_name"` // 与当前平台匹配的包
	AssetURL    string    `json:"-"`
	ChecksumURL string    `json:"-"`
}

// CheckResult 是检查更新的结果。
type CheckResult struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"has_update"`
	// Supported 为 false 表示这个平台没有对应的发布包，或者当前运行方式
	// 不适合自更新（比如在容器里）。
	Supported bool     `json:"supported"`
	Reason    string   `json:"reason,omitempty"`
	Release   *Release `json:"release,omitempty"`
}

// Client 查询与安装更新。
type Client struct {
	Current string // 当前版本
	HTTP    *http.Client
	// APIBase / DownloadHost 只为测试留出替换点，正常运行时用默认值。
	APIBase string
}

// New 构造。
func New(current string) *Client {
	return &Client{
		Current: current,
		HTTP: &http.Client{
			Timeout: 5 * time.Minute, // 下载几十 MB，别设太短
		},
		APIBase: "https://api.github.com",
	}
}

// AssetName 返回当前平台对应的发布包文件名。
//
// 命名规则必须和 .github/workflows/release.yml 里的一致。
func AssetName(version string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("moecard_%s_%s_%s.%s", version, runtime.GOOS, runtime.GOARCH, ext)
}

// Check 查询最新版本。
func (c *Client) Check(ctx context.Context) (*CheckResult, error) {
	rel, err := c.latest(ctx)
	if err != nil {
		return nil, err
	}
	res := &CheckResult{
		Current:   c.Current,
		Latest:    rel.Version,
		HasUpdate: Newer(rel.Version, c.Current),
		Supported: true,
		Release:   rel,
	}
	if reason := unsupportedReason(); reason != "" {
		res.Supported = false
		res.Reason = reason
	} else if rel.AssetURL == "" {
		res.Supported = false
		res.Reason = fmt.Sprintf("这个版本没有提供 %s/%s 的安装包", runtime.GOOS, runtime.GOARCH)
	}
	return res, nil
}

// unsupportedReason 说明为什么这台机器不该走自更新。
func unsupportedReason() string {
	// 容器里的二进制是镜像的一部分，换掉它下次重建容器就没了，
	// 而且会让"镜像标签"和"实际跑的版本"对不上。
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "检测到容器环境。请改用拉取新镜像的方式升级，容器内自更新在重建后会丢失"
	}
	return ""
}

// latest 拉取最新 Release 的元信息。
func (c *Client) latest(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.APIBase, Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MoeCard/"+c.Current)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("仓库还没有发布任何版本")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub 返回 %d", resp.StatusCode)
	}

	var raw struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}

	rel := &Release{
		Version:   strings.TrimPrefix(raw.TagName, "v"),
		Tag:       raw.TagName,
		Name:      raw.Name,
		Notes:     raw.Body,
		URL:       raw.HTMLURL,
		Published: raw.PublishedAt,
	}
	want := AssetName(rel.Version)
	for _, a := range raw.Assets {
		switch {
		case a.Name == want:
			rel.AssetName, rel.AssetURL = a.Name, a.URL
		case a.Name == "SHA256SUMS.txt":
			rel.ChecksumURL = a.URL
		}
	}
	return rel, nil
}

// Apply 下载并安装指定版本，返回被替换掉的旧文件路径。
//
// 步骤刻意是这个顺序：先下完、先校验、先解出可执行文件，全部成功之后
// 才动现有文件。任何一步失败，磁盘上的东西原封不动。
func (c *Client) Apply(ctx context.Context, rel *Release, progress func(string)) (string, error) {
	if rel.AssetURL == "" {
		return "", fmt.Errorf("没有找到 %s/%s 的安装包", runtime.GOOS, runtime.GOARCH)
	}
	if rel.ChecksumURL == "" {
		// 没有校验文件就等于没有校验。宁可不更新，也不装一个来路不明的二进制。
		return "", errors.New("这个版本没有提供 SHA256SUMS.txt，无法校验，已中止")
	}
	if reason := unsupportedReason(); reason != "" {
		return "", errors.New(reason)
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)

	// 临时文件放在目标目录里：跨文件系统 rename 不是原子的，
	// 放系统临时目录再挪过来就失去了原子替换的意义。
	if err := checkWritable(dir); err != nil {
		return "", err
	}

	progress("正在下载 " + rel.AssetName)
	blob, err := c.download(ctx, rel.AssetURL)
	if err != nil {
		return "", err
	}

	progress("正在校验完整性")
	sums, err := c.downloadText(ctx, rel.ChecksumURL)
	if err != nil {
		return "", fmt.Errorf("下载校验文件失败: %w", err)
	}
	if err := verifyChecksum(blob, rel.AssetName, sums); err != nil {
		return "", err
	}

	progress("正在解包")
	bin, err := extractBinary(blob, rel.AssetName)
	if err != nil {
		return "", err
	}
	if len(bin) < 1<<20 {
		// 正常的包十几 MB。明显偏小说明解错了文件。
		return "", fmt.Errorf("解出来的可执行文件只有 %d 字节，不像是完整的包", len(bin))
	}

	progress("正在替换")
	return replaceExecutable(exe, bin)
}

// checkWritable 确认能在目标目录里创建文件。
//
// 提前试一次，比走到最后一步才发现没权限、留下一堆半成品好。
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".moecard-probe-*")
	if err != nil {
		return fmt.Errorf("没有权限写入 %s：%w\n（用 sudo 重跑，或者手动下载替换）", dir, err)
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}

func (c *Client) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MoeCard/"+c.Current)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败，HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAssetSize))
}

func (c *Client) downloadText(ctx context.Context, url string) (string, error) {
	b, err := c.download(ctx, url)
	return string(b), err
}

// verifyChecksum 比对下载内容与 SHA256SUMS.txt 里的条目。
//
// 找不到对应条目算失败：这通常意味着发布流程漏传了某个平台的校验和，
// 这时候"就这么装吧"是最不该做的选择。
func verifyChecksum(blob []byte, name, sums string) error {
	want := ""
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		// sha256sum 的文本模式是两个空格，二进制模式是空格加星号
		if strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("校验文件里没有 %s 的条目，已中止", name)
	}
	sum := sha256.Sum256(blob)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("校验不通过：期望 %s，实际 %s。下载可能被截断或篡改，已中止", want, got)
	}
	return nil
}

// extractBinary 从压缩包里取出可执行文件。
func extractBinary(blob []byte, name string) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		return fromZip(blob)
	}
	return fromTarGz(blob)
}

func isBinaryName(n string) bool {
	base := filepath.Base(n)
	return base == "moecard" || base == "moecard.exe"
}

func fromZip(blob []byte) ([]byte, error) {
	zr, err := zip.NewReader(strings.NewReader(string(blob)), int64(len(blob)))
	if err != nil {
		return nil, fmt.Errorf("解压失败: %w", err)
	}
	for _, f := range zr.File {
		if !isBinaryName(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, maxAssetSize))
	}
	return nil, errors.New("压缩包里没有找到 moecard 可执行文件")
}

func fromTarGz(blob []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(blob)))
	if err != nil {
		return nil, fmt.Errorf("解压失败: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解包失败: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !isBinaryName(h.Name) {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, maxAssetSize))
	}
	return nil, errors.New("压缩包里没有找到 moecard 可执行文件")
}

// replaceExecutable 原子替换当前可执行文件，返回旧文件的备份路径。
//
// 顺序：新文件先落到同目录 → 把自己改名成 .old → 新文件改名顶上。
// Windows 不让覆盖正在运行的 exe，但允许把它改名，所以这个顺序两边都成立。
// 中途失败时把 .old 名字还原回去，不会留下一个没有可执行文件的目录。
func replaceExecutable(exe string, bin []byte) (string, error) {
	dir := filepath.Dir(exe)
	newPath := exe + ".new"
	oldPath := exe + ".old"

	if err := os.WriteFile(newPath, bin, 0o755); err != nil {
		return "", fmt.Errorf("写入新版本失败: %w", err)
	}
	// 保留原来的权限位，别把一个 0700 的部署改成 0755
	if fi, err := os.Stat(exe); err == nil {
		_ = os.Chmod(newPath, fi.Mode())
	}

	_ = os.Remove(oldPath) // 上一次留下的
	if err := os.Rename(exe, oldPath); err != nil {
		os.Remove(newPath)
		return "", fmt.Errorf("备份当前版本失败: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		// 把自己放回去，否则这个目录里就没有可执行文件了
		_ = os.Rename(oldPath, exe)
		os.Remove(newPath)
		return "", fmt.Errorf("替换失败，已回滚: %w", err)
	}
	_ = dir
	return oldPath, nil
}

// 这里曾经有一个 CleanupOld()，在程序启动时删掉 moecard.old。
//
// 拿掉了：更新完提示「确认新版本正常后可以删掉备份」，而「确认正常」
// 就得先把新版本跑起来 —— 一跑，备份就被启动清理删了。回滚的安全网
// 在最需要它的那一刻刚好消失。
//
// 备份的清理交给下一次更新：replaceExecutable 会先 Remove 掉旧的
// .old 再建新的，所以磁盘上永远只有一份，不会越积越多。
// Windows 删不掉正在运行的 exe 这件事也由那里覆盖 —— 那时候
// 旧进程早就退出了。

// Newer 判断 a 是否比 b 新。
//
// 只认 x.y.z，多余的后缀（-beta.1 之类）按"数字相同则视为更旧"处理：
// 预发布版本不该盖过正式版。
func Newer(a, b string) bool {
	as, apre := splitVersion(a)
	bs, bpre := splitVersion(b)
	for i := 0; i < 3; i++ {
		if as[i] != bs[i] {
			return as[i] > bs[i]
		}
	}
	// 数字一致：没有后缀的是正式版，比带后缀的新
	return apre == "" && bpre != ""
}

func splitVersion(v string) ([3]int, string) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre, v = v[i+1:], v[:i]
	}
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return out, pre
		}
		out[i] = n
	}
	return out, pre
}
