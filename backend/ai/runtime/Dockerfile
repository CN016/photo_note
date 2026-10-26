FROM photo-nini-ai-build:alpine3.22-ort1.23.2-cpu-v1 AS ai-build
FROM photo-nini-api:4.1.0
USER root
RUN apk add --no-cache python3 py3-pip py3-numpy py3-opencv py3-coloredlogs py3-flatbuffers py3-packaging py3-protobuf py3-sympy libstdc++ libgcc
COPY --from=ai-build /src/build/Linux/Release/dist/*.whl /tmp/
RUN pip install --no-deps --break-system-packages /tmp/*.whl && rm /tmp/*.whl && ln -s /usr/lib/python3.12/site-packages/onnxruntime/capi/libonnxruntime_providers_shared.so /usr/lib/libonnxruntime_providers_shared.so && python3 -c "import onnxruntime as o,cv2,numpy; print(o.__version__, cv2.__version__, numpy.__version__,o.get_available_providers())"
COPY yolox_nano.onnx /opt/photo-nini-ai/models/yolox_nano.onnx
ENV OMP_NUM_THREADS=1 OPENBLAS_NUM_THREADS=1
LABEL photo-nini.ai.runtime="onnxruntime-1.23.2-musl-cpu" photo-nini.ai.model="yolox-nano-0.1.1rc0" photo-nini.ai.scope="runtime-base-not-feature-release"
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/photo-nini-api"]