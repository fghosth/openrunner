#!/usr/bin/env bash
# 金标准对照：Go 生产路径 vs scripts/golden-check.py 里照发表公式独立写的实现。
#
#   ./scripts/golden-check.sh                 用最新备份快照
#   GOLDEN_DB=~/.openrunner/openrunner.db ./scripts/golden-check.sh
#
# 默认用备份快照而不是活库：Go 侧走 store.Open，它会执行迁移与 ensureJWTSecret，
# 那是写操作——对照不该拿用户的活库当测试夹具。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SNAP_DIR="$BACKUP_DIR/db"
GOLDEN_ATHLETE="${GOLDEN_ATHLETE:-1}"

db="${GOLDEN_DB:-}"
if [ -z "$db" ]; then
  latest=$(ls -1t "$SNAP_DIR"/*.db 2>/dev/null | head -1 || true)
  if [ -z "$latest" ]; then
    echo "✗ 没有备份快照（$SNAP_DIR），也没有指定 GOLDEN_DB" >&2
    echo "  先跑 ./scripts/backup.sh，或 GOLDEN_DB=<路径> 指定一个库" >&2
    exit 1
  fi
  db="$latest"
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/or-golden.XXXXXX")
trap 'rm -rf "$work"' EXIT

# 复制一份再开：即便是快照也可能被别的对照共用，别让它被迁移改动
cp "$db" "$work/golden.db"
chmod u+w "$work/golden.db"

echo "==> 数据源: $db"
echo "==> Go 侧（生产路径 loadDailyLoads → buildPMCInput → ComputePMC）"
( cd "$ROOT/server" && \
  OPENRUNNER_GOLDEN_DB="$work/golden.db" \
  OPENRUNNER_GOLDEN_ATHLETE="$GOLDEN_ATHLETE" \
  OPENRUNNER_GOLDEN_OUT="$work/go_pmc.json" \
  OPENRUNNER_GOLDEN_NP_OUT="$work/go_np.json" \
  OPENRUNNER_GOLDEN_RTSS_OUT="$work/go_rtss.json" \
  OPENRUNNER_GOLDEN_WBAL_OUT="$work/go_wbal.json" \
  go test ./api -run TestGoldenDump -count=1 )

echo "==> Python 侧（照发表公式独立实现）"
./scripts/golden-check.py --db "$work/golden.db" --go "$work/go_pmc.json" \
  --go-np "$work/go_np.json" --go-rtss "$work/go_rtss.json" \
  --go-wbal "$work/go_wbal.json" --athlete "$GOLDEN_ATHLETE"
