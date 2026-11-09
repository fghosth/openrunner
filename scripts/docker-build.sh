#!/usr/bin/env bash
# 构建 openrunner Docker 镜像（三仓同级布局 → 单镜像）
#
# 用法：
#   ./scripts/docker-build.sh                      # 走国内镜像代理（默认）
#   REGISTRY=docker.io/library ./scripts/docker-build.sh   # 直连 Docker Hub
#   TAG=openrunner:v0.1.1 ./scripts/docker-build.sh
#
# 为什么要「预拉取 + retag」这一步：
#   BuildKit（docker 驱动）解析基础镜像 metadata 走自己的 resolver，
#   不走 daemon 的 registry mirror；连不上 Docker Hub 的环境会直接卡死在
#   "load metadata"。而基础镜像若已在本地镜像库里，metadata 本地即可解析（0.0s）。
#   所以先经代理 docker pull，再按 Dockerfile 里的名字 retag，构建就顺了。
set -euo pipefail

REGISTRY="${REGISTRY:-docker.1ms.run/library}"   # 基础镜像仓库前缀
GOPROXY_="${GOPROXY:-https://goproxy.cn,direct}" # Go 模块代理
TAG="${TAG:-openrunner:latest}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

VERSION="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"

echo "==> 仓库根        : $ROOT"
echo "==> 基础镜像前缀 : $REGISTRY"
echo "==> Go 模块代理  : $GOPROXY_"
echo "==> 版本戳        : $VERSION"

# 1) 预拉取基础镜像（走代理）并按 Dockerfile 引用的名字 retag
for img in node:24-alpine golang:1.26-alpine alpine:3.20; do
  want="${REGISTRY}/${img}"
  if docker image inspect "$want" >/dev/null 2>&1; then
    echo "==> 已有 $want"
    continue
  fi
  echo "==> 拉取 $want"
  docker pull "$want"
done

# 2) 构建（前端产物在镜像内重新构建并 embed 进二进制）
echo "==> 构建镜像 $TAG"
docker build \
  -f "$ROOT/docker/Dockerfile" \
  --build-arg "REGISTRY=${REGISTRY}" \
  --build-arg "GOPROXY=${GOPROXY_}" \
  --build-arg "VERSION=${VERSION}" \
  -t "$TAG" \
  "$ROOT"

echo
echo "==> 完成：$TAG"
echo "    运行： docker run --rm -p 8080:8080 -v openrunner-data:/data \\"
echo "             -e TZ=Asia/Shanghai $TAG"
echo "    首次启动打开 http://localhost:8080 → 「首次设置」页创建管理员账号"
echo "    （没有 OPENRUNNER_ADMIN_PASSWORD 这个环境变量，别照抄旧文档）"
