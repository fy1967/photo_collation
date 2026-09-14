// Package exif 提供对 JPEG 文件 EXIF 拍摄时间的统一读取接口。
//
// 设计目标：
//   - 屏蔽 goexif 库的细节，对外只暴露 (time.Time, bool, error)
//   - 优先 DateTimeOriginal，回退 DateTime
//   - 用 errors.Is 区分 "无 EXIF" 与 "时间字段无效" 与 "I/O 错误"
package exif

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	goexif "github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
)

// 错误定义。调用方通过 errors.Is 区分。
var (
	// ErrNoExif 文件完全不含 EXIF 信息（或不是 JPEG）。
	ErrNoExif = errors.New("exif: no EXIF data")

	// ErrInvalidTime EXIF 存在但拍摄时间字段缺失或格式异常。
	ErrInvalidTime = errors.New("exif: invalid DateTime field")
)

// Reader 读取 JPEG EXIF 中的拍摄时间。
//
// 当前实现无状态、可安全并发使用。
type Reader struct{}

// NewReader 构造一个 Reader。
func NewReader() *Reader {
	return &Reader{}
}

// ReadTime 从 path 指向的 JPEG 文件读取拍摄时间。
//
// 返回值：
//   - t:           解析得到的本地时间
//   - isOriginal:  是否取自 DateTimeOriginal；true=原始拍摄，false=取自 DateTime
//   - err:         nil / ErrNoExif / ErrInvalidTime / 其他 I/O 错误
func (r *Reader) ReadTime(path string) (t time.Time, isOriginal bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	x, err := goexif.Decode(f)
	if err != nil {
		// goexif 不暴露 ErrNoExif 哨兵错误，靠错误字符串识别"无 EXIF marker"。
		if isNoExifErr(err) {
			return time.Time{}, false, ErrNoExif
		}
		return time.Time{}, false, fmt.Errorf("decode exif: %w", err)
	}

	// 优先 DateTimeOriginal。
	if tag, terr := x.Get(goexif.DateTimeOriginal); terr == nil {
		if parsed, perr := parseTagTime(tag); perr == nil {
			return parsed, true, nil
		}
		// DateTimeOriginal 格式异常，继续尝试 DateTime。
	}

	// 回退到 DateTime。
	tag, err := x.Get(goexif.DateTime)
	if err != nil {
		if goexif.IsTagNotPresentError(err) {
			return time.Time{}, false, ErrInvalidTime
		}
		return time.Time{}, false, fmt.Errorf("get DateTime: %w", err)
	}

	parsed, err := parseTagTime(tag)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%w: %v", ErrInvalidTime, err)
	}
	return parsed, false, nil
}

// isNoExifErr 判断 goexif.Decode 返回的错误是否表示"无 EXIF"。
//
// goexif 在不同情形下返回不同错误：
//   - "exif: failed to find exif intro marker":  文件不含 APP1 段
//   - EOF:                                       文件太短
//   - 其他读到底的错误
func isNoExifErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "failed to find exif intro marker") ||
		strings.Contains(msg, "no exif data") ||
		strings.Contains(msg, "EOF")
}

// parseTagTime 把 EXIF tag 里的字符串值按 "2006:01:02 15:04:05" 解析为 time.Time。
func parseTagTime(tag *tiff.Tag) (time.Time, error) {
	if tag.Format() != tiff.StringVal {
		return time.Time{}, errors.New("DateTime tag is not in string format")
	}
	dateStr := strings.TrimRight(string(tag.Val), "\x00")
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return time.Time{}, errors.New("DateTime tag is empty")
	}
	return time.ParseInLocation("2006:01:02 15:04:05", dateStr, time.Local)
}