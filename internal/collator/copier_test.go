package collator

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// newCollatorForTest 构造一个带静默 logger 的 Collator（避免测试时日志噪音）。
func newCollatorForTest(t *testing.T) *Collator {
	t.Helper()
	// 我们只需用到 logger 字段，跳过 New 的参数校验，自己直接组装。
	return &Collator{
		opts:   Options{},
		logger: testLogger(t),
	}
}

func TestIsRetryableCopyErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ErrExists", ErrExists, false},
		{"ErrNotExist", fs.ErrNotExist, false}, // 等价 os.ErrNotExist
		{"ErrPermission", fs.ErrPermission, false},
		{"EIO", syscall.EIO, true},
		{"EBUSY", syscall.EBUSY, true},
		{"ETIMEDOUT", syscall.ETIMEDOUT, true},
		{"io.EOF", io.EOF, true},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"copy: read ... I/O error", errors.New("copy: read x.jpg: input/output error"), true},
		// 注意："flush: broken pipe" 含 "flush:" 子串，会被判定为可重试；
		// 这是有意的——flush 阶段出错基本也是磁盘瞬时问题。
		{"random error", errors.New("something else"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableCopyErr(tc.err); got != tc.want {
				t.Errorf("isRetryableCopyErr(%v): got %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestCopyFileWithRetry_SucceedsFirstTry 验证成功路径不重试。
func TestCopyFileWithRetry_SucceedsFirstTry(t *testing.T) {
	c := newCollatorForTest(t)
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst.txt")

	calls := 0
	op := func(s, d string) error {
		calls++
		return c.copyFile(s, d)
	}

	if err := c.copyFileWithRetryOp(src, dst, op); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("dst content: got %q, want %q", data, "hello")
	}
}

// TestCopyFileWithRetry_SucceedsAfterRetry 验证前两次失败、第三次成功时返回 nil。
func TestCopyFileWithRetry_SucceedsAfterRetry(t *testing.T) {
	c := newCollatorForTest(t)
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst.txt")

	calls := 0
	op := func(s, d string) error {
		calls++
		if calls < 3 {
			return syscall.EIO
		}
		return c.copyFile(s, d)
	}

	start := time.Now()
	if err := c.copyFileWithRetryOp(src, dst, op); err != nil {
		t.Fatalf("expected nil after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
	// 至少经历了 200ms + 400ms = 600ms 退避
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Errorf("expected at least 500ms backoff, got %v", elapsed)
	}
}

// TestCopyFileWithRetry_AllAttemptsFail 验证持续失败时返回 wrapped error。
func TestCopyFileWithRetry_AllAttemptsFail(t *testing.T) {
	c := newCollatorForTest(t)
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst.txt")

	calls := 0
	op := func(s, d string) error {
		calls++
		return syscall.EIO
	}

	err := c.copyFileWithRetryOp(src, dst, op)
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if calls != maxCopyAttempts {
		t.Errorf("expected %d calls, got %d", maxCopyAttempts, calls)
	}
	if !errors.Is(err, syscall.EIO) {
		t.Errorf("expected wrapped EIO, got %v", err)
	}
}

// TestCopyFileWithRetry_NonRetryableErrorStops 验证不可重试错误立即返回。
func TestCopyFileWithRetry_NonRetryableErrorStops(t *testing.T) {
	c := newCollatorForTest(t)
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst.txt")

	calls := 0
	op := func(s, d string) error {
		calls++
		return os.ErrPermission
	}

	err := c.copyFileWithRetryOp(src, dst, op)
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("expected ErrPermission, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry), got %d", calls)
	}
}

// TestCopyFileWithRetry_ErrExistsStops 验证目标已存在立即返回。
func TestCopyFileWithRetry_ErrExistsStops(t *testing.T) {
	c := newCollatorForTest(t)
	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst.txt")
	if err := os.WriteFile(dst, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	op := func(s, d string) error {
		calls++
		return c.copyFile(s, d)
	}

	err := c.copyFileWithRetryOp(src, dst, op)
	if !errors.Is(err, ErrExists) {
		t.Errorf("expected ErrExists, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry on exists), got %d", calls)
	}
}

// TestCopyFile_RealFileIO 真实文件 I/O 验证 copyFile + O_EXCL 行为。
func TestCopyFile_RealFileIO(t *testing.T) {
	c := newCollatorForTest(t)

	src := filepath.Join(t.TempDir(), "src.bin")
	content := []byte("hello world\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst.bin")

	// 首次成功
	if err := c.copyFile(src, dst); err != nil {
		t.Fatalf("first copy: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != string(content) {
		t.Errorf("content mismatch")
	}

	// 第二次应返回 ErrExists
	err := c.copyFile(src, dst)
	if !errors.Is(err, ErrExists) {
		t.Errorf("expected ErrExists on second copy, got %v", err)
	}
}

// TestCopyFileWithCollision_NoCollision 验证无碰撞时直接拷贝。
func TestCopyFileWithCollision_NoCollision(t *testing.T) {
	c := newCollatorForTest(t)
	src := filepath.Join(t.TempDir(), "src.bin")
	dst := filepath.Join(t.TempDir(), "dst.bin")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	final, err := c.copyFileWithCollision(src, dst)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if final != dst {
		t.Errorf("expected final == dst, got %q", final)
	}
}

// TestCopyFileWithCollision_SameSizeSkip 验证大小相同时返回 ErrExists。
func TestCopyFileWithCollision_SameSizeSkip(t *testing.T) {
	c := newCollatorForTest(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	content := []byte("same content")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		t.Fatal(err)
	}

	final, err := c.copyFileWithCollision(src, dst)
	if !errors.Is(err, ErrExists) {
		t.Errorf("expected ErrExists, got %v", err)
	}
	if final != dst {
		t.Errorf("expected final == dst, got %q", final)
	}

	// 原文件未被改动
	got, _ := os.ReadFile(dst)
	if string(got) != string(content) {
		t.Errorf("existing file should not be modified")
	}
}

// TestCopyFileWithCollision_DifferentSizeRename 验证大小不同时自动加 _ 后缀。
func TestCopyFileWithCollision_DifferentSizeRename(t *testing.T) {
	c := newCollatorForTest(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	dst := filepath.Join(dir, "dst.jpg")
	dstRenamed := filepath.Join(dir, "dst_.jpg")

	if err := os.WriteFile(src, []byte("new bigger content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old smaller"), 0o644); err != nil {
		t.Fatal(err)
	}

	final, err := c.copyFileWithCollision(src, dst)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if final != dstRenamed {
		t.Errorf("expected final == %q, got %q", dstRenamed, final)
	}

	// 验证新文件内容
	got, err := os.ReadFile(dstRenamed)
	if err != nil {
		t.Fatalf("read renamed: %v", err)
	}
	if string(got) != "new bigger content" {
		t.Errorf("content mismatch: got %q", got)
	}

	// 原文件未被覆盖
	orig, _ := os.ReadFile(dst)
	if string(orig) != "old smaller" {
		t.Errorf("original dst should not be overwritten, got %q", orig)
	}
}

// TestCopyFileWithCollision_MultipleRenames 验证多次碰撞叠加多个下划线。
func TestCopyFileWithCollision_MultipleRenames(t *testing.T) {
	c := newCollatorForTest(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	dst := filepath.Join(dir, "f.jpg")
	dst1 := filepath.Join(dir, "f_.jpg")
	dst2 := filepath.Join(dir, "f__.jpg")

	// 源文件
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 占用前两个候选名（大小不同 → 触发继续找）
	if err := os.WriteFile(dst, []byte("orig 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst1, []byte("orig 2 - diff size"), 0o644); err != nil {
		t.Fatal(err)
	}

	final, err := c.copyFileWithCollision(src, dst)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if final != dst2 {
		t.Errorf("expected final == %q, got %q", dst2, final)
	}

	// 验证内容
	got, _ := os.ReadFile(dst2)
	if string(got) != "new" {
		t.Errorf("content mismatch: got %q", got)
	}
}

// TestCopyFileWithCollision_SrcSmallerDstLarger 大小差异方向不影响行为。
func TestCopyFileWithCollision_SrcSmallerDstLarger(t *testing.T) {
	c := newCollatorForTest(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	dst := filepath.Join(dir, "dst.jpg")

	if err := os.WriteFile(src, []byte("small"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("much larger content here"), 0o644); err != nil {
		t.Fatal(err)
	}

	final, err := c.copyFileWithCollision(src, dst)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	expected := filepath.Join(dir, "dst_.jpg")
	if final != expected {
		t.Errorf("expected %q, got %q", expected, final)
	}
}