# 构建上下文为包含三个仓库的工作区根目录（openrunner / openrunner-core / openrunner-ui）
# 构建阶段 1：UI
FROM node:24-alpine AS ui
WORKDIR /ui
COPY openrunner-ui/package.json openrunner-ui/package-lock.json ./
RUN npm ci --silent
COPY openrunner-ui/ ./
RUN npm run build

# 构建阶段 2：单二进制（纯 Go SQLite 驱动，无 cgo）
FROM golang:1.26-alpine AS server
WORKDIR /src
COPY openrunner/server/go.mod openrunner/server/go.sum ./server/
COPY openrunner-core/ ./openrunner-core/
COPY openrunner/server/ ./server/
RUN cd server && go mod edit -replace=github.com/openrunner/core=/src/openrunner-core
COPY --from=ui /ui/dist /src/server/web/dist
RUN cd server && CGO_ENABLED=0 go build -o /out/openrunner ./cmd/openrunner-server

# 运行阶段
FROM alpine:3.20
RUN adduser -D -H openrunner
USER openrunner
WORKDIR /data
COPY --from=server /out/openrunner /usr/local/bin/openrunner
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["openrunner", "--data", "/data", "--addr", ":8080"]
