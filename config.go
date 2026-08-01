package main

type Config struct {
	ServerAddr string
	Password   string
	ChunkSize  int
}

func DefaultConfig() *Config {
	return &Config{
		ServerAddr: ":9000",
		Password:   "secret123",
		ChunkSize:  16 * 1024 * 1024,
	}
}