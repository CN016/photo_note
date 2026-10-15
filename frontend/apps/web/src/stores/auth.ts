import { defineStore } from 'pinia'
import { api, acceptTokenPair, clearTokens, getRefreshToken, setAccessToken, setAuthLossHandler } from '../api/client'
import { ApiError, type UserProfile } from '../api/types'

let initializeFlight: Promise<void> | null = null

/** 维护仅存在当前页面内存中的用户资料和鉴权初始化状态。 */
export const useAuthStore = defineStore('auth', {
  state: () => ({ user: null as UserProfile | null, ready: false, busy: false }),
  actions: {
    /** 浏览器启动时消费持久化 Refresh Token，再读取服务端当前用户。 */
    async initialize(): Promise<void> {
      if (this.ready) return
      if (initializeFlight) return initializeFlight
      this.busy = true
      setAuthLossHandler(() => { this.user = null })
      initializeFlight = (async () => {
        try {
          if (!getRefreshToken()) return
          const token = await api.refresh()
          if (!token) return
          setAccessToken(token)
          this.user = await api.currentUser()
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
    },
    /** 注册成功直接使用返回的资料和 token pair 建立当前会话。 */
    async signUp(phone: string, password: string, nickname: string, avatar: Blob, filename: string): Promise<void> {
      const result = await api.register(phone, password, nickname, avatar, filename)
      acceptTokenPair(result.tokens)
      this.user = result.user
    },
    /** 服务端撤销失败也立即清除本机凭证，避免界面继续显示已登录状态。 */
    async signOut(): Promise<void> {
      try { if (getRefreshToken()) await api.logout() } finally {
        clearTokens()
        this.user = null
      }
    },
  },
})
