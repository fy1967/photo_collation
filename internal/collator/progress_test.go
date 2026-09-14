package collator

import (
	"context"
	"sync"
	"testing"
	"time"

	"photo_collation/internal/exif"
)

// TestOnProgress_CalledOncePerFile 验证每个文件处理完都触发一次回调。
func TestOnProgress_CalledOncePerFile(t *testing.T) {
	root := t.TempDir()
	src := root + "/src"
	dst := root + "/dst"

	dt := time.Date(2024, 10, 15, 10, 0, 0, 0, time.Local)
	makeJpegWithExif(t, src+"/a.jpg", dt)
	makeJpegWithExif(t, src+"/b.jpg", dt)
	makeJpeg(t, src+"/c.jpg") // 无 EXIF

	var (
		mu     sync.Mutex
		events []FileEvent
	)
	onProgress := func(ev FileEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}

	c, err := New(Options{
		Src:        src,
		Dst:        dst,
		OnProgress: onProgress,
	}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	// 期望事件：1 个 PreScanDone + 3 个 per-file
	var preDone *FileEvent
	var copied, noexif int
	for i := range events {
		ev := &events[i]
		switch ev.Outcome {
		case OutcomePreScanDone:
			preDone = ev
		case OutcomeCopied:
			copied++
			if ev.Dst == "" {
				t.Error("copied event missing Dst")
			}
		case OutcomeNoExif:
			noexif++
		}
	}

	if preDone == nil {
		t.Error("missing OutcomePreScanDone event")
	} else if preDone.Total != 3 {
		t.Errorf("PreScanDone.Total: got %d, want 3", preDone.Total)
	}
	if copied != 2 {
		t.Errorf("expected 2 OutcomeCopied, got %d", copied)
	}
	if noexif != 1 {
		t.Errorf("expected 1 OutcomeNoExif, got %d", noexif)
	}
}

// TestOnProgress_PanicRecovered 验证回调 panic 不会拖垮 collator。
func TestOnProgress_PanicRecovered(t *testing.T) {
	root := t.TempDir()
	src := root + "/src"
	dst := root + "/dst"

	dt := time.Date(2024, 10, 15, 10, 0, 0, 0, time.Local)
	makeJpegWithExif(t, src+"/a.jpg", dt)
	makeJpegWithExif(t, src+"/b.jpg", dt)

	calls := 0
	onProgress := func(ev FileEvent) {
		calls++
		panic("simulated GUI bug")
	}

	c, err := New(Options{
		Src:        src,
		Dst:        dst,
		OnProgress: onProgress,
	}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}

	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error after panics: %v", err)
	}
	if stats.Classified != 2 {
		t.Errorf("expected 2 classified (panics shouldn't break flow), got %d", stats.Classified)
	}
	// 期望：1 次 PreScanDone + 2 次 per-file = 3 次调用
	if calls != 3 {
		t.Errorf("expected callback called 3 times, got %d", calls)
	}
}

// TestOnProgress_NilSafe 验证不传回调时不影响运行。
func TestOnProgress_NilSafe(t *testing.T) {
	root := t.TempDir()
	src := root + "/src"
	dst := root + "/dst"
	dt := time.Date(2024, 10, 15, 10, 0, 0, 0, time.Local)
	makeJpegWithExif(t, src+"/a.jpg", dt)

	c, err := New(Options{Src: src, Dst: dst}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}
	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Classified != 1 {
		t.Errorf("expected 1 classified, got %d", stats.Classified)
	}
}