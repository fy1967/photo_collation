// Command photo_collation 按 EXIF 拍摄时间归类 JPEG 照片。
//
// 用法：
//
//	photo_collation -src <源目录> -dst <目标目录> [-v] [-dry-run]
//
// 行为：
//   - 递归扫描 -src 下所有 .jpg/.jpeg
//   - 读取 EXIF DateTimeOriginal（回退 DateTime），生成 pYYYY/mMM 子目录
//   - 读不到 EXIF 的照片归到 <dst>/Noexif
//   - 同名文件跳过不覆盖
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"photo_collation/internal/collator"
	"photo_collation/internal/exif"
)

const (
	exitOK           = 0
	exitRuntimeError = 1
	exitArgError     = 2
)

// 版本信息：通过 -ldflags 注入，详见 Dockerfile / build-all.sh
var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	var (
		showVersion bool
		src         string
		dst         string
		verbose     bool
		dryRun      bool
	)

	flag.BoolVar(&showVersion, "version", false, "打印版本信息后退出")
	flag.StringVar(&src, "src", "", "源目录（必需）")
	flag.StringVar(&dst, "dst", "", "目标根目录（必需），子目录 pYYYY/mMM 会建在此目录下")
	flag.BoolVar(&verbose, "v", false, "打印调试日志")
	flag.BoolVar(&dryRun, "dry-run", false, "演练模式：不写盘，只读源与统计")
	flag.Usage = usage
	flag.Parse()

	if showVersion {
		fmt.Printf("photo_collation %s (commit %s, built %s)\n", version, commit, builtAt)
		os.Exit(exitOK)
	}

	if src == "" || dst == "" {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "\nerror: -src and -dst are required")
		os.Exit(exitArgError)
	}

	c, err := collator.New(collator.Options{
		Src:     src,
		Dst:     dst,
		Verbose: verbose,
		DryRun:  dryRun,
	}, exif.NewReader())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitArgError)
	}

	stats, err := c.Run(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitRuntimeError)
	}

	// 运行成功时把统计再单独打一行到 stdout，便于脚本消费。
	fmt.Printf("photo_collation: scanned=%d classified=%d renamed=%d noexif=%d skipped=%d failed=%d\n",
		stats.Scanned, stats.Classified, stats.Renamed, stats.NoExif, stats.Skipped, stats.Failed)
}

func usage() {
	fmt.Fprintf(os.Stderr, "用法: photo_collation -src <源目录> -dst <目标目录> [-v] [-dry-run]\n\nFlags:\n")
	flag.PrintDefaults()
}