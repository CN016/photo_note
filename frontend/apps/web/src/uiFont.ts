/**
 * 业务 Web 应用统一使用的字体栈。
 *
 * 优先选择可免费商用的 Noto Sans SC、思源黑体，再回退到操作系统自带的中文字体。
 * 项目不打包方正字体、商业字体文件或其他来源不明的字体，画布测量也复用此字体栈，
 * 避免预览与导出采用不同字形。
 */
export const UI_FONT_STACK = '"Noto Sans SC", "Source Han Sans SC", "Source Han Sans CN", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei UI", "Microsoft YaHei", "Segoe UI", sans-serif'
