# 贡献指南

感谢你考虑为 Transafe 做贡献！

## 许可证

Transafe 使用 Apache License 2.0。所有贡献均在该许可证条款下提交。

## 开发者来源证书（DCO）

本项目使用 DCO（Developer Certificate of Origin），而非 CLA。
每位贡献者在提交时必须添加 `Signed-off-by` 行，表示你认证：

1. 你提交的代码是你本人创作，或你有权利按 Apache-2.0 提交
2. 你理解并同意，本项目及你的贡献是公开的，贡献记录将被永久保留

## 如何签署

在提交时添加 `-s` 参数：

```bash
git commit -s -m "feat: 添加新功能"
```

提交信息会自动包含：

```
Signed-off-by: 你的名字 <你的邮箱>
```

## 提交 PR 的流程

1. **Fork 本仓库** 并创建你的特性分支：
   ```bash
   git checkout -b feat/my-feature
   ```

2. **提交更改**（记得使用 `-s` 签名）：
   ```bash
   git commit -s -m "feat: 添加某某功能"
   ```

3. **推送到你的 fork**：
   ```bash
   git push origin feat/my-feature
   ```

4. **在本仓库创建一个 Pull Request**，并在描述中说明改动内容。

5. 等待维护者 review，可能需要根据反馈进行修改。

> 💡 在提交 PR 之前，建议先创建一个 Issue 讨论你的改动，避免做无用功。

## 提交 PR 前请确认

- [ ] 代码已通过 `go build ./...` 和 `go vet ./...`
- [ ] 已通过 `go test ./...` 运行测试，所有测试通过
- [ ] 代码已使用 `gofmt` 或 `go fmt` 格式化
- [ ] commit 已包含 `Signed-off-by`（通过 `git commit -s`）
- [ ] 已在 PR 描述中说明改动内容

## 代码风格

- 遵循 Go 官方编码规范（Effective Go）
- 使用 `gofmt` 格式化代码
- 变量命名清晰，避免缩写
- 函数长度适中，职责单一

## 报告 Issue

如果你发现了 Bug 或有功能建议，欢迎提交 Issue。在提交 Issue 时，请尽量包含：

- 问题的简要描述
- 复现步骤（如果适用）
- 期望的行为
- 实际的行为
- 环境信息（操作系统、Go 版本等）

## 再次感谢

每一份贡献都很珍贵，无论是代码、文档还是 Issue 反馈。欢迎加入！
