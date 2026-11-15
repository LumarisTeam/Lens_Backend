# syntax=docker/dockerfile:1

# ============ 构建阶段 ============
FROM golang:1.25-alpine AS builder

# GOPROXY 默认走 goproxy.cn（国内构建）；海外 CI（如 GitHub Actions）可用 build-arg 覆盖为官方代理
ARG GOPROXY=https://goproxy.cn,direct
# 关闭 CGO，产出纯静态二进制
ENV GOPROXY=${GOPROXY} \
    CGO_ENABLED=0

WORKDIR /src

# 先复制依赖清单，充分利用 Docker 层缓存。
# cache mount 让 go mod download 的产物在重复构建间复用，配合 CI 的 gha 缓存
# 可以省掉整轮依赖下载。
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 复制源码并编译（-trimpath 去除本地路径，-s -w 裁剪符号与调试信息）。
# 编译缓存同样挂 cache mount，避免每次构建都从零编译全部依赖。
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ============ 运行阶段 ============
FROM alpine:3.20

# ca-certificates：访问 COS 需要 HTTPS；tzdata：支持 TZ 时区（expires_at 本地时间）
RUN apk add --no-cache ca-certificates tzdata

# 以非 root 用户运行
RUN addgroup -S app && adduser -S app -G app
USER app

WORKDIR /app
COPY --from=builder /out/server /app/server

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/server"]
