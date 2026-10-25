import androidDownload from 'virtual:android-downloads';
/** 同源安装包由构建插件核验并打包，页面统一从配置模块取得发布数据。 */
export { androidDownload };
/** 官网公开发布信息；只填写已核验的公共地址，禁止写入凭据。 */
export interface PlatformRelease {
  platform: "windows" | "macos" | "android" | "ios";
  status: "comingSoon" | "available";
  version?: string;
  architecture?: string;
  url?: string;
}

/** 拒绝脚本协议、带凭据地址；未配置地址时由界面显示待开放。 */
export function publicHttpsUrl(value?: string): string | undefined {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password
      ? url.href
      : undefined;
  } catch {
    return undefined;
  }
}

/** 各平台发布信息均读取公开构建变量；只有显式 available 才启用发布。 */
function platformRelease(platform: PlatformRelease["platform"]): PlatformRelease {
  const prefix = `VITE_${platform.toUpperCase()}`;
  const env = import.meta.env;
  return {
    platform,
    status: env[`${prefix}_STATUS`] === "available" ? "available" : "comingSoon",
    version: env[`${prefix}_VERSION`]?.trim(),
    architecture: env[`${prefix}_ARCHITECTURE`]?.trim(),
    url: publicHttpsUrl(env[`${prefix}_DOWNLOAD_URL`]),
  };
}

export const siteConfig = {
  webUrl: publicHttpsUrl(import.meta.env.VITE_WEB_APP_URL),
  siteUrl: publicHttpsUrl(import.meta.env.VITE_SITE_URL),
  releases: [
    platformRelease("windows"),
    platformRelease("macos"),
    platformRelease("android"),
    platformRelease("ios"),
  ],
};

/** 发布必须同时具备版本、架构和安全地址，避免配置不完整产生假下载。 */
export function releaseUrl(release: PlatformRelease): string | undefined {
  return release.status === "available" &&
    release.version?.trim() &&
    release.architecture?.trim()
    ? publicHttpsUrl(release.url)
    : undefined;
}
