//go:build unix

package collator

import "syscall"

// stderrWrite 直接写到 fd=2，绕开 log/slog 自身的锁。
// 用于测试 -v 模式下的日志输出。
func stderrWrite(p []byte) (int, error) {
	return syscall.Write(2, p)
}