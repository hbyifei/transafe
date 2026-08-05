package main

import (
	"os"
	"time"
)

// Config 全局配置
type Config struct {
	ServerAddr string
	Password   string
	ChunkSize  int
	LogFile    string
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		ServerAddr: ":9000",
		Password:   "secret123",
		ChunkSize:  16 * 1024 * 1024, // 16MB
		LogFile:    "transafe.log",
	}
}

// getPassword 获取密码（从环境变量或默认值）
func getPassword() string {
	if pwd := os.Getenv("TRANSAFE_PASSWORD"); pwd != "" {
		return pwd
	}
	return "secret123"
}

// getLogPath 获取日志文件路径
func getLogPath() string {
	if p := os.Getenv("TRANSAFE_LOG"); p != "" {
		return p
	}
	return "transafe.log"
}

// getChunkSize 获取分块大小
func getChunkSize() int {
	return DefaultConfig().ChunkSize
}

// nowStr 返回当前时间字符串
func nowStr() string {
	return time.Now().Format("2006/01/02 15:04:05")
}
