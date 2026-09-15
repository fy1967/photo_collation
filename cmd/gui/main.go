// Command photo_collation-gui 是 photo_collation 的 Fyne 图形界面。
//
// 启动一个原生 macOS/Windows/Linux 窗口，提供：
//   - 源目录、目标目录选择（支持拖放）
//   - 实时进度条 + 旋转动画（百分比基于预扫描总数）
//   - 当前统计：总数/已拷/无EXIF/跳过/失败
//   - 最近处理的文件滚动列表（含缩略图）
//   - 并行 worker 数配置
//   - dry-run / verbose 开关
//
// 设计原则：
//   - 主线程只跑 Fyne 事件循环；collator 在 goroutine 里跑
//   - 缩略图用 LRU 缓存，避免重复解码
//   - 单文件失败不影响整体流程（与 CLI 行为一致）
package main

import (
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"photo_collation/internal/collator"
	"photo_collation/internal/exif"
	"photo_collation/internal/thumb"
)

func main() {
	a := app.New()
	a.SetIcon(resourceIconPng) // 嵌入的 assets/icon.png（相机图形，蓝色调）
	w := a.NewWindow("Photo Collation")
	w.Resize(fyne.NewSize(820, 720))
	w.CenterOnScreen()

	gui := newGUI(w, a)
	w.SetContent(gui.content())

	// 拖放支持：把文件夹拖到窗口任意位置，自动填入源目录
	w.SetOnDropped(func(pos fyne.Position, items []fyne.URI) {
		for _, uri := range items {
			path := uri.Path()
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				gui.srcEntry.SetText(path)
				gui.flashHint(fmt.Sprintf("已拖入源目录: %s", filepath.Base(path)))
				return
			}
		}
		gui.flashHint("拖入的不是文件夹，已忽略")
	})

	w.ShowAndRun()
}

// gui 持有所有 UI 控件 + 运行状态。
type gui struct {
	w   fyne.Window
	a   fyne.App
	log *slog.Logger

	// 缩略图缓存
	thumbCache *thumb.Cache

	// 输入控件
	srcEntry    *widget.Entry
	dstEntry    *widget.Entry
	dryRunChk   *widget.Check
	verboseChk  *widget.Check
	workersEntry *widget.Entry

	// 状态显示
	statusLabel *widget.Label
	hintLabel   *widget.Label
	scannedLbl  *fyne.Container
	copiedLbl   *fyne.Container
	renamedLbl  *fyne.Container
	noexifLbl   *fyne.Container
	skippedLbl  *fyne.Container
	failedLbl   *fyne.Container
	totalLbl    *fyne.Container
	progress    *widget.ProgressBar
	spinner     *widget.ProgressBarInfinite

	// 日志列表
	logList *widget.List
	logData []logEntry

	// 按钮
	startBtn  *widget.Button
	cancelBtn *widget.Button
	openBtn   *widget.Button

	// 运行时
	cancelCh  chan struct{}
	totalN    int
	scannedN  int
}

// logEntry 单条日志记录。
// thumb 字段懒加载，nil 表示还没生成；用 thumbReady 标记是否尝试过（避免重复尝试）。
type logEntry struct {
	thumb      image.Image
	thumbReady bool
	iconKey    string
	text       string
}

func newGUI(w fyne.Window, a fyne.App) *gui {
	return &gui{
		w:          w,
		a:          a,
		log:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		logData:    make([]logEntry, 0, 256),
		thumbCache: thumb.NewCache(thumb.New(80), 500),
	}
}

// content 构造主窗口内容布局。
func (g *gui) content() fyne.CanvasObject {
	// === 路径选择区 ===
	g.srcEntry = widget.NewEntry()
	g.srcEntry.SetPlaceHolder("选择照片所在目录（或拖文件夹到窗口）...")
	srcBrowse := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, g.w)
				return
			}
			if uri != nil {
				g.srcEntry.SetText(uri.Path())
			}
		}, g.w).Show()
	})
	g.dstEntry = widget.NewEntry()
	g.dstEntry.SetPlaceHolder("选择归类输出目录...")
	dstBrowse := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, g.w)
				return
			}
			if uri != nil {
				g.dstEntry.SetText(uri.Path())
			}
		}, g.w).Show()
	})

	// === 选项区 ===
	g.dryRunChk = widget.NewCheck("演练", nil)
	g.verboseChk = widget.NewCheck("详细日志", nil)
	g.workersEntry = widget.NewEntry()
	g.workersEntry.SetText("4")
	g.workersEntry.SetPlaceHolder("workers")
	g.workersEntry.Resize(fyne.NewSize(50, g.workersEntry.MinSize().Height))

	// === 状态显示 ===
	g.statusLabel = widget.NewLabel("就绪 — 拖文件夹到窗口或点浏览选择源目录")
	g.statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	g.statusLabel.Wrapping = fyne.TextWrapWord

	g.hintLabel = widget.NewLabel("")
	g.hintLabel.TextStyle = fyne.TextStyle{Italic: true}

	g.totalLbl = g.makeStatLabel("总数", "0")
	g.scannedLbl = g.makeStatLabel("扫描", "0")
	g.copiedLbl = g.makeStatLabel("已归类", "0")
	g.renamedLbl = g.makeStatLabel("重命名", "0")
	g.noexifLbl = g.makeStatLabel("无 EXIF", "0")
	g.skippedLbl = g.makeStatLabel("跳过", "0")
	g.failedLbl = g.makeStatLabel("失败", "0")

	g.progress = widget.NewProgressBar()
	g.progress.Hide()
	g.spinner = widget.NewProgressBarInfinite()
	g.spinner.Hide()

	// === 日志列表 ===
	g.logList = widget.NewList(
		func() int { return len(g.logData) },
		func() fyne.CanvasObject {
			img := canvas.NewImageFromImage(placeholderThumb())
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(64, 64))
			text := widget.NewLabel("")
			text.TextStyle = fyne.TextStyle{Monospace: true}
			return container.NewHBox(img, text)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id >= len(g.logData) {
				return
			}
			entry := g.logData[id]
			box := item.(*fyne.Container)
			img := box.Objects[0].(*canvas.Image)
			label := box.Objects[1].(*widget.Label)

			if entry.thumb != nil {
				img.Image = entry.thumb
				img.Refresh()
			} else if !entry.thumbReady {
				// 标记已尝试，避免重复（懒加载在 onProgress 里做）
				g.logData[id].thumbReady = true
			}
			label.SetText(entry.text)
		},
	)
	g.logList.HideSeparators = false

	// === 按钮 ===
	g.startBtn = widget.NewButtonWithIcon("开始", theme.MediaPlayIcon(), g.onStart)
	g.cancelBtn = widget.NewButtonWithIcon("取消", theme.CancelIcon(), g.onCancel)
	g.cancelBtn.Disable()
	g.openBtn = widget.NewButtonWithIcon("打开输出目录", theme.FolderOpenIcon(), g.onOpenOutput)
	g.openBtn.Disable()

	// === 组装 ===
	pathForm := container.NewVBox(
		widget.NewLabel("源目录"),
		container.NewBorder(nil, nil, nil, srcBrowse, g.srcEntry),
		widget.NewLabel("目标目录"),
		container.NewBorder(nil, nil, nil, dstBrowse, g.dstEntry),
	)

	optionsRow := container.NewHBox(
		g.dryRunChk,
		g.verboseChk,
		widget.NewLabel("Workers:"),
		g.workersEntry,
		layout.NewSpacer(),
	)

	statsGrid := container.NewGridWithColumns(7,
		g.totalLbl, g.scannedLbl, g.copiedLbl, g.renamedLbl,
		g.noexifLbl, g.skippedLbl, g.failedLbl,
	)

	buttons := container.NewHBox(
		g.startBtn,
		g.cancelBtn,
		layout.NewSpacer(),
		g.openBtn,
	)

	return container.NewVBox(
		widget.NewLabelWithStyle("Photo Collation",
			fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		pathForm,
		widget.NewSeparator(),
		optionsRow,
		widget.NewSeparator(),
		g.statusLabel,
		g.hintLabel,
		statsGrid,
		g.progress,
		g.spinner,
		widget.NewLabel("最近处理"),
		container.NewBorder(nil, nil, nil, nil, g.logList),
		widget.NewSeparator(),
		buttons,
	)
}

// makeStatLabel 构造一个 (标签:值) 形式的统计单元格。
func (g *gui) makeStatLabel(name, value string) *fyne.Container {
	nameLbl := widget.NewLabel(name)
	nameLbl.Alignment = fyne.TextAlignCenter
	valLbl := widget.NewLabelWithStyle(value,
		fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	return container.NewVBox(nameLbl, valLbl)
}

// setStat 安全地写入统计数值。
func (g *gui) setStat(c *fyne.Container, n int) {
	c.Objects[1].(*widget.Label).SetText(strconv.Itoa(n))
}

// placeholderThumb 返回占位缩略图（透明，避免 nil panic）。
func placeholderThumb() image.Image {
	// 1x1 透明 PNG
	return image.NewRGBA(image.Rect(0, 0, 1, 1))
}

// onStart 是"开始"按钮回调：启动 collator goroutine。
func (g *gui) onStart() {
	src := g.srcEntry.Text
	dst := g.dstEntry.Text
	if src == "" || dst == "" {
		dialog.ShowInformation("需要路径", "请先选择源目录与目标目录", g.w)
		return
	}

	workers, err := strconv.Atoi(g.workersEntry.Text)
	if err != nil || workers < 1 {
		workers = 1
	}

	// 重置状态
	g.logData = g.logData[:0]
	g.logList.Refresh()
	g.thumbCache.Clear()
	g.totalN = 0
	g.scannedN = 0
	g.setStat(g.totalLbl, 0)
	g.setStat(g.scannedLbl, 0)
	g.setStat(g.copiedLbl, 0)
	g.setStat(g.renamedLbl, 0)
	g.setStat(g.noexifLbl, 0)
	g.setStat(g.skippedLbl, 0)
	g.setStat(g.failedLbl, 0)
	g.statusLabel.SetText("扫描中...")
	g.progress.SetValue(0)
	g.progress.Max = 1
	g.progress.Show()
	g.spinner.Show()
	g.spinner.Start()
	g.startBtn.Disable()
	g.cancelBtn.Enable()
	g.openBtn.Disable()

	g.cancelCh = make(chan struct{})

	opts := collator.Options{
		Src:        src,
		Dst:        dst,
		Verbose:    g.verboseChk.Checked,
		DryRun:     g.dryRunChk.Checked,
		Workers:    workers,
		OnProgress: g.onProgress,
	}

	// collator 在 goroutine 里跑
	go func() {
		c, err := collator.New(opts, exif.NewReader())
		if err != nil {
			fyne.Do(func() {
				g.spinner.Stop()
				g.spinner.Hide()
				g.progress.Hide()
				g.statusLabel.SetText("启动失败: " + err.Error())
				g.startBtn.Enable()
				g.cancelBtn.Disable()
				dialog.ShowError(err, g.w)
			})
			return
		}

		stats, runErr := c.Run(ctxWithCancel(g.cancelCh))

		fyne.Do(func() {
			g.spinner.Stop()
			g.spinner.Hide()
			g.progress.Hide()
			g.startBtn.Enable()
			g.cancelBtn.Disable()

			if runErr != nil {
				g.statusLabel.SetText("出错: " + runErr.Error())
				dialog.ShowError(runErr, g.w)
				return
			}
			g.statusLabel.SetText(fmt.Sprintf(
				"完成 ✓  总数=%d  扫描=%d  归类=%d  重命名=%d  无EXIF=%d  跳过=%d  失败=%d",
				stats.Total, stats.Scanned, stats.Classified, stats.Renamed,
				stats.NoExif, stats.Skipped, stats.Failed))
			g.openBtn.Enable()
		})
	}()
}

// onCancel 取消当前运行。
func (g *gui) onCancel() {
	if g.cancelCh != nil {
		close(g.cancelCh)
		g.cancelCh = nil
		g.statusLabel.SetText("正在取消...")
	}
}

// onOpenOutput 用系统文件管理器打开目标目录。
func (g *gui) onOpenOutput() {
	dst := g.dstEntry.Text
	if dst == "" {
		return
	}
	if err := openInOS(dst); err != nil {
		dialog.ShowError(err, g.w)
	}
}

// onProgress 是在 collator goroutine 里调用的回调；
// 通过 fyne.Do 把 UI 更新切回主线程。
func (g *gui) onProgress(ev collator.FileEvent) {
	switch ev.Outcome {
	case collator.OutcomePreScanDone:
		fyne.Do(func() {
			g.totalN = ev.Total
			g.progress.Max = float64(ev.Total)
			g.setStat(g.totalLbl, ev.Total)
			g.statusLabel.SetText(fmt.Sprintf("开始处理 %d 个文件...", ev.Total))
		})
		return
	}

	src := ev.Src

	// 缩略图同步加载（缓存命中 O(1)；未命中阻塞当前 worker goroutine，不阻塞 UI）
	var img image.Image
	if ev.Outcome != collator.OutcomeFailed {
		if cached, err := g.thumbCache.Get(src); err == nil {
			img = cached
		}
	}

	// 切回主线程更新 UI
	fyne.Do(func() {
		iconKey := "info"
		text := filepath.Base(src)
		switch ev.Outcome {
		case collator.OutcomeCopied:
			iconKey = "ok"
			text = fmt.Sprintf("✓ %s → %s/%s",
				filepath.Base(src), filepath.Base(filepath.Dir(filepath.Dir(ev.Dst))),
				filepath.Base(filepath.Dir(ev.Dst)))
		case collator.OutcomeRenamed:
			iconKey = "renamed"
			text = fmt.Sprintf("↪ %s → %s (同名不同大小，已加 _)",
				filepath.Base(src), filepath.Base(ev.Dst))
		case collator.OutcomeNoExif:
			iconKey = "noexif"
			text = fmt.Sprintf("? %s → Noexif/", filepath.Base(src))
		case collator.OutcomeSkipped:
			iconKey = "skip"
			text = fmt.Sprintf("⊘ %s (已存在)", filepath.Base(src))
		case collator.OutcomeFailed:
			iconKey = "fail"
			text = fmt.Sprintf("✗ %s: %v", filepath.Base(src), ev.Err)
		}
		g.appendLog(iconKey, text, img)

		// 数字
		g.scannedN++
		g.setStat(g.scannedLbl, g.scannedN)
		switch ev.Outcome {
		case collator.OutcomeCopied:
			g.bumpStat(g.copiedLbl)
		case collator.OutcomeRenamed:
			g.bumpStat(g.renamedLbl)
		case collator.OutcomeNoExif:
			g.bumpStat(g.noexifLbl)
		case collator.OutcomeSkipped:
			g.bumpStat(g.skippedLbl)
		case collator.OutcomeFailed:
			g.bumpStat(g.failedLbl)
		}

		// 进度条
		if g.totalN > 0 {
			g.progress.SetValue(float64(g.scannedN))
		}
	})
}

func (g *gui) bumpStat(c *fyne.Container) {
	lbl := c.Objects[1].(*widget.Label)
	n, _ := strconv.Atoi(lbl.Text)
	lbl.SetText(strconv.Itoa(n + 1))
}

func (g *gui) appendLog(iconKey, text string, thumb image.Image) {
	const maxLines = 200
	g.logData = append(g.logData, logEntry{
		thumb:   thumb,
		iconKey: iconKey,
		text:    text,
	})
	if len(g.logData) > maxLines {
		g.logData = g.logData[len(g.logData)-maxLines:]
	}
	g.logList.Refresh()
	g.logList.ScrollToBottom()
}

// flashHint 在状态栏下面显示一条临时提示，2 秒后清空。
func (g *gui) flashHint(text string) {
	fyne.Do(func() {
		g.hintLabel.SetText(text)
	})
	// 简单定时清空；如需更优雅可用定时器队列
	go func() {
		// 用 AfterFunc 在主线程上清空
		timeAfterFunc(2*timeSecond, func() {
			fyne.Do(func() { g.hintLabel.SetText("") })
		})
	}()
}