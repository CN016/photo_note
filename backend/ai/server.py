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
    def __init__(self, model, tag_model, tag_embeddings, max_pixels, tag_workers=2, tag_top_k=18, tag_min_score=.045):
        options = ort.SessionOptions()
        options.intra_op_num_threads = 1
        options.inter_op_num_threads = 1
        options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
        options.add_session_config_entry('session.intra_op.allow_spinning', '0')
        self.session = ort.InferenceSession(model, sess_options=options, providers=['CPUExecutionProvider'])
        self.tag_session = ort.InferenceSession(tag_model, sess_options=options, providers=['CPUExecutionProvider'])
        self.size = int(self.session.get_inputs()[0].shape[2])
        self.input_name = self.session.get_inputs()[0].name
        self.tag_input_name = self.tag_session.get_inputs()[0].name
        self.max_pixels = max_pixels
        self.tag_top_k = tag_top_k
        self.tag_min_score = tag_min_score
        self.heavy_slots = threading.BoundedSemaphore(tag_workers)
        self.budget = PixelBudget()
        # 十个请求同时推理时，限制进程内图片正文总量，避免大图上传把内存推到 2GB 上限。
        self.body_budget = PixelBudget(capacity=256 * 1024 * 1024)
        with np.load(tag_embeddings, allow_pickle=False) as data:
            self.tag_names = data['names'].tolist()
            self.tag_kinds = data['kinds'].tolist()
            self.tag_embeddings = data['embeddings'].astype(np.float32)
            self.tag_version = str(data['version'])
        if self.tag_embeddings.ndim != 2 or len(self.tag_names) != len(self.tag_embeddings) or len(self.tag_names) != len(self.tag_kinds):
            raise ValueError('tag_embeddings_shape')
        self.tag_embeddings /= np.maximum(np.linalg.norm(self.tag_embeddings, axis=1, keepdims=True), 1e-12)
        grid, stride = [], []
        for step in (8, 16, 32):
            y, x = np.meshgrid(np.arange(self.size // step), np.arange(self.size // step), indexing='ij')
            current = np.stack((x, y), axis=2).reshape(-1, 2)
            grid.append(current)
            stride.append(np.full((len(current), 1), step))
        self.grid = np.concatenate(grid)
        self.stride = np.concatenate(stride)

    def analyze(self, content):
        """十并发请求仍可等待，但只允许两个请求同时解码和跑两个模型，保护 2GB 内存与 CPU。"""
        with self.heavy_slots:
            return self._analyze(content)

    def _analyze(self, content):
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
                    image.draft('RGB', (1280, 1280))
                    image.thumbnail((1280, 1280), Image.Resampling.LANCZOS)
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
                'dominant_hue': hue, 'color_bucket': bucket, 'color_palette': palette,
                'auto_tags': self.tag_image(rgb, width, height, count, bucket, palette)}

    def tag_image(self, rgb, width, height, people_count, color_bucket, palette):
        """用 SigLIP 图像向量选取多类别高相关标签，再合并人数、方向和色彩等确定性标签。"""
        sample = Image.fromarray(rgb).resize((224, 224), Image.Resampling.BILINEAR)
        tensor = (np.asarray(sample, dtype=np.float32) / 127.5 - 1).transpose(2, 0, 1)[None]
        vector = self.tag_session.run(None, {self.tag_input_name: tensor})[1][0]
        vector /= max(float(np.linalg.norm(vector)), 1e-12)
        scores = self.tag_embeddings @ vector.astype(np.float32)
        ranked = np.argsort(-scores)
        best = float(scores[ranked[0]])
        floor = max(self.tag_min_score, best * .56)
        limits = {'composition': 5, 'subject': 6, 'scene': 6, 'lighting': 3, 'color': 3, 'technique': 2, 'style': 2, 'mood': 2}
        used = {key: 0 for key in limits}
        selected = []
        for index in ranked:
            score = float(scores[index])
            kind = self.tag_kinds[index]
            if score < floor or used.get(kind, 0) >= limits.get(kind, 0):
                continue
            used[kind] += 1
            selected.append({'name': self.tag_names[index], 'score': round(score, 4), 'source': 'model'})
            if len(selected) >= self.tag_top_k:
                break
        deterministic = self.deterministic_tags(width, height, people_count, color_bucket, palette)
        names = set()
        result = []
        for item in deterministic + selected:
            if item['name'] in names:
                continue
            names.add(item['name'])
            result.append(item)
        return result

    @staticmethod
    def deterministic_tags(width, height, people_count, color_bucket, palette):
        """不依赖概率阈值的基础标签，保证每张成功图片至少有可检索结果。"""
        tags = []
        if people_count == 0:
            tags.extend(('空镜', '无人'))
        elif people_count == 1:
            tags.extend(('人像', '单人'))
        elif people_count == 2:
            tags.extend(('人像', '双人'))
        else:
            tags.extend(('人像', '多人'))
        ratio = width / max(height, 1)
        tags.append('横构图' if ratio >= 1.15 else '竖构图' if ratio <= .87 else '方形构图')
        color_names = {
            'black': '黑色', 'white': '白色', 'gray': '灰色', 'h00': '红色', 'h01': '橙红色', 'h02': '橙色',
            'h03': '黄色', 'h04': '黄绿色', 'h05': '绿色', 'h06': '青绿色', 'h07': '青色', 'h08': '蓝色',
            'h09': '蓝紫色', 'h10': '紫色', 'h11': '粉紫色',
        }
        if color_bucket in color_names:
            tags.append(color_names[color_bucket])
        brightness = palette.get('average_brightness')
        if isinstance(brightness, (int, float)):
            tags.append('暗调' if brightness < 30 else '明亮' if brightness > 70 else '中间调')
        saturation = palette.get('average_saturation')
        if isinstance(saturation, (int, float)):
            tags.append('高饱和' if saturation > 55 else '低饱和' if saturation < 18 else '自然饱和')
        return [{'name': name, 'score': 1.0, 'source': 'metadata'} for name in dict.fromkeys(tags)]


UnixStreamBase = getattr(socketserver, 'UnixStreamServer', socketserver.TCPServer)


class Server(socketserver.ThreadingMixIn, UnixStreamBase):
    """仅同 UID 的 Go 进程能访问；有界执行器保证模型调用最多十个并发。"""
    daemon_threads = True
    # 容纳十个并发连接和健康探测，避免Unix Socket默认小backlog在突发时拒绝连接。
    request_queue_size = 32


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
            with self.server.analyzer.body_budget.acquire(length):
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
    parser.add_argument('--tag-model', required=True)
    parser.add_argument('--tag-embeddings', required=True)
    parser.add_argument('--workers', type=int, required=True)
    parser.add_argument('--tag-workers', type=int, default=2)
    parser.add_argument('--tag-top-k', type=int, default=18)
    parser.add_argument('--tag-min-score', type=float, default=.045)
    parser.add_argument('--max-pixels', type=int, required=True)
    parser.add_argument('--max-bytes', type=int, required=True)
    args = parser.parse_args()
    if not 1 <= args.workers <= 10 or not 1 <= args.tag_workers <= 10 or not 4 <= args.tag_top_k <= 30 or not 0 <= args.tag_min_score <= 1:
        parser.error('workers must be 1..10, tag-workers 1..10, tag-top-k 4..30, tag-min-score 0..1')
    cv2.setNumThreads(1)
    Image.MAX_IMAGE_PIXELS = args.max_pixels
    analyzer = Analyzer(args.model, args.tag_model, args.tag_embeddings, args.max_pixels,
                        args.tag_workers, args.tag_top_k, args.tag_min_score)
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
