#!/usr/bin/env bash
# 把仓库和数据库快照备份到第二块物理盘，并验证「可恢复」而不只是「文件存在」。
#
#   ./scripts/backup.sh              备份到 $HOME/backups/openrunner
#   BACKUP_DIR=/other ./scripts/backup.sh
#
# 为什么备份到另一块盘：GitHub 是异地副本，但本地这份仍是唯一的工作副本；
# 数据库则始终只有一份。
# 目标盘必须与仓库所在盘不同，否则同盘故障等于没备份——脚本会先核对这一点。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-$HOME/backups/openrunner}"
KEEP_DB="${KEEP_DB:-7}"
REPOS=(openrunner)
DATA_DIR="${OPENRUNNER_DATA:-$HOME/.openrunner}"

echo "==> 备份目标: $BACKUP_DIR"

# 目标盘必须与源盘不同（同盘备份在盘故障时一起没）。
# 目标目录可能还不存在（首次运行），所以向上找最近存在的祖先来取设备号——
# 直接 df 不存在的路径会失败，早先版本因此在整个首次运行里跳过了这道检查。
ancestor() {
  local d="$1"
  while [ ! -d "$d" ] && [ "$d" != "/" ]; do d=$(dirname "$d"); done
  printf '%s' "$d"
}
# df -P 是 POSIX 输出；df -c 是 GNU 专有，在 macOS 上含义不同（会多出 total 表头，
# 于是取到的是 "total" 而不是设备名，守卫会误判成同盘并拒绝执行）。
dev_of() { df -P "$1" 2>/dev/null | awk 'NR>1 && $1 ~ /^\// {print $1; exit}'; }
src_dev=$(dev_of "$ROOT")
dst_dev=$(dev_of "$(ancestor "$BACKUP_DIR")")
if true; then
  if [ -n "$src_dev" ] && [ -n "$dst_dev" ] && [ "$src_dev" = "$dst_dev" ]; then
    echo "✗ 备份目标与仓库在同一磁盘（${src_dev}）——同盘故障会一起丢，拒绝执行" >&2
    echo "  指向另一块盘再试： BACKUP_DIR=/mnt/other-disk/backups/openrunner $0" >&2
    exit 1
  fi
  echo "    源盘 $src_dev → 目标盘 ${dst_dev}（不同磁盘 ✓）"
fi

mkdir -p "$BACKUP_DIR/repos" "$BACKUP_DIR/db"

# ---------- 仓库：bare mirror ----------
# 用 fetch 从源仓库镜像所有分支与标签，不改源仓库的 remote 配置。
for repo in "${REPOS[@]}"; do
  src="$ROOT"
  dst="$BACKUP_DIR/repos/$repo.git"
  if [ ! -d "$src/.git" ]; then
    echo "!! 跳过 ${repo}（不是 git 仓库）" >&2
    continue
  fi
  if [ ! -d "$dst" ]; then
    git init --quiet --bare "$dst" >/dev/null
  fi
  git --git-dir="$dst" fetch --quiet --force "$src" \
    '+refs/heads/*:refs/heads/*' '+refs/tags/*:refs/tags/*'
  # 对齐 HEAD，否则镜像仓库的默认分支会停在 init 时的值
  head_ref=$(git -C "$src" symbolic-ref --short HEAD 2>/dev/null || echo main)
  git --git-dir="$dst" symbolic-ref HEAD "refs/heads/$head_ref" 2>/dev/null || true
  n=$(git --git-dir="$dst" rev-list --count HEAD 2>/dev/null || echo 0)
  src_n=$(git -C "$src" rev-list --count HEAD)
  if [ "$n" != "$src_n" ]; then
    echo "✗ $repo 镜像 commit 数 $n ≠ 源 $src_n" >&2
    exit 1
  fi
  echo "    ✓ $repo  $n commits  (分支: $(git -C "$src" branch --format='%(refname:short)' | tr '\n' ' '))"
done

# ---------- 数据库：一致性快照 ----------
# 用 sqlite3 的 .backup（在线备份 API）而不是 cp：服务正在写时 cp 可能拿到撕裂页。
db="$DATA_DIR/openrunner.db"
if [ -f "$db" ]; then
  stamp=$(date +%Y%m%d-%H%M%S)
  snap="$BACKUP_DIR/db/openrunner-$stamp.db"
  sqlite3 "$db" ".backup '$snap'"
  echo "    ✓ 数据库快照 $(basename "$snap")  $(du -h "$snap" | awk '{print $1}')"
  # 只保留最近 KEEP_DB 份
  ls -1t "$BACKUP_DIR"/db/openrunner-*.db 2>/dev/null | tail -n +$((KEEP_DB + 1)) | while read -r old; do
    rm -f "$old"
  done
  kept=$(ls -1 "$BACKUP_DIR"/db/*.db 2>/dev/null | wc -l | tr -d ' ')
  echo "      保留 $kept 份（上限 ${KEEP_DB}）"
else
  echo "!! 未找到数据库 ${db}，跳过" >&2
fi

# ---------- 验证可恢复（不是只看文件在不在） ----------
echo "==> 验证可恢复"
verify=$(mktemp -d "${TMPDIR:-/tmp}/or-verify.XXXXXX")
trap 'rm -rf "$verify"' EXIT

for repo in "${REPOS[@]}"; do
  dst="$BACKUP_DIR/repos/$repo.git"
  [ -d "$dst" ] || continue
  # 从镜像仓库真的 clone 一份出来，确认最新 commit 拿得到。
  # 用 clone 而不是 worktree add：后者会把 worktree 注册进镜像仓库，反复跑积累垃圾记录。
  git clone --quiet "$dst" "$verify/$repo"
  tip=$(git -C "$verify/$repo" rev-parse HEAD)
  src_tip=$(git -C "$ROOT" rev-parse HEAD)
  [ "$tip" = "$src_tip" ] || { echo "✗ $repo 检出 tip $tip ≠ 源 $src_tip" >&2; exit 1; }
  files=$(git -C "$verify/$repo" ls-files | wc -l | tr -d ' ')
  echo "    ✓ $repo 可检出（$files 个文件，tip ${tip:0:7}）"
done

latest=$(ls -1t "$BACKUP_DIR"/db/*.db 2>/dev/null | head -1)
if [ -n "$latest" ]; then
  # 快照必须能打开且行数与源库一致——否则备份是空的或撕裂的
  bad=$(sqlite3 "$latest" "PRAGMA integrity_check;" | head -1)
  [ "$bad" = "ok" ] || { echo "✗ 快照 integrity_check = $bad" >&2; exit 1; }
  src_n=$(sqlite3 "$db" "SELECT COUNT(*) FROM activities;" 2>/dev/null || echo -1)
  snap_n=$(sqlite3 "$latest" "SELECT COUNT(*) FROM activities;" 2>/dev/null || echo -2)
  [ "$src_n" = "$snap_n" ] || { echo "✗ 快照 activities=$snap_n ≠ 源 $src_n" >&2; exit 1; }
  echo "    ✓ 数据库快照可打开，integrity ok，activities=$snap_n 条与源一致"
fi

echo "✓ 备份完成并通过可恢复验证：$BACKUP_DIR"
