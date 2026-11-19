# SigLIP 2 Model Notice

The image tagger uses the SigLIP 2 Base patch16-224 model published by Google Research under the Apache License 2.0.

- Upstream model: `google/siglip2-base-patch16-224`
- Upstream URL: https://huggingface.co/google/siglip2-base-patch16-224
- Reference implementation: https://github.com/google-research/big_vision
- ONNX conversion source: https://huggingface.co/onnx-community/siglip2-base-patch16-224-ONNX
- Runtime artifact: FP16 vision encoder only
- Runtime artifact SHA-256: `a1959f7bd3993a607e48839f6d01e25b876fe76afda301b028b78eef68aabd95`

The text encoder and tokenizer are used only by the offline embedding preparation script and are not included in the API image.

The model is provided without warranties or conditions of any kind. See `SIGLIP2-LICENSE.txt` for the full Apache License 2.0 text.
