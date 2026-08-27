// Package storage 提供文件存储抽象。
//
// 当前只实现本地存储；接口已为对象存储（S3 / OSS / COS / R2）预留，
// 新增实现只需满足 Provider 接口，业务层无需改动。
package storage

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/moecard/server/internal/utils"
)

// Provider 是存储后端接口。
type Provider interface {
	// Save 保存上传的文件，返回可公开访问的 URL。
	Save(file *multipart.FileHeader) (string, error)
	// Delete 删除文件（传入 Save 返回的 URL）。
	Delete(url string) error
	// Name 返回后端名称。
	Name() string
}

// allowedTypes 是允许上传的图片类型：真实 MIME → 扩展名。
//
// 只信任嗅探出来的真实 MIME，不信任客户端提交的 Content-Type 与扩展名 ——
// 攻击者可以把 shell.php 改名成 a.jpg 并伪造 Content-Type。
var allowedTypes = map[string]string{
	"image/jpeg":    ".jpg",
	"image/png":     ".png",
	"image/gif":     ".gif",
	"image/webp":    ".webp",
	"image/svg+xml": "", // 显式禁止：SVG 可以内嵌 <script>，是存储型 XSS 的常见载体
}

// LocalStorage 把文件保存到本地磁盘。
type LocalStorage struct {
	root      string // 绝对路径
	urlPrefix string // 对外 URL 前缀，如 /uploads
	maxSize   int64
}

// NewLocal 构造本地存储。
func NewLocal(root, urlPrefix string, maxSize int64) (*LocalStorage, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析存储路径失败: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("创建存储目录失败: %w", err)
	}
	if maxSize <= 0 {
		maxSize = 5 << 20
	}
	return &LocalStorage{
		root:      abs,
		urlPrefix: "/" + strings.Trim(urlPrefix, "/"),
		maxSize:   maxSize,
	}, nil
}

// Name 返回后端名称。
func (s *LocalStorage) Name() string { return "local" }

// Root 返回存储根目录（供静态文件路由使用）。
func (s *LocalStorage) Root() string { return s.root }

// URLPrefix 返回 URL 前缀。
func (s *LocalStorage) URLPrefix() string { return s.urlPrefix }

// Save 保存上传文件。
//
// 安全措施（对应 §39 要求）：
//  1. 大小限制
//  2. 通过嗅探真实内容判断 MIME，而非相信客户端
//  3. 扩展名由服务端根据真实 MIME 决定
//  4. 文件名完全由服务端随机生成，用户无法控制任何路径片段
//  5. 按日期分子目录，避免单目录文件过多
func (s *LocalStorage) Save(file *multipart.FileHeader) (string, error) {
	if file == nil {
		return "", errors.New("没有上传文件")
	}
	if file.Size <= 0 {
		return "", errors.New("上传文件为空")
	}
	if file.Size > s.maxSize {
		return "", fmt.Errorf("文件大小超过限制（最大 %d MB）", s.maxSize>>20)
	}

	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("打开上传文件失败: %w", err)
	}
	defer src.Close()

	// 嗅探真实类型：只读前 512 字节，这是 DetectContentType 的要求
	head := make([]byte, 512)
	n, err := io.ReadFull(src, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("读取上传文件失败: %w", err)
	}
	head = head[:n]

	mimeType := http.DetectContentType(head)
	mimeType = strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
	ext, ok := allowedTypes[mimeType]
	if !ok || ext == "" {
		return "", fmt.Errorf("不支持的文件类型: %s（仅支持 JPG / PNG / GIF / WebP）", mimeType)
	}

	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("重置文件读取位置失败: %w", err)
	}

	// 文件名与目录全部由服务端生成，用户输入不参与任何路径拼接
	sub := utils.NowUTC().Format("2006/01")
	dir := filepath.Join(s.root, filepath.FromSlash(sub))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建上传目录失败: %w", err)
	}
	name := utils.RandomFileName() + ext
	dst := filepath.Join(dir, name)

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("创建文件失败: %w", err)
	}
	defer out.Close()

	// LimitReader 兜底：即使 file.Size 被伪造，也不会写超限
	if _, err := io.Copy(out, io.LimitReader(src, s.maxSize+1)); err != nil {
		_ = os.Remove(dst)
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return s.urlPrefix + "/" + sub + "/" + name, nil
}

// Delete 删除文件。
func (s *LocalStorage) Delete(fileURL string) error {
	rel := strings.TrimPrefix(fileURL, s.urlPrefix)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return nil
	}

	// 路径穿越防护：清理后必须仍在 root 之内。
	// 没有这一步，攻击者传 ../../etc/passwd 就能删任意文件。
	full := filepath.Join(s.root, filepath.FromSlash(rel))
	cleaned := filepath.Clean(full)
	if !strings.HasPrefix(cleaned, s.root+string(os.PathSeparator)) && cleaned != s.root {
		return errors.New("非法的文件路径")
	}
	if err := os.Remove(cleaned); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var _ Provider = (*LocalStorage)(nil)
