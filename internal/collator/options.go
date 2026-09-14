package collator

// FileEvent 描述单文件处理结果（用于 GUI 进度回调）。
type FileEvent struct {
	// Src 是源文件绝对路径。
	Src string
	// Dst 是目标路径，仅在已拷贝时非空。
	Dst string
	// Outcome 处理结果分类。
	Outcome Outcome
	// Err 仅当 Outcome == OutcomeFailed 时非空。
	Err error
	// Total 仅在 Outcome == OutcomePreScanDone 时有效；
	// 表示预扫描得到的总文件数，GUI 用来设置进度条分母。
	Total int
}

// Outcome 单文件处理的结果分类。
type Outcome int

const (
	// OutcomeCopied 文件已成功拷贝到目标（无碰撞）。
	OutcomeCopied Outcome = iota
	// OutcomeSkipped 目标已存在且大小相同，跳过。
	OutcomeSkipped
	// OutcomeRenamed 目标已存在但大小不同，已在文件名后追加 "_" 重命名后成功拷贝。
	// FileEvent.Dst 是最终的目标路径。
	OutcomeRenamed
	// OutcomeNoExif 无 EXIF，归到 Noexif。
	OutcomeNoExif
	// OutcomeFailed 处理失败（读 EXIF 或拷贝错误）。
	OutcomeFailed
	// OutcomePreScanDone 预扫描完成；FileEvent.Total 字段有效。
	// GUI 收到此事件后用 Total 设置进度条分母。
	OutcomePreScanDone
)

// Options 配置 Collator 行为。
//
// 字段按"先少后多"演进；后续加缩略图/并行/去重等特性时，
// 在此新增字段而不是改函数签名，以保持向后兼容。
type Options struct {
	// Src 源目录。必须是已存在的目录。
	Src string

	// Dst 目标根目录。不存在时会自动创建。
	Dst string

	// Verbose 是否打印调试日志（每个被跳过的隐藏目录、跳过原因等）。
	Verbose bool

	// DryRun 演练模式：只读源文件、不写目标文件，但仍执行扫描与冲突检测。
	DryRun bool

	// Workers 并行处理 worker 数。
	//   - <= 0 或 1：串行处理（顺序输出，便于调试）
	//   - > 1：worker pool 并发处理；Event 触发顺序不再保证，但单文件语义不变
	Workers int

	// OnProgress 可选回调。每处理完一张图片后调用一次，
	// 传入当前文件事件。GUI 用此实时更新进度；CLI 留空即可。
	//
	// 也会在预扫描完成时调用一次（Outcome == OutcomePreScanDone，Total 字段有效）。
	//
	// 调用方必须保证回调快速且非阻塞（< 1ms），否则会拖慢整个流程。
	// 复杂 UI 更新请通过 channel 异步派发。
	OnProgress func(FileEvent)

	// === 后续扩展点（先空着）===
	// GenerateThumb bool           // 是否生成缩略图
	// Dedup         DedupStrategy  // 哈希去重策略
}