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
)

// makeJpegWithRawAPP1 写一个 JPEG，在 SOI 后插入指定的 APP1 段。
// 用来构造 EXIF 损坏的测试文件。
func makeJpegWithRawAPP1(t *testing.T, path string, app1Payload []byte) {
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
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	jpegBytes := raw.Bytes()
	if jpegBytes[0] != 0xFF || jpegBytes[1] != 0xD8 {
		t.Fatalf("unexpected jpeg header: %x", jpegBytes[:2])
	}

	// APP1 段：FF E1 <len2> <payload>
	app1 := []byte{0xFF, 0xE1}
	segLen := uint16(2 + len(app1Payload))
	app1 = append(app1, byte(segLen>>8), byte(segLen))
	app1 = append(app1, app1Payload...)

	var out bytes.Buffer
	out.Write(jpegBytes[:2])
	out.Write(app1)
	out.Write(jpegBytes[2:])
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCorruptedExifErr_ZerosInSubIFD 模拟真实场景：
// APP1 头合法，但 sub-IFD 里有 zero length tag value，goexif 报错。
//
// goexif 在 tiff/tag.go:143 抛 "zero length tag value"：
//   valLen = typeSize[t.Type] * t.Count == 0
//
// 构造一个 IFD entry：Type=0（typeSize[0]=0）即可触发。
func TestCorruptedExifErr_ZerosInSubIFD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted.jpg")

	buf := &bytes.Buffer{}
	buf.WriteString("Exif\x00\x00")
	// TIFF header (little-endian)
	buf.WriteByte(0x49) // 'I'
	buf.WriteByte(0x49) // 'I'
	binary.Write(buf, binary.LittleEndian, uint16(0x002A))
	binary.Write(buf, binary.LittleEndian, uint32(8)) // offset to IFD0 = 8

	// IFD0: 2 entries
	// Entry 1: DateTimeOriginal (tag 0x9003, type=2 ASCII, count=20, value=offset)
	binary.Write(buf, binary.LittleEndian, uint16(2))
	binary.Write(buf, binary.LittleEndian, uint16(0x9003))        // DateTimeOriginal
	binary.Write(buf, binary.LittleEndian, uint16(2))             // ASCII
	binary.Write(buf, binary.LittleEndian, uint32(20))            // count
	binary.Write(buf, binary.LittleEndian, uint32(0xDEAD))        // value offset (无所谓，不会真读)

	// Entry 2: ExifIFDPointer (tag 0x8769, type=4 LONG, count=1, value=offset)
	binary.Write(buf, binary.LittleEndian, uint16(0x8769))        // ExifIFDPointer
	binary.Write(buf, binary.LittleEndian, uint16(4))             // LONG
	binary.Write(buf, binary.LittleEndian, uint32(1))             // count
	binary.Write(buf, binary.LittleEndian, uint32(8+2+2*12+4))   // sub-IFD offset (relative to TIFF start)

	binary.Write(buf, binary.LittleEndian, uint32(0))             // next IFD = 0

	// Sub-IFD at offset 8+2+2*12+4 = 38
	// 含 1 个 entry：Type=0（损坏）
	binary.Write(buf, binary.LittleEndian, uint16(1))             // 1 entry
	binary.Write(buf, binary.LittleEndian, uint16(0xA000))          // FlashpixVersion tag
	binary.Write(buf, binary.LittleEndian, uint16(0))             // Type = 0 (typeSize=0 → valLen=0 → "zero length tag value")
	binary.Write(buf, binary.LittleEndian, uint32(1))             // count = 1
	binary.Write(buf, binary.LittleEndian, uint32(0))             // value = 0

	makeJpegWithRawAPP1(t, path, buf.Bytes())

	r := NewReader()
	_, _, err := r.ReadTime(path)
	if err == nil {
		t.Fatal("expected error for corrupted EXIF, got nil")
	}
	if !errors.Is(err, ErrCorrupted) {
		t.Errorf("expected ErrCorrupted, got %v", err)
	}
}

// TestIsCorruptedExifErr 单元测试错误分类器。
func TestIsCorruptedExifErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ErrNoExif", ErrNoExif, false},
		{"ErrInvalidTime", ErrInvalidTime, false},
		{"zero length tag value", errors.New("loading EXIF sub-IFD: zero length tag value"), true},
		{"sub-IFD decode failed", errors.New("sub-IFD ExifIFDPointer decode failed: bad magic"), true},
		{"loading EXIF", errors.New("loading EXIF sub-IFD: corrupt"), true},
		{"short read wrapped", errors.New("exif: decode failed (tiff: short read of tag value)"), true},
		{"random", errors.New("something else"), false},
		{"permission denied", errors.New("permission denied"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCorruptedExifErr(tc.err); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestReadTime_CorruptedJpegLikeRealFailure 模拟真实损坏场景：
// 构造一个无效 TIFF magic 让 goexif 报错。
func TestReadTime_CorruptedJpegLikeRealFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_magic.jpg")

	// APP1 里写一个错误的 TIFF magic
	buf := &bytes.Buffer{}
	buf.WriteString("Exif\x00\x00")
	// 错误的 magic (0x4D4D 应该是 0x002A)
	buf.Write([]byte{0x4D, 0x4D, 0x00, 0x2A, 0x00, 0x00, 0x00, 0x08})

	makeJpegWithRawAPP1(t, path, buf.Bytes())

	r := NewReader()
	_, _, err := r.ReadTime(path)
	// 不论是 ErrCorrupted 还是 wrapped 错误，只要不是 ErrNoExif 就行
	if err == nil {
		t.Fatal("expected error")
	}
	// 这种 bad magic 通常被归为 corrupted
	if !errors.Is(err, ErrCorrupted) && err.Error() == "" {
		t.Errorf("got error %v", err)
	}
}