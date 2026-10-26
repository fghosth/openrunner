.PHONY: build ui-dist run dev test vet clean check-ui help

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# 构建 UI 并复制进 server 的嵌入目录
ui-dist:
	cd ../openrunner-ui && npm ci --silent && npm run build
	rm -rf server/web/dist/assets && cp -r ../openrunner-ui/dist/. server/web/dist/

# 单二进制构建（默认 SQLite，纯 Go 驱动无 cgo）
build:
	cd server && CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o ../dist/openrunner ./cmd/openrunner-server

# 启动（需要事先 make ui-dist 过；缺前端产物时会提示）
run: build check-ui
	./dist/openrunner --addr :8080 --data ./data

# 首次/完整运行：先构建前端再启动
dev: ui-dist build
	./dist/openrunner --addr :8080 --data ./data

# 自检：嵌入的前端产物是否存在
check-ui:
	@if [ ! -s server/web/dist/index.html ]; then \
		echo "⚠️  server/web/dist 为空 —— 先执行 'make ui-dist' 构建前端（否则页面空白）"; \
		exit 1; \
	fi

help:
	@echo "openrunner 常用命令："
	@echo "  make dev       首次运行：构建前端 + 后端，然后启动（推荐）"
	@echo "  make ui-dist   仅构建前端并复制到 server/web/dist"
	@echo "  make build     仅构建 Go 单二进制 -> dist/openrunner"
	@echo "  make run       启动（要求已 make ui-dist）"
	@echo "  make test      运行 openrunner-core 与 server 的全部测试 + vet"
	@echo "  make clean     删除 dist/ 与 data/"

test:
	cd ../openrunner-core && go test ./... && go vet ./...
	cd server && go test ./... && go vet ./...

vet:
	cd ../openrunner-core && go vet ./...
	cd server && go vet ./...

clean:
	rm -rf dist data
