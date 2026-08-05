package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

func main() {
	// 确保 log 目录存在
	if err := os.MkdirAll("log", 0755); err != nil {
		log.Fatal(err)
	}

	// 日志重定向到文件，终端只留进度条
	logFile, err := os.OpenFile("log/transfer.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	// 提示用户日志文件位置
	fmt.Println("Logging to log/transfer.log")

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
		jobs := 1
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
			fmt.Println("Usage: transafe senddir [-j N] <server_addr> <local_dir> <remote_root>")
			os.Exit(1)
		}
		serverAddr := args[addrIdx]
		localDir := args[addrIdx+1]
		remoteRoot := args[addrIdx+2]

		if jobs == 1 {
			if err := runStream(serverAddr, localDir, remoteRoot); err != nil {
				log.Fatalf("senddir error: %v", err)
			}
		} else {
			if err := runStreamParallel(serverAddr, localDir, remoteRoot, jobs); err != nil {
				log.Fatalf("senddir error: %v", err)
			}
		}

	case "cleanlog":
		// 清空日志文件
		if err := os.Truncate("log/transfer.log", 0); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to clean log: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Log file log/transfer.log has been cleared.")

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Println("Usage: transafe [server|client|senddir|cleanlog] ...")
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
		case CmdQueryOffset:
			remotePath := string(pkt.Data)
			if err := handleQueryOffset(conn, remotePath); err != nil {
				log.Printf("Query offset error: %v", err)
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

	stats, err := SendFile(conn, localFile, remoteFile, &TransferOptions{Resume: true})
	if err != nil {
		return err
	}
	log.Printf("File sent: %s (%d bytes, hash: %s)", stats.FilePath, stats.Size, stats.Hash)
	return nil
}

func runStream(serverAddr, localDir, remoteRoot string) error {
	absLocalDir, err := filepath.Abs(localDir)
	if err != nil {
		return fmt.Errorf("resolve local dir: %w", err)
	}

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

	var fileEntries []fileEntry
	for _, e := range entries {
		if !e.isDir {
			fileEntries = append(fileEntries, e)
		}
	}
	if len(fileEntries) == 0 {
		return fmt.Errorf("no files to transfer")
	}

	progress := NewProgress(totalFiles, totalBytes, 1)

	conn, err := Dial(serverAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	if err := Auth(conn, getPassword()); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	log.Println("Authenticated")

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
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

	err, actualFiles, actualBytes := SendFileListStream(conn, fileEntries, remoteRoot, nil, func(doneFiles int, doneBytes int64) {
		progress.Add(doneFiles, doneBytes)
	})

	if err == nil {
		progress.SetTotal(actualFiles, actualBytes)
		progress.SetDone(actualFiles, actualBytes)
		progress.Print()
		time.Sleep(400 * time.Millisecond)
	}

	close(done)
	time.Sleep(600 * time.Millisecond)

	if err != nil {
		progress.SetErrorCount(1)
		progress.Done()
		return fmt.Errorf("send stream: %w", err)
	}

	progress.SetErrorCount(0)
	progress.Done()
	log.Printf("Transfer completed: expected %d/%d bytes, actual %d/%d bytes",
		totalFiles, totalBytes, actualFiles, actualBytes)
	return nil
}

func runStreamParallel(serverAddr, localDir, remoteRoot string, jobs int) error {
	absLocalDir, err := filepath.Abs(localDir)
	if err != nil {
		return fmt.Errorf("resolve local dir: %w", err)
	}

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

	var fileEntries []fileEntry
	for _, e := range entries {
		if !e.isDir {
			fileEntries = append(fileEntries, e)
		}
	}
	if len(fileEntries) == 0 {
		return fmt.Errorf("no files to transfer")
	}

	progress := NewProgress(totalFiles, totalBytes, jobs)
	chunks := chunkSlice(fileEntries, jobs)

	var wg sync.WaitGroup
	errChan := make(chan error, jobs)
	type result struct {
		files int
		bytes int64
	}
	resultChan := make(chan result, jobs)

	for i, chunk := range chunks {
		wg.Add(1)
		go func(workerID int, files []fileEntry) {
			defer wg.Done()
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

			err, actualFiles, actualBytes := SendFileListStream(conn, files, remoteRoot, nil, func(doneFiles int, doneBytes int64) {
				progress.Add(doneFiles, doneBytes)
			})
			if err != nil {
				errChan <- fmt.Errorf("worker %d stream: %w", workerID, err)
				return
			}
			resultChan <- result{files: actualFiles, bytes: actualBytes}
		}(i, chunk)
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
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
	close(resultChan)

	var totalActualFiles int
	var totalActualBytes int64
	for r := range resultChan {
		totalActualFiles += r.files
		totalActualBytes += r.bytes
	}

	var lastErr error
	for err := range errChan {
		if err != nil {
			log.Println("ERROR:", err)
			lastErr = err
		}
	}

	if lastErr == nil {
		progress.SetTotal(totalActualFiles, totalActualBytes)
		progress.SetDone(totalActualFiles, totalActualBytes)
		progress.Print()
		time.Sleep(400 * time.Millisecond)
	}

	close(done)
	time.Sleep(600 * time.Millisecond)

	if lastErr != nil {
		progress.SetErrorCount(1)
	} else {
		progress.SetErrorCount(0)
	}
	progress.Done()
	log.Printf("Transfer completed: expected %d/%d bytes, actual %d/%d bytes",
		totalFiles, totalBytes, totalActualFiles, totalActualBytes)

	return lastErr
}

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