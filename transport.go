// transport.go
// 封装 TCP 连接的管理，未来可扩展 TLS、连接池等

package main

import (
	"fmt"
	"net"
	"time"
)

// Dial 连接到指定地址，返回 net.Conn
// timeout 为连接超时（0 表示不设置）
func Dial(addr string, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

// Listen 在指定地址监听，返回 net.Listener
func Listen(addr string) (net.Listener, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	return listener, nil
}