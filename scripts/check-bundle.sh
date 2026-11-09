#!/bin/sh
# 构建期守卫：前端产物链路 UI dist → embed 目录 → 二进制 必须完整。
#
# 拦住三类真实发生过的白屏形态：
#   1. index.html 引用的 hash 文件不存在（变异测试复现过的形态）
#   2. Docker 忘 COPY dist 进 embed 目录（Round 28 事故）
#   3. make ui-dist 之后忘了 make build —— 二进制比 dist 旧
#
# 用法: check-bundle.sh <embed_dist_dir> [binary]
#   dist 自检 + （给了二进制时）二进制内含同名资产 + （UI 有产物时）embed 与 UI 构建一致
set -eu

DIST=${1:?用法: check-bundle.sh <embed_dist_dir> [binary]}
BIN=${2:-}

fail() { echo "✗ $*" >&2; exit 1; }

[ -s "$DIST/index.html" ] || fail "$DIST/index.html 缺失或为空 → 先执行 make ui-dist（Docker 里则是 COPY web/dist 缺失）"

# 1) index.html 引用的每个 /assets 文件必须真实存在且非空
refs=$(grep -o '/assets/[^"]*' "$DIST/index.html" | sort -u)
[ -n "$refs" ] || fail "index.html 里没有任何 /assets 引用——这是占位页，不是 Vite 产物"
for f in $refs; do
  [ -s "${DIST}${f}" ] || fail "index.html 引用 ${f}，但 embed 目录里不存在（拷贝不完整或产物过期）"
done

# 2) 二进制必须包含这些文件名（go:embed 把文件名以字符串留在二进制里）。
#    这是「dist 更新了但二进制还是旧的」的判据——旧二进制 grep 不到新 hash 名。
if [ -n "$BIN" ] && [ -f "$BIN" ]; then
  for f in $refs; do
    grep -qa "$f" "$BIN" || fail "二进制 $BIN 不含 $f —— 二进制比 dist 旧，重跑 make build"
  done
fi

# 3) UI 仓库若存在构建产物，embed 目录必须与之一致（拦住「npm run build 了但没拷贝」）。
#    以主仓库根为基准解析相对路径；Docker 构建阶段没有这个目录，自动跳过。
script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
ui_index="$script_dir/../web/dist/index.html"
if [ -f "$ui_index" ] && ! cmp -s "$ui_index" "$DIST/index.html"; then
  fail "web/dist 与 $DIST 不一致 —— 跑 make ui-dist 同步（或 make build 前它俩必须同源）"
fi

n=$(printf '%s\n' "$refs" | wc -l | tr -d ' ')
echo "✓ 前端产物链路完整：$n 个资产文件$( [ -n "$BIN" ] && [ -f "$BIN" ] && echo '，二进制已核验' )"
