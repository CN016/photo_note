import { defineStore } from 'pinia'

/** 全局反馈抽屉由页面壳和公共页面共用，避免多数页面再叠一个悬浮按钮。 */
export const useFeedbackStore = defineStore('feedback', {
  state: () => ({ open: false }),
  actions: {
    /** 打开反馈抽屉；表单内容与提交状态仍由抽屉组件自己维护。 */
    openPanel() {
      this.open = true
    },
    /** 关闭抽屉，供抽屉自身和未来的键盘快捷入口复用。 */
    closePanel() {
      this.open = false
    },
  },
})
