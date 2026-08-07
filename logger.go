package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TransafeLogger struct {
	mu      sync.Mutex
	file    *os.File
	verbose bool
}

var (
	appLogger *TransafeLogger
	once      sync.Once
)

func InitLogger(logPath string, verbose bool) (*TransafeLogger, error) {
	var err error
	once.Do(func() {
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

		ts := time.Now().Format("2006/01/02 15:04:05")
		header := fmt.Sprintf("\n========== Transafe Session Started: %s ==========\n", ts)
		f.WriteString(header)
	})
	return appLogger, err
}

func (l *TransafeLogger) Close() {
	if l != nil && l.file != nil {
		ts := time.Now().Format("2006/01/02 15:04:05")
		l.file.WriteString(fmt.Sprintf("========== Transafe Session Ended: %s ==========\n", ts))
		l.file.Close()
	}
}

func (l *TransafeLogger) Log(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006/01/02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] %s\n", ts, level, msg)

	l.mu.Lock()
	if l.file != nil {
		l.file.WriteString(line)
	}
	l.mu.Unlock()

	// 控制台输出（纯文本，无颜色）
	fmt.Print(line)
}

func (l *TransafeLogger) Info(format string, args ...interface{}) {
	l.Log("INFO", format, args...)
}

func (l *TransafeLogger) Error(format string, args ...interface{}) {
	l.Log("ERROR", format, args...)
}

func (l *TransafeLogger) Warn(format string, args ...interface{}) {
	l.Log("WARN", format, args...)
}

func (l *TransafeLogger) Debug(format string, args ...interface{}) {
	l.Log("DEBUG", format, args...)
}

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

func GetLogger() *TransafeLogger {
	return appLogger
}