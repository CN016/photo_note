import { defineStore } from 'pinia'
import { api } from '../api/client'

const NOTIFICATION_POLL_INTERVAL_MS = 30_000

let avatarRequestVersion = 0
let notificationTimer: number | undefined
let notificationPollingBusinessID = ''
let notificationFlight: { businessID: string; generation: number; promise: Promise<boolean> } | null = null
let newNotificationListener: (() => void) | null = null

/** 缓存跨页面共享的侧边栏数据；头像内容只保留在当前应用内存中，不写入持久存储。 */
export const useAppShellStore = defineStore('app-shell', {
  state: () => ({
    avatarBusinessID: '',
    avatarStoreKey: null as string | null,
    avatarSrc: '',
    avatarLoaded: false,
    avatarLoading: false,
    unreadCount: 0,
    unreadCountRevision: 0,
    notificationBusinessID: '',
    notificationGeneration: 0,
    latestNotificationID: '',
    notificationBaselineReady: false,
  }),
  actions: {
    /** 先复用当前账号和头像文件对应的内存缓存；只有缓存缺失或身份变化时才鉴权下载。 */
    async ensureAvatar(businessID: string, storekey: string | null): Promise<void> {
      if (this.avatarBusinessID !== businessID || this.avatarStoreKey !== storekey) {
        this.clearAvatarCache()
        this.avatarBusinessID = businessID
        this.avatarStoreKey = storekey
        this.avatarLoaded = !storekey
      }
      if (!storekey || this.avatarLoaded || this.avatarLoading) return

      this.avatarLoading = true
      const requestVersion = ++avatarRequestVersion
      try {
        // 整页刷新会重建 Pinia；reload 确保新进程读取最新头像，而不是命中旧 HTTP 缓存。
        const blob = await api.currentAvatar()
        if (requestVersion !== avatarRequestVersion || this.avatarBusinessID !== businessID || this.avatarStoreKey !== storekey) return
        this.avatarSrc = URL.createObjectURL(blob)
        this.avatarLoaded = true
      } catch {
        // 失败后本页保留昵称首字母回退，避免切页时反复请求；整页刷新后可再次尝试。
        if (requestVersion === avatarRequestVersion && this.avatarBusinessID === businessID && this.avatarStoreKey === storekey) {
          this.avatarSrc = ''
          this.avatarLoaded = true
        }
      } finally {
        if (requestVersion === avatarRequestVersion) this.avatarLoading = false
      }
    },

    /** 用刚上传成功的头像字节更新共享缓存，避免保存资料后再下载同一张图片。 */
    cacheAvatar(businessID: string, storekey: string, blob: Blob): void {
      this.clearAvatarCache()
      this.avatarBusinessID = businessID
      this.avatarStoreKey = storekey
      this.avatarSrc = URL.createObjectURL(blob)
      this.avatarLoaded = true
    },

    /** 图片无法解码时让侧边栏和个人中心共同显示回退头像，并避免重复下载坏文件。 */
    markAvatarUnavailable(businessID: string, storekey: string | null): void {
      if (this.avatarBusinessID !== businessID || this.avatarStoreKey !== storekey) return
      if (this.avatarSrc) URL.revokeObjectURL(this.avatarSrc)
      avatarRequestVersion += 1
      this.avatarSrc = ''
      this.avatarLoading = false
      this.avatarLoaded = true
    },

    /** 释放头像对象地址并使尚未完成的旧账号下载结果失效。 */
    clearAvatarCache(): void {
      avatarRequestVersion += 1
      if (this.avatarSrc) URL.revokeObjectURL(this.avatarSrc)
      this.avatarBusinessID = ''
      this.avatarStoreKey = null
      this.avatarSrc = ''
      this.avatarLoaded = false
      this.avatarLoading = false
    },

    /** 注册当前可见侧边栏的消息提示回调，并返回只移除该回调的清理函数。 */
    subscribeToNewNotifications(listener: () => void): () => void {
      newNotificationListener = listener
      return () => {
        if (newNotificationListener === listener) newNotificationListener = null
      }
    },

    /** 启动全应用唯一的未读数轮询；路由重新挂载侧边栏时复用原轮询和已有数量。 */
    startNotificationPolling(businessID: string): void {
      if (!businessID) return
      if (notificationTimer !== undefined && notificationPollingBusinessID === businessID) return

      if (notificationTimer !== undefined) window.clearInterval(notificationTimer)
      notificationTimer = undefined
      notificationPollingBusinessID = businessID
      if (this.notificationBusinessID !== businessID) {
        this.notificationGeneration += 1
        this.unreadCountRevision += 1
        this.notificationBusinessID = businessID
        this.unreadCount = 0
        this.latestNotificationID = ''
        this.notificationBaselineReady = false
      }

      void this.refreshNotifications(businessID)
      notificationTimer = window.setInterval(() => { void this.refreshNotifications(businessID) }, NOTIFICATION_POLL_INTERVAL_MS)
    },

    /** 首次读取或定时读取最新通知，并从同一分页响应更新共享角标，减少一次独立计数请求。 */
    async refreshNotifications(businessID: string): Promise<boolean> {
      if (!businessID || this.notificationBusinessID !== businessID) return false
      const generation = this.notificationGeneration
      const unreadCountRevision = this.unreadCountRevision
      if (notificationFlight?.businessID === businessID && notificationFlight.generation === generation) return notificationFlight.promise

      const promise = (async () => {
        try {
          const page = await api.notifications(1, 1, false)
          if (this.notificationBusinessID !== businessID || this.notificationGeneration !== generation) return false

          // 请求途中如果消息中心已读操作或更新的列表先改了角标，就保留较新的本地值。
          if (this.unreadCountRevision === unreadCountRevision) {
            this.unreadCount = Math.max(0, page.unread_count)
            this.unreadCountRevision += 1
          }
          const latestID = Array.isArray(page.items) ? page.items[0]?.business_id ?? '' : ''
          const hasNewNotification = this.notificationBaselineReady && Boolean(latestID) && latestID !== this.latestNotificationID
          this.latestNotificationID = latestID
          this.notificationBaselineReady = true
          if (hasNewNotification) {
            try { newNotificationListener?.() } catch { /* 提示组件故障不影响角标同步和后续轮询。 */ }
          }
          return true
        } catch {
          // 请求失败时保留上次角标和基线；下一次轮询或整页刷新会继续更新。
          return false
        }
      })()
      notificationFlight = { businessID, generation, promise }
      try {
        return await promise
      } finally {
        if (notificationFlight?.promise === promise) notificationFlight = null
      }
    },

    /** 消息中心读取列表后同步导航角标；账号不一致的迟到响应不得污染当前用户状态。 */
    setUnreadCount(businessID: string, count: number, expectedRevision?: number): boolean {
      if (!businessID || (this.notificationBusinessID && this.notificationBusinessID !== businessID)) return false
      if (expectedRevision !== undefined && expectedRevision !== this.unreadCountRevision) return false
      this.notificationBusinessID = businessID
      this.unreadCount = Math.max(0, count)
      this.unreadCountRevision += 1
      return true
    },

    /** 已读操作成功后立即同步减少角标，不必等待后台轮询周期。 */
    decrementUnreadCount(businessID: string): void {
      if (this.notificationBusinessID !== businessID) return
      this.unreadCount = Math.max(0, this.unreadCount - 1)
      this.unreadCountRevision += 1
    },

    /** 全部已读后立即清零共享角标。 */
    clearUnreadCount(businessID: string): void {
      if (this.notificationBusinessID === businessID) {
        this.unreadCount = 0
        this.unreadCountRevision += 1
      }
    },

    /** 退出或鉴权失效时停止后台轮询并释放当前账号的头像和消息缓存。 */
    reset(): void {
      if (notificationTimer !== undefined) window.clearInterval(notificationTimer)
      notificationTimer = undefined
      notificationPollingBusinessID = ''
      notificationFlight = null
      newNotificationListener = null
      this.clearAvatarCache()
      this.unreadCount = 0
      this.unreadCountRevision += 1
      this.notificationBusinessID = ''
      this.notificationGeneration += 1
      this.latestNotificationID = ''
      this.notificationBaselineReady = false
    },
  },
})
