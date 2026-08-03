// progress.go
package main

import (
	"fmt"	
	"math"
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
	workerCount int
}

func NewProgress(totalFiles int, totalBytes int64, workers int) *ProgressInfo {
	return &ProgressInfo{
		totalFiles:  totalFiles,
		totalBytes:  totalBytes,
		startTime:   time.Now(),
		lastPrint:   time.Now(),
		workerCount: workers,
	}
}

func (p *ProgressInfo) Add(doneFiles int, doneBytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.doneFiles += doneFiles
	p.doneBytes += doneBytes
}

func (p *ProgressInfo) Print() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(p.startTime).Seconds()
	if elapsed < 0.1 {
		return // 太短，不打印
	}

	speed := float64(p.doneBytes) / elapsed // bytes per second
	percent := 0.0
	if p.totalBytes > 0 {
		percent = float64(p.doneBytes) / float64(p.totalBytes) * 100
	}

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

	// 进度条（简单版本）
	barWidth := 40
	filled := int(percent / 100 * float64(barWidth))
	bar := "["
	for i := 0; i < barWidth; i++ {
		if i < filled {
			bar += "="
		} else if i == filled {
			bar += ">"
		} else {
			bar += " "
		}
	}
	bar += "]"

	fmt.Printf("\r%s %.1f%% | %d/%d files | %.2f MB/s | ETA %s    ",
		bar, percent, p.doneFiles, p.totalFiles, speed/1024/1024, eta)

	p.lastPrint = now
}

func (p *ProgressInfo) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("\nTransfer completed in %s\n", formatDuration(time.Since(p.startTime)))
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