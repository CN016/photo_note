/** 构建期读取服务主机地址；路径前缀由调用各接口的前端模块负责维护。 */
const configuredBaseUrl = import.meta.env.VITE_API_BASE_URL as string | undefined
if (!configuredBaseUrl?.trim()) throw new Error('缺少 VITE_API_BASE_URL，请配置后端服务的协议、主机和端口。')

const parsedApiHost = new URL(configuredBaseUrl.trim())
if (!['http:', 'https:'].includes(parsedApiHost.protocol) || parsedApiHost.pathname !== '/' || parsedApiHost.search || parsedApiHost.hash) {
  throw new Error('VITE_API_BASE_URL 只能配置后端主机地址，不能包含 /api/v1 等接口路径。')
}

/** 标准化后的后端主机源，不包含接口版本路径或查询参数。 */
export const API_HOST_URL = parsedApiHost.origin

/** 开发环境使用同源相对路径以经过 Vite 代理；生产构建直连配置的服务主机。 */
export const API_REQUEST_ORIGIN = import.meta.env.DEV ? '' : API_HOST_URL
