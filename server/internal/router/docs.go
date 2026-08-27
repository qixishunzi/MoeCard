package router

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed openapi.yaml
var openAPISpec []byte

// registerDocsRoutes 提供 OpenAPI 规范与在线文档页面。
//
// 不引入 swaggo 代码生成：那会给项目加上一堆构建期依赖和生成文件。
// 这里用手写的 openapi.yaml（随代码一起 review），
// 文档页面用 Swagger UI 的 CDN 资源渲染，Go 侧零依赖。
func registerDocsRoutes(r *gin.Engine) {
	r.GET("/api/docs/openapi.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", openAPISpec)
	})
	r.GET("/api/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerHTML))
	})
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MoeCard API 文档</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>body{margin:0}#swagger-ui{max-width:1200px;margin:0 auto}</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.onload = () => {
      window.ui = SwaggerUIBundle({
        url: '/api/docs/openapi.yaml',
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis],
        layout: 'BaseLayout',
        persistAuthorization: true,
      });
    };
  </script>
</body>
</html>`
