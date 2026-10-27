# backend/dist清理前内容记录

实际执行状态：清理前记录和清单已提交为 `70631a7`。两条PowerShell清理命令均在执行前被工具安全策略拒绝（`blocked by policy`，未提供更具体原因）；未删除目录内容。固定绝对路径复核通过、未发现重解析点或Git跟踪文件。当前仍需在用户本机手动清空目录，不能将本记录视为清理完成证明。

## 目的、范围与执行顺序

用户要求清空后端dist，并在清空前记录内容。本记录及完整文件清单先提交Git，再删除目录中的全部内容；保留 `E:/work/photo_nini/backend/dist` 目录本身。只清理这一已解析的绝对路径；不修改Docker镜像、容器、数据库、上传存储、安卓artifacts、Web dist、官网dist或仓库外签名私钥。

清点时间：2026-10-27T12:47:52+08:00。共 **625个文件、111个子目录、4,560,280,211字节（约4.25GiB）**；PowerShell递归检查未发现重解析点，目标本身不是链接。Git跟踪的源码不位于此忽略目录内。

完整路径、大小和最后修改时间见 [清理前文件清单](20261027-backend-dist-清理前文件清单.csv)。清单只保存元数据，不复制文件正文、图片或临时会话凭据。本次不保留dist内容备份；旧日志、截图、恢复副本等被删除后不会继续本地保留。

## 内容分类与重新生成依据

- **API及AI基础镜像导出**：4.2.0/4.2.1的tar、gzip及校验文件，4.2.1重新部署包与原gzip内容相同；AI Alpine运行时导出包。删除导出文件不删除Docker中的镜像，可重新 `docker save` 并gzip；基础镜像标签仍在即可避免重新编译ONNX Runtime。
- **Web与官网部署包**：4.1.0、4.2.0、4.2.1 Web ZIP，包含安卓更新目录的Web ZIP，以及安卓1.1.0官网ZIP；可分别从 `frontend/apps/web`、`website` 构建。APK位于安卓artifacts，不在本次清理范围。
- **迁移及文档副本**：000006/000011/000013/000014/000023/000024 SQL，以及交付说明、改动记录副本。正式SQL在 `backend/migrations`，长期文档在 `docs`；删除dist副本不影响其受控原件。
- **AI基准与合成上传素材**：`alpine-ai-validation/`、`inspiration-benchmark/`、`library-test-uploads/`；用于Alpine运行时、识别准确率/并发与缩略图验收，并非生产上传目录。
- **租户测试与旧恢复目录**：`tenant-e2e/`、`tenant-e2e-uploads/`、`_tenant-recovery/`，包括测试素材、旧损坏源码副本和Vite依赖缓存。`thumbnail-index-sync/` 为独立测试库索引同步临时工具。
- **测试证据与临时脚本**：library、thumbnail421、android-update前缀的JSON、Python脚本、日志、截图、提交说明与资源指标，另含临时测试会话文件。本记录不保存会话正文。已提交的长期验收数据在 `docs/benchmarks`。

## 清理前一级目录及文件

| 名称 | 文件数 | 字节数 |
| --- | ---: | ---: |
| `000006_add_inspiration_file_size.sql` | 1 | 413 |
| `000011_create_notification_center.sql` | 1 | 9,009 |
| `000013_feedback_notifications.sql` | 1 | 4,233 |
| `000014_seed_feishu_feedback_image_upload.sql` | 1 | 492 |
| `000023_inspiration_library.sql` | 1 | 11,070 |
| `000024_inspiration_thumbnail_triggers.sql` | 1 | 986 |
| `20261027-安卓签名资源动态更新与独立更新页.md` | 1 | 4,866 |
| `20261027-灵感库缩略图缓存与识别触发收紧.md` | 1 | 3,284 |
| `_tenant-recovery/` | 370 | 36,934,008 |
| `alpine-ai-validation/` | 21 | 4,740,526 |
| `android-update-commit-message.txt` | 1 | 755 |
| `android-update-delivery.json` | 1 | 1,848 |
| `android-update-device-tests.log` | 1 | 1,016 |
| `android-update-final-device-tests.log` | 1 | 180 |
| `android-update-gradle-test.log` | 1 | 4,568 |
| `android-update-gradle.log` | 1 | 6,058 |
| `android-update-package-device-tests.log` | 1 | 86 |
| `android-update-screen.png` | 1 | 39,492 |
| `android-update-web-build.log` | 1 | 18,590 |
| `android-update-web-tests.log` | 1 | 1,400 |
| `android-web-updates.md` | 1 | 5,260 |
| `api421-redeploy-manifest.json` | 1 | 482 |
| `api421-redeployment-build.log` | 1 | 1,785 |
| `DELIVERY-4.2.0.md` | 1 | 5,297 |
| `inspiration-benchmark/` | 49 | 40,205,562 |
| `library-accuracy-validation.json` | 1 | 2,879 |
| `library-accuracy-validation.py` | 1 | 2,260 |
| `library-api-validation.json` | 1 | 2,035 |
| `library-api-validation.py` | 1 | 2,998 |
| `library-backend-commit.txt` | 1 | 747 |
| `library-build.log` | 1 | 3,228 |
| `library-calendar-desktop-en.png` | 1 | 67,765 |
| `library-colors-mobile-dark.png` | 1 | 44,800 |
| `library-desktop.png` | 1 | 52,171 |
| `library-go-tests.log` | 1 | 946 |
| `library-goal-en-mobile.png` | 1 | 57,616 |
| `library-mobile-en-dark.png` | 1 | 44,958 |
| `library-mobile.png` | 1 | 33,797 |
| `library-queue-validation.json` | 1 | 674 |
| `library-release-commit.txt` | 1 | 686 |
| `library-reminder-mobile-en.png` | 1 | 42,320 |
| `library-resource-limits.txt` | 1 | 35 |
| `library-test-uploads/` | 93 | 13,332,826 |
| `library-test.log` | 1 | 14,420 |
| `library-web-build.log` | 1 | 18,416 |
| `library-web-commit.txt` | 1 | 714 |
| `photo-nini-4.2.0-delivery-manifest.json` | 1 | 5,243 |
| `photo-nini-4.2.1-manifest.json` | 1 | 2,205 |
| `photo-nini-ai-runtime-alpine3.22-ort1.23.2-cpu-v1-linux-amd64.tar` | 1 | 1,003,022,336 |
| `photo-nini-ai-runtime-alpine3.22-ort1.23.2-cpu-v1-linux-amd64.tar.sha256` | 1 | 133 |
| `photo-nini-api-4.2.0-gzip-validation.json` | 1 | 418 |
| `photo-nini-api-4.2.0-linux-amd64.tar` | 1 | 1,041,895,424 |
| `photo-nini-api-4.2.0-linux-amd64.tar.gz` | 1 | 346,043,885 |
| `photo-nini-api-4.2.0-linux-amd64.tar.gz.sha256` | 1 | 107 |
| `photo-nini-api-4.2.0-linux-amd64.tar.sha256` | 1 | 104 |
| `photo-nini-api-4.2.1-linux-amd64.tar` | 1 | 1,042,178,560 |
| `photo-nini-api-4.2.1-linux-amd64.tar.gz` | 1 | 346,140,460 |
| `photo-nini-api-4.2.1-linux-amd64.tar.gz.sha256` | 1 | 107 |
| `photo-nini-api-4.2.1-linux-amd64.tar.sha256` | 1 | 104 |
| `photo-nini-api-4.2.1-redeploy-20261027.tar.gz` | 1 | 346,140,460 |
| `photo-nini-api-4.2.1-redeploy-20261027.tar.gz.sha256` | 1 | 113 |
| `photo-nini-web-20261027-android-updates.zip` | 1 | 38,870,954 |
| `photo-nini-web-20261027-android-updates.zip.sha256` | 1 | 111 |
| `photo-nini-web-4.2.0.zip` | 1 | 19,542,216 |
| `photo-nini-web-4.2.0.zip.sha256` | 1 | 92 |
| `photo-nini-web-4.2.1.zip` | 1 | 19,543,291 |
| `photo-nini-web-4.2.1.zip.sha256` | 1 | 92 |
| `photo-nini-web-v4.1.0.zip` | 1 | 18,182,859 |
| `photo-nini-web-v4.1.0.zip.sha256` | 1 | 92 |
| `photo-nini-www-android-1.1.0-20261027.zip` | 1 | 242,143,704 |
| `photo-nini-www-android-1.1.0-20261027.zip.sha256` | 1 | 109 |
| `shutdown-request.txt` | 1 | 160 |
| `tenant-e2e/` | 1 | 126 |
| `tenant-e2e-uploads/` | 4 | 504 |
| `thumbnail-index-sync/` | 1 | 773 |
| `thumbnail421-api-validation.json` | 1 | 2,315 |
| `thumbnail421-api-validation.py` | 1 | 3,047 |
| `thumbnail421-browser.png` | 1 | 517,758 |
| `thumbnail421-build.log` | 1 | 3,228 |
| `thumbnail421-commit-message.txt` | 1 | 630 |
| `thumbnail421-daily-candidate.json` | 1 | 160 |
| `thumbnail421-daily-session.json` | 1 | 701 |
| `thumbnail421-go-tests.log` | 1 | 1,110 |
| `thumbnail421-mobile.png` | 1 | 283,037 |
| `thumbnail421-performance.json` | 1 | 700 |
| `thumbnail421-performance.py` | 1 | 2,464 |
| `thumbnail421-problem-key.txt` | 1 | 55 |
| `thumbnail421-queue.json` | 1 | 675 |
| `thumbnail421-resources.txt` | 1 | 35 |
| `thumbnail421-session.json` | 1 | 703 |
| `thumbnail421-web-build.log` | 1 | 18,417 |
| `thumbnail421-web-tests.log` | 1 | 1,430 |
| `website-android110-build.log` | 1 | 1,447 |

## 原有交付文件校验记录

下表读取原有 `.sha256` 文件，仅保存已记录的校验值，本次未重新对所有大文件计算哈希。

| 原文件 | SHA-256 |
| --- | --- |
| `photo-nini-ai-runtime-alpine3.22-ort1.23.2-cpu-v1-linux-amd64.tar` | `81bb18626ef00f94ba2113a67747eba839126e6ffbedde0ed9518710030a817b` |
| `photo-nini-api-4.2.0-linux-amd64.tar.gz` | `6d019ae53dbcd6913c357920a034492feb64e06e071bd4981613dc27258ec682` |
| `photo-nini-api-4.2.0-linux-amd64.tar` | `b120f9b4a8b75caf38abb0276f78aa568210f5dcd407c44650851762cf9281bf` |
| `photo-nini-api-4.2.1-linux-amd64.tar.gz` | `9f614b682ee77b108ea7c6c865402aa85ab2ac0f4b25fb646bbe97fd84de0198` |
| `photo-nini-api-4.2.1-linux-amd64.tar` | `cb8cab3fd5792494613b0d9935250cd618cbc913e152f1b3b4f507aef9a34e31` |
| `photo-nini-api-4.2.1-redeploy-20261027.tar.gz` | `9f614b682ee77b108ea7c6c865402aa85ab2ac0f4b25fb646bbe97fd84de0198` |
| `photo-nini-web-20261027-android-updates.zip` | `9f7b3f87a54b16f9bec89770c2dbc748d9f26f32915347b79215474a48497b93` |
| `photo-nini-web-4.2.0.zip` | `30aa77e683f5ac455c689b8a7c2eb7b064e9fadb3d7cffd29f9e39a9ec35395e` |
| `photo-nini-web-4.2.1.zip` | `3bedb7ad1046606e4085c88ea05ca855397c60427d519a7cdef33e65ed8828f4` |
| `photo-nini-web-v4.1.0.zip` | `b78abfa968ec4f9d8c8041794354c4f2519ba5b9e9dc32f5a3a28ff4c1e57d9a` |
| `photo-nini-www-android-1.1.0-20261027.zip` | `51bf1f0a13445f860351e564d8bf33712097d59c6bbf79f51798c7d209e24c92` |

## 本机Docker镜像保留情况

清理前通过Docker实际查询得到；本次只删除磁盘导出文件，不执行Docker清理。

| 标签 | 镜像ID（短） |
| --- | --- |
| `photo-nini-api:4.2.1` | `7be8590bfbfe` |
| `photo-nini-api:local` | `7be8590bfbfe` |
| `photo-nini-api:4.2.0` | `4483efedac4a` |
| `photo-nini-ai-runtime:alpine3.22-ort1.23.2-cpu-v1` | `61c3ca19abcf` |
| `photo-nini-ai-build:alpine3.22-ort1.23.2-cpu-v1` | `70adb6e13af0` |
| `photo-nini-api:4.1.0` | `ba3884316885` |
| `photo-nini-api:4.0.0` | `d596fedf54b4` |
| `photo-nini-api:3.0.1` | `0cd9da9d51c1` |
| `photo-nini-api:3.0.0` | `d61dc265fd15` |
| `photo-nini-api:2.10.0` | `85f37e3f66ed` |
| `photo-nini-api:2.9.0` | `305ed7dec6a3` |
| `photo-nini-api:2.7.0` | `9cba220b7985` |
| `photo-nini-api:2.6.0` | `41cb94ee0add` |
| `photo-nini-api:metadata-check` | `41cb94ee0add` |
| `photo-nini-api:2.3.5` | `eec6ccc3459d` |
| `photo-nini-api:2.3.3` | `4d0c81ed3bd4` |
| `photo-nini-api:2.3.1` | `b6ce6c09e0d5` |
| `photo-nini-api:2.3.0` | `a2976ca5a89e` |

## 验证与限制

清理前检查：绝对路径、重解析点、文件数量及体积、完整CSV清单、受控文件状态和Docker镜像标签。先提交本记录和清单，再用PowerShell原生命令清空目标；执行后再次确认目录存在且无子项。未涉及业务代码修改，不重新运行业务测试。删除清单是内容目录，不是备份，也不能作为生产已部署或恢复能力的证明。
