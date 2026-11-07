# syntax=docker/dockerfile:1

# ---- 1. 构建前端 ----
FROM node:22-alpine AS web
WORKDIR /app
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
# vite 的 outDir 指向 ../backend/web/dist，先准备好目录
RUN mkdir -p /app/backend/web/dist && cd web && npm run build

# ---- 2. 构建后端（前端产物内嵌进二进制） ----
FROM golang:1.27-alpine AS api
WORKDIR /app/backend
ENV CGO_ENABLED=0 GOOS=linux
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /app/backend/web/dist ./web/dist
RUN go build -trimpath -ldflags "-s -w" -o /out/edgepilot .

# ---- 3. 运行镜像 ----
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
