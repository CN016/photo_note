import { defineStore } from 'pinia'
import { api, acceptTokenPair, clearTokens, getRefreshToken, getSessionInfo, SPACE_SWITCH_KEY, setAccessToken, setAuthLossHandler } from '../api/client'
import { ApiError, type UserProfile } from '../api/types'
import { useAppShellStore } from './appShell'

let initializeFlight: Promise<void> | null = null
let sessionListenerRegistered = false

/** 维护仅存在当前页面内存中的用户资料和鉴权初始化状态。 */
export const useAuthStore = defineStore('auth', {
  state: () => ({ user: null as UserProfile | null, ready: false, busy: false, orgid: -1, sessionVersion: 0, sessionID: '' }),
  actions: {
    /** 组织或会话变化时清空空间缓存；普通刷新保持同一代次，不重复重建页面。 */
    syncSession(): void {
      const info = getSessionInfo()
      if (!info) { this.sessionID = ''; this.sessionVersion = 0; this.orgid = -1; return }
      if (this.sessionID && this.sessionID !== info.session_id) {
        // 其他标签页更换了登录账号/会话时重新恢复资料，不能复用上一账号的表单。
        this.user = null
        window.location.reload()
        return
      }
      if (this.orgid !== info.orgid || this.sessionID !== info.session_id || this.sessionVersion !== info.session_version) {
        useAppShellStore().reset()
        this.orgid = info.orgid
        this.sessionID = info.session_id
        this.sessionVersion = info.session_version
      }
    },
    async refreshProfile(): Promise<void> { this.syncSession(); this.user = await api.currentUser() },
    /** 浏览器启动时消费持久化 Refresh Token，再读取服务端当前用户。 */
    async initialize(): Promise<void> {
      if (this.ready) return
      if (initializeFlight) return initializeFlight
      this.busy = true
      if (!sessionListenerRegistered) {
        window.addEventListener('photo-nini-session-updated', () => this.syncSession())
        sessionListenerRegistered = true
      }
      setAuthLossHandler(() => {
        this.user = null
        this.sessionID = ''
        this.sessionVersion = 0
        this.orgid = -1
        useAppShellStore().reset()
      })
      initializeFlight = (async () => {
        try {
          if (!getRefreshToken()) return
          // 新标签页在同步切换完成前不刷新凭证或挂载旧空间业务页面。
          while (localStorage.getItem(SPACE_SWITCH_KEY)) await new Promise(resolve => window.setTimeout(resolve, 100))
          const token = await api.refresh()
          if (!token) return
          setAccessToken(token)
          this.user = await api.currentUser()
          this.syncSession()
        } catch (cause) {
          if (cause instanceof ApiError && cause.status === 401) clearTokens()
          this.user = null
        } finally {
          this.ready = true
          this.busy = false
          initializeFlight = null
        }
      })()
      return initializeFlight
    },
    /** 注册或登录成功后保存 Refresh Token，并仅在 store 中持有用户资料。 */
    async signIn(phone: string, password: string): Promise<void> {
      const result = await api.login(phone, password)
      acceptTokenPair(result.tokens)
      this.user = result.user
      this.syncSession()
    },
    /** 注册成功直接使用返回的资料和 token pair 建立当前会话。 */
    async signUp(phone: string, password: string, nickname: string, avatar: Blob, filename: string): Promise<void> {
      const result = await api.register(phone, password, nickname, avatar, filename)
      acceptTokenPair(result.tokens)
      this.user = result.user
      this.syncSession()
    },
    /** 服务端撤销失败也立即清除本机凭证，避免界面继续显示已登录状态。 */
    async signOut(): Promise<void> {
      try { if (getRefreshToken()) await api.logout() } finally {
        clearTokens()
        this.user = null
        useAppShellStore().reset()
      }
    },
  },
})
