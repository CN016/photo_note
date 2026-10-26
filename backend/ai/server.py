"""容器内人物及颜色分析服务。仅监听私有 Unix Socket，不访问数据库、外网或任意文件路径。

人物预处理与解码遵循 Megvii YOLOX 官方 ONNX 示例（Apache-2.0）；仅运行 CPU person 类检测。
"""
import argparse
import colorsys
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler
from io import BytesIO
import json
import math
import os
from pathlib import Path
import socketserver
import threading
import warnings

import numpy as np
import cv2
import onnxruntime as ort
from PIL import Image, ImageOps


class PixelBudget:
    """大图解码共用像素内存额度；限制解码峰值，缩成预览后即释放，不限制后续十并发推理。"""
    def __init__(self, capacity=384 * 1024 * 1024):
        self.capacity = capacity
        self.available = capacity
        self.condition = threading.Condition()

    @contextmanager
    def acquire(self, requested):
        needed = min(requested, self.capacity)
        with self.condition:
            self.condition.wait_for(lambda: self.available >= needed)
            self.available -= needed
        try:
            yield
        finally:
            with self.condition:
                self.available += needed
                self.condition.notify_all()


def color_profile(rgb):
    """在最多 64×64 像素上统计四位 RGB 直方图，最大余数法保证代表色占比精确合计 100%。"""
    sample = Image.fromarray(rgb)
    sample.thumbnail((64, 64), Image.Resampling.LANCZOS)
    pixels = np.asarray(sample, dtype=np.uint8).reshape(-1, 3)
    values = pixels.astype(np.int32)
    keys = ((values[:, 0] >> 4) << 8) | ((values[:, 1] >> 4) << 4) | (values[:, 2] >> 4)
    buckets, indices, counts = np.unique(keys, return_inverse=True, return_counts=True)
    order = sorted(range(len(buckets)), key=lambda i: (-int(counts[i]), int(buckets[i])))[:5]
    sizes = [int(counts[i]) for i in order]
    sizes.append(len(pixels) - sum(sizes))
    exact = [n * 10000 / len(pixels) for n in sizes]
    units = [math.floor(n) for n in exact]
    for i in sorted(range(len(units)), key=lambda i: (-(exact[i] - units[i]), i))[:10000 - sum(units)]:
        units[i] += 1
    palette = []
    dominant = None
    for position, bucket in enumerate(order):
        means = np.floor(values[indices == bucket].mean(axis=0) + .5).astype(int)
        if dominant is None:
            dominant = means
        palette.append({'hex': '#%02x%02x%02x' % tuple(means), 'percentage': units[position] / 100})
    h, saturation, value = colorsys.rgb_to_hsv(*(float(n) / 255 for n in dominant))
    bucket = 'black' if value <= .15 else 'white' if value >= .9 and saturation <= .15 else 'gray' if saturation <= .15 else f'h{int(((h * 360 + 15) % 360) // 30):02}'
    maximum = values.max(axis=1)
    minimum = values.min(axis=1)
    saturation_values = np.divide(maximum - minimum, maximum, out=np.zeros(len(values), dtype=float), where=maximum != 0)
    return palette[0]['hex'], h * 360, bucket, {
        'version': 1, 'palette': palette, 'other_percentage': units[-1] / 100,
        'average_brightness': round(float((values @ np.array([.2126, .7152, .0722])).mean() / 255 * 100), 1),
        'average_saturation': round(float(saturation_values.mean() * 100), 1),
    }


class Analyzer:
    """共享一个只读 ONNX Session；每次调用使用独立像素和输出，不在请求之间复用可变数组。"""
    def __init__(self, model, max_pixels):
        options = ort.SessionOptions()
        options.intra_op_num_threads = 1
        options.inter_op_num_threads = 1
        options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
        options.add_session_config_entry('session.intra_op.allow_spinning', '0')
        self.session = ort.InferenceSession(model, sess_options=options, providers=['CPUExecutionProvider'])
        self.size = int(self.session.get_inputs()[0].shape[2])
        self.input_name = self.session.get_inputs()[0].name
        self.max_pixels = max_pixels
        self.budget = PixelBudget()
        grid, stride = [], []
        for step in (8, 16, 32):
            y, x = np.meshgrid(np.arange(self.size // step), np.arange(self.size // step), indexing='ij')
            current = np.stack((x, y), axis=2).reshape(-1, 2)
            grid.append(current)
            stride.append(np.full((len(current), 1), step))
        self.grid = np.concatenate(grid)
        self.stride = np.concatenate(stride)

    def analyze(self, content):
        """先验证文件及像素上限，再解码有界预览；不写图片或文件名到日志。"""
        with warnings.catch_warnings():
            warnings.simplefilter('error', Image.DecompressionBombWarning)
            with Image.open(BytesIO(content)) as image:
                width, height = image.size
                if width < 1 or height < 1 or width * height > self.max_pixels:
                    raise ValueError('image_pixel_limit')
                if image.format not in ('JPEG', 'PNG', 'WEBP', 'GIF', 'BMP', 'TIFF'):
                    raise ValueError('unsupported_image_format')
                with self.budget.acquire(width * height * 8):
                    image.draft('RGB', (1600, 1600))
                    image.thumbnail((1600, 1600), Image.Resampling.LANCZOS)
                    preview = ImageOps.exif_transpose(image)
                    if preview.mode in ('RGBA', 'LA') or 'transparency' in preview.info:
                        layer = preview.convert('RGBA')
                        white = Image.new('RGBA', layer.size, (255, 255, 255, 255))
                        preview = Image.alpha_composite(white, layer)
                    rgb = np.asarray(preview.convert('RGB')).copy()
        height, width = rgb.shape[:2]
        ratio = min(self.size / height, self.size / width)
        resized = cv2.resize(rgb[:, :, ::-1], (int(width * ratio), int(height * ratio)))
        padded = np.full((self.size, self.size, 3), 114, dtype=np.uint8)
        padded[:resized.shape[0], :resized.shape[1]] = resized
        tensor = np.ascontiguousarray(padded.transpose(2, 0, 1), dtype=np.float32)[None]
        output = self.session.run(None, {self.input_name: tensor})[0][0]
        if len(output) != len(self.grid):
            raise ValueError('model_output_shape')
        predictions = output.copy()
        predictions[:, :2] = (predictions[:, :2] + self.grid) * self.stride
        predictions[:, 2:4] = np.exp(predictions[:, 2:4]) * self.stride
        scores = predictions[:, 4] * predictions[:, 5]
        keep = scores >= .3
        boxes = predictions[keep, :4]
        scores = scores[keep]
        boxes[:, :2] -= boxes[:, 2:4] / 2
        count = len(cv2.dnn.NMSBoxes(boxes.tolist(), scores.tolist(), .3, .45)) if len(boxes) else 0
        dominant, hue, bucket, palette = color_profile(rgb)
        return {'people_count': count, 'is_empty_shot': count == 0, 'dominant_color': dominant,
                'dominant_hue': hue, 'color_bucket': bucket, 'color_palette': palette}


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    """仅同 UID 的 Go 进程能访问；有界执行器保证模型调用最多十个并发。"""
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *_):
        """禁止默认访问日志泄露请求内容及异常正文。"""
        pass

    def reply(self, status, payload):
        body = json.dumps(payload, separators=(',', ':')).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.reply(200 if self.path == '/health' else 404, {'ready': self.path == '/health'})

    def do_POST(self):
        try:
            length = int(self.headers.get('Content-Length', '-1'))
            if self.path != '/infer' or length < 1 or length > self.server.max_bytes:
                self.close_connection = True
                self.reply(400, {'error': 'invalid_image_request'})
                return
            self.connection.settimeout(60)
            content = self.rfile.read(length)
            if len(content) != length:
                raise ValueError('incomplete_image')
            result = self.server.pool.submit(self.server.analyzer.analyze, content).result()
            self.reply(200, result)
        except (ValueError, OSError, Image.DecompressionBombError, Image.DecompressionBombWarning):
            self.close_connection = True
            self.reply(422, {'error': 'invalid_image'})
        except Exception:
            self.close_connection = True
            self.reply(503, {'error': 'inference_unavailable'})


def main():
    """启动参数只来自 Go 的私有系统配置，不接受网络端指定模型或任意输出路径。"""
    parser = argparse.ArgumentParser()
    parser.add_argument('--socket', required=True)
    parser.add_argument('--model', required=True)
    parser.add_argument('--workers', type=int, required=True)
    parser.add_argument('--max-pixels', type=int, required=True)
    parser.add_argument('--max-bytes', type=int, required=True)
    args = parser.parse_args()
    if not 1 <= args.workers <= 10:
        parser.error('workers must be 1..10')
    cv2.setNumThreads(1)
    Image.MAX_IMAGE_PIXELS = args.max_pixels
    analyzer = Analyzer(args.model, args.max_pixels)
    path = Path(args.socket)
    if path.exists():
        if not path.is_socket():
            raise RuntimeError('socket_path_not_socket')
        path.unlink()
    with Server(str(path), Handler) as server, ThreadPoolExecutor(max_workers=args.workers) as pool:
        os.chmod(path, 0o600)
        server.pool = pool
        server.analyzer = analyzer
        server.max_bytes = args.max_bytes
        server.serve_forever(poll_interval=.2)


if __name__ == '__main__':
    main()
