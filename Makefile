# EdgePilot 构建入口
# 版本号取当前 git tag；没有 tag 时退化为短 SHA，可 make VERSION=1.2.3 覆盖
BINARY  := edgepilot
VERSION ?= $(shell v=$$(git describe --tags --always --dirty 2>/dev/null); echo $${v:-dev} | sed 's/^v//')
LDFLAGS := -s -w -X edgepilot/internal/httpapi.Version=$(VERSION)

.PHONY: all build web test fmt docker clean

all: build

## web: 构建前端到 backend/web/dist（后端 go:embed 依赖该目录存在）
web:
	cd web && npm ci && npm run build

## build: 前端 + 后端，产出单二进制 dist/edgepilot
build: web
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ../dist/$(BINARY) .

## test: 静态检查 + 单元测试
test:
	cd backend && go vet ./... && go test ./...

## fmt: 格式化 Go 代码
fmt:
	cd backend && gofmt -w .

## docker: 本地构建镜像（版本号作为 build-arg 注入）
docker:
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY):$(VERSION) .

## clean: 清理构建产物（不删数据目录）
clean:
	rm -rf dist pkg artifacts backend/web/dist
