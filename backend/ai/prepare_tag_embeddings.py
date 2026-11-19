"""生成 SigLIP 2 图片标签文本向量；该工具只在构建镜像前运行，不进入 API 运行时。"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import urllib.request

import numpy as np
import onnxruntime as ort
from tokenizers import Tokenizer


SOURCE = "https://huggingface.co/onnx-community/siglip2-base-patch16-224-ONNX/resolve/main"
FILES = {
    "vision_model_fp16.onnx": "a1959f7bd3993a607e48839f6d01e25b876fe76afda301b028b78eef68aabd95",
    "text_model_int8.onnx": "3a0603d3a00c05a80a6ded4743c16aaac7b1e62cdcc7e362e7ce418659b96400",
    "tokenizer.json": "cb9140fae3ac5122c972d37adf83e1248471a38147ad76f8215c8872c6fd8322",
}
MAX_LENGTH = 64


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def download(cache: Path, name: str) -> Path:
    """下载指定 ONNX 资产并按固定摘要验证，拒绝静默替换模型。"""
    cache.mkdir(parents=True, exist_ok=True)
    target = cache / name
    if not target.exists():
        remote = "onnx/" + name if name.endswith(".onnx") else name
        with urllib.request.urlopen(f"{SOURCE}/{remote}", timeout=60) as response, target.open("wb") as output:
            while chunk := response.read(1024 * 1024):
                output.write(chunk)
    actual = sha256(target)
    if actual != FILES[name]:
        raise RuntimeError(f"{name} 摘要不匹配: {actual}")
    return target


def prompts(tag: dict) -> list[str]:
    """同一标签使用中英文与类别模板，降低单一措辞造成的误判。"""
    name, english, kind = tag["name"], tag["en"], tag["kind"]
    templates = {
        "scene": [f"a photograph taken at {english}", f"一张在{name}拍摄的照片"],
        "composition": [f"a photograph with {english}", f"一张使用{name}景别的照片"],
        "lighting": [f"a photograph with {english}", f"一张具有{name}的照片"],
        "color": [f"a photograph with {english}", f"一张{name}的照片"],
        "style": [f"a photograph in {english}", f"一张{name}照片"],
        "mood": [f"a photograph with {english}", f"一张具有{name}氛围的照片"],
        "technique": [f"a photograph using {english}", f"一张使用{name}的照片"],
        "subject": [f"a photograph of {english}", f"一张包含{name}的照片"],
    }
    return templates[kind]


def encode_text(model: Path, tokenizer_path: Path, tags: list[dict]) -> np.ndarray:
    """按固定 64 token 分批编码，平均多模板向量并重新归一化。"""
    tokenizer = Tokenizer.from_file(str(tokenizer_path))
    tokenizer.enable_truncation(max_length=MAX_LENGTH)
    tokenizer.enable_padding(direction="right", length=MAX_LENGTH, pad_id=0, pad_type_id=0, pad_token="<pad>")
    options = ort.SessionOptions()
    options.intra_op_num_threads = 2
    options.inter_op_num_threads = 1
    session = ort.InferenceSession(str(model), sess_options=options, providers=["CPUExecutionProvider"])
    vectors = []
    batch_size = 16
    for start in range(0, len(tags), batch_size):
        batch = tags[start:start + batch_size]
        variants = [prompts(tag) for tag in batch]
        flat = [prompt for group in variants for prompt in group]
        ids = np.asarray([tokenizer.encode(prompt).ids for prompt in flat], dtype=np.int64)
        pooled = np.asarray(session.run(None, {"input_ids": ids})[1], dtype=np.float32)
        norm = np.linalg.norm(pooled, axis=1, keepdims=True)
        pooled = pooled / np.maximum(norm, 1e-12)
        width = len(variants[0])
        averaged = pooled.reshape(len(batch), width, -1).mean(axis=1)
        averaged /= np.maximum(np.linalg.norm(averaged, axis=1, keepdims=True), 1e-12)
        vectors.append(averaged)
    return np.concatenate(vectors).astype(np.float32)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--catalog", type=Path, default=Path(__file__).with_name("tag_catalog.json"))
    parser.add_argument("--models-dir", type=Path, default=Path(__file__).with_name("models"))
    parser.add_argument("--cache-dir", type=Path, default=Path(__file__).with_name("models") / ".cache")
    args = parser.parse_args()

    catalog = json.loads(args.catalog.read_text(encoding="utf-8"))
    tags = catalog["tags"]
    names = [tag["name"] for tag in tags]
    if len(names) != len(set(names)) or not 32 <= len(names) <= 512:
        raise RuntimeError("标签目录必须唯一且包含 32 至 512 个标签")
    if any(not 1 <= len(name) <= 64 for name in names):
        raise RuntimeError("标签名称长度必须为 1 至 64 个字符")

    text_model = download(args.cache_dir, "text_model_int8.onnx")
    tokenizer = download(args.cache_dir, "tokenizer.json")
    vision_model = download(args.cache_dir, "vision_model_fp16.onnx")
    embeddings = encode_text(text_model, tokenizer, tags)
    args.models_dir.mkdir(parents=True, exist_ok=True)
    (args.models_dir / "siglip2_vision_fp16.onnx").write_bytes(vision_model.read_bytes())
    np.savez_compressed(
        args.models_dir / "siglip2_tag_embeddings.npz",
        names=np.asarray(names, dtype="<U64"),
        kinds=np.asarray([tag["kind"] for tag in tags], dtype="<U16"),
        embeddings=embeddings,
        version=np.asarray(catalog["version"]),
    )
    print(f"生成的标签向量: {len(names)} 个，视觉模型 {vision_model.stat().st_size} 字节")


if __name__ == "__main__":
    main()
