# 容器内 AI 第三方来源

- YOLOX Nano 模型与预处理/原始输出解码参考：Megvii-BaseDetection/YOLOX，Apache-2.0，官方 0.1.1rc0 发布的 yolox_nano.onnx。模型文件不进入 Git，内容摘要见交付清单；版权及许可见 licenses/YOLOX-LICENSE.txt。
- ONNX Runtime 1.23.2：Microsoft，MIT，原生 musl 构建产物复用已有基础镜像；见 licenses/ONNXRuntime-LICENSE.txt。
- Pillow 11.2.1：Python-Pillow，HPND/PIL 许可，补充有界图片解码、EXIF 方向及透明背景处理；见 licenses/Pillow-LICENSE.txt。
- NumPy 2.2.4、OpenCV 4.11、Python 3.12 由 Alpine 包提供；保留其发行包版权信息与上游许可。
- SigLIP 2 Base patch16-224 模型来自 Google（Apache-2.0），ONNX FP16 转换来自 `onnx-community/siglip2-base-patch16-224-ONNX`；运行时仅保留视觉编码器和预计算标签文本向量。见 licenses/SigLIP2-MODEL-NOTICE.md 与 licenses/SIGLIP2-LICENSE.txt。

Go 业务服务通过私有 Unix Socket 传递已授权字节；推理进程不获得数据库、存储路径或上传 URL。COCO 验证图片仅位于忽略的测试产物目录，不加入正式镜像。
