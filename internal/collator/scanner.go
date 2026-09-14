package collator

import (
	"path/filepath"
	"regexp"
	"strings"
)

// skipDirs 整棵树都要跳过的目录名（精确匹配，大小写敏感）。
//
// 一些常见来源：
//   - @eaDir:   Synology NAS 缩略图目录
//   - #recycle: Synology NAS 回收站
//   - .git/.svn/.hg: 版本控制元数据
var skipDirs = map[string]struct{}{
	"@eaDir":   {},
	"#recycle": {},
	".git":     {},
	".svn":     {},
	".hg":      {},
}

// reYearDir 匹配 pYYYY 形式的年份目录（自身输出）。
var reYearDir = regexp.MustCompile(`^p\d{4}$`)

// reMonthDir 匹配 mMM 形式的月份目录（自身输出）。
var reMonthDir = regexp.MustCompile(`^m\d{2}$`)

// photoExts 受支持的图片扩展名（小写，含点）。
var photoExts = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
}

// shouldSkipDir 判断目录是否应整棵跳过。
// 规则：
//   - 名称以 '.' 开头（隐藏目录）
//   - 名称在 skipDirs 中（NAS 缩略图、版本控制等）
//   - 名称 == Noexif（本程序自身的输出目录）
//   - 名称匹配 pYYYY 或 mMM（本程序自身的输出目录）
//
// 跳过自身输出是为了避免二次运行时把已归类的文件再拷一次。
func shouldSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if name == NoexifDir {
		return true
	}
	if reYearDir.MatchString(name) || reMonthDir.MatchString(name) {
		return true
	}
	_, skip := skipDirs[name]
	return skip
}

// isPhotoFile 判断文件是否为受支持的图片。
//
// 额外跳过以 "._" 开头的 macOS 资源叉文件（拷贝到 exFAT/FAT 时自动产生），
// 这些不是真正的照片，没有 EXIF，全部归到 Noexif 会污染结果。
func isPhotoFile(name string) bool {
	if strings.HasPrefix(name, "._") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := photoExts[ext]
	return ok
}