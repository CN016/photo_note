import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import ts from "typescript";

// 检验真实配置模块；仅替换 Vite 注入的构建环境，不复制被测校验逻辑。
const source = readFileSync(
  new URL("../src/config.ts", import.meta.url),
  "utf8",
);
async function config(env) {
  // 虚拟模块的真实 APK 在构建阶段另行校验；此处注入空元数据，保留真实地址校验逻辑。
  const compiled = ts.transpileModule(
    source.replace("import androidDownload from 'virtual:android-downloads';", "const androidDownload = { version: '', variants: [] };").replaceAll("import.meta.env", JSON.stringify(env)),
    {
      compilerOptions: {
        module: ts.ModuleKind.ESNext,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText;
  return import(
    `data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`
  );
}
const empty = await config({});
assert.equal(empty.siteConfig.webUrl, undefined);
assert.equal(empty.siteConfig.siteUrl, undefined);
for (const release of empty.siteConfig.releases)
  assert.equal(empty.releaseUrl(release), undefined);
for (const url of [
  "javascript:alert(1)",
  "http://example.com",
  "https://user:pass@example.com",
  "bad",
]) {
  assert.equal(empty.publicHttpsUrl(url), undefined);
}
const configured = await config({
  VITE_WEB_APP_URL: "https://example.com/app/",
  VITE_SITE_URL: "https://example.org/",
});
assert.equal(configured.siteConfig.webUrl, "https://example.com/app/");
const release = {
  platform: "windows",
  status: "available",
  version: "1.0.0",
  architecture: "x64",
  url: "https://example.com/app.exe",
};
assert.equal(configured.releaseUrl(release), release.url);
assert.equal(configured.releaseUrl({ ...release, version: "" }), undefined);
assert.equal(
  configured.releaseUrl({ ...release, architecture: "" }),
  undefined,
);
assert.equal(
  configured.releaseUrl({ ...release, status: "comingSoon" }),
  undefined,
);
console.log("配置校验通过：空入口、安全地址、已发布入口与不完整发布信息。");
for (const platform of ["windows", "macos", "android", "ios"]) {
  const prefix = `VITE_${platform.toUpperCase()}`;
  const env = {
    [`${prefix}_STATUS`]: "available",
    [`${prefix}_VERSION`]: "1.2.3",
    [`${prefix}_ARCHITECTURE`]: "arm64",
    [`${prefix}_DOWNLOAD_URL`]: "https://example.com/download",
  };
  const loaded = await config(env);
  const item = loaded.siteConfig.releases.find(item => item.platform === platform);
  assert.equal(item.version, "1.2.3");
  assert.equal(item.architecture, "arm64");
  assert.equal(loaded.releaseUrl(item), env[`${prefix}_DOWNLOAD_URL`]);
  const pending = await config({ ...env, [`${prefix}_STATUS`]: "comingSoon" });
  assert.equal(pending.releaseUrl(pending.siteConfig.releases.find(item => item.platform === platform)), undefined);
}
console.log("四个平台环境变量映射与发布开关检查通过。");
