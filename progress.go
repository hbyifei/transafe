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
	mu         sync.Mutex
	totalFiles int
	totalBytes int64
	doneFiles  int
	doneBytes  int64
	startTime  time.Time
	writer     io.Writer
	errorCount int
	// 记录上一次打印的进度行长度，用于覆盖时补空格
	lastLineLen int
}

func NewProgress(totalFiles int, totalBytes int64, workers int) *ProgressInfo {
	return &ProgressInfo{
		totalFiles: totalFiles,
		totalBytes: totalBytes,
		startTime:  time.Now(),
		writer:     os.Stderr,
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

func (p *ProgressInfo) AddError(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errorCount += count
}

func (p *ProgressInfo) SetErrorCount(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errorCount = count
}

// clearPrevLine 用回车回到行首，并用空格覆盖上一行内容，再回车
func (p *ProgressInfo) clearPrevLine() {
	if p.lastLineLen > 0 {
		fmt.Fprint(p.writer, "\r")
		for i := 0; i < p.lastLineLen; i++ {
			fmt.Fprint(p.writer, " ")
		}
		fmt.Fprint(p.writer, "\r")
	}
}

// Print 刷新进度信息（原地更新一行）
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

	line := fmt.Sprintf("Progress: %d/%d files | %.2f MB/s | Elapsed %s | ETA %s",
		p.doneFiles, p.totalFiles, speed/1024/1024,
		formatDuration(time.Since(p.startTime)), eta)

	p.clearPrevLine()
	fmt.Fprint(p.writer, line)
	p.lastLineLen = len(line)
}

// Done 传输完成，打印最终报告
func (p *ProgressInfo) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 先换行，结束进度行的原地更新
	fmt.Fprint(p.writer, "\r\n")

	barWidth := 40
	bar := ""
	for i := 0; i < barWidth; i++ {
		bar += "="
	}

	elapsed := time.Since(p.startTime).Seconds()
	speed := float64(p.doneBytes) / elapsed
	if math.IsInf(speed, 0) || math.IsNaN(speed) {
		speed = 0
	}

	fmt.Fprintf(p.writer, "%s 100.0%% | %d/%d files | %.2f MB/s | Completed in %s\n",
		bar, p.totalFiles, p.totalFiles, speed/1024/1024,
		formatDuration(time.Since(p.startTime)))
	fmt.Fprintf(p.writer, "Errors: %d | Skipped: 0 | Warnings: 0\n",
		p.errorCount)

	p.lastLineLen = 0
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