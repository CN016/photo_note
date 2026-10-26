"""验证真实图片的十任务并发 CPU 推理；输出阶段耗时、并发重叠和容器资源证据。"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import platform
import resource
import threading
import time

import cv2
import numpy as np
import onnxruntime as ort


def read_control(name):
    """读取 cgroup v2 限额和峰值；旧版 Docker 使用 v1 文件作为回退。"""
    paths = {
        'cpu': ['/sys/fs/cgroup/cpu.max', '/sys/fs/cgroup/cpu/cpu.cfs_quota_us'],
        'cpu_period': ['/sys/fs/cgroup/cpu/cpu.cfs_period_us'],
        'cpuset': ['/sys/fs/cgroup/cpuset.cpus.effective', '/sys/fs/cgroup/cpuset/cpuset.cpus'],
        'memory_limit': ['/sys/fs/cgroup/memory.max', '/sys/fs/cgroup/memory/memory.limit_in_bytes'],
        'memory_swap_limit': ['/sys/fs/cgroup/memory.swap.max', '/sys/fs/cgroup/memory/memory.memsw.limit_in_bytes'],
        'memory_peak': ['/sys/fs/cgroup/memory.peak', '/sys/fs/cgroup/memory/memory.max_usage_in_bytes'],
        'memory_current': ['/sys/fs/cgroup/memory.current', '/sys/fs/cgroup/memory/memory.usage_in_bytes'],
        'memory_events': ['/sys/fs/cgroup/memory.events', '/sys/fs/cgroup/memory/memory.failcnt'],
        'cpu_stats': ['/sys/fs/cgroup/cpu.stat', '/sys/fs/cgroup/cpu/cpu.stat'],
    }
    for path in paths[name]:
        if Path(path).exists():
            return Path(path).read_text().strip()
    return None


def session_for(model):
    """每次推理使用一个计算线程，禁止运行时为十个任务各自扩展为多核线程池。"""
    options = ort.SessionOptions()
    options.intra_op_num_threads = 1
    options.inter_op_num_threads = 1
    options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    options.add_session_config_entry('session.intra_op.allow_spinning', '0')
    return ort.InferenceSession(str(model), sess_options=options, providers=['CPUExecutionProvider'])


def prepare_tensor(path, size):
    """按 YOLOX 新版官方预处理使用 BGR、114 右下填充和原始 0–255 浮点值。"""
    image = cv2.imread(str(path), cv2.IMREAD_COLOR)
    if image is None:
        raise ValueError(f'Invalid image: {path.name}')
    height, width = image.shape[:2]
    ratio = min(size / height, size / width)
    resized = cv2.resize(image, (int(width * ratio), int(height * ratio)))
    padded = np.full((size, size, 3), 114, dtype=np.uint8)
    padded[:resized.shape[0], :resized.shape[1]] = resized
    return np.ascontiguousarray(padded.transpose(2, 0, 1), dtype=np.float32)[None], [width, height]


def person_detections(output, size):
    """解码官方 ONNX 原始输出，只对 COCO person 类做置信度过滤与 NMS。"""
    predictions = output[0].copy()
    grids, strides = [], []
    for stride in (8, 16, 32):
        y, x = np.meshgrid(np.arange(size // stride), np.arange(size // stride), indexing='ij')
        grid = np.stack((x, y), axis=2).reshape(-1, 2)
        grids.append(grid)
        strides.append(np.full((len(grid), 1), stride))
    grid, stride = np.concatenate(grids), np.concatenate(strides)
    if len(predictions) != len(grid):
        raise ValueError('Unexpected model output; do not silently count invalid predictions')
    predictions[:, :2] = (predictions[:, :2] + grid) * stride
    predictions[:, 2:4] = np.exp(predictions[:, 2:4]) * stride
    scores = predictions[:, 4] * predictions[:, 5]
    keep = scores >= 0.3
    boxes = predictions[keep, :4]
    scores = scores[keep]
    if not len(boxes):
        return 0
    boxes[:, :2] -= boxes[:, 2:4] / 2
    indices = cv2.dnn.NMSBoxes(boxes.tolist(), scores.tolist(), 0.3, 0.45)
    return len(indices)


def overlap_peak(rows, start_key, finish_key):
    """用实际起止时间计算重叠数量，证明十个任务均进入推理调用而非串行排队。"""
    events = [(row[start_key], 1) for row in rows] + [(row[finish_key], -1) for row in rows]
    active = peak = 0
    for _, delta in sorted(events):
        active += delta
        peak = max(peak, active)
    return peak


def main():
    """同步放行十个线程，分别记录首次并发和重复热运行；每次保留逐图原始结果。"""
    parser = argparse.ArgumentParser()
    parser.add_argument('--model', choices=['nano', 'tiny'], default='nano')
    parser.add_argument('--sessions', choices=['shared', 'per-worker'], default='shared')
    parser.add_argument('--rounds', type=int, default=10)
    parser.add_argument('--images', default='/data')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    if args.rounds < 1:
        parser.error('--rounds 必须至少为 1，保留首次并发与重复运行两类结果')
    cv2.setNumThreads(1)
    images = sorted(Path(args.images).glob('*.jpg'))
    if len(images) != 10:
        raise ValueError('Exactly 10 distinct input files are required')
    model = Path('/data') / f'yolox_{args.model}.onnx'
    process_start = time.perf_counter()
    sessions = [session_for(model) for _ in range(10 if args.sessions == 'per-worker' else 1)]
    load_seconds = time.perf_counter() - process_start
    size = int(sessions[0].get_inputs()[0].shape[2])
    report = {'model': args.model, 'session_mode': args.sessions, 'model_bytes': model.stat().st_size,
              'model_sha256': hashlib.sha256(model.read_bytes()).hexdigest(),
              'runtime': ort.__version__, 'opencv': cv2.__version__, 'numpy': np.__version__,
              'platform': platform.platform(), 'cpu_model': next((line.split(':', 1)[1].strip()
                  for line in Path('/proc/cpuinfo').read_text().splitlines() if line.startswith('model name')), ''),
              'cpu_limit': read_control('cpu'), 'cpu_period': read_control('cpu_period'),
              'cpuset': read_control('cpuset'), 'memory_swap_limit': read_control('memory_swap_limit'),
              'memory_limit': read_control('memory_limit'), 'input_size': size,
              'session_load_seconds': load_seconds, 'rounds': []}
    for round_index in range(args.rounds + 1):
        gate = threading.Barrier(11)

        def work(index):
            """每个线程独立解码自己的图片并执行真实模型调用；异常向上传播，不记为成功。"""
            gate.wait(timeout=60)
            started = time.perf_counter()
            tensor, dimensions = prepare_tensor(images[index], size)
            prepared = time.perf_counter()
            session = sessions[index] if len(sessions) == 10 else sessions[0]
            output = session.run(None, {session.get_inputs()[0].name: tensor})[0]
            inferred = time.perf_counter()
            count = person_detections(output, size)
            finished = time.perf_counter()
            return {'file': images[index].name, 'dimensions': dimensions, 'people': count,
                    'start': started, 'infer_start': prepared, 'infer_finish': inferred, 'finish': finished,
                    'decode_resize_seconds': prepared - started, 'inference_seconds': inferred - prepared,
                    'postprocess_seconds': finished - inferred, 'total_seconds': finished - started}

        with ThreadPoolExecutor(max_workers=10) as executor:
            futures = [executor.submit(work, index) for index in range(10)]
            batch_start = time.perf_counter()
            gate.wait(timeout=60)
            rows = [future.result(timeout=120) for future in futures]
            batch_seconds = time.perf_counter() - batch_start
        result = {'round': round_index, 'cold': round_index == 0, 'batch_seconds': batch_seconds,
                  'max_task_overlap': overlap_peak(rows, 'start', 'finish'),
                  'max_inference_overlap': overlap_peak(rows, 'infer_start', 'infer_finish'), 'items': rows}
        report['rounds'].append(result)
        print(json.dumps({key: value for key, value in result.items() if key != 'items'}), flush=True)
    report['resources'] = {key: read_control(key) for key in (
        'memory_peak', 'memory_current', 'memory_events', 'cpu_stats')}
    report['process_peak_rss_kib'] = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    report['all_rounds_under_10_seconds'] = all(row['batch_seconds'] <= 10 for row in report['rounds'])
    Path(args.output).write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps({'all_rounds_under_10_seconds': report['all_rounds_under_10_seconds'],
                      'process_peak_rss_kib': report['process_peak_rss_kib'],
                      'resources': report['resources']}), flush=True)


if __name__ == '__main__':
    main()
