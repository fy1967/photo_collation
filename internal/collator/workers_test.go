package collator

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"photo_collation/internal/exif"
)

// TestWorkers_ConcurrentProcessing 验证 Workers>1 时结果正确且并发执行。
func TestWorkers_ConcurrentProcessing(t *testing.T) {
	root := t.TempDir()
	src := root + "/src"
	dst := root + "/dst"

	dt := time.Date(2024, 10, 15, 10, 0, 0, 0, time.Local)
	const N = 20
	for i := 0; i < N; i++ {
		makeJpegWithExif(t, src+"/file_"+itoa(i)+".jpg", dt)
	}

	// 用并发安全计数器确认回调并发触发。
	var (
		concurrent   int64
		maxConcurrent int64
	)
	onProgress := func(ev FileEvent) {
		cur := atomic.AddInt64(&concurrent, 1)
		for {
			prev := atomic.LoadInt64(&maxConcurrent)
			if cur <= prev || atomic.CompareAndSwapInt64(&maxConcurrent, prev, cur) {
				break
			}
		}
		// 模拟 GUI 处理耗时
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt64(&concurrent, -1)
	}

	c, err := New(Options{
		Src:        src,
		Dst:        dst,
		Workers:    4,
		OnProgress: onProgress,
	}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}

	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Classified != N {
		t.Errorf("classified: got %d, want %d", stats.Classified, N)
	}
	if maxConcurrent < 2 {
		t.Errorf("expected concurrent execution (max>=2), got %d", maxConcurrent)
	}
	t.Logf("max concurrent OnProgress calls: %d", maxConcurrent)
}

// TestWorkers_1SameAsNoWorkers 验证 Workers=1 与默认行为一致。
func TestWorkers_1SameAsNoWorkers(t *testing.T) {
	root := t.TempDir()
	src := root + "/src"
	dst := root + "/dst"
	dt := time.Date(2024, 10, 15, 10, 0, 0, 0, time.Local)
	makeJpegWithExif(t, src+"/a.jpg", dt)

	c, err := New(Options{Src: src, Dst: dst, Workers: 1}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}
	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Classified != 1 {
		t.Errorf("classified: got %d, want 1", stats.Classified)
	}
}

// TestWorkers_HighConcurrencySafety 高并发下结果正确性。
func TestWorkers_HighConcurrencySafety(t *testing.T) {
	root := t.TempDir()
	src := root + "/src"
	dst := root + "/dst"

	dt := time.Date(2024, 10, 15, 10, 0, 0, 0, time.Local)
	const N = 100
	for i := 0; i < N; i++ {
		makeJpegWithExif(t, src+"/f"+itoa(i)+".jpg", dt)
	}

	var mu sync.Mutex
	var events []FileEvent
	onProgress := func(ev FileEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}

	c, err := New(Options{
		Src:        src,
		Dst:        dst,
		Workers:    16,
		OnProgress: onProgress,
	}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}
	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != N {
		t.Errorf("Total: got %d, want %d", stats.Total, N)
	}
	if stats.Classified != N {
		t.Errorf("classified: got %d, want %d", stats.Classified, N)
	}
	// 1 PreScanDone + N per-file
	if len(events) != N+1 {
		t.Errorf("event count: got %d, want %d", len(events), N+1)
	}
}

// itoa 避免引入 strconv 依赖混淆测试代码。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}