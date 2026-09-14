#!/usr/bin/env bash
# scripts/docker-build.sh — 跨平台 Docker 镜像构建
#
# 用 buildx 出 linux/amd64 和 linux/arm64 两个架构的镜像
# 产物会推送到本地（不会 push 到 registry）
#
# 用法：
#   ./scripts/docker-build.sh           # 默认 tag: photo_collation:cli
#   ./scripts/docker-build.sh v1.2.3    # 指定 tag 后缀

set -euo pipefail

# === 配置 ===
IMAGE="${IMAGE:-photo_collation}"
TAG="${1:-cli}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

# === 校验 buildx ===
if ! docker buildx version >/dev/null 2>&1; then
  echo "error: docker buildx not available" >&2
  echo "  安装方法：https://docs.docker.com/go/buildx/" >&2
  exit 1
fi

# === 拿版本信息（注入镜像） ===
VERSION="${TAG}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "==> 构建参数："
echo "  IMAGE     : ${IMAGE}:${TAG}"
echo "  PLATFORMS : ${PLATFORMS}"
echo "  VERSION   : ${VERSION}"
echo "  COMMIT    : ${COMMIT}"
echo "  BUILT_AT  : ${BUILT_AT}"
echo

# === buildx build ===
# --load 把构建产物加载到本地 docker（多平台只能 --push 或不输出；这里用 --load + 单平台更直观）
# 多平台本地用需要 --platform 单独跑或 manifest
if [[ "$PLATFORMS" == *","* ]]; then
  echo "==> 多平台构建（产物在临时 builder，不会写入本地 docker）"
  echo "    加 --push 推送到 registry，或拆分单平台构建"
  docker buildx build \
    --platform "$PLATFORMS" \
    --build-arg "VERSION=${VERSION}" \
    --build-arg "COMMIT=${COMMIT}" \
    --build-arg "BUILT_AT=${BUILT_AT}" \
    -t "${IMAGE}:${TAG}" \
    .
else
  echo "==> 单平台构建（直接加载到本地 docker）"
  docker buildx build \
    --platform "$PLATFORMS" \
    --load \
    --build-arg "VERSION=${VERSION}" \
    --build-arg "COMMIT=${COMMIT}" \
    --build-arg "BUILT_AT=${BUILT_AT}" \
    -t "${IMAGE}:${TAG}" \
    .
fi

echo
echo "==> 验证镜像："
docker run --rm "${IMAGE}:${TAG}" -version
