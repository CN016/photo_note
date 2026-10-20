# 项目文档 API（V1）

所有 `/api/v1/projects` 接口需要 Access Token，并以令牌中的用户业务 UUID 限定项目、灵感图和附件范围。项目正文是 `document_schema_version: 1` 的 ProseMirror JSON；服务端不接收或执行自由 HTML。

## 项目列表与正文

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/v1/projects?page=1&page_size=20&q=关键词` | 分页搜索项目标题和正文；每页最多 100 条。 |
| `POST` | `/api/v1/projects` | 创建项目和第一份正文快照。 |
| `GET` | `/api/v1/projects/{business_id}` | 读取当前用户未删除项目。 |
| `PATCH` | `/api/v1/projects/{business_id}` | 保存标题、封面、正文和资源关联。 |
| `DELETE` | `/api/v1/projects/{business_id}` | 软删除项目；正文和关联关系继续保留。 |

创建和更新都提交完整快照：

```json
{
  "title": "项目标题",
  "cover_emoji": "✨",
  "cover_image_key": "",
  "cover_file_name": "",
  "document_schema_version": 1,
  "document": { "type": "doc", "content": [{ "type": "paragraph" }] },
  "revision": 1
}
```

图片封面要求 `cover_emoji` 为空；emoji 封面要求 `cover_image_key` 为空。创建请求忽略 `revision`，服务端从 1 开始。更新必须传入最近一次读取或保存返回的修订号；旧修订会收到 HTTP 409，客户端应重新加载后再编辑。

列表响应使用通用分页结构 `data.items/page/page_size/total`，每个项目项只包含业务 ID、标题、封面信息、修订号和更新时间，不加载正文 JSON。搜索文本对标题与正文文字执行 Unicode NFKC 规范化、小写转换和空白折叠，再以一元/二元 Unicode 字符 n-gram GIN 索引筛选候选，并使用 `strpos` 核对连续子串。单个汉字查询同样可命中；不检索附件二进制内容，也不执行 OCR。

文档资源节点示例：

```json
{
  "type": "inspirationImage",
  "attrs": { "src": "inspiration://<business_id>", "width": 100, "layout": "original" }
}
```

```json
{
  "type": "projectAttachment",
  "attrs": {
    "storekey": "local_20261020_<uuid>.pdf",
    "fileName": "参考资料.pdf",
    "contentType": "application/pdf",
    "fileSize": 1024,
    "width": 100,
    "layout": "original"
  }
}
```

正文静态图片必须引用当前用户的灵感图；服务端在保存事务中同步 `project_inspiration_usages`。软删除项目仍保留关系，所以只要任何有效或已删除项目引用某张灵感图，删除灵感图都会返回 HTTP 409。

## 封面与附件

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/v1/projects/cover-upload` | multipart 上传 `file`；服务端验证、压缩图片并返回私有 `storekey/file_name/file_size/content_type`。 |
| `POST` | `/api/v1/files` | 使用现有鉴权上传入口上传正文视频、GIF、Office 和 PDF 文件。 |
| `GET` | `/api/v1/projects/{business_id}/attachments/content?storekey=...` | 仅当当前项目已登记该 storekey 时以内联流返回附件。 |

附件正文支持 PDF、DOC/DOCX、XLS/XLSX、JPEG/PNG/WebP、GIF 及 MP4/WebM/Ogg 视频。服务端核对扩展名、文件头、实际文件大小并记录 `project_attachments`。封面也写入该表；一个 storekey 同时用于封面和正文时用途为 `both`。上传文件只在保存正文或封面引用后登记。移除引用会移除项目附件记录，但不会删除底层文件。

浏览器预览使用 `@zrimo/viewer` 的本地 Office/PDF/图像查看器；视频通过 Video.js 播放，GIF 由浏览器图片元素内嵌展示。查看器不上传文件到第三方服务。密码保护文件不支持，复杂旧版 Office 对象可能降低保真度，视频实际可播放编码取决于浏览器/WebView。应用构建时从依赖复制查看器 worker、WASM 和字体资源。

服务启动前由 schema 检查确认 `projects`、`project_inspiration_usages`、`project_attachments` 已通过 `backend/migrations/000020_project_documents.sql` 创建。V1 文件存储仍不提供全局文件清单或文件删除接口。
