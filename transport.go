package main

import (
	"fmt"
	"net"
	"time"
)

func Dial(addr string, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

func Listen(addr string) (net.Listener, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	return listener, nil
}