# 独立性能验证镜像，不修改业务 API 镜像或加载用户私有图片。
FROM python:3.11-slim-bookworm@sha256:f06d01d4ba3f8ad5af45a3ec87df19c1d317d8e8961edc0fabda61811af6f7cf
ENV PYTHONUNBUFFERED=1 OMP_NUM_THREADS=1 OPENBLAS_NUM_THREADS=1
WORKDIR /benchmark
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY prepare.py benchmark.py ./
ENTRYPOINT ["python", "benchmark.py"]
