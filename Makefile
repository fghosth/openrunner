.PHONY: build ui-dist run test vet clean

# 构建 UI 并复制进 server 的嵌入目录
ui-dist:
	cd ../openrunner-ui && npm ci --silent && npm run build
	rm -rf server/web/dist && cp -r ../openrunner-ui/dist server/web/dist

# 单二进制构建（默认 SQLite，纯 Go 驱动无 cgo）
build:
	cd server && CGO_ENABLED=0 go build -o ../dist/openrunner ./cmd/openrunner-server

run: build
	./dist/openrunner --addr :8080 --data ./data

test:
	cd ../openrunner-core && go test ./... && go vet ./...
	cd server && go test ./... && go vet ./...

vet:
	cd ../openrunner-core && go vet ./...
	cd server && go vet ./...

clean:
	rm -rf dist server/web/dist_bak data
