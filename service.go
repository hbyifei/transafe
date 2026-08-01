package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
)

const DefaultChunkSize = 16 * 1024 * 1024

type TransferOptions struct {
	ChunkSize int
	Resume    bool
}

type TransferStats struct {
	FilePath string
	Size     int64
	Hash     string
}

func Auth(conn net.Conn, password string) error {
	if err := SendPacket(conn, CmdAuth, []byte(password)); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}
	pkt, err := RecvPacket(conn)
	if err != nil {
		return fmt.Errorf("recv auth response: %w", err)
	}
	if pkt.Cmd == CmdError {
		return fmt.Errorf("auth failed: %s", string(pkt.Data))
	}
	return nil
}

// ---------- 单文件传输（保留向后兼容） ----------

func SendFile(conn net.Conn, localPath, remotePath string, opts *TransferOptions) (*TransferStats, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open local file: %w", err)
	}
	defer f.Close()

	stat, _ := f.Stat()
	fileSize := stat.Size()

	nameBytes := []byte(remotePath)
	meta := make([]byte, 4+len(nameBytes)+8)
	binary.BigEndian.PutUint32(meta[:4], uint32(len(nameBytes)))
	copy(meta[4:], nameBytes)
	binary.BigEndian.PutUint64(meta[4+len(nameBytes):], uint64(fileSize))
	if err := SendPacket(conn, CmdFile, meta); err != nil {
		return nil, fmt.Errorf("send file meta: %w", err)
	}

	chunkSize := DefaultChunkSize
	if opts != nil && opts.ChunkSize > 0 {
		chunkSize = opts.ChunkSize
	}

	buf := make([]byte, chunkSize)
	hasher := sha256.New()
	var offset int64
	for offset < fileSize {
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read file at offset %d: %w", offset, err)
		}
		if n == 0 {
			break
		}
		hasher.Write(buf[:n])
		chunkData := make([]byte, 8+n)
		binary.BigEndian.PutUint64(chunkData[:8], uint64(offset))
		copy(chunkData[8:], buf[:n])
		if err := SendPacket(conn, CmdChunk, chunkData); err != nil {
			return nil, fmt.Errorf("send chunk at offset %d: %w", offset, err)
		}
		offset += int64(n)
	}

	hashHex := hex.EncodeToString(hasher.Sum(nil))
	if err := SendPacket(conn, CmdHash, []byte(hashHex)); err != nil {
		return nil, fmt.Errorf("send hash: %w", err)
	}

	pkt, err := RecvPacket(conn)
	if err != nil {
		return nil, fmt.Errorf("recv response: %w", err)
	}
	if pkt.Cmd == CmdError {
		return nil, fmt.Errorf("server error: %s", string(pkt.Data))
	}
	return &TransferStats{FilePath: remotePath, Size: fileSize, Hash: hashHex}, nil
}

func ReceiveFile(conn net.Conn, data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("invalid file meta")
	}
	nameLen := binary.BigEndian.Uint32(data[:4])
	fileName := string(data[4 : 4+nameLen])
	fileSize := int64(binary.BigEndian.Uint64(data[4+nameLen : 12+nameLen]))

	savePath := fileName
	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	f, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if fileSize == 0 {
		pkt, err := RecvPacket(conn)
		if err != nil {
			return fmt.Errorf("recv hash for empty file: %w", err)
		}
		if pkt.Cmd != CmdHash {
			return fmt.Errorf("expected CmdHash for empty file, got %d", pkt.Cmd)
		}
		hasher := sha256.New()
		localHex := hex.EncodeToString(hasher.Sum(nil))
		if string(pkt.Data) == localHex {
			return SendPacket(conn, CmdOK, []byte(localHex))
		}
		return SendPacket(conn, CmdError, []byte("hash mismatch"))
	}

	var received int64
	for received < fileSize {
		pkt, err := RecvPacket(conn)
		if err != nil {
			return fmt.Errorf("recv chunk: %w", err)
		}
		switch pkt.Cmd {
		case CmdChunk:
			if len(pkt.Data) < 8 {
				return fmt.Errorf("invalid chunk data")
			}
			offset := int64(binary.BigEndian.Uint64(pkt.Data[:8]))
			chunkData := pkt.Data[8:]
			if _, err := f.WriteAt(chunkData, offset); err != nil {
				return fmt.Errorf("write at offset %d: %w", offset, err)
			}
			received += int64(len(chunkData))
		case CmdHash:
			f.Close()
			ff, err := os.Open(savePath)
			if err != nil {
				return fmt.Errorf("reopen file for hash: %w", err)
			}
			defer ff.Close()
			hasher := sha256.New()
			if _, err := io.Copy(hasher, ff); err != nil {
				return fmt.Errorf("compute hash: %w", err)
			}
			localHex := hex.EncodeToString(hasher.Sum(nil))
			if string(pkt.Data) == localHex {
				return SendPacket(conn, CmdOK, []byte(localHex))
			}
			return SendPacket(conn, CmdError, []byte("hash mismatch"))
		default:
			return fmt.Errorf("unexpected command during file receive: %d", pkt.Cmd)
		}
	}
	// 所有分块收完但未收到 CmdHash
	pkt, err := RecvPacket(conn)
	if err != nil {
		return fmt.Errorf("recv final hash: %w", err)
	}
	if pkt.Cmd == CmdHash {
		f.Close()
		ff, _ := os.Open(savePath)
		hasher := sha256.New()
		io.Copy(hasher, ff)
		ff.Close()
		localHex := hex.EncodeToString(hasher.Sum(nil))
		if string(pkt.Data) == localHex {
			return SendPacket(conn, CmdOK, []byte(localHex))
		}
		return SendPacket(conn, CmdError, []byte("hash mismatch"))
	}
	return fmt.Errorf("expected CmdHash after all chunks, got %d", pkt.Cmd)
}

// ---------- 目录传输（逐文件模式，保留） ----------

func SendDir(conn net.Conn, localDir, remoteRoot string, opts *TransferOptions) error {
	absLocalDir, err := filepath.Abs(localDir)
	if err != nil {
		return fmt.Errorf("resolve local dir: %w", err)
	}

	var items []string
	var stack []string
	stack = append(stack, absLocalDir)
	for len(stack) > 0 {
		n := len(stack)
		currentPath := stack[n-1]
		stack = stack[:n-1]
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			log.Printf("Warning: skip dir %s: %v", currentPath, err)
			continue
		}
		for _, entry := range entries {
			fullPath := filepath.Join(currentPath, entry.Name())
			items = append(items, fullPath)
			if entry.IsDir() {
				stack = append(stack, fullPath)
			}
		}
	}

	rootBytes := []byte(remoteRoot)
	startData := make([]byte, 4+len(rootBytes)+4)
	binary.BigEndian.PutUint32(startData[:4], uint32(len(rootBytes)))
	copy(startData[4:], rootBytes)
	binary.BigEndian.PutUint32(startData[4+len(rootBytes):], uint32(len(items)))
	if err := SendPacket(conn, CmdDirStart, startData); err != nil {
		return fmt.Errorf("send dir start: %w", err)
	}

	for _, localPath := range items {
		relPath, _ := filepath.Rel(absLocalDir, localPath)
		remotePath := filepath.ToSlash(filepath.Join(remoteRoot, relPath))

		info, err := os.Stat(localPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", localPath, err)
		}

		if info.IsDir() {
			nameBytes := []byte(remotePath)
			meta := make([]byte, 4+len(nameBytes))
			binary.BigEndian.PutUint32(meta[:4], uint32(len(nameBytes)))
			copy(meta[4:], nameBytes)
			if err := SendPacket(conn, CmdDir, meta); err != nil {
				return fmt.Errorf("send dir meta: %w", err)
			}
			pkt, err := RecvPacket(conn)
			if err != nil {
				return fmt.Errorf("recv dir response: %w", err)
			}
			if pkt.Cmd == CmdError {
				return fmt.Errorf("server error for dir %s: %s", remotePath, string(pkt.Data))
			}
		} else {
			if _, err := SendFile(conn, localPath, remotePath, opts); err != nil {
				return fmt.Errorf("send file %s: %w", localPath, err)
			}
		}
	}

	if err := SendPacket(conn, CmdDirEnd, nil); err != nil {
		return fmt.Errorf("send dir end: %w", err)
	}
	return nil
}

func ReceiveDir(conn net.Conn, data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("invalid dir start data")
	}
	rootLen := binary.BigEndian.Uint32(data[:4])
	remoteRoot := string(data[4 : 4+rootLen])
	itemCount := binary.BigEndian.Uint32(data[4+rootLen:])

	log.Printf("Receiving directory: %s (%d items)", remoteRoot, itemCount)

	if err := os.MkdirAll(remoteRoot, 0755); err != nil {
		return fmt.Errorf("create root dir: %w", err)
	}

	for i := uint32(0); i < itemCount; i++ {
		pkt, err := RecvPacket(conn)
		if err != nil {
			return fmt.Errorf("recv item meta %d/%d: %w", i+1, itemCount, err)
		}

		switch pkt.Cmd {
		case CmdFile:
			if err := ReceiveFile(conn, pkt.Data); err != nil {
				return fmt.Errorf("receive file %d/%d: %w", i+1, itemCount, err)
			}
		case CmdDir:
			if len(pkt.Data) < 4 {
				return fmt.Errorf("invalid dir meta")
			}
			dirNameLen := binary.BigEndian.Uint32(pkt.Data[:4])
			dirName := string(pkt.Data[4 : 4+dirNameLen])
			if err := os.MkdirAll(dirName, 0755); err != nil {
				return fmt.Errorf("create dir %s: %w", dirName, err)
			}
			log.Printf("Created directory: %s", dirName)
			if err := SendPacket(conn, CmdOK, []byte("dir created")); err != nil {
				return fmt.Errorf("send dir ok: %w", err)
			}
		default:
			return fmt.Errorf("unexpected command during dir receive: %d", pkt.Cmd)
		}
		log.Printf("Received item %d/%d", i+1, itemCount)
	}

	pkt, err := RecvPacket(conn)
	if err != nil {
		return fmt.Errorf("recv dir end: %w", err)
	}
	if pkt.Cmd != CmdDirEnd {
		return fmt.Errorf("expected CmdDirEnd, got %d", pkt.Cmd)
	}

	log.Printf("Directory received: %s", remoteRoot)
	return SendPacket(conn, CmdOK, []byte("dir received"))
}

// ---------- Phase 2: 单连接流式传输 ----------

func SendStream(conn net.Conn, localDir, remoteRoot string, opts *TransferOptions) error {
	absLocalDir, err := filepath.Abs(localDir)
	if err != nil {
		return fmt.Errorf("resolve local dir: %w", err)
	}

	type fileEntry struct {
		localPath  string
		remotePath string
		isDir      bool
		size       int64
	}
	var entries []fileEntry
	var totalFiles int
	var totalSize int64

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
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk dir: %w", err)
	}

	// 1. 发送流开始包
	rootBytes := []byte(remoteRoot)
	startData := make([]byte, 4+len(rootBytes)+4+8)
	binary.BigEndian.PutUint32(startData[:4], uint32(len(rootBytes)))
	copy(startData[4:], rootBytes)
	binary.BigEndian.PutUint32(startData[4+len(rootBytes):], uint32(totalFiles))
	binary.BigEndian.PutUint64(startData[4+len(rootBytes)+4:], uint64(totalSize))

	if err := SendPacket(conn, CmdStreamStart, startData); err != nil {
		return fmt.Errorf("send stream start: %w", err)
	}

	// 2. 逐个发送文件头和文件数据
	chunkSize := DefaultChunkSize
	if opts != nil && opts.ChunkSize > 0 {
		chunkSize = opts.ChunkSize
	}
	buf := make([]byte, chunkSize)

	for _, entry := range entries {
		if entry.isDir {
			// 目录：文件名末尾加 '/' 标记
			dirName := entry.remotePath
			if dirName[len(dirName)-1] != '/' {
				dirName += "/"
			}
			headerData := make([]byte, 4+len(dirName)+8)
			binary.BigEndian.PutUint32(headerData[:4], uint32(len(dirName)))
			copy(headerData[4:], []byte(dirName))
			binary.BigEndian.PutUint64(headerData[4+len(dirName):], 0)
			if err := SendPacket(conn, CmdStreamFileHeader, headerData); err != nil {
				return fmt.Errorf("send dir header: %w", err)
			}
			continue
		}

		// 文件头
		headerData := make([]byte, 4+len(entry.remotePath)+8)
		binary.BigEndian.PutUint32(headerData[:4], uint32(len(entry.remotePath)))
		copy(headerData[4:], []byte(entry.remotePath))
		binary.BigEndian.PutUint64(headerData[4+len(entry.remotePath):], uint64(entry.size))
		if err := SendPacket(conn, CmdStreamFileHeader, headerData); err != nil {
			return fmt.Errorf("send file header: %w", err)
		}

		// 文件数据
		f, err := os.Open(entry.localPath)
		if err != nil {
			return fmt.Errorf("open file %s: %w", entry.localPath, err)
		}
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if err := SendPacket(conn, CmdStreamData, buf[:n]); err != nil {
					f.Close()
					return fmt.Errorf("send stream data: %w", err)
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				f.Close()
				return fmt.Errorf("read file %s: %w", entry.localPath, err)
			}
		}
		f.Close()
	}

	// 3. 发送流结束包
	if err := SendPacket(conn, CmdStreamEnd, nil); err != nil {
		return fmt.Errorf("send stream end: %w", err)
	}

	// 4. 等待服务端响应
	pkt, err := RecvPacket(conn)
	if err != nil {
		return fmt.Errorf("recv stream response: %w", err)
	}
	if pkt.Cmd == CmdError {
		return fmt.Errorf("stream error: %s", string(pkt.Data))
	}
	log.Printf("Stream completed: %s", string(pkt.Data))
	return nil
}

// ReceiveStream 接收流式目录（startData 是已经收到的 CmdStreamStart 包的数据体）
func ReceiveStream(conn net.Conn, startData []byte) error {
	data := startData
	if len(data) < 4+8 {
		return fmt.Errorf("invalid stream start data")
	}
	rootLen := binary.BigEndian.Uint32(data[:4])
	remoteRoot := string(data[4 : 4+rootLen])
	offset := 4 + rootLen
	totalFiles := binary.BigEndian.Uint32(data[offset : offset+4])
	totalSize := binary.BigEndian.Uint64(data[offset+4 : offset+12])

	log.Printf("Receiving stream: %s (%d files, %d bytes)", remoteRoot, totalFiles, totalSize)

	if err := os.MkdirAll(remoteRoot, 0755); err != nil {
		return fmt.Errorf("create root dir: %w", err)
	}

	var currentFile *os.File
	var currentFileName string
	var currentFileSize int64
	var receivedBytes int64
	hasher := sha256.New()

	for {
		pkt, err := RecvPacket(conn)
		if err != nil {
			if currentFile != nil {
				currentFile.Close()
			}
			return fmt.Errorf("recv stream packet: %w", err)
		}

		switch pkt.Cmd {
		case CmdStreamFileHeader:
			if currentFile != nil {
				currentFile.Close()
				currentFile = nil
			}
			if len(pkt.Data) < 4+8 {
				return fmt.Errorf("invalid file header")
			}
			nameLen := binary.BigEndian.Uint32(pkt.Data[:4])
			fileName := string(pkt.Data[4 : 4+nameLen])
			fileSize := int64(binary.BigEndian.Uint64(pkt.Data[4+nameLen : 12+nameLen]))

			if fileSize == 0 {
				// 可能是空文件或目录
				if len(fileName) > 0 && fileName[len(fileName)-1] == '/' {
					// 目录
					dirName := fileName[:len(fileName)-1]
					if err := os.MkdirAll(dirName, 0755); err != nil {
						return fmt.Errorf("create dir %s: %w", dirName, err)
					}
					log.Printf("Created directory: %s", dirName)
				} else {
					// 空文件
					parentDir := filepath.Dir(fileName)
					if err := os.MkdirAll(parentDir, 0755); err != nil {
						return fmt.Errorf("create parent dir for %s: %w", fileName, err)
					}
					f, err := os.Create(fileName)
					if err != nil {
						return fmt.Errorf("create empty file %s: %w", fileName, err)
					}
					f.Close()
					log.Printf("Created empty file: %s", fileName)
				}
			} else {
				parentDir := filepath.Dir(fileName)
				if err := os.MkdirAll(parentDir, 0755); err != nil {
					return fmt.Errorf("create parent dir for %s: %w", fileName, err)
				}
				f, err := os.Create(fileName)
				if err != nil {
					return fmt.Errorf("create file %s: %w", fileName, err)
				}
				currentFile = f
				currentFileName = fileName
				currentFileSize = fileSize
				receivedBytes = 0
			}

		case CmdStreamData:
			if currentFile == nil {
				log.Printf("Warning: received data without file header, ignoring")
				continue
			}
			n, err := currentFile.Write(pkt.Data)
			if err != nil {
				return fmt.Errorf("write to %s: %w", currentFileName, err)
			}
			hasher.Write(pkt.Data[:n])
			receivedBytes += int64(n)
			if receivedBytes >= currentFileSize {
				currentFile.Close()
				currentFile = nil
				log.Printf("Received file: %s (%d bytes)", currentFileName, currentFileSize)
			}

		case CmdStreamEnd:
			if currentFile != nil {
				currentFile.Close()
				currentFile = nil
			}
			hashHex := hex.EncodeToString(hasher.Sum(nil))
			log.Printf("Stream finished, overall hash: %s", hashHex)
			return SendPacket(conn, CmdOK, []byte(hashHex))

		default:
			log.Printf("Unexpected command during stream: %d", pkt.Cmd)
		}
	}
}

// SendText 预留
func SendText(conn net.Conn, text string) error {
	return fmt.Errorf("chat feature not implemented yet")
}