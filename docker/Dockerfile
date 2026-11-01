# openrunner 单镜像构建（前端产物 embed 进 Go 二进制）
#
# 构建上下文：主上下文 = openrunner 仓库；core / ui 由 additional_contexts 注入
# （三个仓库需同级摆放，见 docker-compose.yml）。
#
# 国内网络构建：
#   docker build -f docker/Dockerfile \
#     --build-arg REGISTRY=docker.1ms.run/library \
#     --build-arg GOPROXY=https://goproxy.cn,direct \
#     --build-context core=../openrunner-core --build-context ui=../openrunner-ui \
#     -t openrunner .
#
# 用 `docker compose build` 时无需手写 --build-context（compose 已配置）。

# 基础镜像仓库前缀：默认 Docker Hub；国内可换 docker.1ms.run/library
ARG REGISTRY=docker.io/library

# ---------- 阶段 1：构建前端 ----------
FROM ${REGISTRY}/node:24-alpine AS ui-build
COPY --from=ui / /src/ui
WORKDIR /src/ui
RUN npm ci --silent && npm run build

# ---------- 阶段 2：构建单二进制（把前端 embed 进去）----------
FROM ${REGISTRY}/golang:1.26-alpine AS server-build
ARG VERSION=docker
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
ENV CGO_ENABLED=0

# 依赖层先缓存：go.mod 的 replace 指向 ../../openrunner-core，
# 下面的目录布局让该相对路径自然成立（/src/openrunner/server → /src/openrunner-core）
COPY --from=core / /src/openrunner-core/
COPY server/go.mod server/go.sum /src/openrunner/server/
WORKDIR /src/openrunner/server
RUN go mod download

COPY server/ ./
# 关键：前端产物必须放进 Go 的 embed 目录（server/web/dist）。
# .dockerignore 排除了仓库里那份构建产物，缺这一步镜像只会返回占位页 → 白屏。
COPY --from=ui-build /src/ui/dist ./web/dist

RUN go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/openrunner ./cmd/openrunner-server

# ---------- 运行阶段 ----------
FROM ${REGISTRY}/alpine:3.20
# tzdata：日粒度统计按本地时区切分，容器默认 UTC 会让「今天」错位 8 小时
RUN apk add --no-cache tzdata \
    && adduser -D -H openrunner \
    && mkdir -p /data \
    && chown openrunner:openrunner /data
USER openrunner
WORKDIR /data
COPY --from=server-build /out/openrunner /usr/local/bin/openrunner
EXPOSE 8080
VOLUME /data
# 健康检查：/api/health 会顺带验证数据库连通
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["openrunner", "--data", "/data", "--addr", ":8080"]
