// protocol.go
// 定义定长包头（4字节长度 + 1字节命令）和封包/解包函数

package main

import (
	"encoding/binary"
	"fmt"
	"io"
)

// 命令类型常量（0x01-0xFF 可用，目前只用了几个）
const (
	CmdAuth   = 0x01 // 认证
	CmdFile   = 0x02 // 文件传输
	CmdHash   = 0x03 // 文件校验
	CmdOK     = 0x04 // 成功响应
	CmdError  = 0x05 // 错误响应
	CmdText   = 0x06 // 文本消息（预留，后续实现）
	CmdChunk  = 0x07  // 新增：文件分块
	CmdDirStart = 0x08  // 新增：目录传输开始
	CmdDirEnd   = 0x09  // 新增：目录传输结束
	CmdDir      = 0x0A  // 新增：目录项
)

// Packet 表示一个协议包
type Packet struct {
	Cmd  byte   // 命令类型
	Data []byte // 数据体（不含命令字节）
}

// SendPacket 发送一个完整的包：4字节长度（大端）+ 1字节命令 + 数据体
func SendPacket(w io.Writer, cmd byte, data []byte) error {
	length := uint32(1 + len(data)) // 命令本身占1字节
	buf := make([]byte, 4+length)

	// 写入长度头（大端序）
	binary.BigEndian.PutUint32(buf[:4], length)
	// 写入命令
	buf[4] = cmd
	// 写入数据
	copy(buf[5:], data)

	_, err := w.Write(buf)
	return err
}

// RecvPacket 从读取器中接收一个完整的包，返回命令和数据
func RecvPacket(r io.Reader) (*Packet, error) {
	// 读取4字节长度头
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	length := binary.BigEndian.Uint32(header)

	// 读取命令+数据体
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return &Packet{
		Cmd:  body[0],
		Data: body[1:],
	}, nil
}