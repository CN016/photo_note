# Alpine AI 运行时复用

正式 API Dockerfile 使用 `photo-nini-ai-runtime:alpine3.22-ort1.23.2-cpu-v1`。Compose 保持原文件，旧 `RUNTIME_BASE_IMAGE` 参数只为兼容保留，实际 AI 运行基底由 `AI_RUNTIME_BASE_IMAGE` 构建参数指定。新机器先 `docker load -i photo-nini-ai-runtime-alpine3.22-ort1.23.2-cpu-v1-linux-amd64.tar`，后续构建只编译 Go 与复制 Python 服务，不编译 ONNX Runtime。

`runtime/Dockerfile` 保存从已编译 builder wheel 组装基础镜像的操作，构建上下文需放官方 Nano 模型（模型不提交 Git）。它依赖本地 `photo-nini-ai-build:alpine3.22-ort1.23.2-cpu-v1` 和既有 Alpine API 基底；正式交付优先回载已经验证的 runtime 导出文件。

原生构建使用 ONNX Runtime 1.23.2、Alpine 3.22、Python 3.12、Ninja，CPUINFO 开启，CPU provider、单次推理内部线程数 1；禁用 contrib/ML ops、上游单元测试和 perftest 目标，构建并行 20。保留 Alpine no-execinfo 补丁和 logger 参数 maybe_unused 补丁。此次为恢复构建目录后生成 wheel，并非再次完整冷编译；已交付 builder 标签可复用，未声称这里的组装 Dockerfile 是完整原生冷构建脚本。

人物模型来自官方 YOLOX 0.1.1rc0 Nano，416 像素输入，人物置信阈值 0.3、NMS 0.45；属于估计检测数量，遮挡、小人像和人群可能漏检或重复检测。只有成功识别且数量为 0 才记为空镜。

颜色版本 `color-v1` 使用入库压缩图的有界 RGB 预览、4-bit RGB 直方图；保存最多五个代表色与其全图占比，剩余占比单独记录，合计 100%。12 色相按红色居中分区；V≤0.15 黑，V≥0.9 且 S≤0.15 白，其余 S≤0.15 灰。算法或模型变更应更新版本；4.2.1起每日扫描只补未识别图片，不自动重算已有成功记录。

Python 不继承 DB/Redis 凭证，只监听权限 0600 的私有 Unix Socket，目录 0700；Go 负责启动、重启与退出。格式错误、无归属历史 URL 不会被当作空镜，也不会向外网抓图。
