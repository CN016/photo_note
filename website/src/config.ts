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

export const siteConfig = {
  webUrl: publicHttpsUrl(import.meta.env.VITE_WEB_APP_URL),
  siteUrl: publicHttpsUrl(import.meta.env.VITE_SITE_URL),
  releases: [
    { platform: "windows", status: "comingSoon" },
    { platform: "macos", status: "comingSoon" },
    { platform: "android", status: "comingSoon" },
    { platform: "ios", status: "comingSoon" },
  ] satisfies PlatformRelease[] as PlatformRelease[],
};

/** 发布必须同时具备版本、架构和安全地址，避免配置不完整产生假下载。 */
export function releaseUrl(release: PlatformRelease): string | undefined {
  return release.status === "available" &&
    release.version?.trim() &&
    release.architecture?.trim()
    ? publicHttpsUrl(release.url)
    : undefined;
}
