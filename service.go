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

// fileEntry 文件条目，用于并行传输
type fileEntry struct {
	localPath  string
	remotePath string
	isDir      bool
	size       int64
}

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

// ---------- 单文件传输（支持断点续传） ----------

func SendFile(conn net.Conn, localPath, remotePath string, opts *TransferOptions) (*TransferStats, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open local file: %w", err)
	}
	defer f.Close()

	stat, _ := f.Stat()
	fileSize := stat.Size()

	// 查询服务端已接收的偏移量
	resumeOffset := int64(0)
	if opts != nil && opts.Resume {
		SendPacket(conn, CmdQueryOffset, []byte(remotePath))
		pkt, err := RecvPacket(conn)
		if err != nil {
			return nil, fmt.Errorf("query offset: %w", err)
		}
		if pkt.Cmd == CmdOffsetResp {
			resumeOffset = int64(binary.BigEndian.Uint64(pkt.Data))
			if resumeOffset > 0 {
				log.Printf("Resuming %s from offset %d/%d", remotePath, resumeOffset, fileSize)
			}
		}
	}

	// 发送文件元信息（含起始偏移量）
	nameBytes := []byte(remotePath)
	meta := make([]byte, 4+len(nameBytes)+8+8)
	binary.BigEndian.PutUint32(meta[:4], uint32(len(nameBytes)))
	copy(meta[4:], nameBytes)
	binary.BigEndian.PutUint64(meta[4+len(nameBytes):], uint64(fileSize))
	binary.BigEndian.PutUint64(meta[4+len(nameBytes)+8:], uint64(resumeOffset))
	if err := SendPacket(conn, CmdFile, meta); err != nil {
		return nil, fmt.Errorf("send file meta: %w", err)
	}

	if resumeOffset > 0 {
		if _, err := f.Seek(resumeOffset, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek to offset %d: %w", resumeOffset, err)
		}
	}

	chunkSize := DefaultChunkSize
	if opts != nil && opts.ChunkSize > 0 {
		chunkSize = opts.ChunkSize
	}

	buf := make([]byte, chunkSize)
	hasher := sha256.New()
	var offset int64 = resumeOffset

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
	if len(data) < 20 {
		return fmt.Errorf("invalid file meta")
	}
	nameLen := binary.BigEndian.Uint32(data[:4])
	fileName := string(data[4 : 4+nameLen])
	fileSize := int64(binary.BigEndian.Uint64(data[4+nameLen : 12+nameLen]))
	startOffset := int64(binary.BigEndian.Uint64(data[12+nameLen : 20+nameLen]))

	savePath := fileName
	partPath := savePath + ".part"
	progressPath := savePath + ".progress"

	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	var f *os.File
	var err error
	if startOffset > 0 {
		f, err = os.OpenFile(partPath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open .part for append: %w", err)
		}
	} else {
		f, err = os.Create(partPath)
		if err != nil {
			return fmt.Errorf("create .part: %w", err)
		}
	}
	defer f.Close()

	writeProgress := func(offset int64) {
		pData := make([]byte, 8)
		binary.BigEndian.PutUint64(pData, uint64(offset))
		os.WriteFile(progressPath, pData, 0644)
	}

	if startOffset > 0 {
		writeProgress(startOffset)
	}

	hasher := sha256.New()
	if startOffset > 0 {
		ff, _ := os.Open(partPath)
		io.Copy(hasher, ff)
		ff.Close()
	}

	if fileSize == 0 {
		f.Close()
		os.Remove(partPath)
		pkt, err := RecvPacket(conn)
		if err != nil {
			return fmt.Errorf("recv hash for empty file: %w", err)
		}
		if pkt.Cmd != CmdHash {
			return fmt.Errorf("expected CmdHash for empty file, got %d", pkt.Cmd)
		}
		emptyHash := hex.EncodeToString(sha256.New().Sum(nil))
		if string(pkt.Data) == emptyHash {
			os.WriteFile(savePath, []byte{}, 0644)
			os.Remove(progressPath)
			return SendPacket(conn, CmdOK, []byte(emptyHash))
		}
		return SendPacket(conn, CmdError, []byte("hash mismatch"))
	}

	var received int64 = startOffset
	for received < fileSize {
		pkt, err := RecvPacket(conn)
		if err != nil {
			writeProgress(received)
			return fmt.Errorf("recv chunk (interrupted at %d/%d): %w", received, fileSize, err)
		}
		switch pkt.Cmd {
		case CmdChunk:
			if len(pkt.Data) < 8 {
				return fmt.Errorf("invalid chunk data")
			}
			chunkOffset := int64(binary.BigEndian.Uint64(pkt.Data[:8]))
			chunkData := pkt.Data[8:]
			if _, err := f.WriteAt(chunkData, chunkOffset); err != nil {
				writeProgress(received)
				return fmt.Errorf("write at offset %d: %w", chunkOffset, err)
			}
			hasher.Write(chunkData)
			received += int64(len(chunkData))
			if received%DefaultChunkSize == 0 {
				writeProgress(received)
			}
		case CmdHash:
			f.Close()
			if err := os.Rename(partPath, savePath); err != nil {
				return fmt.Errorf("rename .part to final: %w", err)
			}
			os.Remove(progressPath)
			ff, err := os.Open(savePath)
			if err != nil {
				return fmt.Errorf("reopen file for hash: %w", err)
			}
			defer ff.Close()
			finalHasher := sha256.New()
			if _, err := io.Copy(finalHasher, ff); err != nil {
				return fmt.Errorf("compute final hash: %w", err)
			}
			localHex := hex.EncodeToString(finalHasher.Sum(nil))
			if string(pkt.Data) == localHex {
				return SendPacket(conn, CmdOK, []byte(localHex))
			}
			return SendPacket(conn, CmdError, []byte("hash mismatch"))
		default:
			writeProgress(received)
			return fmt.Errorf("unexpected command during file receive: %d", pkt.Cmd)
		}
	}
	writeProgress(received)
	return fmt.Errorf("connection closed before hash received (at %d/%d)", received, fileSize)
}

// ---------- 断点续传：查询/响应偏移量 ----------

func handleQueryOffset(conn net.Conn, remotePath string) error {
	progressPath := remotePath + ".progress"
	partPath := remotePath + ".part"

	if data, err := os.ReadFile(progressPath); err == nil && len(data) >= 8 {
		offset := binary.BigEndian.Uint64(data[:8])
		resp := make([]byte, 8)
		binary.BigEndian.PutUint64(resp, offset)
		return SendPacket(conn, CmdOffsetResp, resp)
	}

	if info, err := os.Stat(partPath); err == nil {
		resp := make([]byte, 8)
		binary.BigEndian.PutUint64(resp, uint64(info.Size()))
		return SendPacket(conn, CmdOffsetResp, resp)
	}

	resp := make([]byte, 8)
	binary.BigEndian.PutUint64(resp, 0)
	return SendPacket(conn, CmdOffsetResp, resp)
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

	rootBytes := []byte(remoteRoot)
	startData := make([]byte, 4+len(rootBytes)+4+8)
	binary.BigEndian.PutUint32(startData[:4], uint32(len(rootBytes)))
	copy(startData[4:], rootBytes)
	binary.BigEndian.PutUint32(startData[4+len(rootBytes):], uint32(totalFiles))
	binary.BigEndian.PutUint64(startData[4+len(rootBytes)+4:], uint64(totalSize))

	if err := SendPacket(conn, CmdStreamStart, startData); err != nil {
		return fmt.Errorf("send stream start: %w", err)
	}

	chunkSize := DefaultChunkSize
	if opts != nil && opts.ChunkSize > 0 {
		chunkSize = opts.ChunkSize
	}
	buf := make([]byte, chunkSize)

	for _, entry := range entries {
		if entry.isDir {
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

		headerData := make([]byte, 4+len(entry.remotePath)+8)
		binary.BigEndian.PutUint32(headerData[:4], uint32(len(entry.remotePath)))
		copy(headerData[4:], []byte(entry.remotePath))
		binary.BigEndian.PutUint64(headerData[4+len(entry.remotePath):], uint64(entry.size))
		if err := SendPacket(conn, CmdStreamFileHeader, headerData); err != nil {
			return fmt.Errorf("send file header: %w", err)
		}

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

	if err := SendPacket(conn, CmdStreamEnd, nil); err != nil {
		return fmt.Errorf("send stream end: %w", err)
	}

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

// ReceiveStream 接收流式目录（返回16字节确认数据）
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
	var recvFileCount int
	var totalBytesReceived int64
	hasher := sha256.New()

	defer func() {
		if currentFile != nil {
			currentFile.Close()
		}
	}()

	for {
		pkt, err := RecvPacket(conn)
		if err != nil {
			if currentFile != nil {
				currentFile.Close()
				currentFile = nil
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
				if len(fileName) > 0 && fileName[len(fileName)-1] == '/' {
					dirName := fileName[:len(fileName)-1]
					if err := os.MkdirAll(dirName, 0755); err != nil {
						return fmt.Errorf("create dir %s: %w", dirName, err)
					}
					log.Printf("Created directory: %s", dirName)
				} else {
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
					recvFileCount++
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
			totalBytesReceived += int64(n)
			if receivedBytes >= currentFileSize {
				currentFile.Close()
				currentFile = nil
				recvFileCount++
				log.Printf("Received file: %s (%d bytes)", currentFileName, currentFileSize)
			}

		case CmdStreamEnd:
			if currentFile != nil {
				currentFile.Sync()
				currentFile.Close()
				currentFile = nil
				recvFileCount++
			}
			hashHex := hex.EncodeToString(hasher.Sum(nil))
			log.Printf("Stream finished, overall hash: %s", hashHex)
			// 返回16字节：前8字节为实际文件数，后8字节为实际字节数
			resp := make([]byte, 16)
			binary.BigEndian.PutUint64(resp[:8], uint64(recvFileCount))
			binary.BigEndian.PutUint64(resp[8:], uint64(totalBytesReceived))
			return SendPacket(conn, CmdOK, resp)

		default:
			log.Printf("Unexpected command during stream: %d", pkt.Cmd)
		}
	}
}

// ---------- Phase 3: 文件列表流式发送（用于并行传输） ----------

// SendFileListStream 发送给定文件列表，带进度回调
// 返回：(error, 实际接收文件数, 实际接收字节数)
func SendFileListStream(conn net.Conn, files []fileEntry, remoteRoot string, opts *TransferOptions, progressFn func(doneFiles int, doneBytes int64)) (error, int, int64) {
	totalFiles := len(files)
	var totalSize int64
	for _, f := range files {
		totalSize += f.size
	}

	rootBytes := []byte(remoteRoot)
	startData := make([]byte, 4+len(rootBytes)+4+8)
	binary.BigEndian.PutUint32(startData[:4], uint32(len(rootBytes)))
	copy(startData[4:], rootBytes)
	binary.BigEndian.PutUint32(startData[4+len(rootBytes):], uint32(totalFiles))
	binary.BigEndian.PutUint64(startData[4+len(rootBytes)+4:], uint64(totalSize))

	if err := SendPacket(conn, CmdStreamStart, startData); err != nil {
		return fmt.Errorf("send stream start: %w"), totalFiles, totalSize
	}

	chunkSize := DefaultChunkSize
	if opts != nil && opts.ChunkSize > 0 {
		chunkSize = opts.ChunkSize
	}
	buf := make([]byte, chunkSize)

	for _, entry := range files {
		headerData := make([]byte, 4+len(entry.remotePath)+8)
		binary.BigEndian.PutUint32(headerData[:4], uint32(len(entry.remotePath)))
		copy(headerData[4:], []byte(entry.remotePath))
		binary.BigEndian.PutUint64(headerData[4+len(entry.remotePath):], uint64(entry.size))
		if err := SendPacket(conn, CmdStreamFileHeader, headerData); err != nil {
			return fmt.Errorf("send file header: %w"), totalFiles, totalSize
		}

		f, err := os.Open(entry.localPath)
		if err != nil {
			return fmt.Errorf("open file %s: %w", entry.localPath, err), totalFiles, totalSize
		}
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if err := SendPacket(conn, CmdStreamData, buf[:n]); err != nil {
					f.Close()
					return fmt.Errorf("send stream data: %w"), totalFiles, totalSize
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				f.Close()
				return fmt.Errorf("read file %s: %w", entry.localPath, err), totalFiles, totalSize
			}
		}
		f.Close()

		if progressFn != nil {
			progressFn(1, entry.size)
		}
	}

	if err := SendPacket(conn, CmdStreamEnd, nil); err != nil {
		return fmt.Errorf("send stream end: %w"), totalFiles, totalSize
	}

	pkt, err := RecvPacket(conn)
	if err != nil {
		return fmt.Errorf("recv stream response: %w"), totalFiles, totalSize
	}
	if pkt.Cmd == CmdError {
		return fmt.Errorf("stream error: %s", string(pkt.Data)), totalFiles, totalSize
	}

	// 解析服务端返回的实际文件数和字节数
	if pkt.Cmd == CmdOK && len(pkt.Data) >= 16 {
		actualFiles := int(binary.BigEndian.Uint64(pkt.Data[:8]))
		actualBytes := int64(binary.BigEndian.Uint64(pkt.Data[8:16]))
		log.Printf("Server confirmed: %d files, %d bytes (client expected: %d files, %d bytes)",
			actualFiles, actualBytes, totalFiles, totalSize)
		return nil, actualFiles, actualBytes
	}
	return nil, totalFiles, totalSize
}

// SendText 预留
func SendText(conn net.Conn, text string) error {
	return fmt.Errorf("chat feature not implemented yet")
}