"""从 COCO 官方标注核对固定样例；按 ZIP 范围读取，不下载整个训练集或再次下载图片。"""
import argparse
import hashlib
import io
import json
from pathlib import Path
import urllib.request
import zipfile

SOURCE = 'https://s3.amazonaws.com/images.cocodataset.org/annotations/annotations_trainval2017.zip'
SAMPLE_IDS = [139, 285, 632, 724, 776, 785, 802, 872, 885, 1000]

class RemoteZip(io.RawIOBase):
    """对官方归档提供只读随机访问；拒绝忽略Range的响应，避免误下载整包。"""
    def __init__(self):
        self.position = 0
        with urllib.request.urlopen(urllib.request.Request(SOURCE, method='HEAD'), timeout=30) as response:
            self.size = int(response.headers['Content-Length'])
    def readable(self): return True
    def seekable(self): return True
    def tell(self): return self.position
    def seek(self, offset, whence=0):
        self.position = offset if whence == 0 else self.position + offset if whence == 1 else self.size + offset
        return self.position
    def read(self, size=-1):
        if self.position >= self.size or size == 0: return b''
        if size < 0: size = self.size - self.position
        end = min(self.size, self.position + size) - 1
        request = urllib.request.Request(SOURCE, headers={'Range': f'bytes={self.position}-{end}'})
        with urllib.request.urlopen(request, timeout=60) as response:
            if response.status != 206: raise RuntimeError('服务器不支持范围读取，拒绝整包下载')
            content = response.read()
        self.position += len(content)
        return content

def main():
    """预测人数按prepare.py固定十图的顺序提供，只报告小样本结果，不推导全量准确率。"""
    root = Path(__file__).resolve().parents[3]
    parser = argparse.ArgumentParser()
    parser.add_argument('--predictions', type=Path, default=root/'backend/dist/library-api-validation.json')
    parser.add_argument('--output', type=Path, default=root/'backend/dist/library-accuracy-validation.json')
    args = parser.parse_args()
    predicted = json.loads(args.predictions.read_text(encoding='utf-8-sig'))['people_counts']
    if len(predicted) != len(SAMPLE_IDS) or any(type(value) is not int or value < 0 for value in predicted):
        raise ValueError('需要固定十张样例的有效人物预测数')
    annotation_file = 'annotations/instances_val2017.json'
    with zipfile.ZipFile(RemoteZip()) as archive: annotation_bytes = archive.read(annotation_file)
    data = json.loads(annotation_bytes)
    assert any(category['id'] == 1 and category['name'] == 'person' for category in data['categories'])
    counts, crowds = dict.fromkeys(SAMPLE_IDS, 0), dict.fromkeys(SAMPLE_IDS, 0)
    for annotation in data['annotations']:
        image_id = annotation['image_id']
        if image_id in counts and annotation['category_id'] == 1:
            target = crowds if annotation.get('iscrowd') else counts
            target[image_id] += 1
    samples = [{'image_id': image_id, 'person_annotations_excluding_crowd': counts[image_id], 'crowd_annotations': crowds[image_id],
                'predicted_people': count, 'exact_count': counts[image_id] == count,
                'presence_matches': (counts[image_id] > 0) == (count > 0),
                'group_matches': min(counts[image_id], 3) == min(count, 3)} for image_id, count in zip(SAMPLE_IDS, predicted)]
    result = {'source': SOURCE, 'annotation_file': annotation_file, 'annotation_sha256': hashlib.sha256(annotation_bytes).hexdigest(),
              'sample_size': len(samples), 'exact_count_matches': sum(row['exact_count'] for row in samples),
              'presence_matches': sum(row['presence_matches'] for row in samples), 'group_matches': sum(row['group_matches'] for row in samples),
              'samples': samples, 'limitation': '仅十张固定样例，不是全量精度；crowd区域不代表可精确计数人数。'}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
    print(json.dumps({key: result[key] for key in ['sample_size','exact_count_matches','presence_matches','group_matches']}))

if __name__ == '__main__': main()
