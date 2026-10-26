# 可通过 Docker Compose 构建参数切换到就近的 Go 基础镜像仓库。
ARG GO_BASE_IMAGE=golang:1.24-alpine
# 运行镜像较小，构建参数允许部署环境使用相同的镜像代理。
ARG RUNTIME_BASE_IMAGE=alpine:3.22
# 复用已编译的原生 musl 推理运行时；保留旧 Compose 参数兼容，但不再次编译 ORT。
ARG AI_RUNTIME_BASE_IMAGE=photo-nini-ai-runtime:alpine3.22-ort1.23.2-cpu-v1

FROM ${GO_BASE_IMAGE} AS build

WORKDIR /src
# Go 模块代理只影响构建阶段；默认使用 Go 官方代理，也可由 Compose 构建参数覆盖。
ARG GOPROXY=https://goproxy.cn|https://proxy.golang.org|direct
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/photo-nini-api ./cmd/api

FROM ${AI_RUNTIME_BASE_IMAGE}
USER root
RUN apk add --no-cache py3-pillow
COPY ai/server.py /opt/photo-nini-ai/server.py
COPY ai/licenses/ /opt/photo-nini-ai/licenses/
COPY ai/THIRD_PARTY_NOTICES.md /opt/photo-nini-ai/THIRD_PARTY_NOTICES.md
COPY --from=build /out/photo-nini-api /usr/local/bin/photo-nini-api
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/photo-nini-api"]
