// Package collator 实现"按 EXIF 拍摄时间归类照片"的核心流程。
//
// 目录结构：
//
//	<dst>/
//	├── pYYYY/mMM/   按年/月归类
//	└── Noexif/      无 EXIF 的照片
//
// 流程：
//   1. 预扫描：walk src，统计目标文件总数并收集路径
//   2. 处理：按 Options.Workers 串行/并发处理每个文件
//      a. 读取 EXIF DateTimeOriginal / DateTime
//      b. 按年/月生成 pYYYY/mMM 子目录（或 Noexif）
//      c. 流式拷贝到 Dst/pYYYY/mMM/<原文件名>
//
// 错误策略：单文件失败不影响整体流程；最终通过 Stats 汇总。
package collator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"photo_collation/internal/exif"
)

const (
	// NoexifDir 无 EXIF 拍摄时间的照片归类目录名。
	NoexifDir = "Noexif"

	// YearDirLayout 年份目录的时间格式。
	YearDirLayout = "2006"

	// MonthDirLayout 月份目录的时间格式（零填充）。
	MonthDirLayout = "01"

	// YearPrefix 年份目录前缀，例如 p2026。
	YearPrefix = "p"

	// MonthPrefix 月份目录前缀，例如 m01。
	MonthPrefix = "m"
)

// Collator 一次扫描/归类的执行器。
type Collator struct {
	opts   Options
	reader *exif.Reader
	logger *slog.Logger
	stats  Stats
	mu     sync.Mutex // 保护 stats
}

// New 构造 Collator。
//
// 校验 src/dst 路径是否合法，但不立即创建 dst。
func New(opts Options, reader *exif.Reader) (*Collator, error) {
	if opts.Src == "" {
		return nil, errors.New("src directory is required")
	}
	if opts.Dst == "" {
		return nil, errors.New("dst directory is required")
	}

	srcInfo, err := os.Stat(opts.Src)
	if err != nil {
		return nil, fmt.Errorf("stat src: %w", err)
	}
	if !srcInfo.IsDir() {
		return nil, fmt.Errorf("src %q is not a directory", opts.Src)
	}

	// src 与 dst 不能是同一路径，避免把照片拷回源。
	srcAbs, _ := filepath.Abs(opts.Src)
	dstAbs, _ := filepath.Abs(opts.Dst)
	if srcAbs == dstAbs {
		return nil, errors.New("src and dst must be different paths")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: func() slog.Level {
			if opts.Verbose {
				return slog.LevelDebug
			}
			return slog.LevelInfo
		}(),
	}))

	return &Collator{
		opts:   opts,
		reader: reader,
		logger: logger,
	}, nil
}

// Run 执行扫描与归类，返回统计结果。
//
// 每次调用都会重置内部 stats，保证同一 Collator 可重复 Run。
//
// ctx 暂未用于早退（Run 在所有 worker 完成前不会返回），但保留接口供将来扩展。
func (c *Collator) Run(ctx context.Context) (Stats, error) {
	_ = ctx // 保留接口

	c.mu.Lock()
	c.stats = Stats{}
	c.mu.Unlock()

	c.logger.Info("starting photo collation",
		"src", c.opts.Src,
		"dst", c.opts.Dst,
		"dry_run", c.opts.DryRun,
		"workers", c.workerCount(),
	)

	if err := ensureDir(c.opts.Dst); err != nil {
		return c.stats, fmt.Errorf("create dst: %w", err)
	}

	// 1. 预扫描：walk 收集所有目标文件路径，并写入 stats.Total
	paths, err := c.collectPaths()
	if err != nil {
		return c.stats, fmt.Errorf("prescan: %w", err)
	}
	c.mu.Lock()
	c.stats.Total = len(paths)
	c.mu.Unlock()

	// 通知 GUI 预扫描完成，便于设置进度条分母
	c.emitProgress(FileEvent{
		Outcome: OutcomePreScanDone,
		Total:   len(paths),
	})

	if len(paths) == 0 {
		c.logger.Info("done (no photo files found)",
			"total", 0)
		return c.stats, nil
	}

	// 2. 处理：按 worker 数决定串行还是并发
	c.processAll(paths)

	c.logger.Info("done",
		"total", c.stats.Total,
		"scanned", c.stats.Scanned,
		"classified", c.stats.Classified,
		"renamed", c.stats.Renamed,
		"noexif", c.stats.NoExif,
		"skipped", c.stats.Skipped,
		"failed", c.stats.Failed,
	)
	return c.stats, nil
}

// workerCount 返回有效的 worker 数（至少 1）。
func (c *Collator) workerCount() int {
	if c.opts.Workers > 1 {
		return c.opts.Workers
	}
	return 1
}

// collectPaths 预扫描 src，返回所有待处理的照片文件绝对路径。
func (c *Collator) collectPaths() ([]string, error) {
	var paths []string
	err := filepath.WalkDir(c.opts.Src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			c.logger.Warn("walk error", "path", path, "err", walkErr)
			return nil // 不中断遍历
		}

		if d.IsDir() {
			if path == c.opts.Src {
				return nil
			}
			if shouldSkipDir(d.Name()) {
				c.logger.Debug("skipping directory", "path", path)
				return filepath.SkipDir
			}
			return nil
		}

		if !isPhotoFile(d.Name()) {
			return nil
		}

		paths = append(paths, path)
		return nil
	})
	return paths, err
}

// processAll 处理路径切片：单 worker 串行；多 worker 并发。
//
// 所有 processFile 的内部调用对并发都是安全的：
//   - exif.Reader 无状态
//   - ensureDir 幂等
//   - copyFile 用 O_EXCL 原子创建
//   - emitProgress 内部有 panic recover
func (c *Collator) processAll(paths []string) {
	workers := c.workerCount()
	if workers == 1 {
		for _, p := range paths {
			c.processFile(p)
		}
		return
	}

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()
			c.processFile(path)
		}(p)
	}
	wg.Wait()
}

// processFile 处理一张图片。
//
// 流程：读 EXIF → 决定子目录（pYYYY/mMM 或 Noexif）→ 拷贝 → 更新统计 → 触发回调。
// 任何错误都通过日志记录，不向外抛出。
func (c *Collator) processFile(path string) {
	c.mu.Lock()
	c.stats.Scanned++
	c.mu.Unlock()

	t, _, err := c.reader.ReadTime(path)
	targetDir := ""
	isNoExif := false

	switch {
	case err == nil:
		targetDir = yearMonthDir(c.opts.Dst, t)
	case errors.Is(err, exif.ErrNoExif),
		errors.Is(err, exif.ErrInvalidTime),
		errors.Is(err, exif.ErrCorrupted):
		// ErrCorrupted（EXIF 损坏）也归 Noexif，保证文件不被丢弃。
		// 用户后续可手动修复或用其他工具归类。
		targetDir = filepath.Join(c.opts.Dst, NoexifDir)
		isNoExif = true
	default:
		// 读 EXIF 的非预期错误（如权限）。
		c.logger.Warn("read exif failed", "path", path, "err", err)
		c.mu.Lock()
		c.stats.Failed++
		c.mu.Unlock()
		c.emitProgress(FileEvent{Src: path, Outcome: OutcomeFailed, Err: err})
		return
	}

	if err := ensureDir(targetDir); err != nil {
		c.logger.Warn("create target dir failed",
			"path", targetDir, "err", err)
		c.mu.Lock()
		c.stats.Failed++
		c.mu.Unlock()
		c.emitProgress(FileEvent{Src: path, Outcome: OutcomeFailed, Err: err})
		return
	}

	target := filepath.Join(targetDir, filepath.Base(path))

	start := time.Now()
	finalDst, err := c.copyFileWithCollision(path, target)
	switch {
	case err == nil:
		renamed := finalDst != target
		if renamed {
			c.logger.Info("copied (renamed)",
				"src", path,
				"dst", finalDst,
				"took_ms", time.Since(start).Milliseconds(),
			)
		} else {
			c.logger.Info("copied",
				"src", path,
				"dst", finalDst,
				"took_ms", time.Since(start).Milliseconds(),
			)
		}
		c.mu.Lock()
		switch {
		case renamed:
			c.stats.Renamed++
		case isNoExif:
			c.stats.NoExif++
		default:
			c.stats.Classified++
		}
		c.mu.Unlock()
		outcome := OutcomeCopied
		if renamed {
			outcome = OutcomeRenamed
		} else if isNoExif {
			outcome = OutcomeNoExif
		}
		c.emitProgress(FileEvent{
			Src:     path,
			Dst:     finalDst,
			Outcome: outcome,
		})

	case errors.Is(err, ErrExists):
		c.logger.Debug("skipped (target exists, same size)", "src", path, "dst", target)
		c.mu.Lock()
		c.stats.Skipped++
		c.mu.Unlock()
		c.emitProgress(FileEvent{
			Src:     path,
			Dst:     target,
			Outcome: OutcomeSkipped,
		})

	default:
		c.logger.Warn("copy failed", "src", path, "dst", target, "err", err)
		c.mu.Lock()
		c.stats.Failed++
		c.mu.Unlock()
		c.emitProgress(FileEvent{
			Src:     path,
			Dst:     target,
			Outcome: OutcomeFailed,
			Err:     err,
		})
	}
}

// emitProgress 安全地调用 OnProgress 回调。回调里的 panic 会被 recover 住，
// 避免单条 GUI 卡死拖垮整个 collator。
func (c *Collator) emitProgress(ev FileEvent) {
	if c.opts.OnProgress == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			c.logger.Warn("OnProgress callback panicked", "recover", r)
		}
	}()
	c.opts.OnProgress(ev)
}

// outcomeFor 根据是否无 EXIF + 是否成功决定 Outcome。
func outcomeFor(isNoExif, copied bool) Outcome {
	switch {
	case !copied:
		return OutcomeFailed
	case isNoExif:
		return OutcomeNoExif
	default:
		return OutcomeCopied
	}
}

// Stats 安全读取当前统计快照。
func (c *Collator) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// yearMonthDir 生成 "<dst>/pYYYY/mMM" 形式的嵌套目标目录。
//
// 例如 dst=/tmp/Sorted, t=2026-10-15 → /tmp/Sorted/p2026/m10。
func yearMonthDir(dst string, t time.Time) string {
	year := YearPrefix + t.Format(YearDirLayout)
	month := MonthPrefix + t.Format(MonthDirLayout)
	return filepath.Join(dst, year, month)
}