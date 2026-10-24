.PHONY: build ui-dist run test vet clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# 构建 UI 并复制进 server 的嵌入目录
ui-dist:
	cd ../openrunner-ui && npm ci --silent && npm run build
	rm -rf server/web/dist/assets && cp -r ../openrunner-ui/dist/. server/web/dist/

# 单二进制构建（默认 SQLite，纯 Go 驱动无 cgo）
build:
	cd server && CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o ../dist/openrunner ./cmd/openrunner-server

run: build
	./dist/openrunner --addr :8080 --data ./data

test:
	cd ../openrunner-core && go test ./... && go vet ./...
	cd server && go test ./... && go vet ./...

vet:
	cd ../openrunner-core && go vet ./...
	cd server && go vet ./...

clean:
	rm -rf dist data
