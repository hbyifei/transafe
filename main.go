package main

import (
	"fmt"
	"log"
	"os"
	"net"
	"strconv"
	"sync"
	"time"
	"path/filepath"
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
			fmt.Println("Usage: transafe stream [-j N] <server_addr> <local_dir> <remote_root>")
			os.Exit(1)
		}
		// 解析 -j 参数
		jobs := 4 // 默认并行数
		args := os.Args[2:]
		addrIdx := 0
		if len(args) >= 2 && args[0] == "-j" {
			n, err := strconv.Atoi(args[1])
			if err == nil && n > 0 {
				jobs = n
			}
			addrIdx = 2
		}
		if len(args) < addrIdx+3 {
			fmt.Println("Usage: transafe stream [-j N] <server_addr> <local_dir> <remote_root>")
			os.Exit(1)
		}
		serverAddr := args[addrIdx]
		localDir := args[addrIdx+1]
		remoteRoot := args[addrIdx+2]
		if err := runStreamParallel(serverAddr, localDir, remoteRoot, jobs); err != nil {
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
	if pkt.Cmd != CmdAuth || string(pkt.Data) != getPassword() {
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

	if err := Auth(conn, getPassword()); err != nil {
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

	if err := Auth(conn, getPassword()); err != nil {
		return err
	}
	log.Println("Authenticated")

	if err := SendDir(conn, localDir, remoteRoot, nil); err != nil {
		return err
	}
	log.Printf("Directory sent: %s -> %s", localDir, remoteRoot)
	return nil
}

// runStreamParallel 并行流式发送目录
func runStreamParallel(serverAddr, localDir, remoteRoot string, jobs int) error {
	absLocalDir, err := filepath.Abs(localDir)
	if err != nil {
		return fmt.Errorf("resolve local dir: %w", err)
	}

	// 扫描目录获取文件列表
	var entries []fileEntry
	var totalFiles int
	var totalBytes int64

	err = filepath.Walk(absLocalDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Warning: skip %s: %v", path, err)
			return nil
		}
		relPath, _ := filepath.Rel(absLocalDir, path)
		remotePath := filepath.ToSlash(filepath.Join(remoteRoot, relPath))
		entries = append(entries, fileEntry{
			localPath:  path,
			remotePath: remotePath,
			isDir:      info.IsDir(),
			size:       info.Size(),
		})
		if !info.IsDir() {
			totalFiles++
			totalBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk dir: %w", err)
	}

	// 提取文件列表（不含目录）
	var fileEntries []fileEntry
	for _, e := range entries {
		if !e.isDir {
			fileEntries = append(fileEntries, e)
		}
	}
	if len(fileEntries) == 0 {
		return fmt.Errorf("no files to transfer")
	}

	// 创建进度跟踪
	progress := NewProgress(totalFiles, totalBytes, jobs)

	// 将文件列表分片
	chunks := chunkSlice(fileEntries, jobs)

	// 启动 worker
	var wg sync.WaitGroup
	errChan := make(chan error, jobs)

	for i, chunk := range chunks {
		wg.Add(1)
		go func(workerID int, files []fileEntry) {
			defer wg.Done()
			// 每个 worker 建立独立连接
			conn, err := Dial(serverAddr, 5*time.Second)
			if err != nil {
				errChan <- fmt.Errorf("worker %d dial: %w", workerID, err)
				return
			}
			defer conn.Close()

			if err := Auth(conn, getPassword()); err != nil {
				errChan <- fmt.Errorf("worker %d auth: %w", workerID, err)
				return
			}

			// 使用 SendFileListStream 发送该 worker 的文件列表
			err = SendFileListStream(conn, files, remoteRoot, nil, func(doneFiles int, doneBytes int64) {
				progress.Add(doneFiles, doneBytes)
			})
			if err != nil {
				errChan <- fmt.Errorf("worker %d stream: %w", workerID, err)
			}
		}(i, chunk)
	}

	// 启动进度显示协程
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				progress.Print()
			case <-done:
				return
			}
		}
	}()

	wg.Wait()
	close(errChan)
	close(done)
	progress.Done()

	// 检查错误
	for err := range errChan {
		if err != nil {
			return err
		}
	}
	return nil
}

// chunkSlice 将切片分成 n 份
func chunkSlice[T any](slice []T, n int) [][]T {
	if n <= 0 {
		n = 1
	}
	chunks := make([][]T, 0, n)
	chunkSize := (len(slice) + n - 1) / n
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}
	return chunks
}

// getPassword 获取密码（从环境变量或默认值）
func getPassword() string {
	if pwd := os.Getenv("TRANSAFE_PASSWORD"); pwd != "" {
		return pwd
	}
	return "secret123"
}