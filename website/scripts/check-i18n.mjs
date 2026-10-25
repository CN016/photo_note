import { readFileSync } from "node:fs";

// 递归核对页面命名空间，缺键或多键均使交付检查失败。
function keys(value, prefix = "") {
  return Object.entries(value)
    .flatMap(([key, child]) => {
      const path = prefix ? `${prefix}.${key}` : key;
      return typeof child === "object" && child !== null
        ? keys(child, path)
        : [path];
    })
    .sort();
}
for (const namespace of ["common", "home", "download"]) {
  const read = (locale) =>
    JSON.parse(
      readFileSync(
        new URL(`../src/i18n/${namespace}.${locale}.json`, import.meta.url),
        "utf8",
      ),
    );
  if (
    JSON.stringify(keys(read("zh-CN"))) !== JSON.stringify(keys(read("en-US")))
  ) {
    throw new Error(`${namespace}: 中英文键集合不一致`);
  }
  console.log(`${namespace}: 中英文键集合一致`);
}
