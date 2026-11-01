.PHONY: build ui-dist run dev test vet clean check-ui help \
       docker-build docker-up docker-down docker-logs docker-ps docker-clean docker-rm

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
	@echo "  make dev           首次运行：构建前端 + 后端，然后启动（推荐）"
	@echo "  make ui-dist       仅构建前端并复制到 server/web/dist"
	@echo "  make build         仅构建 Go 单二进制 -> dist/openrunner"
	@echo "  make run           启动（要求已 make ui-dist）"
	@echo "  make test          运行 openrunner-core 与 server 的全部测试 + vet"
	@echo "  make clean         删除 dist/ 与 data/"
	@echo ""
	@echo "Docker（默认走国内镜像代理，直连 Docker Hub 传 REGISTRY=docker.io/library）："
	@echo "  make docker-build  构建镜像 $(TAG)（先经代理预拉基础镜像）"
	@echo "  make docker-up     compose 起服务（含 TZ/同步间隔，数据在卷 openrunner-data）"
	@echo "  make docker-ps     查看容器状态与健康检查"
	@echo "  make docker-logs   跟踪日志"
	@echo "  make docker-down   停止（保留数据卷）"
	@echo "  make docker-rm     删除本地镜像（不动数据）"
	@echo "  make docker-clean  停止并删除数据卷 —— 需加 CONFIRM=1"

test:
	cd ../openrunner-core && go test ./... && go vet ./...
	cd server && go test ./... && go vet ./...

vet:
	cd ../openrunner-core && go vet ./...
	cd server && go vet ./...

# ---------------- Docker ----------------
# 国内网络默认走镜像代理；直连 Docker Hub 的环境传 REGISTRY=docker.io/library
REGISTRY ?= docker.1ms.run/library
GOPROXY  ?= https://goproxy.cn,direct
TAG      ?= openrunner:latest
TZ       ?= Asia/Shanghai
COMPOSE  ?= docker/docker-compose.yml

# 构建镜像（脚本会先经代理预拉基础镜像 —— BuildKit 不走 daemon mirror，
# 基础镜像不在本地时 build 会卡死在 "load metadata"）
docker-build:
	REGISTRY=$(REGISTRY) GOPROXY=$(GOPROXY) TAG=$(TAG) ./scripts/docker-build.sh

# 起服务（含 TZ / 同步间隔等默认配置）；数据在命名卷 openrunner-data
docker-up:
	cd docker && REGISTRY=$(REGISTRY) GOPROXY=$(GOPROXY) TZ=$(TZ) \
		docker compose up -d --build

docker-down:
	cd docker && docker compose down

docker-ps:
	@docker compose -f $(COMPOSE) ps

docker-logs:
	cd docker && docker compose logs -f

# 删除镜像（不动数据卷）
docker-rm:
	@docker image inspect $(TAG) >/dev/null 2>&1 && docker rmi $(TAG) || echo "无镜像 $(TAG)"

# 停服并删数据卷 —— 会清掉全部活动数据，必须显式确认
docker-clean:
	@if [ "$(CONFIRM)" != "1" ]; then \
		echo "⚠️  docker-clean 会删除数据卷 openrunner-data（全部活动数据，不可恢复）"; \
		echo "    确认请执行： make docker-clean CONFIRM=1"; exit 1; \
	fi
	cd docker && docker compose down --volumes --remove-orphans

clean:
	rm -rf dist data
