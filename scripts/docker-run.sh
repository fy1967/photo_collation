#!/usr/bin/env bash
# scripts/docker-run.sh — 一行运行容器化 photo_collation
#
# 用法：
#   ./scripts/docker-run.sh ~/Pictures/All ~/Pictures/Sorted
#   ./scripts/docker-run.sh /Volumes/Camera/DCIM /Volumes/Backup/ByDate -v
#
# 前置：已 docker build 构建过镜像（默认 tag: photo_collation:cli）

set -euo pipefail

if [ $# -lt 2 ]; then
  echo "用法: $0 <源目录> <目标目录> [额外 photo_collation 参数...]" >&2
  echo "示例: $0 ~/Photos/All ~/Photos/Sorted -v" >&2
  exit 1
fi

SRC="$1"
DST="$2"
shift 2

# 把宿主机路径转绝对路径（避免 docker 找不到）
SRC_ABS="$(cd "$SRC" && pwd)"
DST_ABS="$(mkdir -p "$DST" && cd "$DST" && pwd)"

# 关键：
#   - 源目录 :ro  只读挂载，防止容器误写
#   - 目标目录 :rw 读写
#   - --rm   跑完自动清理容器
#   - user   用非 root 跑
docker run --rm \
  --user 65532:65532 \
  -v "${SRC_ABS}:/photos:ro" \
  -v "${DST_ABS}:/sorted" \
  photo_collation:cli \
  -src /photos -dst /sorted "$@"
