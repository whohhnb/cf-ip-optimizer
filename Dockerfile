# syntax=docker/dockerfile:1

# 两个构建阶段都钉在「构建机原生架构」上，不跟随目标平台。
#
# 不加 --platform=$BUILDPLATFORM 的话，buildx 多架构构建会把这两个阶段按目标
# 架构各跑一遍：arm64 那一遍是在 amd64 runner 上用 QEMU 模拟执行 npm ci 和
# go build，整条推送镜像的流水线从 1 分钟涨到 10 分钟。
#
# 现在前端只构建一次（产物是纯 JS/CSS，与架构无关），Go 用交叉编译出目标架构
# 的二进制——CGO 关掉之后交叉编译是原生的，不经过任何模拟。

# ---- 1. 构建前端 ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /app
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
# vite 的 outDir 指向 ../backend/web/dist，先准备好目录
RUN mkdir -p /app/backend/web/dist && cd web && npm run build

# ---- 2. 构建后端（前端产物内嵌进二进制） ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS api
WORKDIR /app/backend
# buildx 会自动注入这两个参数（linux/arm64 时 TARGETARCH=arm64）
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /app/backend/web/dist ./web/dist
# 版本号由 CI 传入（release workflow 的 build-arg），本地构建默认 dev
ARG VERSION=dev
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X edgepilot/internal/httpapi.Version=${VERSION}" \
    -o /out/edgepilot .

# ---- 3. 运行镜像 ----
# 这一层才是真正的 per-arch：拉对应架构的 alpine、拷一个静态二进制，几秒钟
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 edgepilot
WORKDIR /app
COPY --from=api /out/edgepilot /app/edgepilot
RUN mkdir -p /data && chown -R edgepilot:edgepilot /data
USER edgepilot
ENV PILOT_ADDR=":8080" \
    PILOT_DATA_DIR="/data" \
    TZ="Asia/Shanghai"
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/edgepilot"]
