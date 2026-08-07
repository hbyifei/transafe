# Transafe 🚀

> 面向海量小文件场景的流式文件传输工具 | 单连接流式传输架构 | 规划中的企业级审计 | Apache-2.0

## 设计哲学

Transafe 采用 **单连接流式传输协议**（Single-Connection Streaming Protocol）：

- 在一条 TCP 连接上，将整个目录的文件元信息和数据按顺序连续写入流
- 接收端顺序解析并写入磁盘
- 流结束时进行整体 SHA-256 校验

这使得协议开销从 O(N) 降至 O(1)，专为百万级小文件场景设计。

## 与现有工具的关系

rsync、rclone 等工具在各自的场景里表现出色：

- rsync 的增量差量传输算法，在单/少量大文件同步场景无可替代
- rclone 在云存储同步场景支持 70+ 云服务商

Transafe 选择了一个补充性的定位：

| 特性 | Transafe | rsync | rclone |
|------|----------|-------|--------|
| 设计哲学 | 单连接流式传输 | 增量差量传输 | 云存储同步 |
| 海量小文件 | ⚡ 流式优化（Phase 2） | 逐文件串行 | 多流并行 |
| 大文件增量 | ❌ 规划中 | ✅ 差量传输算法 | ✅ 支持（效率中等） |
| 传输审计 | 📋 规划中（Phase 5） | ❌ 无 | ❌ 弱 |
| 跨平台单二进制 | ✅ | ✅ | ✅ |
| 开源免费 | ✅ Apache-2.0 | ✅ GPL | ✅ MIT |

两者并非替代关系——rsync 擅长增量同步，Transafe 专注于流式批量传输与合规审计。

## 快速开始

### 安装

从 [GitHub Releases](https://github.com/hbyifei/transafe/releases) 下载对应平台的预编译二进制，解压后即可使用。

### 启动服务端（接收端）

```bash
transafe server :9000
```

### 发送文件

```bash
transafe send 192.168.1.100:9000 /path/to/file /dest/
```

### 发送目录

```bash
transafe senddir 192.168.1.100:9000 ./project/ /backup/
```

### 接收文件

```bash
transafe receive 192.168.1.100:9000 /dest/ -o ./received/
```

### 验证传输（即将推出）

```bash
transafe verify /dest/ --manifest manifest.json
```

## 核心架构

### 解决的痛点

在百万级小文件场景下，传统逐文件串行模式（如默认配置的 rsync）存在严重的协议开销：

- 90% 以上的耗时消耗在目录遍历和元数据比对
- 每次文件元数据远程比对都会叠加 TCP 往返延迟
- 100 万小文件默认配置下耗时可长达数小时，带宽利用率 < 5%

单连接流式传输将协议开销从 O(N) 降至 O(1)，海量小文件场景下性能显著提升。

> 📌 注：Transafe 的流式引擎（Phase 2）正在开发中，当前 Phase 1 版本已实现基础传输能力。

## 当前状态

- ✅ 基础传输协议（CmdAuth / CmdFile / CmdChunk / CmdHash / CmdOK / CmdError）
- ✅ 单文件传输 + SHA-256 整体校验
- ✅ 目录传输（SendDir / ReceiveDir）
- ✅ 空目录 / 空文件正确处理
- ✅ 基础认证
- ✅ 单连接流式引擎重构（Phase 2，开发中）
- 📋 性能强化：并行传输、LZ4/Zstd 压缩、零拷贝（Phase 3）
- ✅ 断点续传与状态管理（Phase 4）
- 📋 企业级审计与合规（CSV/JSON 报告、Manifest、verify 子命令）（Phase 5）

## 路线图

| Phase | 功能 | 状态 |
|-------|------|------|
| 1 | MVP 核心传输 | ✅ 已完成 |
| 2 | 单连接流式引擎 | ✅ 已完成 |
| 3 | 性能强化（并行/压缩/零拷贝） | 📋 规划 |
| 4 | 断点续传与状态管理 | ✅ 已完成 |
| 5 | 企业级审计与合规 | 📋 规划 |
| 6 | AI Agent 集成 | 📋 远期 |
| 7 | 产品化与商业化 | 📋 远期 |
| 8 | 极限性能优化（按需 Rust 化） | 📋 按需触发 |

## 技术栈

- **语言**：Go
- **协议**：自定义二进制协议（单连接流式传输）
- **校验**：SHA-256（默认）/ xxHash（极速模式）
- **压缩**：LZ4 / Zstd（规划）
- **传输层**：TCP（MVP）/ QUIC（规划）

## 使用示例

### 基本传输

```bash
# 发送单个文件
transafe send 192.168.1.100:9000 ./data.db /var/backups/

# 发送整个目录
transafe senddir 192.168.1.100:9000 ./website/ /var/www/

# 接收文件
transafe receive 192.168.1.100:9000 /var/backups/ -o ./downloaded/
```

### 审计与验证（即将推出）

```bash
# 传输时生成审计报告
transafe senddir 192.168.1.100:9000 ./project/ /dest/ --audit audit-report.json

# 验证目标端文件与 Manifest 一致性
transafe verify /dest/ --manifest manifest.json

# 查看传输历史
transafe history --format table
```

## 性能目标

Transafe 在 Phase 2 完成后的性能目标是：在 100 万小文件（总 2GB，单文件平均 20KB）场景下，总耗时控制在 400 秒以内。该目标值基于以下估算：流式协议将协议开销从 O(N) 降至 O(1)，并假设千兆网络带宽利用率 ≥ 80%。

正式 benchmark 将在 Phase 2 完成后公布，届时将提供与 rsync、rclone、tar+ssh 等同场景的实测对比数据。

## 贡献

欢迎贡献！请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 了解如何参与。

本项目采用 DCO（Developer Certificate of Origin），提交时请使用 `git commit -s`。

## 许可证

Transafe 核心引擎采用 **Apache License 2.0** 开源。
企业版增值功能（如有）将另行授权。

Copyright 2026 hbyifei

## 作者

- **hbyifei**（靳一飞）- 初始开发者

## 所有权声明

本项目由 hbyifei 在个人业余时间独立开发，与任何雇主或第三方的业务无关联。
详见 [OWNERSHIP.md](OWNERSHIP.md)。

## 致谢

感谢所有贡献者和用户的信任与支持。
