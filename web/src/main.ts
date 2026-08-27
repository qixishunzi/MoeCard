import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import router from './router'
import { registerAuthInterceptor } from './stores/auth'
import './styles/main.css'

const app = createApp(App)

app.use(createPinia())
app.use(router)

// 登录失效时统一跳回登录页。
// 这里注册而不是在 api 层直接依赖 router，避免循环依赖。
registerAuthInterceptor(() => {
  if (router.currentRoute.value.path.startsWith('/admin')) {
    router.replace({
      name: 'admin-login',
      query: { redirect: router.currentRoute.value.fullPath },
    })
  }
})

// 生产环境屏蔽 Vue 的详细错误提示（可能泄露组件结构），只记录到控制台
app.config.errorHandler = (err, _instance, info) => {
  console.error('[MoeCard]', info, err)
}

app.mount('#app')
