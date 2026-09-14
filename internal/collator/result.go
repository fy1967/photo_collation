package collator

// Stats 一次 Run 的统计结果。
//
// 各字段含义：
//   - Total:      预扫描得到的目标文件总数（用于进度条分母）
//   - Scanned:    扫描到的 jpg/jpeg 文件总数（包括失败、被跳过的）
//   - Classified: 成功按 EXIF 日期归类到 pYYYY/mMM 目录（无碰撞）
//   - Renamed:    目标已存在但大小不同，已重命名后成功归类
//   - NoExif:     无 EXIF 或 EXIF 时间字段无效，归到 Noexif
//   - Skipped:    目标已存在且大小相同，跳过未覆盖
//   - Failed:     读 EXIF 失败（非 NoExif/InvalidTime）或拷贝失败
type Stats struct {
	Total      int
	Scanned    int
	Classified int
	Renamed    int
	NoExif     int
	Skipped    int
	Failed     int
}