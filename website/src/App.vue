<script setup lang="ts">
import { computed, ref, watchEffect } from "vue";
import { useI18n } from "vue-i18n";
import {
  ArrowRight,
  Picture,
  Calendar,
  Document,
  Bell,
  Wallet,
  List,
  Monitor,
  Iphone,
  Menu,
  Close,
  Collection,
  Link,
} from "@element-plus/icons-vue";
import { releaseUrl, siteConfig, androidDownload } from "./config";

const { t, locale } = useI18n();
const isDownload = /\/download\/?(?:index\.html)?$/.test(
  window.location.pathname,
);
const menuOpen = ref(false);
const homeLink = computed(() => (isDownload ? "/" : "#top"));
const features = [Picture, Document, Calendar, List, Wallet, Bell];
const releases = siteConfig.releases.filter(release => release.platform !== 'android' || !androidDownload.variants.length);
/** 安装包大小按当前语言格式化；摘要来自构建时对真实文件的校验。 */
function packageSize(bytes: number) { return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(bytes / 1024 / 1024)} MiB` }
/** UA 仅用于推荐，不限制用户手动选择；精简 UA 无架构时优先推荐主流 64 位。 */
const androidRecommendation = /Android/i.test(navigator.userAgent)
  ? (/aarch64|arm64|armv8|x86_64|x64/i.test(navigator.userAgent) ? 'arm64-v8a'
    : /armv7|armeabi|armv6|i[3-6]86/i.test(navigator.userAgent) ? 'armeabi-v7a' : 'arm64-v8a')
  : null;
const androidPackages = androidDownload.variants
  .filter(item => ['arm64-v8a', 'armeabi-v7a'].includes(item.abi))
  .sort((a, b) => Number(b.abi === androidRecommendation) - Number(a.abi === androidRecommendation));
const scenes = ["mountain", "light", "sea", "city"];
const year = new Intl.DateTimeFormat("en", { year: "numeric" }).format(
  new Date(),
);

/** 语言切换同步标题、可访问性与分享文案；存储被禁用不影响当前页面。 */
function toggleLanguage() {
  locale.value = locale.value === "zh-CN" ? "en-US" : "zh-CN";
  try {
    localStorage.setItem("qujingtai.website.locale", locale.value);
  } catch {
    /* 不阻断语言切换。 */
  }
}
watchEffect(() => {
  const page = isDownload ? "download" : "home";
  document.documentElement.lang = locale.value;
  document.title = t(`${page}.title`);
  for (const [selector, value] of [
    ['meta[name="description"]', t(`${page}.description`)],
    ['meta[property="og:title"]', t(`${page}.title`)],
    ['meta[property="og:description"]', t(`${page}.description`)],
  ])
    document.querySelector(selector)?.setAttribute("content", value);
});
// 仅在维护者明确配置正式域名时输出规范地址，避免索引到占位域名。
if (siteConfig.siteUrl) {
  const canonical = document.createElement("link");
  canonical.rel = "canonical";
  canonical.href = new URL(
    isDownload ? "download/" : "./",
    siteConfig.siteUrl.replace(/\/?$/, "/"),
  ).href;
  document.head.append(canonical);
}
</script>

<template>
  <a class="skip" href="#main">{{ t("common.skip") }}</a>
  <header id="top" class="header">
    <div class="nav-wrap">
      <a class="brand" :href="homeLink"
        >{{ t("common.brand")
        }}<span class="brand-dot" aria-hidden="true"></span
      ></a>
      <button
        class="menu-toggle icon-button"
        :aria-label="t('common.menu')"
        :aria-expanded="menuOpen"
        aria-controls="main-nav"
        @click="menuOpen = !menuOpen"
      >
        <Close v-if="menuOpen" /><Menu v-else />
      </button>
      <nav
        id="main-nav"
        :class="{ open: menuOpen }"
        :aria-label="t('common.nav')"
      >
        <a
          :href="isDownload ? '/#features' : '#features'"
          @click="menuOpen = false"
          >{{ t("common.features") }}</a
        >
        <a
          :href="isDownload ? '/#workflow' : '#workflow'"
          @click="menuOpen = false"
          >{{ t("common.workflow") }}</a
        >
        <a
          href="/download/"
          :aria-current="isDownload ? 'page' : undefined"
          @click="menuOpen = false"
          >{{ t("common.download") }}</a
        >
        <button
          class="language"
          :aria-label="t('common.languageLabel')"
          @click="toggleLanguage"
        >
          {{ t("common.language") }}
        </button>
        <a
          v-if="siteConfig.webUrl"
          class="button small"
          :href="siteConfig.webUrl"
          >{{ t("common.openWeb") }}<ArrowRight
        /></a>
        <span v-else class="status nav-status">{{ t("common.pending") }}</span>
      </nav>
    </div>
  </header>

  <main id="main" tabindex="-1">
    <template v-if="!isDownload">
      <section class="hero section">
        <div class="hero-copy">
          <p class="eyebrow">
            <span aria-hidden="true"></span>{{ t("home.eyebrow") }}
          </p>
          <h1>
            {{ t("home.headline1") }}<br /><em>{{ t("home.headline2") }}</em>
          </h1>
          <p class="intro">{{ t("home.intro") }}</p>
          <div class="actions">
            <a class="button" href="/download/"
              >{{ t("common.download") }}<ArrowRight /></a
            ><a class="text-link" href="#features"
              >{{ t("home.explore") }}<ArrowRight
            /></a>
          </div>
          <p class="hero-foot">{{ t("home.index") }}</p>
        </div>
        <div class="hero-art" aria-hidden="true">
          <div class="art-caption">{{ t("home.scene1") }}</div>
          <div class="sun"></div>
          <div class="ridge ridge-back"></div>
          <div class="ridge ridge-front"></div>
          <span class="art-number">01 / 04</span>
          <div class="art-note">{{ t("home.collectionSub") }}</div>
        </div>
      </section>

      <section class="product section" :aria-label="t('common.demo')">
        <div class="section-top">
          <span>{{ t("home.workspace") }}</span
          ><span>{{ t("common.demo") }}</span>
        </div>
        <div class="workspace">
          <aside class="mock-sidebar">
            <strong>{{ t("common.brand") }}</strong>
            <div
              v-for="(Icon, index) in features.slice(0, 4)"
              :key="index"
              :class="{ selected: index === 0 }"
            >
              <component :is="Icon" />{{ t(`home.feature${index + 1}`) }}
            </div>
          </aside>
          <div class="mock-main">
            <div class="mock-heading">
              <div>
                <h2>{{ t("home.collection") }}</h2>
                <p>{{ t("home.collectionSub") }}</p>
              </div>
              <Collection />
            </div>
            <div class="mock-tags">
              <span
                v-for="tag in ['all', 'portrait', 'landscape', 'light']"
                :key="tag"
                >{{ t(`home.${tag}`) }}</span
              >
            </div>
            <div class="scene-grid">
              <div
                v-for="(scene, index) in scenes"
                :key="scene"
                class="scene-card"
              >
                <div class="scene" :class="scene" aria-hidden="true"></div>
                <p>{{ t(`home.scene${index + 1}`) }}</p>
              </div>
            </div>
          </div>
          <aside class="mock-detail">
            <p class="eyebrow">{{ t("home.palette") }}</p>
            <div class="swatches" aria-hidden="true">
              <i></i><i></i><i></i><i></i>
            </div>
            <Calendar />
            <h3>{{ t("home.shoot") }}</h3>
            <strong>{{ t("home.shootName") }}</strong>
            <p>{{ t("home.shootNote") }}</p>
          </aside>
        </div>
      </section>

      <section id="features" class="features section">
        <p class="eyebrow">{{ t("home.sectionLabel") }}</p>
        <h2>{{ t("home.sectionTitle") }}</h2>
        <p class="section-intro">{{ t("home.sectionIntro") }}</p>
        <div class="feature-grid">
          <article v-for="(Icon, index) in features" :key="index">
            <div class="feature-icon"><component :is="Icon" /></div>
            <span class="feature-number">0{{ index + 1 }}</span>
            <h3>{{ t(`home.feature${index + 1}`) }}</h3>
            <p>{{ t(`home.detail${index + 1}`) }}</p>
          </article>
        </div>
      </section>

      <section id="workflow" class="workflow">
        <div class="section workflow-inner">
          <div>
            <p class="eyebrow">{{ t("home.flowLabel") }}</p>
            <h2>{{ t("home.flowTitle") }}</h2>
            <div class="workflow-art" aria-hidden="true">
              <div class="scene sea"></div>
              <div class="paper">
                <Document /><span>{{ t("home.feature2") }}</span
                ><i></i><i></i><i></i>
              </div>
            </div>
          </div>
          <ol>
            <li v-for="index in 3" :key="index">
              <span class="step-number">0{{ index }}</span>
              <div>
                <h3>{{ t(`home.step${index}`) }}</h3>
                <p>{{ t(`home.step${index}Body`) }}</p>
              </div>
            </li>
          </ol>
        </div>
      </section>

      <section class="faq section">
        <h2>{{ t("home.faqTitle") }}</h2>
        <div>
          <details v-for="index in 3" :key="index">
            <summary>{{ t(`home.q${index}`) }}</summary>
            <p>{{ t(`home.a${index}`) }}</p>
          </details>
        </div>
      </section>
      <section class="cta section">
        <p class="eyebrow">{{ t("common.brand") }}</p>
        <h2>{{ t("home.ctaTitle") }}</h2>
        <p>{{ t("home.ctaBody") }}</p>
        <a class="button" href="/download/"
          >{{ t("common.download") }}<ArrowRight
        /></a>
      </section>
    </template>

    <template v-else>
      <section class="download-hero section">
        <a class="text-link back" href="/">{{ t("common.back") }}</a>
        <p class="eyebrow">{{ t("download.eyebrow") }}</p>
        <h1>{{ t("download.heading") }}</h1>
        <p class="intro">{{ t("download.intro") }}</p>
      </section>
      <section class="downloads section">
        <article class="web-card">
          <div class="web-symbol"><Monitor /></div>
          <div class="web-copy">
            <span class="eyebrow">{{ t("download.webTag") }}</span>
            <h2>{{ t("download.web") }}</h2>
            <p>{{ t("download.webBody") }}</p>
            <a v-if="siteConfig.webUrl" class="button" :href="siteConfig.webUrl"
              >{{ t("common.openWeb") }}<ArrowRight /></a
            ><template v-else
              ><span class="status">{{ t("common.pending") }}</span>
              <p class="availability-note">
                {{ t("common.webPending") }}
              </p></template
            >
          </div>
          <div class="web-decoration" aria-hidden="true">
            <div class="scene mountain"></div>
            <div class="scene sea"></div>
          </div>
        </article>
        <article v-if="androidDownload.variants.length" id="android" class="android-download">
          <div class="android-heading"><Iphone class="platform-icon" /><div><span class="status">{{ t('download.androidTest') }}</span><h2>{{ t('download.androidHeading') }}</h2></div></div>
          <p class="android-version">{{ t('download.version') }} {{ androidDownload.version }} · {{ t('download.androidMinimum') }}</p>
          <div class="android-packages">
            <section v-for="item in androidPackages" :key="item.abi" class="android-package">
              <h3>{{ t(`download.abi.${item.abi}`) }} <span v-if="item.abi === androidRecommendation" class="status">{{ t('download.recommended') }}</span></h3>
              <p>{{ packageSize(item.bytes) }}</p>
              <a class="button small" :href="item.url" :download="item.filename">{{ t('download.downloadApk') }}<ArrowRight /></a>
              <details class="checksum"><summary>{{ t('download.checksum') }}</summary><code>{{ item.sha256 }}</code><a class="text-link" :href="item.checksumUrl" download>{{ t('download.checksumFile') }}</a></details>
            </section>
          </div>
        </article>
        <div class="platform-heading">
          <h2>{{ t("download.platforms") }}</h2>
          <p>{{ t("download.platformNote") }}</p>
        </div>
        <div class="platform-grid">
          <article
            v-for="release in releases"
            :key="release.platform"
          >
            <component
              :is="
                ['android', 'ios'].includes(release.platform) ? Iphone : Monitor
              "
              class="platform-icon"
            />
            <h3>{{ t(`download.${release.platform}`) }}</h3>
            <p>
              {{
                t(
                  ["android", "ios"].includes(release.platform)
                    ? "download.mobile"
                    : "download.desktop",
                )
              }}
            </p>
            <template v-if="releaseUrl(release)"
              ><p>
                {{ t("download.version") }} {{ release.version }} ·
                {{ t("download.architecture") }} {{ release.architecture }}
              </p>
              <a class="button small" :href="releaseUrl(release)"
                >{{ t("common.available") }}<ArrowRight /></a></template
            ><span v-else class="status">{{ t("common.comingSoon") }}</span>
          </article>
        </div>
        <article class="mini-card">
          <Link />
          <div>
            <h3>
              {{ t("download.mini")
              }}<span class="status">{{ t("download.planned") }}</span>
            </h3>
            <p>{{ t("download.miniBody") }}</p>
          </div>
        </article>
        <aside class="release-notice">
          <h3>{{ t("download.notice") }}</h3>
          <p>{{ t("download.noticeBody") }}</p>
        </aside>
      </section>
    </template>
  </main>
  <footer class="footer section">
    <div>
      <a class="brand" href="/"
        >{{ t("common.brand")
        }}<span class="brand-dot" aria-hidden="true"></span
      ></a>
      <p>{{ t("common.note") }}</p>
    </div>
    <div class="footer-links">
      <a href="/">{{ t("common.home") }}</a
      ><a href="/download/">{{ t("common.download") }}</a
      ><span>© {{ year }} {{ t("common.copyright") }}</span>
    </div>
  </footer>
</template>
