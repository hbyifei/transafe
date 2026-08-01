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
		RunShell()
		return
	}

	switch os.Args[1] {
	case "server":
		addr := ":9000"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		if err := startServer(addr); err != nil {
			log.Fatalf("Server error: %v", err)
		}

	case "client":
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

	case "stream":
		if len(os.Args) < 4 {
			fmt.Println("Usage: transafe stream <server_addr> <local_dir> <remote_root>")
			os.Exit(1)
		}
		serverAddr := os.Args[2]
		localDir := os.Args[3]
		remoteRoot := os.Args[4]
		if err := runStream(serverAddr, localDir, remoteRoot); err != nil {
			log.Fatalf("Stream error: %v", err)
		}

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Println("Usage: transafe [server|client|senddir|stream] ...")
		os.Exit(1)
	}
}

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

func handleConnection(conn net.Conn) {
	defer conn.Close()
	log.Printf("New connection from %s", conn.RemoteAddr())

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
		case CmdStreamStart:
			// 将已收到的开始包数据传递给 ReceiveStream
			if err := ReceiveStream(conn, pkt.Data); err != nil {
				log.Printf("Receive stream error: %v", err)
				SendPacket(conn, CmdError, []byte(err.Error()))
			}
		case CmdText:
			log.Printf("[Chat] %s", string(pkt.Data))
			SendPacket(conn, CmdOK, []byte("text received"))
		default:
			SendPacket(conn, CmdError, []byte("unknown command"))
		}
	}
}

func runClient(serverAddr, localFile, remoteFile string) error {
	conn, err := Dial(serverAddr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := Auth(conn, "secret123"); err != nil {
		return err
	}
	log.Println("Authenticated")

	stats, err := SendFile(conn, localFile, remoteFile, nil)
	if err != nil {
		return err
	}
	log.Printf("File sent: %s (%d bytes, hash: %s)", stats.FilePath, stats.Size, stats.Hash)
	return nil
}

func runSendDir(serverAddr, localDir, remoteRoot string) error {
	conn, err := Dial(serverAddr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := Auth(conn, "secret123"); err != nil {
		return err
	}
	log.Println("Authenticated")

	if err := SendDir(conn, localDir, remoteRoot, nil); err != nil {
		return err
	}
	log.Printf("Directory sent: %s -> %s", localDir, remoteRoot)
	return nil
}

func runStream(serverAddr, localDir, remoteRoot string) error {
	conn, err := Dial(serverAddr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := Auth(conn, "secret123"); err != nil {
		return err
	}
	log.Println("Authenticated")

	if err := SendStream(conn, localDir, remoteRoot, nil); err != nil {
		return err
	}
	log.Printf("Stream sent: %s -> %s", localDir, remoteRoot)
	return nil
}