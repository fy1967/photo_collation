# syntax=docker/dockerfile:1.7
#
# 多阶段构建：CLI 版 photo_collation 容器镜像
#
# 编译目标：CGO_ENABLED=0，纯静态二进制，可放到任何 Linux 基础镜像
# 最终镜像：~12 MB（distroless static）
# 用户：nonroot（uid 65532）
#
# 构建：
#   docker build -t photo_collation:cli .
#
# 多平台构建：
#   docker buildx create --use
#   docker buildx build --platform linux/amd64,linux/arm64 -t photo_collation:cli .
#
# 使用：
#   docker run --rm \
#     -v /path/to/photos:/photos:ro \
#     -v /path/to/sorted:/sorted \
#     photo_collation:cli \
#     -src /photos -dst /sorted
#
# ===========================================================================

# ===== Stage 1: 构建 =====
FROM golang:1.22-alpine AS builder

# 构建参数：可注入版本号（CI 用）
ARG VERSION=dev
ARG COMMIT=unknown
# BUILT_AT 默认用构建时刻，CI 可注入 SOURCE_DATE_EPOCH 字符串保证可复现
ARG BUILT_AT="1970-01-01T00:00:00Z"

WORKDIR /src

# 先复制 go.mod / go.sum，利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

# 再复制源码
COPY . .

# 编译：纯静态，去符号表，注入版本信息
#   -X main.version   注入版本号
#   -X main.commit    注入 commit hash
#   -X main.builtAt   注入构建时间（用 SOURCE_DATE_EPOCH 保证可复现）
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
      -o /out/photo_collation .

# ===== Stage 2: 运行 =====
FROM gcr.io/distroless/static-debian12:nonroot

# 从 builder 复制二进制
COPY --from=builder /out/photo_collation /usr/local/bin/photo_collation

# 元数据
LABEL org.opencontainers.image.title="photo_collation" \
      org.opencontainers.image.description="EXIF-based photo organizer (CLI)" \
      org.opencontainers.image.source="https://github.com/fy1967/photo_collation" \
      org.opencontainers.image.licenses="MIT"

# 用 ENTRYPOINT + CMD 模式：ENTRYPOINT 是固定的 photo_collation，CMD 是默认参数
# 这样 `docker run photo_collation -src /photos -dst /sorted` 和 `docker run photo_collation` 都能用
ENTRYPOINT ["/usr/local/bin/photo_collation"]
CMD ["-h"]
