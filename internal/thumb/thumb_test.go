package thumb

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// makeBigJpeg 生成指定尺寸的纯色 JPEG，用于测试缩略图。
func makeBigJpeg(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	c := color.RGBA{uint8(w % 256), uint8(h % 256), 128, 255}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerator_ScalesDown(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.jpg")
	makeBigJpeg(t, src, 800, 600)

	g := New(80)
	img, err := g.Generate(src)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b := img.Bounds()
	// 横向较长：宽度应为 80，高度按比例 60
	if b.Dx() != 80 {
		t.Errorf("width: got %d, want 80", b.Dx())
	}
	if b.Dy() != 60 {
		t.Errorf("height: got %d, want 60", b.Dy())
	}
}

func TestGenerator_NoScaleIfSmaller(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "small.jpg")
	makeBigJpeg(t, src, 40, 30)

	g := New(80)
	img, err := g.Generate(src)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 40 || b.Dy() != 30 {
		t.Errorf("size: got %dx%d, want 40x30", b.Dx(), b.Dy())
	}
}

func TestGenerator_FileNotFound(t *testing.T) {
	g := New(80)
	_, err := g.Generate("/nonexistent/file.jpg")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCache_HitMiss(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.jpg")
	makeBigJpeg(t, src, 200, 150)

	c := NewCache(New(80), 100)

	// 首次：miss
	img1, err := c.Get(src)
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 1 {
		t.Errorf("after first Get: cache len = %d, want 1", c.Len())
	}

	// 二次：hit（应当返回同一个对象）
	img2, err := c.Get(src)
	if err != nil {
		t.Fatal(err)
	}
	if img1 != img2 {
		t.Error("second Get should return cached image")
	}
}

func TestCache_Eviction(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(New(80), 3) // 最多 3 个

	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, "f"+string(rune('0'+i))+".jpg")
		makeBigJpeg(t, path, 200, 150)
		if _, err := c.Get(path); err != nil {
			t.Fatal(err)
		}
	}

	if c.Len() != 3 {
		t.Errorf("cache len: got %d, want 3 (LRU evicted older entries)", c.Len())
	}
}

func TestCache_LRUOrdering(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(New(80), 3)

	paths := []string{
		filepath.Join(dir, "a.jpg"),
		filepath.Join(dir, "b.jpg"),
		filepath.Join(dir, "c.jpg"),
	}
	for _, p := range paths {
		makeBigJpeg(t, p, 100, 100)
		c.Get(p) // 顺序：a → b → c
	}

	// 访问 a，让它成为最新
	c.Get(paths[0])

	// 新增 d，应淘汰 b（最久未访问）
	d := filepath.Join(dir, "d.jpg")
	makeBigJpeg(t, d, 100, 100)
	c.Get(d)

	if c.Len() != 3 {
		t.Errorf("cache len: got %d, want 3", c.Len())
	}
	// 验证 a 还在
	if _, ok := getCacheEntry(c, paths[0]); !ok {
		t.Error("path a should still be cached after recent access")
	}
	// 验证 b 被淘汰
	if _, ok := getCacheEntry(c, paths[1]); ok {
		t.Error("path b should have been evicted (LRU)")
	}
}

// getCacheEntry 反射访问 Cache.entries 验证键是否存在。
// 测试代码与生产代码的耦合，可接受。
func getCacheEntry(c *Cache, key string) (image.Image, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	return e.img, true
}

func TestCache_Concurrent(t *testing.T) {
	dir := t.TempDir()
	makeBigJpeg(t, filepath.Join(dir, "x.jpg"), 200, 150)

	c := NewCache(New(80), 10)
	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Get(filepath.Join(dir, "x.jpg"))
		}()
	}
	wg.Wait()
	if c.Len() != 1 {
		t.Errorf("after concurrent Gets: cache len = %d, want 1", c.Len())
	}
}

func TestCache_Clear(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.jpg")
	makeBigJpeg(t, src, 100, 100)

	c := NewCache(New(80), 10)
	c.Get(src)
	if c.Len() != 1 {
		t.Fatalf("setup: expected 1 entry, got %d", c.Len())
	}
	c.Clear()
	if c.Len() != 0 {
		t.Errorf("after Clear: got %d, want 0", c.Len())
	}
}

// 占位：使用 stdlib jpeg.Encode 不依赖 buildJpegWithExif，避免 thumb 包反向依赖 collator 测试工具。
var _ = binary.BigEndian
var _ = bytes.NewBuffer
