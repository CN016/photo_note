import { createI18n } from "vue-i18n";
import commonZh from "./common.zh-CN.json";
import commonEn from "./common.en-US.json";
import homeZh from "./home.zh-CN.json";
import homeEn from "./home.en-US.json";
import downloadZh from "./download.zh-CN.json";
import downloadEn from "./download.en-US.json";

// 官网语言偏好独立保存；隐私模式拒绝存储时仍可正常浏览与切换。
let locale = "zh-CN";
try {
  if (localStorage.getItem("qujingtai.website.locale") === "en-US")
    locale = "en-US";
} catch {
  /* 存储不可用时使用中文。 */
}
export const i18n = createI18n({
  legacy: false,
  locale,
  fallbackLocale: "zh-CN",
  messages: {
    "zh-CN": { common: commonZh, home: homeZh, download: downloadZh },
    "en-US": { common: commonEn, home: homeEn, download: downloadEn },
  },
});
