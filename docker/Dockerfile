# 构建上下文：openrunner 仓库本身（compose 通过 additional_contexts 提供 core/ui，见 docker-compose.yml）
# 构建阶段 1：UI（来自 additional context "ui"）
FROM node:24-alpine AS ui-build
COPY --from=ui / /repo
WORKDIR /repo
RUN npm ci --silent && npm run build

# 构建阶段 2：单二进制（纯 Go SQLite 驱动，无 cgo）
FROM golang:1.26-alpine AS server
ARG VERSION=docker
# 依赖代理可覆盖（国内网络构建时：--build-arg GOPROXY=https://goproxy.cn,direct）
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
COPY --from=core / /src/openrunner-core
COPY server/go.mod server/go.sum ./server/
COPY server/ ./server/
RUN cd server && go mod edit -replace=github.com/openrunner/core=/src/openrunner-core \
    && CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" \
       -o /out/openrunner ./cmd/openrunner-server

# 运行阶段
FROM alpine:3.20
RUN adduser -D -H openrunner
USER openrunner
WORKDIR /data
COPY --from=server /out/openrunner /usr/local/bin/openrunner
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["openrunner", "--data", "/data", "--addr", ":8080"]
