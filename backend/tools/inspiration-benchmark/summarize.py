"""将逐图原始报告压缩为可提交的性能证据，不提交模型、样例图片或构建产物。"""
import hashlib
import json
import math
from pathlib import Path


def main():
    """仅汇总完整八场景；缺失场景或非十任务的报告直接报错，避免部分结果冒充完整验收。"""
    root = Path('/data')
    summary = {'date': '2026-10-26', 'concurrency': 10, 'batch_deadline_seconds': 10,
               'timing_scope': 'local file read + decode + resize + inference + person filtering/NMS; excludes API, compression, DB',
               'manifest': json.loads((root / 'manifest.json').read_text()), 'scenarios': []}
    for model in ('nano', 'tiny'):
        for sessions in ('shared', 'per-worker'):
            for image_set in ('standard', 'large'):
                path = root / f'{model}-{sessions}-{image_set}.json'
                raw = path.read_bytes()
                report = json.loads(raw)
                rounds = report['rounds']
                if len(rounds) < 2 or any(len(row['items']) != 10 for row in rounds):
                    raise ValueError(f'Incomplete result: {path.name}')
                times = sorted(row['batch_seconds'] for row in rounds)
                rows = [row for batch in rounds for row in batch['items']]
                result = {key: report[key] for key in ('model', 'session_mode', 'model_bytes',
                    'model_sha256', 'runtime', 'opencv', 'numpy', 'platform', 'cpu_model',
                    'cpu_limit', 'cpu_period', 'cpuset', 'memory_limit', 'memory_swap_limit',
                    'input_size', 'session_load_seconds', 'resources', 'process_peak_rss_kib')}
                result.update({'image_set': image_set, 'raw_report_sha256': hashlib.sha256(raw).hexdigest(),
                    'batch_count': len(rounds), 'completed_images': len(rows),
                    'batch_min_seconds': times[0], 'batch_max_seconds': times[-1],
                    'batch_p95_nearest_rank_seconds': times[math.ceil(len(times) * .95) - 1],
                    'single_image_max_seconds': max(row['total_seconds'] for row in rows),
                    'initialization_plus_first_batch_seconds': report['session_load_seconds'] + rounds[0]['batch_seconds'],
                    'all_batches_under_10_seconds': all(value <= 10 for value in times),
                    'all_batches_task_overlap_10': all(row['max_task_overlap'] == 10 for row in rounds),
                    'max_inference_overlap': max(row['max_inference_overlap'] for row in rounds),
                    'batch_results': [{key: value for key, value in batch.items() if key != 'items'} for batch in rounds],
                    'first_batch_detections': [{key: row[key] for key in ('file', 'dimensions', 'people')}
                                               for row in rounds[0]['items']],
                    'counts_stable_across_rounds': all(
                        [row['people'] for row in batch['items']] == [row['people'] for row in rounds[0]['items']]
                        for batch in rounds)})
                summary['scenarios'].append(result)
                print(f"{model:4} {sessions:10} {image_set:8} "
                      f"batch={times[0]:.3f}–{times[-1]:.3f}s "
                      f"cgroup_peak={int(report['resources']['memory_peak']) / 1048576:.1f}MiB "
                      f"RSS_peak={report['process_peak_rss_kib'] / 1024:.1f}MiB", flush=True)
    (root / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding='utf-8')


if __name__ == '__main__':
    main()
