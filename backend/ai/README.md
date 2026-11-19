# Alpine AI 运行时复用

正式 API Dockerfile 使用 `photo-nini-ai-runtime:alpine3.22-ort1.23.2-cpu-v1`。Compose 保持原文件，旧 `RUNTIME_BASE_IMAGE` 参数只为兼容保留，实际 AI 运行基底由 `AI_RUNTIME_BASE_IMAGE` 构建参数指定。新机器先 `docker load -i photo-nini-ai-runtime-alpine3.22-ort1.23.2-cpu-v1-linux-amd64.tar`，后续构建只编译 Go 与复制 Python 服务，不编译 ONNX Runtime。

## 自动标签模型

标签使用 SigLIP 2 Base patch16-224（Apache-2.0，86M 参数）做零样本多标签识别。结合对象/场景、景别、构图、光线、色彩、风格和氛围共 127 个候选标签；运行时还会补充人数、空镜、横竖构图、主色、明暗和饱和度等确定性标签。常用景别包含大远景、远景、全景、中景、中近景、近景、特写、大特写、半身、全身和胸像。

对比结果：

- RAM++ 的完整模型约 769MB，标签覆盖直接，但对当前 2GB CPU 容器偏重。
- MobileCLIP2-S0 更小，但模型使用 Apple ML Research Model TOU，商用发布边界不如 Apache-2.0 清晰。
- SigLIP 2 Base 使用 Apache-2.0，适合当前业务；正式选用 FP16 视觉编码器约 186MB。

当前 ONNX Runtime 构建缺少 INT8 `ConvInteger` 实现，实测 INT8 模型无法加载；Q4F16 也依赖未注册的 `MatMulNBits`。因此不能为了文件更小而交付启动失败的模型，正式镜像使用已验证可加载的 FP16 视觉编码器。文本编码器与 tokenizer 只在构建前运行，生成的 127 个标签向量约 0.36MB 随镜像发布，运行时不加载 283MB 文本模型。

在 Docker `--memory=2g --cpus=2` 隔离环境内实测最大推理重叠数为 10：10 张图约 14.20 秒，Python 进程峰值 RSS 约 1.228GB，单图约 24 个标签。模型只加载一份共享 ONNX Session，每次推理单线程；请求正文总量预算 256MB，解码像素预算 384MB，避免十张超大图片同时复制完整 RGBA。

`runtime/Dockerfile` 保存从已编译 builder wheel 组装基础镜像的操作，构建上下文需放官方 Nano 模型（模型不提交 Git）。它依赖本地 `photo-nini-ai-build:alpine3.22-ort1.23.2-cpu-v1` 和既有 Alpine API 基底；正式交付优先回载已经验证的 runtime 导出文件。

原生构建使用 ONNX Runtime 1.23.2、Alpine 3.22、Python 3.12、Ninja，CPUINFO 开启，CPU provider、单次推理内部线程数 1；禁用 contrib/ML ops、上游单元测试和 perftest 目标，构建并行 20。保留 Alpine no-execinfo 补丁和 logger 参数 maybe_unused 补丁。此次为恢复构建目录后生成 wheel，并非再次完整冷编译；已交付 builder 标签可复用，未声称这里的组装 Dockerfile 是完整原生冷构建脚本。

人物模型来自官方 YOLOX 0.1.1rc0 Nano，416 像素输入，人物置信阈值 0.3、NMS 0.45；属于估计检测数量，遮挡、小人像和人群可能漏检或重复检测。只有成功识别且数量为 0 才记为空镜。

颜色版本 `color-v1` 使用入库压缩图的有界 RGB 预览、4-bit RGB 直方图；保存最多五个代表色与其全图占比，剩余占比单独记录，合计 100%。12 色相按红色居中分区；V≤0.15 黑，V≥0.9 且 S≤0.15 白，其余 S≤0.15 灰。算法或模型变更应更新版本；4.2.1起每日扫描只补未识别图片，不自动重算已有成功记录。

Python 不继承 DB/Redis 凭证，只监听权限 0600 的私有 Unix Socket，目录 0700；Go 负责启动、重启与退出。格式错误、无归属历史 URL 不会被当作空镜，也不会向外网抓图。
