package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TransafeLogger 带文件输出的日志器
type TransafeLogger struct {
	mu      sync.Mutex
	file    *os.File
	verbose bool
}

var (
	appLogger *TransafeLogger
	once      sync.Once
)

// InitLogger 初始化全局日志器
func InitLogger(logPath string, verbose bool) (*TransafeLogger, error) {
	var err error
	once.Do(func() {
		// 确保日志目录存在
		dir := filepath.Dir(logPath)
		if dir != "" && dir != "." {
			os.MkdirAll(dir, 0755)
		}

		f, openErr := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if openErr != nil {
			err = fmt.Errorf("open log file: %w", openErr)
			return
		}

		appLogger = &TransafeLogger{
			file:    f,
			verbose: verbose,
		}

		// 写入启动分隔符
		ts := time.Now().Format("2006/01/02 15:04:05")
		header := fmt.Sprintf("\n========== Transafe Session Started: %s ==========\n", ts)
		f.WriteString(header)
	})
	return appLogger, err
}

// Close 关闭日志文件
func (l *TransafeLogger) Close() {
	if l != nil && l.file != nil {
		ts := time.Now().Format("2006/01/02 15:04:05")
		l.file.WriteString(fmt.Sprintf("========== Transafe Session Ended: %s ==========\n", ts))
		l.file.Close()
	}
}

// Log 写入一条日志（同时输出到控制台和文件）
func (l *TransafeLogger) Log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006/01/02 15:04:05")

	line := fmt.Sprintf("[%s] [%s] %s\n", ts, level, msg)

	l.mu.Lock()
	if l.file != nil {
		l.file.WriteString(line)
	}
	l.mu.Unlock()

	// 控制台输出
	switch level {
	case "ERROR":
		fmt.Printf("\033[31m%s\033[0m", line) // 红色
	case "WARN":
		fmt.Printf("\033[33m%s\033[0m", line) // 黄色
	case "DEBUG":
		if l.verbose {
			fmt.Print(line)
		}
	default:
		fmt.Print(line)
	}
}

// Info 信息日志
func (l *TransafeLogger) Info(format string, args ...interface{}) {
	l.Log("INFO", format, args...)
}

// Error 错误日志
func (l *TransafeLogger) Error(format string, args ...interface{}) {
	l.Log("ERROR", format, args...)
}

// Warn 警告日志
func (l *TransafeLogger) Warn(format string, args ...interface{}) {
	l.Log("WARN", format, args...)
}

// Debug 调试日志
func (l *TransafeLogger) Debug(format string, args ...interface{}) {
	l.Log("DEBUG", format, args...)
}

// FileLog 仅写入文件，不输出控制台
func (l *TransafeLogger) FileLog(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006/01/02 15:04:05")
	line := fmt.Sprintf("[%s] [TRACE] %s\n", ts, msg)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.WriteString(line)
	}
}

// GetLogger 获取全局日志器
func GetLogger() *TransafeLogger {
	return appLogger
}
