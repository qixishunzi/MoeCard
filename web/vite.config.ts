import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// 构建产物直接输出到 Go 项目的 embed 目录，
// 这样 `go build` 出来的单个二进制就自带完整前端。
const GO_EMBED_DIR = fileURLToPath(new URL('../server/internal/web/dist', import.meta.url))

/**
 * dist/ 不入版本库，但 Go 的 //go:embed all:dist 需要目录存在才能编译，
 * 因此仓库里保留了一个 .gitkeep。
 *
 * Vite 的 emptyOutDir 会把它一起删掉 —— 构建完再写回去，
 * 免得开发者每次构建都在 git status 里看到一条 .gitkeep 被删除。
 */
function keepGoEmbedDir(): Plugin {
  return {
    name: 'moecard-keep-go-embed-dir',
    closeBundle() {
      if (!existsSync(GO_EMBED_DIR)) mkdirSync(GO_EMBED_DIR, { recursive: true })
      const keep = join(GO_EMBED_DIR, '.gitkeep')
      if (!existsSync(keep)) writeFileSync(keep, '')
    },
  }
}

export default defineConfig(({ mode }) => ({
  plugins: [vue(), tailwindcss(), keepGoEmbedDir()],

  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },

  server: {
    port: 5173,
    host: '127.0.0.1',
    proxy: {
      // 开发环境把 API 转发给 Go 服务
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
      '/uploads': { target: 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },

  build: {
    outDir: GO_EMBED_DIR,
    emptyOutDir: true,
    // 生产环境不产出 sourcemap：会泄露源码结构
    sourcemap: mode !== 'production',
    rollupOptions: {
      output: {
        // 手动分包：商城首页只需要 vue 运行时 + 首页代码。
        // 图标库与二维码库都只在特定页面用到，单独成块按需加载。
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          // 路径统一成正斜杠，否则 Windows 上的 \ 会让匹配全部落空
          const p = id.replace(/\\/g, '/')
          if (p.includes('/lucide-vue-next/')) return 'vendor-icons'
          if (p.includes('/qrcode/') || p.includes('/dijkstrajs/') || p.includes('/encode-utf8/')) {
            return 'vendor-qrcode'
          }
          if (p.includes('/axios/') || p.includes('/follow-redirects/')) return 'vendor-axios'
          if (
            p.includes('/@vue/') ||
            p.includes('/vue/dist/') ||
            p.includes('/vue-router/') ||
            p.includes('/pinia/')
          ) {
            return 'vendor-vue'
          }
          return 'vendor'
        },
      },
    },
  },
}))
