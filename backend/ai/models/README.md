# SigLIP 2 标签模型

自动标签使用 `google/siglip2-base-patch16-224` 的 Apache-2.0 模型。运行时只加载 `onnx-community/siglip2-base-patch16-224-ONNX` 的 FP16 视觉编码器，文本编码器和 tokenizer 只在构建前生成固定标签向量时使用。

- 模型来源：`https://huggingface.co/onnx-community/siglip2-base-patch16-224-ONNX`
- 基础模型来源：`https://huggingface.co/google/siglip2-base-patch16-224`
- 许可：Apache-2.0
- 视觉模型：`siglip2_vision_fp16.onnx`，186,039,516 字节
- 视觉模型 SHA-256：`a1959f7bd3993a607e48839f6d01e25b876fe76afda301b028b78eef68aabd95`
- 标签文本向量：`siglip2_tag_embeddings.npz`，由 `prepare_tag_embeddings.py` 生成，约 0.36 MB
- 标签目录版本：`siglip2-photo-tags-v1`

ONNX 模型文件不提交 Git。构建镜像前在 `backend/ai` 目录运行：

```sh
python prepare_tag_embeddings.py
```

脚本会下载约定版本、校验 SHA-256、生成标签向量，并写入本目录。该步骤需要 NumPy、ONNX Runtime 和 Tokenizers；正式容器运行时不需要 Tokenizers 或文本模型。

标签向量与 `tag_catalog.json` 必须同步更新。修改标签目录后应递增目录版本并重新生成向量，同时更新后端配置中的 `algorithm`。
