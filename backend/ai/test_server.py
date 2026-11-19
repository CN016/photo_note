"""验证颜色边界、占比及人物输出的真实模型输入；测试不需要数据库或网络。"""
import os
import unittest
from io import BytesIO
from pathlib import Path
import numpy as np
from PIL import Image
from server import color_profile, Analyzer

class AnalysisTests(unittest.TestCase):
    """固定颜色避免算法版本发生静默漂移。"""
    def test_color_boundaries(self):
        for rgb, bucket in [((0,0,0),'black'),((255,255,255),'white'),((128,128,128),'gray'),((255,0,0),'h00'),((0,255,0),'h04'),((0,0,255),'h08')]:
            with self.subTest(bucket=bucket):
                color,hue,actual,palette=color_profile(np.full((32,32,3),rgb,dtype=np.uint8))
                self.assertEqual(actual,bucket)
                self.assertAlmostEqual(sum(p['percentage'] for p in palette['palette'])+palette['other_percentage'],100)
                self.assertTrue(0<=hue<360)
    def test_real_model_and_invalid_input(self):
        models=os.getenv('PHOTO_NINI_TEST_MODELS','/opt/photo-nini-ai/models')
        analyzer=Analyzer(os.path.join(models,'yolox_nano.onnx'),os.path.join(models,'siglip2_vision_fp16.onnx'),os.path.join(models,'siglip2_tag_embeddings.npz'),50000000,10,18,.045)
        content=BytesIO();Image.new('RGB',(320,240),'white').save(content,format='PNG')
        result=analyzer.analyze(content.getvalue())
        self.assertEqual(result['people_count'],0);self.assertTrue(result['is_empty_shot'])
        self.assertEqual(result['color_bucket'],'white')
        names={item['name'] for item in result['auto_tags']}
        self.assertTrue({'空镜','无人','横构图','白色','明亮','低饱和'} <= names)
        self.assertTrue({'大远景','远景','全景','中景','中近景','近景','特写','大特写','半身','全身','胸像'} <= set(analyzer.tag_names))
        with self.assertRaises((ValueError,OSError)):analyzer.analyze(b'invalid-image')

if __name__=='__main__':unittest.main()
