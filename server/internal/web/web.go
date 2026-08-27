// Package web 把编译后的 Vue 前端嵌入二进制，实现单文件部署。
//
// dist/ 是 `npm run build` 的产物目录（见 web/vite.config.ts 的 outDir）。
// 它**不入版本库**，只保留一个 .gitkeep 让 go:embed 有目录可嵌 ——
// 否则新克隆的仓库会因为缺目录而编译失败。
//
// 没有构建前端时（如只想跑后端 API），自动回退到 placeholder.html，
// 因此 `go build ./...` 在任何情况下都能通过。
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var distFS embed.FS

// placeholderHTML 是未构建前端时展示的引导页。始终随代码提交。
//
//go:embed placeholder.html
var placeholderHTML []byte

// Assets 返回 dist 的子文件系统。
func Assets() (fs.FS, error) { return fs.Sub(distFS, "dist") }

// HasRealBuild 判断嵌入的是不是真实构建产物。
//
// 判据是 index.html 与 assets/ 同时存在 —— Vite 的产物一定两者都有。
func HasRealBuild() bool {
	sub, err := Assets()
	if err != nil {
		return false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return false
	}
	entries, err := fs.ReadDir(sub, "assets")
	return err == nil && len(entries) > 0
}

// longCacheExt 是带内容哈希的静态资源，可以长期缓存。
var longCacheExt = map[string]bool{
	".js": true, ".css": true, ".woff": true, ".woff2": true,
	".ttf": true, ".otf": true, ".eot": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".avif": true, ".ico": true, ".svg": true,
}

// SPAHandler 返回处理前端 SPA 的 gin.HandlerFunc。
//
// 关键点：**任何未命中静态文件的路径都回退到 index.html**。
// 没有这个回退，用户在 /admin/orders 页面刷新就会 404 ——
// 因为那是前端路由，服务器上并不存在对应文件。
func SPAHandler(adminPath func() string) (gin.HandlerFunc, error) {
	sub, err := Assets()
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	// 没有构建产物时用占位页兜底，服务照样能起来（只是前端不可用）
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		indexHTML = placeholderHTML
	}
	prefix, suffix := splitAdminMeta(indexHTML)

	return func(c *gin.Context) {
		reqPath := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")

		// API 路径绝不回退到 index.html，否则前端拿到 HTML 会解析 JSON 失败，
		// 报出莫名其妙的错误而不是清晰的 404。
		if strings.HasPrefix(reqPath, "api/") {
			c.JSON(http.StatusNotFound, gin.H{
				"code": 40401, "message": "接口不存在", "data": nil,
			})
			return
		}

		if reqPath != "" && reqPath != "." {
			if f, err := sub.Open(reqPath); err == nil {
				stat, serr := f.Stat()
				_ = f.Close()
				if serr == nil && !stat.IsDir() {
					if longCacheExt[strings.ToLower(path.Ext(reqPath))] &&
						strings.HasPrefix(reqPath, "assets/") {
						// Vite 产物文件名带内容哈希，可以放心长期缓存
						c.Header("Cache-Control", "public, max-age=31536000, immutable")
					}
					fileServer.ServeHTTP(c.Writer, c.Request)
					return
				}
			}
		}

		// SPA 回退：index.html 绝不能缓存，否则前端更新后用户拿到旧版本
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")

		// 后台入口路径只告诉「已经找到入口的人」。
		//
		// 访问 /<入口>/... 时把真实路径填进 <meta>，前端据此注册后台路由；
		// 访问其它任何页面时填空串，前端连后台路由都不会注册。
		// 要是无差别地下发，扫描器抓一下首页就知道入口在哪，改路径就白改了。
		want := ""
		if adminPath != nil {
			if p := adminPath(); p != "" && firstSegment(reqPath) == p {
				want = p
			}
		}
		if prefix == nil {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
			return
		}
		out := make([]byte, 0, len(prefix)+len(want)+len(suffix))
		out = append(out, prefix...)
		out = append(out, want...)
		out = append(out, suffix...)
		c.Data(http.StatusOK, "text/html; charset=utf-8", out)
	}, nil
}

// adminMetaMarker 是 index.html 里那个占位 meta 的前半段。
const adminMetaMarker = `<meta name="moecard-admin-path" content="`

// splitAdminMeta 把 index.html 按 content 值的位置切成前后两半，
// 之后每次请求只要把中间那段拼进去即可，不必反复做字符串搜索。
//
// 找不到标记（比如用的是占位页）时返回 nil，调用方原样输出。
func splitAdminMeta(html []byte) (prefix, suffix []byte) {
	i := bytes.Index(html, []byte(adminMetaMarker))
	if i < 0 {
		return nil, nil
	}
	valueStart := i + len(adminMetaMarker)
	j := bytes.IndexByte(html[valueStart:], '"')
	if j < 0 {
		return nil, nil
	}
	return html[:valueStart], html[valueStart+j:]
}

// firstSegment 取路径的第一段，例如 "abc/def" -> "abc"。
func firstSegment(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}
