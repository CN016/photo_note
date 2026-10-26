"""下载官方模型和公开样例到忽略的 dist 目录，记录来源与内容摘要供复核。"""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.request


def main():
    """准备两个轻量模型和十张 COCO 图片；失败即退出，不用随机张量代替真实图片。"""
    parser = argparse.ArgumentParser()
    parser.add_argument('--large', action='store_true', help='生成用于解码内存压力测试的 4000×3000 放大图')
    args = parser.parse_args()
    root = Path('/data')
    root.mkdir(parents=True, exist_ok=True)
    sources = {}
    for model in ('nano', 'tiny'):
        sources[f'yolox_{model}.onnx'] = (
            'https://github.com/Megvii-BaseDetection/YOLOX/releases/download/'
            f'0.1.1rc0/yolox_{model}.onnx'
        )
    for image_id in (139, 285, 632, 724, 776, 785, 802, 872, 885, 1000):
        sources[f'{image_id:012}.jpg'] = (
            f'https://s3.amazonaws.com/images.cocodataset.org/val2017/{image_id:012}.jpg'
        )
    manifest = []
    for filename, url in sources.items():
        destination = root / filename
        if not destination.exists():
            print(f'Downloading {filename}', flush=True)
            # 使用临时后缀，避免中断下载被后续运行误当作完整模型。
            temporary = destination.with_suffix(destination.suffix + '.partial')
            request = urllib.request.Request(url, headers={'User-Agent': 'photo-nini-benchmark'})
            with urllib.request.urlopen(request, timeout=120) as response:
                temporary.write_bytes(response.read())
            temporary.rename(destination)
        manifest.append({'file': filename, 'url': url, 'bytes': destination.stat().st_size,
                         'sha256': hashlib.sha256(destination.read_bytes()).hexdigest()})
    (root / 'manifest.json').write_text(json.dumps(manifest, indent=2), encoding='utf-8')
    if args.large:
        import cv2
        cv2.setNumThreads(1)
        target = root / 'large'
        target.mkdir(exist_ok=True)
        # 放大图仅用于解码和内存压测，不用于宣称高分辨率图片检测准确率。
        for item in manifest:
            if not item['file'].endswith('.jpg'):
                continue
            image = cv2.imread(str(root / item['file']))
            if image is None:
                raise ValueError(f"Invalid source image: {item['file']}")
            destination = target / item['file']
            if not cv2.imwrite(str(destination), cv2.resize(image, (4000, 3000)), [cv2.IMWRITE_JPEG_QUALITY, 90]):
                raise IOError('Cannot write large benchmark image')


if __name__ == '__main__':
    main()
