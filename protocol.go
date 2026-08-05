package main

import (
	"encoding/binary"
	"fmt"
	"io"
)

// 协议命令常量
const (
	CmdAuth      = 0x01
	CmdFile      = 0x02
	CmdHash      = 0x03
	CmdOK        = 0x04
	CmdError     = 0x05
	CmdText      = 0x06
	CmdChunk     = 0x07
	CmdDirStart  = 0x08
	CmdDirEnd    = 0x09
	CmdDir       = 0x0A

	// Phase 2 流式传输
	CmdStreamStart      = 0x0B
	CmdStreamFileHeader = 0x0C
	CmdStreamData       = 0x0D
	CmdStreamEnd        = 0x0E

	// Phase 4 断点续传
	CmdQueryOffset = 0x0F
	CmdOffsetResp  = 0x10

	// Phase 5 确认回执
	CmdAck         = 0x11 // 单个文件接收确认
	CmdStreamAck   = 0x12 // 流式传输结束确认（含实际文件数）
	CmdLogRequest  = 0x13 // 请求日志
	CmdLogData     = 0x14 // 日志数据
)

// Packet 协议包结构
type Packet struct {
	Cmd  byte
	Data []byte
}

// SendPacket 发送一个协议包
func SendPacket(w io.Writer, cmd byte, data []byte) error {
	length := uint32(1 + len(data))
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[:4], length)
	buf[4] = cmd
	copy(buf[5:], data)
	_, err := w.Write(buf)
	return err
}

// RecvPacket 接收一个协议包
func RecvPacket(r io.Reader) (*Packet, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	length := binary.BigEndian.Uint32(header)
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return &Packet{Cmd: body[0], Data: body[1:]}, nil
}
