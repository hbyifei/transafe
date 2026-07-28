// shell.go
// 交互式命令行界面，方便调试和日常使用

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// RunShell 启动交互式 shell
func RunShell() {
	fmt.Println("Transafe Interactive Shell")
	fmt.Println("Commands: connect <addr>, auth <password>, send <local> <remote>, senddir <local_dir> <remote_root>, text <msg>, help, quit")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	var conn net.Conn // 当前连接

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 3)
		cmd := parts[0]

		switch cmd {
		case "quit", "exit":
			fmt.Println("Bye.")
			return

		case "help":
			printHelp()

		case "connect":
			if len(parts) < 2 {
				fmt.Println("Usage: connect <addr>")
				continue
			}
			addr := parts[1]
			var err error
			conn, err = Dial(addr, 5*time.Second)
			if err != nil {
				fmt.Printf("Connect error: %v\n", err)
			} else {
				fmt.Printf("Connected to %s\n", addr)
			}

		case "auth":
			if conn == nil {
				fmt.Println("Not connected. Use 'connect' first.")
				continue
			}
			password := "secret123" // 默认密码，可改为从配置读取
			if len(parts) >= 2 {
				password = parts[1]
			}
			if err := Auth(conn, password); err != nil {
				fmt.Printf("Auth error: %v\n", err)
			} else {
				fmt.Println("Authenticated.")
			}

		case "send":
			if conn == nil {
				fmt.Println("Not connected.")
				continue
			}
			if len(parts) < 3 {
				fmt.Println("Usage: send <local_path> <remote_path>")
				continue
			}
			localPath := parts[1]
			remotePath := parts[2]
			stats, err := SendFile(conn, localPath, remotePath, nil)
			if err != nil {
				fmt.Printf("Send error: %v\n", err)
			} else {
				fmt.Printf("Sent: %s (%d bytes, hash: %s)\n", stats.FilePath, stats.Size, stats.Hash)
			}

		case "senddir":
			if conn == nil {
				fmt.Println("Not connected.")
				continue
			}
			if len(parts) < 3 {
				fmt.Println("Usage: senddir <local_dir> <remote_root>")
				continue
			}
			localDir := parts[1]
			remoteRoot := parts[2]
			if err := SendDir(conn, localDir, remoteRoot, nil); err != nil {
				fmt.Printf("SendDir error: %v\n", err)
			} else {
				fmt.Println("Directory sent.")
			}

		case "text":
			if conn == nil {
				fmt.Println("Not connected.")
				continue
			}
			if len(parts) < 2 {
				fmt.Println("Usage: text <message>")
				continue
			}
			msg := parts[1]
			if err := SendText(conn, msg); err != nil {
				fmt.Printf("Text error: %v\n", err)
			} else {
				fmt.Println("Text sent (not fully implemented).")
			}

		default:
			fmt.Printf("Unknown command: %s\n", cmd)
			fmt.Println("Type 'help' for available commands.")
		}
	}
}

func printHelp() {
	fmt.Println("Commands:")
	fmt.Println("  connect <addr>           Connect to server (e.g., 127.0.0.1:9000)")
	fmt.Println("  auth [password]          Authenticate with password")
	fmt.Println("  send <local> <remote>    Send a single file")
	fmt.Println("  senddir <local_dir> <remote_root>  Send an entire directory")
	fmt.Println("  text <message>           Send a text message (reserved)")
	fmt.Println("  help                     Show this help")
	fmt.Println("  quit                     Exit shell")
}