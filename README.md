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
| 设计哲学 | 单连接流式传输 | 增量差量传输（rsync 算法） | 云存储同步 |
| 海量小文件 | ⚡ 流式优化（Phase 2） | 逐文件串行 | 多流并行 |
| 大文件增量 | ❌ 规划中 | ✅ 差量传输算法 | ✅ 支持（效率中等） |
| 传输审计 | 📋 规划中（Phase 5） | ❌ 无 | ❌ 弱 |
| 跨平台单二进制 | ✅ | ✅ | ✅ |
| 开源免费 | ✅ Apache-2.0 | ✅ GPL | ✅ MIT |

两者并非替代关系——rsync 擅长增量同步，Transafe 专注于流式批量传输与合规审计。

## 快速开始

### 安装

从 [GitHub Releases](https://github.com/hbyifei/transafe/releases) 下载对应平台的预编译二进制，解压后即可使用。

Transafe 的**服务端和客户端是同一个二进制文件**，通过子命令区分角色。配置文件为同一份 `transafe.yaml`，通过 `server:` 与 `client:` 分段隔离两端配置。

### 启动服务端（接收端）

```bash
transafe server :9000
```

### 发送文件（客户端 → 服务端）

```bash
transafe client 192.168.1.100:9000 /path/to/file /dest/
```

### 发送目录（客户端 → 服务端）

```bash
transafe senddir 192.168.1.100:9000 ./project/ /backup/
```

### 接收文件（服务端 → 客户端，拉取模式）

```bash
# 只能使用【相对路径】，相对于服务端配置中 allow_pull 指定的根目录
transafe receive 192.168.1.100:9000 shared/docs/report.pdf ./downloaded/
```

> ⚠️ `receive` 采用**客户端拉取（Pull）模式**：客户端发起连接，服务端在连接上回传文件数据。客户端**只能指定相对路径**，且目标目录必须由服务端在 `allow_pull` 中显式授权，禁止访问白名单之外的任何文件。

### 验证传输（规划中）

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

> 📌 Phase 2（单连接流式引擎）已完成。当前版本已具备基础传输与流式传输能力。

## 当前状态

- ✅ 基础传输协议（CmdAuth / CmdFile / CmdChunk / CmdHash / CmdOK / CmdError）
- ✅ 单文件传输 + SHA-256 整体校验
- ✅ 目录传输（SendDir / ReceiveDir）
- ✅ 空目录 / 空文件正确处理
- ✅ 基础认证
- ✅ 单连接流式引擎（Phase 2）
- ✅ 并行传输（`senddir -j N`）
- 📋 流式传输整体 xxHash 校验（规划中，用于替代当前的 SHA-256 以提升性能）
- ✅ 断点续传与状态管理（Phase 4，单文件 `.part` / `.progress`）
- 📋 `receive` 子命令 + 服务端白名单鉴权（规划中）
- 📋 企业级审计与合规（CSV/JSON 报告、Manifest、verify 子命令）（Phase 5）

## 路线图

| Phase | 功能 | 状态 |
|-------|------|------|
| 1 | MVP 核心传输 | ✅ 已完成 |
| 2 | 单连接流式引擎 | ✅ 已完成 |
| 3 | 性能强化（并行 ✅ / 压缩 / 零拷贝 / xxHash 校验） | 📋 规划 |
| 4 | 断点续传与状态管理 | ✅ 已完成 |
| 5 | 企业级审计与合规 | 📋 规划 |
| 6 | AI Agent 集成 | 📋 远期 |
| 7 | 产品化与商业化 | 📋 远期 |
| 8 | 极限性能优化（按需 Rust 化） | 📋 按需触发 |

## 技术栈

- **语言**：Go
- **协议**：自定义二进制协议（单连接流式传输）
- **校验**：SHA-256（当前默认）/ xxHash（规划中，用于极速模式）
- **压缩**：LZ4 / Zstd（规划）
- **传输层**：TCP（当前）/ QUIC（规划）

## 配置文件（transafe.yaml）

服务端和客户端共用同一份配置文件，通过分段隔离：

```yaml
# 服务端配置段（执行 server 命令时读取）
server:
  port: 9000
  password: "secret123"
  # receive 功能：允许客户端拉取的目录白名单（必须为绝对路径）
  # 未配置或为空时，receive 功能完全禁用
  allow_pull:
    - /data/shared
    - /data/backups
  log_dir: "./log"

# 客户端配置段（执行 client / senddir / receive 命令时读取）
client:
  password: "secret123"
  default_server: "127.0.0.1:9000"
  log_dir: "./log"
```

> 客户端机器上**只需保留 `client:` 段**（甚至可为空，通过命令行参数指定），无需也不能查看或修改服务端的 `server:` 段。配置文件不通过网络传输，仅在本机本地生效。

## `receive` 功能安全设计（规划中）

`receive` 采用**客户端拉取模式**，由客户端发起连接并请求文件，服务端校验通过后通过同一条连接回传数据。为保障安全，设计如下权限模型：

### 核心原则：服务端白名单制

- 服务端在 `transafe.yaml` 的 `server.allow_pull` 中**显式声明**允许被拉取的目录列表。
- 客户端**只能使用相对路径**指定待拉取文件，服务端将其拼接至白名单根目录后校验。
- **绝对路径一律拒绝**（防止客户端直接指定 `/etc/passwd` 等敏感文件）。
- 未配置 `allow_pull` 时，`receive` 功能默认**完全禁用**。

### 路径校验规则

1. 拒绝任何绝对路径（以 `/` 或盘符开头的请求）。
2. 将客户端传入的相对路径拼接至白名单根目录，做标准化处理。
3. 校验最终路径是否仍在白名单目录内，拒绝 `..` 路径穿越（如 `../../etc/passwd`）。
4. 不跟随指向白名单外目标的符号链接。

### 示例

服务端配置：
```yaml
server:
  allow_pull:
    - /data/shared
```

合法请求：
```bash
transafe receive 192.168.1.100:9000 shared/report.pdf ./download/
# 服务端解析为 /data/shared/report.pdf ✅ 允许传输
```

非法请求（均被服务端拒绝）：
```bash
# 使用绝对路径
transafe receive 192.168.1.100:9000 /etc/passwd ./hack/

# 尝试路径穿越
transafe receive 192.168.1.100:9000 ../../etc/passwd ./hack/
```

### 安全加固清单

| 防护项 | 实现方式 |
|--------|---------|
| 路径白名单 | 配置文件中显式声明允许拉取的目录 |
| 仅允许相对路径 | 客户端禁止传绝对路径，服务端二次校验 |
| 路径穿越防护 | 标准化后前缀匹配，拒绝 `..` 逃逸 |
| 符号链接防护 | 不跟随，或限制在白名单内 |
| 认证 | 复用现有密码认证 |
| 只读访问 | `receive` 模式下服务端只读取，不写入 |
| 审计日志 | 记录请求者 IP、认证用户、拉取路径与时间 |
| 默认拒绝 | 未配置 `allow_pull` 时 `receive` 禁用 |

## 使用示例

### 基本传输

```bash
# 发送单个文件
transafe client 192.168.1.100:9000 ./data.db /var/backups/

# 发送整个目录
transafe senddir 192.168.1.100:9000 ./website/ /var/www/

# 并行发送目录（4 个连接）
transafe senddir -j 4 192.168.1.100:9000 ./website/ /var/www/

# 接收（拉取）文件
transafe receive 192.168.1.100:9000 shared/report.pdf ./downloaded/
```

### 审计与验证（规划中）

```bash
# 传输时生成审计报告
transafe senddir 192.168.1.100:9000 ./project/ /dest/ --audit audit-report.json

# 验证目标端文件与 Manifest 一致性
transafe verify /dest/ --manifest manifest.json

# 查看传输历史
transafe history --format table
```

## 性能目标

Transafe 在 Phase 2 完成后的性能目标是：在 100 万小文件（总 2GB，单文件平均 20KB）场景下，总耗时控制在 400 秒以内。该目标值基于以下估算：流式协议将协议开销从 O(N) 降至 O(1)，并假设千兆网络、SSD 环境、带宽利用率 ≥ 80%。

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
