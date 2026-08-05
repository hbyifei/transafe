package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"time"
)

type ProgressInfo struct {
	mu          sync.Mutex
	totalFiles  int
	totalBytes  int64
	doneFiles   int
	doneBytes   int64
	startTime   time.Time
	lastPrint   time.Time
	writer      io.Writer
	errorCount  int // 新增：错误计数
}

func NewProgress(totalFiles int, totalBytes int64, workers int) *ProgressInfo {
	return &ProgressInfo{
		totalFiles:  totalFiles,
		totalBytes:  totalBytes,
		startTime:   time.Now(),
		lastPrint:   time.Now(),
		writer:      os.Stderr,
		errorCount:  0,
	}
}

func (p *ProgressInfo) SetTotal(files int, bytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalFiles = files
	p.totalBytes = bytes
}

func (p *ProgressInfo) SetDone(files int, bytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.doneFiles = files
	p.doneBytes = bytes
}

func (p *ProgressInfo) Add(doneFiles int, doneBytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.doneFiles += doneFiles
	p.doneBytes += doneBytes
}

// AddError 增加错误计数
func (p *ProgressInfo) AddError(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errorCount += count
}

// SetErrorCount 直接设置错误计数
func (p *ProgressInfo) SetErrorCount(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errorCount = count
}

// ClearLine 清除当前行的内容
func (p *ProgressInfo) ClearLine() {
	fmt.Fprint(p.writer, "\r\033[K")
}

// Print 刷新进度信息（先清行，再打印）
func (p *ProgressInfo) Print() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(p.startTime).Seconds()
	if elapsed < 0.1 {
		return
	}

	speed := float64(p.doneBytes) / elapsed
	var eta string
	if speed > 0 {
		remainingBytes := float64(p.totalBytes - p.doneBytes)
		etaSec := remainingBytes / speed
		if math.IsInf(etaSec, 0) || math.IsNaN(etaSec) {
			eta = "∞"
		} else {
			eta = formatDuration(time.Duration(etaSec) * time.Second)
		}
	} else {
		eta = "?"
	}

	p.ClearLine()
	fmt.Fprintf(p.writer, "Files: %d/%d | Speed: %.2f MB/s | Elapsed: %s | ETA %s",
		p.doneFiles, p.totalFiles, speed/1024/1024, formatDuration(time.Since(p.startTime)), eta)
}

// Done 传输完成，打印最终报告（包括错误统计）
func (p *ProgressInfo) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ClearLine()

	barWidth := 52
	bar := "["
	for i := 0; i < barWidth; i++ {
		bar += "="
	}
	bar += "]"

	elapsed := time.Since(p.startTime).Seconds()
	speed := float64(p.doneBytes) / elapsed
	if math.IsInf(speed, 0) || math.IsNaN(speed) {
		speed = 0
	}

	// 打印 100% 进度条
	fmt.Fprintf(p.writer, "%s 100.0%% | %d/%d files | %.2f MB/s | Completed in %s\n",
		bar, p.totalFiles, p.totalFiles, speed/1024/1024, formatDuration(time.Since(p.startTime)))

	// 打印错误统计（类似 FastCopy 风格）
	fmt.Fprintf(p.writer, "Errors: %d | Skipped: 0 | Warnings: 0\n", p.errorCount)
}

func formatDuration(d time.Duration) string {
	sec := d.Seconds()
	if sec < 60 {
		return fmt.Sprintf("%.0fs", sec)
	}
	min := int(sec) / 60
	secRemain := int(sec) % 60
	if min < 60 {
		return fmt.Sprintf("%dm%ds", min, secRemain)
	}
	hour := min / 60
	minRemain := min % 60
	return fmt.Sprintf("%dh%dm%ds", hour, minRemain, secRemain)
}