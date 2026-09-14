package exif

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildJpegWithExif 生成一个最小 JPEG，内嵌 DateTimeOriginal 和 DateTime。
func buildJpegWithExif(t *testing.T, dt time.Time) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jpg")

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			img.Set(x, y, color.RGBA{128, 128, 128, 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
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
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestReader_ReadTime_Success(t *testing.T) {
	want := time.Date(2024, 10, 15, 12, 34, 56, 0, time.Local)
	path := buildJpegWithExif(t, want)

	r := NewReader()
	got, isOriginal, err := r.ReadTime(path)
	if err != nil {
		t.Fatalf("ReadTime: %v", err)
	}
	if !isOriginal {
		t.Errorf("expected isOriginal=true (DateTimeOriginal present)")
	}
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReader_ReadTime_NoExif(t *testing.T) {
	// 创建一个不含 EXIF 的 JPEG（用 stdlib 直接写）
	dir := t.TempDir()
	path := filepath.Join(dir, "noexif.jpg")
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
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r := NewReader()
	_, _, err = r.ReadTime(path)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrNoExif) {
		t.Errorf("expected ErrNoExif, got %v", err)
	}
}

func TestReader_ReadTime_NonExistentFile(t *testing.T) {
	r := NewReader()
	_, _, err := r.ReadTime("/nonexistent/path/to/file.jpg")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 不应是 ErrNoExif，而是 I/O 错误
	if errors.Is(err, ErrNoExif) {
		t.Errorf("expected I/O error, got ErrNoExif")
	}
}