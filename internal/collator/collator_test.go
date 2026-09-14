package collator

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"photo_collation/internal/exif"
)

// makeJpeg 在 path 生成一个最小 JPEG（不带 EXIF）。
func makeJpeg(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{64, 64, 64, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// makeJpegWithExif 在 path 生成带 DateTimeOriginal/DateTime 的 JPEG。
func makeJpegWithExif(t *testing.T, path string, dt time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{128, 128, 128, 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	jpegBytes := raw.Bytes()

	const (
		tagDateTimeOriginal = 0x9003
		tagDateTime         = 0x0132
		count               = uint16(20)
	)

	buf := &bytes.Buffer{}
	buf.WriteByte(0x49)
	buf.WriteByte(0x49)
	binary.Write(buf, binary.LittleEndian, uint16(0x002A))
	binary.Write(buf, binary.LittleEndian, uint32(8))
	binary.Write(buf, binary.LittleEndian, uint16(2))
	valueOffset := uint32(8 + 2 + 2*12 + 4)

	binary.Write(buf, binary.LittleEndian, uint16(tagDateTime))
	binary.Write(buf, binary.LittleEndian, uint16(2))
	binary.Write(buf, binary.LittleEndian, uint32(count))
	binary.Write(buf, binary.LittleEndian, valueOffset)

	binary.Write(buf, binary.LittleEndian, uint16(tagDateTimeOriginal))
	binary.Write(buf, binary.LittleEndian, uint16(2))
	binary.Write(buf, binary.LittleEndian, uint32(count))
	binary.Write(buf, binary.LittleEndian, valueOffset)

	binary.Write(buf, binary.LittleEndian, uint32(0))

	timeStr := []byte(dt.Format("2006:01:02 15:04:05") + "\x00")
	padded := make([]byte, count)
	copy(padded, timeStr)
	buf.Write(padded)
	buf.Write(padded)

	exifHeader := []byte{'E', 'x', 'i', 'f', 0x00, 0x00}
	tiffBytes := buf.Bytes()

	var out bytes.Buffer
	out.WriteByte(0xFF)
	out.WriteByte(0xE1)
	segLen := uint16(2 + len(exifHeader) + len(tiffBytes))
	binary.Write(&out, binary.BigEndian, segLen)
	out.Write(exifHeader)
	out.Write(tiffBytes)
	out.Write(jpegBytes[2:])

	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollator_Run_Full(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")

	// 构造测试场景：
	//   src/a/2024-10-15_001.jpg (EXIF 2024-10-15)        → dst/p2024/m10/
	//   src/a/2024-10-15_002.jpg (EXIF 2024-10-15)        → dst/p2024/m10/
	//   src/b/c/2024-11-20.jpg   (EXIF 2024-11-20)        → dst/p2024/m11/
	//   src/screenshot.jpg        (no EXIF)                → dst/Noexif/
	//   src/.hidden/hidden.jpg    (EXIF, 但隐藏目录跳过)    → 不归类
	//   src/readme.txt                                    → 不处理
	dt1 := time.Date(2024, 10, 15, 10, 0, 0, 0, time.Local)
	dt2 := time.Date(2024, 11, 20, 14, 30, 0, 0, time.Local)

	makeJpegWithExif(t, filepath.Join(src, "a", "2024-10-15_001.jpg"), dt1)
	makeJpegWithExif(t, filepath.Join(src, "a", "2024-10-15_002.jpg"), dt1)
	makeJpegWithExif(t, filepath.Join(src, "b", "c", "2024-11-20.jpg"), dt2)
	makeJpeg(t, filepath.Join(src, "screenshot.jpg"))
	makeJpegWithExif(t, filepath.Join(src, ".hidden", "hidden.jpg"), dt1)
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := New(Options{Src: src, Dst: dst}, exif.NewReader())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 期望：scanned=4 (5 张 jpg - 1 张 .hidden)
	if stats.Scanned != 4 {
		t.Errorf("Scanned: got %d, want 4", stats.Scanned)
	}
	// 期望：classified=3 (两张 dt1 + 一张 dt2)
	if stats.Classified != 3 {
		t.Errorf("Classified: got %d, want 3", stats.Classified)
	}
	if stats.NoExif != 1 {
		t.Errorf("NoExif: got %d, want 1", stats.NoExif)
	}
	if stats.Failed != 0 {
		t.Errorf("Failed: got %d, want 0", stats.Failed)
	}

	// 验证文件分布：pYYYY/mMM 嵌套结构
	for _, p := range []string{
		filepath.Join(dst, "p2024", "m10", "2024-10-15_001.jpg"),
		filepath.Join(dst, "p2024", "m10", "2024-10-15_002.jpg"),
		filepath.Join(dst, "p2024", "m11", "2024-11-20.jpg"),
		filepath.Join(dst, "Noexif", "screenshot.jpg"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s, got %v", p, err)
		}
	}
}

func TestCollator_Run_SkipExisting(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	dt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)

	makeJpegWithExif(t, filepath.Join(src, "a.jpg"), dt)

	c, err := New(Options{Src: src, Dst: dst}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}

	// 第一次：成功
	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Classified != 1 {
		t.Fatalf("first run classified: got %d, want 1", stats.Classified)
	}

	// 第二次：目标已存在且大小相同，全部 skipped
	stats, err = c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skipped != 1 {
		t.Errorf("second run skipped: got %d, want 1", stats.Skipped)
	}
	if stats.Classified != 0 {
		t.Errorf("second run classified: got %d, want 0", stats.Classified)
	}
}

// TestCollator_Run_RenameOnDifferentSize 验证目标已存在但大小不同时自动重命名。
func TestCollator_Run_RenameOnDifferentSize(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	dt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)

	// 第一轮：建一个稍大的目标
	makeJpegWithExif(t, src+"/a.jpg", dt)
	c, err := New(Options{Src: src, Dst: dst}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 拿目标文件，改大一点
	target := filepath.Join(dst, "p2025", "m01", "a.jpg")
	orig, _ := os.ReadFile(target)
	if err := os.WriteFile(target, append(orig, []byte("extra pad to differ")...), 0o644); err != nil {
		t.Fatal(err)
	}
	origSize := int64(len(orig) + len("extra pad to differ"))

	// 第二轮：源文件（更小）再次跑
	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Renamed != 1 {
		t.Errorf("Renamed: got %d, want 1", stats.Renamed)
	}
	if stats.Skipped != 0 {
		t.Errorf("Skipped: got %d, want 0 (different size, should rename)", stats.Skipped)
	}
	// 验证重命名后的新文件存在
	renamed := filepath.Join(dst, "p2025", "m01", "a_.jpg")
	if _, err := os.Stat(renamed); err != nil {
		t.Errorf("renamed file not found: %v", err)
	}
	// 原文件未被覆盖
	curSize := mustFileSize(t, target)
	if curSize != origSize {
		t.Errorf("original file should be untouched, size %d != %d", curSize, origSize)
	}
}

func mustFileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func TestCollator_DryRun(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	dt := time.Date(2025, 3, 1, 0, 0, 0, 0, time.Local)

	makeJpegWithExif(t, filepath.Join(src, "a.jpg"), dt)

	c, err := New(Options{Src: src, Dst: dst, DryRun: true}, exif.NewReader())
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
	// 目标目录不应有文件
	if _, err := os.Stat(filepath.Join(dst, "p2025", "m03", "a.jpg")); !os.IsNotExist(err) {
		t.Errorf("expected file not exist in dry-run, got %v", err)
	}
}

func TestCollator_SrcDstSamePath(t *testing.T) {
	root := t.TempDir()
	_, err := New(Options{Src: root, Dst: root}, exif.NewReader())
	if err == nil {
		t.Fatal("expected error for src == dst")
	}
}

func TestCollator_SrcNotExist(t *testing.T) {
	_, err := New(Options{
		Src: "/this/path/does/not/exist",
		Dst: "/tmp/dst",
	}, exif.NewReader())
	if err == nil {
		t.Fatal("expected error for missing src")
	}
}

func TestCollator_EmptySrc(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	c, err := New(Options{Src: src, Dst: dst}, exif.NewReader())
	if err != nil {
		t.Fatal(err)
	}
	stats, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Scanned != 0 {
		t.Errorf("Scanned: got %d, want 0", stats.Scanned)
	}
}

func TestIsPhotoFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"a.jpg", true},
		{"a.JPG", true},
		{"a.Jpeg", true},
		{"a.JPEG", true},
		{"a.png", false},
		{"a.heic", false},
		{"a.txt", false},
		{"a", false},
		// macOS 资源叉文件
		{"._DSC0001.JPG", false},
		{"._a.jpg", false},
		// 容易混淆的：含 ._ 但不以 ._ 开头
		{"a._b.jpg", true},
	}
	for _, tc := range cases {
		if got := isPhotoFile(tc.name); got != tc.want {
			t.Errorf("isPhotoFile(%q): got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestShouldSkipDir(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{".git", true},
		{".hidden", true},
		{"@eaDir", true},
		{"#recycle", true},
		{"normal", false},
		{"photos", false},
		// 自身输出目录
		{"Noexif", true},
		{"p2026", true},
		{"p2018", true},
		{"p9999", true},
		{"m01", true},
		{"m10", true},
		{"m99", true},
		// p/m 前缀但不是 pYYYY/mMM 形式
		{"p202", false},  // 3 位数字
		{"p20260", false}, // 5 位数字
		{"pabcd", false},  // 非数字
		{"m1", false},     // 1 位数字
		{"m100", false},   // 3 位数字
		{"photos", false}, // 普通目录
	}
	for _, tc := range cases {
		if got := shouldSkipDir(tc.name); got != tc.want {
			t.Errorf("shouldSkipDir(%q): got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestYearMonthDir(t *testing.T) {
	dst := "/tmp/Sorted"
	dt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	got := yearMonthDir(dst, dt)
	want := filepath.Join(dst, "p2026", "m10")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// 一月也要零填充到 m01
	dt2 := time.Date(2024, 1, 31, 23, 59, 59, 0, time.Local)
	got2 := yearMonthDir(dst, dt2)
	want2 := filepath.Join(dst, "p2024", "m01")
	if got2 != want2 {
		t.Errorf("got %s, want %s", got2, want2)
	}
}