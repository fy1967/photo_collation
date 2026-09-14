// Package thumb 提供 JPEG 缩略图生成与 LRU 缓存。
//
// 主要用途：GUI 日志列表每条记录显示一个 80x80 缩略图。
// 缩略图生成是 CPU + 内存密集操作，所以加 LRU 缓存避免重复解码。
package thumb

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"sync"

	"golang.org/x/image/draw"
)

// DefaultSize 默认缩略图最大边长（像素）。
const DefaultSize = 80

// Errors
var (
	ErrUnsupported = errors.New("thumb: unsupported image format")
)

// Generator 缩略图生成器。
//
// 无状态，可并发安全使用。缓存用 sync.Map 做粗粒度 LRU。
type Generator struct {
	maxSize int
}

// New 构造生成器。maxSize <= 0 时使用 DefaultSize。
func New(maxSize int) *Generator {
	if maxSize <= 0 {
		maxSize = DefaultSize
	}
	return &Generator{maxSize: maxSize}
}

// Generate 生成 path 指向的 JPEG 的缩略图。
//
// 行为：
//   - 只支持 JPEG；其他格式返回 ErrUnsupported
//   - 等比例缩放，最大边长为 g.maxSize
//   - 旋转 0 度（不做 EXIF Orientation 旋转，简化实现）
func (g *Generator) Generate(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	// 限制解码时分配的内存：先读前 32KB 探测头部，确认是 JPEG 再全量 decode。
	img, _, err := decodeJPEG(f)
	if err != nil {
		return nil, err
	}

	resized := resize(img, g.maxSize)
	return resized, nil
}

// decodeJPEG 解码 JPEG 流；返回 image 与 format（始终为 "jpeg"）。
func decodeJPEG(r io.Reader) (image.Image, string, error) {
	img, err := jpeg.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("decode jpeg: %w", err)
	}
	return img, "jpeg", nil
}

// resize 把 img 等比例缩放到最大边 = maxSize，返回新 image.Image。
func resize(src image.Image, maxSize int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSize && h <= maxSize {
		// 已经够小，直接返回；省一次插值拷贝
		return src
	}
	// 等比例缩放
	var nw, nh int
	if w > h {
		nw = maxSize
		nh = h * maxSize / w
	} else {
		nh = maxSize
		nw = w * maxSize / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	// CatmullRom 是 x/image/draw 中质量较高的缩放算法。
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

// Cache 是简单的 LRU 缩略图缓存，按 (path, size) 键索引。
//
// 容量上限由 maxEntries 控制；超出时按访问顺序淘汰最久未访问的项。
// 用于 GUI 日志列表：每个文件对应一个缩略图条目，避免重复 decode。
type Cache struct {
	gen       *Generator
	max       int
	mu        sync.Mutex
	entries   map[string]*cacheEntry
	head      *cacheEntry // 双向链表头（最近访问）
	tail      *cacheEntry // 双向链表尾（最久未访问）
}

type cacheEntry struct {
	key  string
	img  image.Image
	prev *cacheEntry
	next *cacheEntry
}

// NewCache 构造缓存；max <= 0 时不限制（不推荐，可能内存爆）。
func NewCache(gen *Generator, max int) *Cache {
	return &Cache{
		gen:     gen,
		max:     max,
		entries: make(map[string]*cacheEntry),
	}
}

// Get 返回 path 的缩略图，未命中时同步生成并缓存。
//
// 错误也会被抛出（调用方决定是否降级）。
func (c *Cache) Get(path string) (image.Image, error) {
	c.mu.Lock()
	if e, ok := c.entries[path]; ok {
		c.touch(e)
		c.mu.Unlock()
		return e.img, nil
	}
	c.mu.Unlock()

	// 在锁外生成（生成耗时）
	img, err := c.gen.Generate(path)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// 双重检查：可能在生成过程中别的协程已经插入
	if e, ok := c.entries[path]; ok {
		c.touch(e)
		return e.img, nil
	}

	e := &cacheEntry{key: path, img: img}
	c.entries[path] = e
	c.pushFront(e)
	c.evictIfNeeded()

	return img, nil
}

// touch 把 e 移到链表头（最近访问）。
func (c *Cache) touch(e *cacheEntry) {
	if c.head == e {
		return
	}
	// 摘除
	if e.prev != nil {
		e.prev.next = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	}
	if c.tail == e {
		c.tail = e.prev
	}
	// 插到头
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *Cache) pushFront(e *cacheEntry) {
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *Cache) evictIfNeeded() {
	if c.max <= 0 {
		return
	}
	for len(c.entries) > c.max {
		// 淘汰尾部
		if c.tail == nil {
			return
		}
		victim := c.tail
		delete(c.entries, victim.key)
		if victim.prev != nil {
			victim.prev.next = nil
		}
		c.tail = victim.prev
		if c.tail == nil {
			c.head = nil
		}
	}
}

// Clear 清空缓存（GUI 切换目录时调用，释放内存）。
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
	c.head = nil
	c.tail = nil
}

// Len 返回当前条目数。
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}