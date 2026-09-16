# Photo Collation

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Build Status](https://img.shields.io/github/actions/workflow/status/fy1967/photo_collation/ci.yml?branch=main&logo=github)](https://github.com/fy1967/photo_collation/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/fy1967/photo_collation?logo=github)](https://github.com/fy1967/photo_collation/releases/latest)
[![License](https://img.shields.io/github/license/fy1967/photo_collation)](./LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/fy1967/photo_collation)](https://goreportcard.com/report/github.com/fy1967/photo_collation)

按 EXIF 拍摄时间自动归类 JPEG 照片。提供 **CLI** 与 **Fyne GUI** 两种使用方式。

```
~/Pictures/All/                            ~/Pictures/Sorted/
├── vacation/                              ├── p2018/
│   ├── IMG_1234.jpg  (拍摄 2018-04-15)    │   ├── m04/
│   └── IMG_1235.jpg  (拍摄 2018-04-15)    │   ├── m05/
├── 2019/                                  │   └── m06/
│   ├── a.jpg        (拍摄 2018-06-01)    ├── p2020/
│   └── b.jpg        (拍摄 2020-07-25)    │   └── m07/
├── screenshot.jpg    (无 EXIF)            └── Noexif/
                                          └── screenshot.jpg
```

## 功能特性

- **递归扫描**：多层子目录自动遍历
- **EXIF 解析**：读取 `DateTimeOriginal`（拍摄时间），回退到 `DateTime`
- **嵌套目录**：`<dst>/pYYYY/mMM/` 按年/月两级分类
- **跳过无 EXIF / EXIF 损坏**：都归到 `<dst>/Noexif/`，避免文件丢失
- **跳过已存在**：同名文件不覆盖
  - **大小相同**：直接跳过（视为已归类）
  - **大小不同**：自动加 `_` 后缀重命名（`IMG_001.jpg` → `IMG_001_.jpg`），多次冲突累加 `_`
- **跳过隐藏目录**：`.git`、`.thumbnails`、macOS 资源叉（`._*`）等
- **跳过自身输出**：二次运行不会重拷已归类文件
- **I/O 重试**：外置盘 cable 抖动自动重试（指数退避 200/400/800ms）
- **并发处理**：可配 worker 数（CLI/Flags/GUI 均可）
- **GUI 实时进度**：进度条 + 缩略图 + 文件滚动日志
- **拖放支持**：直接把文件夹拖到 GUI 窗口

## 系统要求

- **Go 1.22+**（CLI 无 CGO；GUI 编译需要 C 工具链）
- **macOS GUI**：Xcode Command Line Tools（`xcode-select --install`）
- **Linux GUI**：GCC + X11 + OpenGL 开发头文件
- **Windows GUI**：MinGW-w64（推荐 TDM-GCC）
- 操作系统：macOS / Linux / Windows

## 快速开始

### 方式一：下载预编译二进制（推荐）

从 [Releases](../../releases) 下载对应平台的二进制：

| 平台 | 文件 |
|---|---|
| macOS Apple Silicon | `photo_collation-gui-darwin-arm64` |
| macOS Intel | `photo_collation-gui-darwin-amd64` |
| Linux x86_64 | `photo_collation-gui-linux-amd64` |
| Linux ARM64 | `photo_collation-gui-linux-arm64` |
| Windows x86_64 | `photo_collation-gui-windows-amd64.exe` |

> macOS 首次启动如遇 Gatekeeper 拦截：`系统设置 → 隐私与安全性 → 仍要打开`。

### 方式二：从源码编译

```bash
git clone <repo> photo_collation && cd photo_collation

# CLI（任意平台，无需 CGO）
go build -o photo_collation .

# GUI（当前平台，需要 CGO）
go build -o photo_collation-gui ./cmd/gui
```

### 方式三：Docker（CLI 版）

```bash
# 构建镜像
docker build -t photo_collation:cli .

# 跑一次：源只读，目标读写
docker run --rm \
  -v ~/Photos/All:/photos:ro \
  -v ~/Photos/Sorted:/sorted \
  --user 65532:65532 \
  photo_collation:cli \
  -src /photos -dst /sorted
```

详见下方 [Docker](#docker-容器化) 章节。

### 方式四：macOS `.app`（GUI 用户推荐）

从 [Releases](../../releases) 下载对应架构的 zip：

| 你的 Mac | 文件 |
|---|---|
| Apple Silicon (M1/M2/M3/M4) | `PhotoCollation-macos-arm64.zip` |
| Intel | `PhotoCollation-macos-amd64.zip` |

**首次启动**（未签名应用，Gatekeeper 会拦截）：

1. 双击 zip 解压
2. **右键** `Photo Collation.app` → 选择「打开」
3. 弹出警告点「打开」即可，之后双击正常

或者用命令行去隔离属性：

```bash
xattr -d com.apple.quarantine "/Applications/Photo Collation.app"
```

## CLI 用法

```bash
photo_collation -src <源目录> -dst <目标目录> [-v] [-dry-run]
```

| 参数 | 说明 |
|---|---|
| `-src` | 源目录（必需） |
| `-dst` | 目标根目录（必需） |
| `-v` | 详细日志（每个文件处理记录） |
| `-dry-run` | 演练模式：不写盘，只读源 |

**示例**：

```bash
# 基本归类
./photo_collation -src ~/Pictures/All -dst ~/Pictures/Sorted

# 演练（先看会怎么处理，再决定实跑）
./photo_collation -src /Volumes/Camera/DCIM -dst /Volumes/Backup/ByDate -v -dry-run

# 看帮助
./photo_collation -h
```

**输出示例**：

```
photo_collation: starting src=./p1 dst=./P-C dry_run=false
photo_collation: copied src=p1/石潭-马鬃岭/DSC04177.JPG dst=./P-C/p2018/m02/DSC04177.JPG
...
photo_collation: done total=594 scanned=594 classified=594 noexif=0 skipped=0 failed=0
```

## GUI 用法

```bash
./photo_collation-gui
```

**界面元素**：

- **源目录 / 目标目录**：点 `浏览` 选目录，**或直接把文件夹拖到窗口**
- **选项**：演练模式、详细日志、Workers（并发数）
- **统计**：总数 / 扫描 / 已归类 / 无 EXIF / 跳过 / 失败
- **进度条**：基于预扫描总数的百分比
- **缩略图列表**：每条记录显示 80×80 缩略图
- **按钮**：开始 / 取消 / 完成后打开输出目录

## 跨平台编译

### 通用原则

- **CLI** 是纯 Go 的（无 CGO），可任意交叉编译
- **GUI** 用了 Fyne + CGO，**强烈建议在目标平台本地编译**

### CLI 跨平台编译（一行命令）

```bash
# macOS Apple Silicon (M1/M2/M3/M4)
GOOS=darwin GOARCH=arm64 go build -o dist/photo_collation-darwin-arm64 .

# macOS Intel
GOOS=darwin GOARCH=amd64 go build -o dist/photo_collation-darwin-amd64 .

# Linux x86_64（服务器、NAS）
GOOS=linux GOARCH=amd64 go build -o dist/photo_collation-linux-amd64 .

# Linux ARM64（树莓派、ARM 服务器）
GOOS=linux GOARCH=arm64 go build -o dist/photo_collation-linux-arm64 .

# Windows x86_64
GOOS=windows GOARCH=amd64 go build -o dist/photo_collation-windows-amd64.exe .

# Windows ARM64（Surface Pro X 等）
GOOS=windows GOARCH=arm64 go build -o dist/photo_collation-windows-arm64.exe .
```

### GUI 跨平台编译

#### 方法 A：在目标机本地编译（**最稳**）

| 平台 | 准备 | 编译 |
|---|---|---|
| **macOS Apple Silicon** | 无 | `go build -o photo_collation-gui ./cmd/gui` |
| **macOS Intel** | 无 | `GOARCH=amd64 go build -o photo_collation-gui ./cmd/gui` |
| **Ubuntu/Debian** | `sudo apt install gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev` | `CGO_ENABLED=1 go build -o photo_collation-gui ./cmd/gui` |
| **Fedora/RHEL** | `sudo dnf install gcc libX11-devel libXcursor-devel mesa-libGL-devel libXi-devel` | 同上 |
| **Windows** | 安装 [TDM-GCC](https://jmeubank.github.io/tdm-gcc/) 或 MinGW-w64 | `set CGO_ENABLED=1` + `go build -o photo_collation-gui.exe .\cmd\gui` |

#### 方法 B：macOS 上交叉编译 GUI

需要先安装跨平台工具链：

```bash
# macOS → Linux GUI（amd64）
brew install FiloSottile/musl-cross/musl-cross
brew install gtk+3
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
  CC=x86_64-linux-musl-gcc \
  PKG_CONFIG_PATH=/usr/local/opt/gtk+3/lib/pkgconfig \
  go build -o dist/photo_collation-gui-linux-amd64 ./cmd/gui

# macOS → Windows GUI（amd64）
brew install mingw-w64
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
  CC=x86_64-w64-mingw32-gcc \
  go build -o dist/photo_collation-gui-windows-amd64.exe ./cmd/gui
```

#### 方法 C：Docker 隔离编译（CI/CD 推荐）

```bash
# Linux GUI（任何主机）
docker run --rm -v "$PWD":/src -w /src golang:1.22 \
  bash -c "apt-get update && \
           apt-get install -y gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev && \
           CGO_ENABLED=1 go build -o dist/photo_collation-gui-linux-amd64 ./cmd/gui"

# Windows GUI
docker run --rm -v "$PWD":/src -w /src golang:1.22 \
  bash -c "apt-get update && \
           apt-get install -y gcc-mingw-w64-x86-64 && \
           GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
           CC=x86_64-w64-mingw32-gcc \
           go build -o dist/photo_collation-gui-windows-amd64.exe ./cmd/gui"
```

### 一键构建脚本（macOS）

```bash
#!/bin/bash
# build-all.sh
set -e
mkdir -p dist

echo "==> CLI (5 个目标)"
for target in darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 windows-amd64; do
  GOOS=${target%%-*} GOARCH=${target##*-} ext=""
  [ "$GOOS" = "windows" ] && ext=".exe"
  go build -o dist/photo_collation-${target}${ext} .
done

echo "==> GUI 当前平台"
go build -o dist/photo_collation-gui ./cmd/gui

echo "==> GUI Linux（如有工具链）"
if command -v x86_64-linux-musl-gcc >/dev/null && [ -d /usr/local/opt/gtk+3 ]; then
  GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
    CC=x86_64-linux-musl-gcc PKG_CONFIG_PATH=/usr/local/opt/gtk+3/lib/pkgconfig \
    go build -o dist/photo_collation-gui-linux-amd64 ./cmd/gui || echo "skip linux"
fi

echo "==> GUI Windows（如有工具链）"
if command -v x86_64-w64-mingw32-gcc >/dev/null; then
  GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
    go build -o dist/photo_collation-gui-windows-amd64.exe ./cmd/gui || echo "skip windows"
fi

echo "==> 产物："
ls -la dist/
```

### 产物大小参考

| 平台 | CLI | GUI |
|---|---|---|
| macOS arm64 | ~3.8 MB | ~32 MB |
| macOS amd64 | ~3.9 MB | ~33 MB |
| Linux amd64 | ~3.7 MB | ~30 MB |
| Windows amd64 | ~3.9 MB | ~30 MB |

## Docker 容器化

CLI 已有完整 Dockerfile 方案：**多阶段构建 + distroless 基础镜像 + nonroot 用户**。最终镜像约 **12 MB**，可放到任何 Linux 主机或 NAS。

### 文件

| 文件 | 作用 |
|---|---|
| `Dockerfile` | 多阶段构建：golang:alpine 编译 → distroless 运行 |
| `.dockerignore` | 排除 GUI 源码、用户数据、构建产物等 |
| `docker-compose.yml` | 一键编排（构建 + 挂卷 + 默认参数） |
| `scripts/docker-build.sh` | 跨平台 buildx 构建（linux/amd64 + arm64） |
| `scripts/docker-run.sh` | 一行运行容器化 photo_collation |

### 构建

```bash
# 当前平台
docker build -t photo_collation:cli .

# 查看版本
docker run --rm photo_collation:cli -version

# 用脚本（注入 git 版本号）
./scripts/docker-build.sh v1.0.0

# 多平台（推送到 registry）
docker buildx create --use
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t fy1967/photo_collation:v1.0.0 \
  --push .
```

### 运行

```bash
# 1. 准备目录
mkdir -p ./photos ./sorted

# 2. 复制照片到 ./photos（容器会看到 /photos）

# 3. 跑容器
docker run --rm \
  --user 65532:65532 \
  -v "$(pwd)/photos":/photos:ro \
  -v "$(pwd)/sorted":/sorted \
  photo_collation:cli \
  -src /photos -dst /sorted

# 4. 完成后 ./sorted 会有 p2018/、p2020/、Noexif/ 目录
```

### docker-compose

```bash
# 编辑 docker-compose.yml 把 ./photos / ./sorted 改成实际路径
docker compose build
docker compose run --rm collation -src /photos -dst /sorted -v
```

### 镜像特性

- **大小**：~12 MB（distroless static + 单一二进制）
- **安全**：以 uid 65532 nonroot 运行
- **可复现**：`BUILT_AT` 构建参数支持 `SOURCE_DATE_EPOCH`
- **版本**：通过 `-ldflags` 注入 `version/commit/builtAt`，用 `-version` flag 查看
- **多平台**：buildx 同时出 linux/amd64 + linux/arm64

### 典型使用场景

| 场景 | 命令 |
|---|---|
| NAS / 家庭服务器 | 部署一次到 Synology，群晖 Container Manager 用同一个镜像 |
| 一次性批量处理 | `docker run --rm -v ... photo_collation:cli -src ... -dst ...` |
| 自动化流水线 | GitHub Actions 用 buildx 出多平台镜像并 push 到 GHCR |
| 测试不同 Go 版本 | 修改 Dockerfile 第一行的 `golang:1.22-alpine` |

## 项目结构

```
photo_collation/
├── main.go                      # CLI 入口
├── go.mod / go.sum
├── README.md
├── Dockerfile                   # CLI 多阶段 Docker 构建
├── .dockerignore
├── docker-compose.yml           # 一键编排
├── scripts/
│   ├── docker-build.sh          # 跨平台 buildx 构建
│   └── docker-run.sh            # 一行运行容器
├── cmd/
│   └── gui/                     # GUI 入口
│       ├── main.go
│       └── helpers.go
└── internal/
    ├── exif/                    # EXIF 读取（封装 goexif）
    │   ├── reader.go
    │   └── reader_test.go
    ├── collator/                # 核心扫描/归类/拷贝
    │   ├── collator.go          # 主流程：预扫描 → worker pool
    │   ├── copier.go            # 流式拷贝 + I/O 重试
    │   ├── options.go           # 配置（Workers、OnProgress）
    │   ├── result.go            # Stats 统计结构
    │   ├── scanner.go           # 目录遍历 + 文件过滤
    │   ├── progress_test.go     # OnProgress 回调测试
    │   ├── workers_test.go      # 并发安全测试
    │   └── collator_test.go     # 集成测试
    └── thumb/                   # 缩略图生成 + LRU 缓存
        ├── thumb.go
        └── thumb_test.go
```

## 测试

```bash
# 全部测试
go test ./...

# 详细输出
go test ./... -v

# 指定包
go test ./internal/collator/...
go test ./internal/exif/...
go test ./internal/thumb/...

# 性能基准
go test ./internal/collator/... -bench=.
```

当前测试覆盖：23 个测试用例。

## 故障排除

### `ld: warning: ignoring duplicate libraries: '-lobjc'`

**Fyne 在 macOS 上的已知 linker 警告，无害**，产物正常。

### macOS Gatekeeper 拦截

`系统设置 → 隐私与安全性 → 仍要打开`

或先去掉隔离属性：

```bash
xattr -d com.apple.quarantine photo_collation-gui
```

### EXIF 读取失败 / 大量 Noexif

- 检查照片是否被 EXIF stripping 工具处理过
- 部分 PNG 截图无 EXIF（程序会自动归到 Noexif）
- macOS 外置盘照片带 `._DSC*.JPG` 资源叉（程序自动跳过）

### Linux GUI 编译失败

```bash
# Ubuntu/Debian
sudo apt install gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev

# Fedora
sudo dnf install gcc libX11-devel libXcursor-devel mesa-libGL-devel libXi-devel

# Arch
sudo pacman -S gcc mesa xorg-server-devel libxkbcommon-x11
```

### Windows GUI 编译失败

1. 安装 [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)（推荐）或 MinGW-w64
2. 确认 `gcc` 在 PATH 中：`gcc --version`
3. PowerShell 中：`$env:CGO_ENABLED="1"` 后再 `go build`

### I/O 错误频繁

外置盘接触不良时即使有重试也会失败：

```bash
# 检查磁盘健康
diskutil verifyVolume /Volumes/M1/    # macOS
sudo fsck -n /dev/sdX                  # Linux
chkdsk X: /f                           # Windows（需管理员）
```

### GUI 不响应 / 启动失败

```bash
# 终端启动查看错误
./photo_collation-gui 2>&1 | tee /tmp/gui.log

# macOS 额外检查
log show --predicate 'process == "photo_collation-gui"' --last 5m
```

## 已知限制

- **取消按钮非真中断**：会等当前批次完成才停（worker 间未传递 ctx）
- **首次缩略图有延迟**：每张多 ~30ms 解码；第二次扫同目录秒过（LRU 缓存）
- **GUI 进度条基于预扫描**：首次启动会有一小段"扫描中..."，等总数出来才显示百分比
- **跨平台 GUI 编译需目标平台工具链**：见上文方法 B/C

## 路线图

- [ ] 取消按钮真正早退（ctx 透传到 exif + copy）
- [ ] HEIC / RAW 格式支持
- [ ] 缩略图后台 worker pool（避免单张 decode 卡住日志流）
- [ ] 双击日志项在 Finder 中显示文件
- [ ] 处理报告导出（CSV / JSON）
- [ ] 文件去重（按内容哈希）
- [ ] `-move` 模式（拷贝后删除源）

## 许可

MIT

## 致谢

- [goexif](https://github.com/rwcarlsen/goexif) — EXIF 解析
- [Fyne](https://fyne.io/) — 跨平台 GUI
- [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) — 图像缩放
