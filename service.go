// service.go
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

type TransferOptions struct {
	ChunkSize int
	Resume    bool
}

type TransferStats struct {
	FilePath string
	Size     int64
	Hash     string
}

const DefaultChunkSize = 16 * 1024 * 1024

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

// SendFile 发送单个文件（包括空文件）
func SendFile(conn net.Conn, localPath, remotePath string, opts *TransferOptions) (*TransferStats, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open local file: %w", err)
	}
	defer f.Close()

	stat, _ := f.Stat()
	fileSize := stat.Size()

	// 1. 发送文件元信息
	nameBytes := []byte(remotePath)
	meta := make([]byte, 4+len(nameBytes)+8)
	binary.BigEndian.PutUint32(meta[:4], uint32(len(nameBytes)))
	copy(meta[4:], nameBytes)
	binary.BigEndian.PutUint64(meta[4+len(nameBytes):], uint64(fileSize))
	if err := SendPacket(conn, CmdFile, meta); err != nil {
		return nil, fmt.Errorf("send file meta: %w", err)
	}

	// 2. 分块发送（空文件跳过）
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

	// 3. 发送哈希
	hashHex := hex.EncodeToString(hasher.Sum(nil))
	if err := SendPacket(conn, CmdHash, []byte(hashHex)); err != nil {
		return nil, fmt.Errorf("send hash: %w", err)
	}

	// 4. 等待响应
	pkt, err := RecvPacket(conn)
	if err != nil {
		return nil, fmt.Errorf("recv response: %w", err)
	}
	if pkt.Cmd == CmdError {
		return nil, fmt.Errorf("server error: %s", string(pkt.Data))
	}
	return &TransferStats{FilePath: remotePath, Size: fileSize, Hash: hashHex}, nil
}

// ReceiveFile 接收单个文件（支持空文件）
func ReceiveFile(conn net.Conn, data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("invalid file meta")
	}
	nameLen := binary.BigEndian.Uint32(data[:4])
	fileName := string(data[4 : 4+nameLen])
	fileSize := int64(binary.BigEndian.Uint64(data[4+nameLen : 12+nameLen]))

	// 创建父目录
	savePath := fileName
	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	// 创建文件（即使是空文件也要创建）
	f, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	// 如果是空文件，直接等待哈希
	if fileSize == 0 {
		// 空文件：等待 CmdHash
		pkt, err := RecvPacket(conn)
		if err != nil {
			return fmt.Errorf("recv hash for empty file: %w", err)
		}
		if pkt.Cmd != CmdHash {
			return fmt.Errorf("expected CmdHash for empty file, got %d", pkt.Cmd)
		}
		// 计算空文件的哈希
		hasher := sha256.New()
		localHex := hex.EncodeToString(hasher.Sum(nil))
		if string(pkt.Data) == localHex {
			return SendPacket(conn, CmdOK, []byte(localHex))
		} else {
			return SendPacket(conn, CmdError, []byte(fmt.Sprintf("hash mismatch: local=%s remote=%s", localHex, string(pkt.Data))))
		}
	}

	// 正常文件：循环接收分块
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
			} else {
				return SendPacket(conn, CmdError, []byte(fmt.Sprintf("hash mismatch: local=%s remote=%s", localHex, string(pkt.Data))))
			}
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
		} else {
			return SendPacket(conn, CmdError, []byte("hash mismatch"))
		}
	}
	return fmt.Errorf("expected CmdHash after all chunks, got %d", pkt.Cmd)
}

// SendDir 发送整个目录
func SendDir(conn net.Conn, localDir, remoteRoot string, opts *TransferOptions) error {
	absLocalDir, err := filepath.Abs(localDir)
	if err != nil {
		return fmt.Errorf("resolve local dir: %w", err)
	}

	// 收集所有文件和目录
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

	// 发送目录开始标记
	rootBytes := []byte(remoteRoot)
	startData := make([]byte, 4+len(rootBytes)+4)
	binary.BigEndian.PutUint32(startData[:4], uint32(len(rootBytes)))
	copy(startData[4:], rootBytes)
	binary.BigEndian.PutUint32(startData[4+len(rootBytes):], uint32(len(items)))
	if err := SendPacket(conn, CmdDirStart, startData); err != nil {
		return fmt.Errorf("send dir start: %w", err)
	}

	// 逐个发送
	for _, localPath := range items {
		relPath, _ := filepath.Rel(absLocalDir, localPath)
		remotePath := filepath.ToSlash(filepath.Join(remoteRoot, relPath))

		info, err := os.Stat(localPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", localPath, err)
		}

		if info.IsDir() {
			// 目录：使用 CmdDir 命令
			nameBytes := []byte(remotePath)
			meta := make([]byte, 4+len(nameBytes))
			binary.BigEndian.PutUint32(meta[:4], uint32(len(nameBytes)))
			copy(meta[4:], nameBytes)
			if err := SendPacket(conn, CmdDir, meta); err != nil {
				return fmt.Errorf("send dir meta: %w", err)
			}
			// 等待响应
			pkt, err := RecvPacket(conn)
			if err != nil {
				return fmt.Errorf("recv dir response: %w", err)
			}
			if pkt.Cmd == CmdError {
				return fmt.Errorf("server error for dir %s: %s", remotePath, string(pkt.Data))
			}
		} else {
			// 文件：调用 SendFile
			if _, err := SendFile(conn, localPath, remotePath, opts); err != nil {
				return fmt.Errorf("send file %s: %w", localPath, err)
			}
		}
	}

	// 发送目录结束标记
	if err := SendPacket(conn, CmdDirEnd, nil); err != nil {
		return fmt.Errorf("send dir end: %w", err)
	}
	return nil
}

// ReceiveDir 接收整个目录
func ReceiveDir(conn net.Conn, data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("invalid dir start data")
	}
	rootLen := binary.BigEndian.Uint32(data[:4])
	remoteRoot := string(data[4 : 4+rootLen])
	itemCount := binary.BigEndian.Uint32(data[4+rootLen:])

	log.Printf("Receiving directory: %s (%d items)", remoteRoot, itemCount)

	// 确保根目录存在
	if err := os.MkdirAll(remoteRoot, 0755); err != nil {
		return fmt.Errorf("create root dir: %w", err)
	}

	// 循环接收每个项目
	for i := uint32(0); i < itemCount; i++ {
		pkt, err := RecvPacket(conn)
		if err != nil {
			return fmt.Errorf("recv item meta %d/%d: %w", i+1, itemCount, err)
		}

		switch pkt.Cmd {
		case CmdFile:
			// 文件
			if err := ReceiveFile(conn, pkt.Data); err != nil {
				return fmt.Errorf("receive file %d/%d: %w", i+1, itemCount, err)
			}
		case CmdDir:
			// 目录
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

	// 接收目录结束标记
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

func SendText(conn net.Conn, text string) error {
	return fmt.Errorf("chat feature not implemented yet")
}