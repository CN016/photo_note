# 灵感图人物检测 Docker 基准

此工具用于验证 2 核 CPU、2 GiB 内存下最高 10 个异步任务并发推理的可行性，不是业务识别接口。模型、公开图片和原始结果保存在 Git 忽略的 `backend/dist/inspiration-benchmark/`，不访问应用数据库或用户文件。

## 复现

在仓库根目录使用 PowerShell 执行：

```powershell
& backend/tools/inspiration-benchmark/run.ps1 -Rounds 10
```

需要 Docker Linux 容器和可下载 Python 镜像、PyPI 依赖、GitHub 官方模型、COCO 公开图片的网络。首次运行包含准备时间，下载与镜像构建不计入推理耗时。COCO 原图片域名存在证书主机名不匹配时，使用同一个公开 S3 桶的 HTTPS 路径，不关闭证书校验。下载的 SHA-256 记录在 `manifest.json`；这些摘要用于复核本次输入内容，不等于由上游签发的校验。

脚本顺序运行八个容器场景：Nano/Tiny × 共用 ONNX Session/每 worker 独立 Session × 普通图/4000×3000 放大图。每场景先执行一批未预热的 10 任务，再重复十批，共 110 次图片处理；所有任务同时由屏障放行，没有用串行队列代替 10 并发。大图由同样十张图片放大，仅用于解码内存与耗时压测。

限制参数为 `--cpus=2 --cpuset-cpus=0,1 --memory=2g --memory-swap=2g`，同时固定两个逻辑 CPU、限制 CPU 配额并禁用交换空间；推理容器使用 `--network=none`。每次 ONNX 推理的内部计算线程数为 1，OpenCV 和 BLAS 也限制为 1，避免十个任务分别启动多核计算线程池。共用 Session 仍允许多个线程同时调用 `session.run`，不是把十个请求合成一个批量输入。

## 指标与解释

- `batch_seconds`：从释放屏障到十张图完成本地文件读取、解码、缩放、模型推理、人物置信度过滤和 NMS；不含上传、压缩、数据库写回及真实队列等待。
- `session_load_seconds`：单独记录模型加载初始化时间；`cold=true` 仅表示第一次未预热推理，不包含镜像启动、Python 导入或模型下载。
- `max_task_overlap` / `max_inference_overlap`：根据真实起止时间计算的在途任务数/推理调用数。CPU 仍只有两个核；在途十任务不是十个核同时执行指令。解码速度差异可能让部分大图批次的推理调用峰值为 9，但十个图片处理任务仍同时在途。
- `resources.memory_peak`：cgroup 峰值；`process_peak_rss_kib`：进程峰值 RSS。两种计量口径分开保留，不能相加。Linux cgroup v1 的 `memory_events` 为 `memory.failcnt`，不是 v2 的 OOM 次数字段。
- `summary.json`：保留八场景环境、模型与图片摘要、每批耗时、并发峰值、初次人数结果，以及原始报告 SHA-256。P95 采用 nearest-rank，样本只有 11 批，不能作为长期服务 SLA。

同一份样例不足以验证背影、遮挡、远景、拥挤画面、海报或屏幕中人物的业务准确率；此工具没有标注真值，人数输出不能当作准确率报告。未检测到人物不等于保证没有人物。共享容器资源下的 Go API、压缩、PostgreSQL、Redis 和 HTTP 调用开销不在本工具内，需要业务接入后联合压测。

## 模型与算法来源

模型来自 [Megvii 官方 YOLOX 0.1.1rc0](https://github.com/Megvii-BaseDetection/YOLOX/releases/tag/0.1.1rc0)，Nano 和 Tiny 的输入均为 416×416。预处理和输出解码遵循 [官方 ONNX 示例](https://github.com/Megvii-BaseDetection/YOLOX/tree/main/demo/ONNXRuntime)及 [data_augment.py](https://github.com/Megvii-BaseDetection/YOLOX/blob/main/yolox/data/data_augment.py)：BGR、0–255 浮点输入、114 填充、步长 8/16/32 解码。只统计 COCO person 类，分数阈值 0.3、NMS IoU 阈值 0.45。官方仓库使用 [Apache-2.0](https://github.com/Megvii-BaseDetection/YOLOX/blob/main/LICENSE)；工具不复制或提交权重及样例图片，图片权利仍属于各原作者。

## 业务接入后的验证（4.2.0）

`queue-verify/main.go` 通过 `PHOTO_NINI_TENANT_TEST_DSN` 与 `PHOTO_NINI_QUEUE_TEST_USER` 指向显式的独立测试库和十图测试用户；工具拒绝非 `photo_nini_test` 数据库，临时暂停领取、释放十张已压缩任务并轮询结果落库，输出每轮耗时和active分析租约峰值。API须已在独立网络中运行；该工具会修改测试任务和处理启用配置，不能用于生产库。

`accuracy.py --predictions <JSON路径> --output <JSON路径>` 读取 `people_counts` 数组（顺序为prepare.py十张固定COCO图片），通过官方ZIP的HTTP Range只读取验证实例标注，输出精确人数、有无人和0/1/2/3+分组对照。只属于固定小样本验收，不代表全量准确率。最终证据与导出文件校验见 `docs/20261027-灵感库420交付验收与打包.md`。
