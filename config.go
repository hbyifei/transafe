// config.go
// 配置结构，后续可从配置文件加载

package main

type Config struct {
	ServerAddr string `json:"server_addr"` // 服务端地址，如 ":9000"
	Password   string `json:"password"`    // 认证密码
	ChunkSize  int    `json:"chunk_size"`  // 分块大小（字节）
	LogLevel   string `json:"log_level"`   // 日志级别
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		ServerAddr: ":9000",
		Password:   "secret123",
		ChunkSize:  16 * 1024 * 1024, // 16MB
		LogLevel:   "info",
	}
}