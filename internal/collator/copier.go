package collator

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrExists 目标文件已存在，跳过拷贝。
var ErrExists = errors.New("target file already exists")

// maxCollisionRetries 单个源文件最多尝试改名的次数。
// 每次碰撞追加一个 "_"，超过此次数视为异常情况（极少见）。
const maxCollisionRetries = 1000

// copyRetryDefaults 拷贝重试的默认参数。
const (
	maxCopyAttempts   = 4               // 总尝试次数（含首次）
	initialRetryDelay = 200 * time.Millisecond // 首次重试前等待
)

// copyFile 把 src 流式拷贝到 dst。
//
// 行为：
//   - 若 dst 已存在 → 返回 ErrExists，不覆盖
//   - DryRun 模式 → 只校验 src 可读，不写盘
//   - 用 O_EXCL 原子创建，避免 TOCTOU 竞态
//   - 写入失败会清理已创建的 dst 文件
//
// 调用方应通过 copyFileWithRetry 间接调用以获得重试能力。
func (c *Collator) copyFile(src, dst string) error {
	if c.opts.DryRun {
		// 演练模式：仅校验源文件存在且可读。
		f, err := os.Open(src)
		if err != nil {
			return fmt.Errorf("dry-run open src: %w", err)
		}
		return f.Close()
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer in.Close()

	// O_EXCL 原子避免 TOCTOU。
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrExists
		}
		return fmt.Errorf("open dst: %w", err)
	}

	// 写入失败时清理半成品文件。
	copied := false
	defer func() {
		_ = out.Close()
		if !copied {
			_ = os.Remove(dst)
		}
	}()

	bw := bufio.NewWriterSize(out, 256*1024)
	if _, err := io.Copy(bw, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	copied = true
	return nil
}

// copyFileWithRetry 是 copyFile 的重试包装，专门针对外置盘等容易瞬时失败的场景
// （cable 抖动、磁盘休眠唤醒、控制器 busy 等）。
//
// 重试策略：
//   - 最多尝试 maxCopyAttempts 次（默认 4，即首次 + 3 次重试）
//   - 指数退避：initialRetryDelay 翻倍（200ms / 400ms / 800ms）
//   - 每次重试前都记录 warn 日志，便于排查
//
// 不可重试的错误（直接返回，不浪费重试配额）：
//   - ErrExists:           目标已存在，是程序的预期分支
//   - os.ErrNotExist:      源文件消失，重试无意义
//   - os.ErrPermission:    权限问题，重试不会改变
//   - 任何非 syscall.EIO / EOF / ErrUnexpectedEOF 的错误：通常说明源路径或权限有问题
//
// 其他错误（典型如 syscall.EIO）会触发重试。
func (c *Collator) copyFileWithRetry(src, dst string) error {
	return c.copyFileWithRetryOp(src, dst, c.copyFile)
}

// copyFileWithCollision 在 copyFileWithRetry 之上处理"目标已存在"碰撞。
//
// 行为：
//   - 第一次尝试拷贝到 dst
//   - 成功 → 返回 (dst, nil)
//   - 目标已存在 → 比较 src 与 dst 大小：
//     * 大小相同 → 返回 (dst, ErrExists)，调用方按"已归类"对待（视为 skip）
//     * 大小不同 → 在文件名后追加 "_" 后重试：IMG_001.jpg → IMG_001_.jpg → IMG_001__.jpg …
//       成功则返回 (newDst, nil)；仍冲突继续加下划线，最多 maxCollisionRetries 次
//   - 其他错误 → 直接透传
//
// 返回值：
//   - finalDst: 实际写入的路径（未碰撞时等于 dst）
//   - err:      nil / ErrExists / 其他 I/O 错误
func (c *Collator) copyFileWithCollision(src, dst string) (string, error) {
	err := c.copyFileWithRetry(src, dst)
	if err == nil {
		return dst, nil
	}
	if !errors.Is(err, ErrExists) {
		return dst, err
	}

	// 碰撞：先比较大小
	srcInfo, sErr := os.Stat(src)
	dstInfo, dErr := os.Stat(dst)
	if sErr == nil && dErr == nil && srcInfo.Size() == dstInfo.Size() {
		// 大小相同，确实应该跳过
		return dst, ErrExists
	}

	// 大小不同（或 stat 失败）：在文件名后追加 "_" 重试
	dir, base, ext := splitName(dst)
	for i := 0; i < maxCollisionRetries; i++ {
		base += "_"
		candidate := filepath.Join(dir, base+ext)
		err := c.copyFileWithRetry(src, candidate)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, ErrExists) {
			return candidate, err
		}
		// 继续加下划线重试
	}
	return dst, fmt.Errorf("too many collisions renaming %s (tried %d times)", dst, maxCollisionRetries)
}

// splitName 把 path 拆成 (dir, basename, extension)。
// extension 包含前导点（如 ".jpg"）；dir 与 base 都不含扩展名。
func splitName(path string) (dir, base, ext string) {
	dir, file := filepath.Split(path)
	ext = filepath.Ext(file)
	base = file[:len(file)-len(ext)]
	return
}

// copyOp 是单次拷贝的抽象，便于测试时注入失败/成功序列。
type copyOp func(src, dst string) error

// copyFileWithRetryOp 是实际的重试实现；生产代码通过 copyFileWithRetry 调用。
// 测试代码可以传入自定义 copyOp 模拟失败序列。
func (c *Collator) copyFileWithRetryOp(src, dst string, op copyOp) error {
	var (
		backoff = initialRetryDelay
		lastErr error
	)

	for attempt := 1; attempt <= maxCopyAttempts; attempt++ {
		if attempt > 1 {
			c.logger.Warn("retry copy",
				"src", src,
				"dst", dst,
				"attempt", attempt,
				"backoff_ms", backoff.Milliseconds(),
				"last_err", lastErr.Error(),
			)
			time.Sleep(backoff)
			backoff *= 2
		}

		err := op(src, dst)
		if err == nil {
			if attempt > 1 {
				c.logger.Info("copy succeeded after retry",
					"src", src, "dst", dst, "attempts", attempt)
			}
			return nil
		}

		lastErr = err
		if !isRetryableCopyErr(err) {
			return err
		}
	}

	return fmt.Errorf("after %d attempts: %w", maxCopyAttempts, lastErr)
}

// isRetryableCopyErr 判断拷贝错误是否值得重试。
//
// 判定标准（保守）：只有典型的瞬时 I/O 错误才重试，避免把权限/路径问题拖慢。
func isRetryableCopyErr(err error) bool {
	if err == nil {
		return false
	}
	// 一次性硬错误
	if errors.Is(err, ErrExists) ||
		errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, os.ErrPermission) {
		return false
	}
	// 典型的瞬时/驱动层错误
	if errors.Is(err, syscall.EIO) ||
		errors.Is(err, syscall.EBUSY) ||
		errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// 兜底：拷贝阶段（即 io.Copy 路径）的错误一律视为可重试，
	// 因为 copyFile 自己产生的 copy / flush 错误通常与磁盘瞬时状态相关。
	msg := err.Error()
	if containsAny(msg, "copy:", "flush:", "input/output error") {
		return true
	}
	return false
}

// containsAny 判断 s 是否包含任一 substr。
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) <= len(s) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// ensureDir 确保目录存在，目录已存在视为成功。
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// 并发场景下另一个协程可能已经创建，再用 Stat 兜底确认。
		if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
			return nil
		}
		return err
	}
	return nil
}