// main.go
// 程序入口：根据命令行参数决定启动模式

package main

import (
	"fmt"
	"log"
	"os"
	"net"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		// 无参数 → 启动交互式 shell
		RunShell()
		return
	}

	switch os.Args[1] {
	case "server":
		// 启动服务端
		addr := ":9000"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		if err := startServer(addr); err != nil {
			log.Fatalf("Server error: %v", err)
		}

	case "client":
		// 启动客户端（直接发送文件）
		if len(os.Args) < 4 {
			fmt.Println("Usage: transafe client <server_addr> <local_file> <remote_path>")
			os.Exit(1)
		}
		serverAddr := os.Args[2]
		localFile := os.Args[3]
		remoteFile := os.Args[4]
		if err := runClient(serverAddr, localFile, remoteFile); err != nil {
			log.Fatalf("Client error: %v", err)
		}

	case "senddir":
		// 发送目录
		if len(os.Args) < 4 {
			fmt.Println("Usage: transafe senddir <server_addr> <local_dir> <remote_root>")
			os.Exit(1)
		}
		serverAddr := os.Args[2]
		localDir := os.Args[3]
		remoteRoot := os.Args[4]
		if err := runSendDir(serverAddr, localDir, remoteRoot); err != nil {
			log.Fatalf("SendDir error: %v", err)
		}

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Println("Usage: transafe [server|client|senddir] ...")
		os.Exit(1)
	}
}

// startServer 启动 TCP 服务端
func startServer(addr string) error {
	listener, err := Listen(addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("Server listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

// handleConnection 处理单个客户端连接
func handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("New connection from %s", conn.RemoteAddr())

	// 1. 认证
	pkt, err := RecvPacket(conn)
	if err != nil {
		log.Printf("Recv auth error: %v", err)
		return
	}
	if pkt.Cmd != CmdAuth || string(pkt.Data) != "secret123" {
		SendPacket(conn, CmdError, []byte("auth failed"))
		return
	}
	SendPacket(conn, CmdOK, []byte("auth ok"))

	// 2. 循环处理命令
	for {
		pkt, err := RecvPacket(conn)
		if err != nil {
			log.Printf("Connection closed: %v", err)
			return
		}

		switch pkt.Cmd {
		case CmdFile:
			if err := ReceiveFile(conn, pkt.Data); err != nil {
				log.Printf("Receive file error: %v", err)
				SendPacket(conn, CmdError, []byte(err.Error()))
			}
		case CmdDirStart:
			if err := ReceiveDir(conn, pkt.Data); err != nil {
				log.Printf("Receive dir error: %v", err)
				SendPacket(conn, CmdError, []byte(err.Error()))
			}
		case CmdText:
			// 预留：处理文本消息
			log.Printf("[Chat] %s", string(pkt.Data))
			SendPacket(conn, CmdOK, []byte("text received"))
		default:
			SendPacket(conn, CmdError, []byte("unknown command"))
		}
	}
}

// runClient 运行客户端：连接、认证、发送文件
func runClient(serverAddr, localFile, remoteFile string) error {
	conn, err := Dial(serverAddr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	// 认证
	if err := Auth(conn, "secret123"); err != nil {
		return err
	}
	log.Println("Authenticated")

	// 发送文件
	stats, err := SendFile(conn, localFile, remoteFile, nil)
	if err != nil {
		return err
	}
	log.Printf("File sent: %s (%d bytes, hash: %s)", stats.FilePath, stats.Size, stats.Hash)
	return nil
}

// runSendDir 运行客户端：连接、认证、发送目录
func runSendDir(serverAddr, localDir, remoteRoot string) error {
	conn, err := Dial(serverAddr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	// 认证
	if err := Auth(conn, "secret123"); err != nil {
		return err
	}
	log.Println("Authenticated")

	// 发送目录
	if err := SendDir(conn, localDir, remoteRoot, nil); err != nil {
		return err
	}
	log.Printf("Directory sent: %s -> %s", localDir, remoteRoot)
	return nil
}