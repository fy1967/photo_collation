// genicon - 一次性生成项目 icon (相机图形)
//
// 用法: go run scripts/genicon/main.go assets/icon.png
// 生成的 PNG 用 fyne bundle 嵌入到二进制。

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: go run scripts/genicon/main.go <输出 png>")
		os.Exit(1)
	}
	out := os.Args[1]

	const size = 256
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	drawIcon(img, size)

	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("写入 %s (%dx%d)\n", out, size, size)
}

// drawIcon 在 img 上画一个相机图形（蓝色调 + 镜头 + 闪光灯）。
func drawIcon(img *image.RGBA, size int) {
	bg := color.RGBA{30, 41, 82, 255}       // 深蓝
	body := color.RGBA{59, 130, 246, 255}   // 蓝
	bodyL := color.RGBA{96, 165, 250, 255}  // 蓝浅
	lensOuter := color.RGBA{15, 23, 42, 255} // 近黑
	lensInner := color.RGBA{99, 102, 241, 255} // 靛
	flash := color.RGBA{250, 204, 21, 255}  // 黄
	highlight := color.RGBA{255, 255, 255, 220}

	// 整个图标圆角矩形背景
	r := 36 // 圆角半径
	drawRoundRect(img, 8, 8, size-16, size-16, r, bg)

	// 相机机身（中间的大圆角矩形）
	bodyX, bodyY, bodyW, bodyH := 32, 80, size-64, 144
	drawRoundRect(img, bodyX, bodyY, bodyW, bodyH, 20, body)

	// 机身顶部的"凸起"（取景器部分）
	drawRoundRect(img, 88, 56, 80, 28, 8, bodyL)

	// 镜头（同心圆）
	cx, cy := size/2, bodyY+bodyH/2+8
	drawCircle(img, cx, cy, 56, lensOuter)
	drawCircle(img, cx, cy, 44, bodyL)
	drawCircle(img, cx, cy, 36, lensInner)
	drawCircle(img, cx, cy, 24, lensOuter)
	drawCircle(img, cx, cy, 16, bodyL)

	// 镜头反光高光
	drawCircle(img, cx-8, cy-10, 8, highlight)

	// 闪光灯（小圆点，右上角）
	drawCircle(img, size-58, 78, 7, flash)

	// 快门按钮（小圆，顶部）
	drawCircle(img, 60, 60, 7, flash)
}

// drawRoundRect 在 img 上画一个填充圆角矩形。
func drawRoundRect(img *image.RGBA, x, y, w, h, r int, c color.RGBA) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			if isInRoundRect(i, j, x, y, w, h, r) {
				img.Set(i, j, c)
			}
		}
	}
}

func isInRoundRect(i, j, x, y, w, h, r int) bool {
	// 圆角外的四个角
	corners := []struct{ cx, cy int }{
		{x + r, y + r},
		{x + w - r, y + r},
		{x + r, y + h - r},
		{x + w - r, y + h - r},
	}
	for _, c := range corners {
		var inside bool
		switch {
		case i < x+r && j < y+r:
			inside = distSq(i, j, c.cx, c.cy) <= r*r
		case i >= x+w-r && j < y+r:
			inside = distSq(i, j, c.cx, c.cy) <= r*r
		case i < x+r && j >= y+h-r:
			inside = distSq(i, j, c.cx, c.cy) <= r*r
		case i >= x+w-r && j >= y+h-r:
			inside = distSq(i, j, c.cx, c.cy) <= r*r
		default:
			inside = true
		}
		if !inside {
			return false
		}
	}
	return true
}

func drawCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for j := cy - r; j <= cy+r; j++ {
		for i := cx - r; i <= cx+r; i++ {
			if distSq(i, j, cx, cy) <= r*r {
				img.Set(i, j, c)
			}
		}
	}
}

func distSq(x1, y1, x2, y2 int) int {
	dx, dy := x1-x2, y1-y2
	return dx*dx + dy*dy
}

// 引入 math 防止编译器优化掉未使用的 import
var _ = math.Pi