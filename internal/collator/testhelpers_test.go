package collator

import (
	"io"
	"log/slog"
	"testing"
)

// testLogger 返回一个丢弃所有输出的 slog logger，避免测试时日志噪音。
// 当 t 处于 -v 模式时改为输出到 stderr 便于调试。
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	var w io.Writer = io.Discard
	if testing.Verbose() {
		w = stderrWriter{}
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// stderrWriter 直接写到 stderr，避免循环依赖 testing 框架。
type stderrWriter struct{}

func (stderrWriter) Write(p []byte) (int, error) {
	// 测试时走 stderr，避免与被测代码的日志混在一起。
	// 使用底层 syscall 直接绕过 fmt，确保无锁。
	return stderrWrite(p)
}