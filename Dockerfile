# ============================================================
# Cloud Clip Lite — 多阶段 Dockerfile
# 阶段 1: 前端构建 (Node.js)
# 阶段 2: Go 编译
# 阶段 3: 最终运行镜像 (distroless)
# ============================================================

# ---- 阶段 1: 前端构建 ----
FROM node:22-alpine AS frontend-builder

WORKDIR /app/web

# 先复制 package 文件以利用 Docker 缓存
COPY web/package.json web/package-lock.json ./
RUN npm ci

# 复制源码并构建
COPY web/ ./
RUN npm run build

# ---- 阶段 2: Go 编译 ----
FROM golang:1.26-alpine AS go-builder

WORKDIR /app

# 安装 git（go mod 需要）
RUN apk add --no-cache git

# 先复制 go.mod 以利用缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 复制前端构建产物到 ./web/dist，由 Go 进程从磁盘伺服
COPY --from=frontend-builder /app/web/dist ./web/dist

# 目标架构（buildx 自动注入，默认 amd64 兼容非 buildx 构建）
ARG TARGETARCH=amd64

# 静态编译（CGO_ENABLED=0 适配 distroless）
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build \
    -ldflags="-s -w -X main.Version=docker" \
    -o /app/bin/server \
    ./cmd/server/

# 预建数据目录并归属 nonroot（uid 65532），确保最终镜像挂载卷后 nonroot 可写
RUN mkdir -p /app/data && chown 65532:65532 /app/data

# ---- 阶段 3: 最终运行镜像 ----
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="Cloud Clip Lite"
LABEL org.opencontainers.image.description="轻量级云剪切板 Web 应用"
LABEL org.opencontainers.image.source="https://github.com/leaf/cloud-clip-lite"

WORKDIR /app

# 复制二进制
COPY --from=go-builder /app/bin/server /app/server

# 复制前端构建产物到工作目录下的 ./web/dist，由 Go 进程从磁盘伺服
COPY --from=frontend-builder /app/web/dist /app/web/dist

# 数据目录（从 builder 带入属主为 nonroot 的空目录，命名卷初始化后 nonroot 可写）
COPY --from=go-builder --chown=nonroot:nonroot /app/data /app/data
USER nonroot:nonroot
VOLUME ["/app/data"]

EXPOSE 8080

ENTRYPOINT ["/app/server"]
