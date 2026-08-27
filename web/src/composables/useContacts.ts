import { computed } from 'vue'
import { Copy, ExternalLink, Mail, MessageCircle, Send } from 'lucide-vue-next'
import type { ShopContact } from '@/api/types'
import { useShopStore } from '@/stores/shop'
import { copyText } from '@/utils/format'
import { toast } from '@/ui'

/**
 * 客服联系方式的共用逻辑。
 *
 * 页脚的弹出菜单、页头导航、移动端菜单三处都要用同一套点击行为，
 * 抽出来免得复制三份 —— 复制出来的那几份迟早会走散。
 */
const ICONS: Record<string, typeof Send> = {
  telegram: Send,
  whatsapp: MessageCircle,
  wechat: MessageCircle,
  qq: MessageCircle,
  email: Mail,
}

export function useContacts() {
  const shop = useShopStore()
  const contacts = computed<ShopContact[]>(() => shop.config.contacts ?? [])

  function iconOf(c: ShopContact) {
    return ICONS[c.type] ?? MessageCircle
  }

  /** 右侧的动作提示图标：能跳转的显示外链，其余显示复制 */
  function actionIconOf(c: ShopContact) {
    return c.action === 'link' && c.url ? ExternalLink : Copy
  }

  /**
   * 点击一条联系方式。
   *
   * 能跳转的（Telegram / WhatsApp）开新标签 —— 别把正在下单的页面顶掉；
   * 其余（微信号、QQ 号、邮箱）复制到剪贴板，那才是用户真正要做的事。
   */
  async function pick(c: ShopContact) {
    if (c.action === 'link' && c.url) {
      window.open(c.url, '_blank', 'noopener,noreferrer')
      return
    }
    const ok = await copyText(c.value)
    ok ? toast.success(`${c.label || c.value} 已复制`) : toast.error('复制失败，请手动选中')
  }

  return { contacts, iconOf, actionIconOf, pick }
}
