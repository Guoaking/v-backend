# 本地CI测试配置说明

## 📋 已配置的自动化测试

### 1. Git Hooks（自动化验证）

已配置以下 Git hooks，会在特定操作时自动运行测试：

#### Pre-commit Hook（提交前验证）
- **触发时机**：每次 `git commit` 时
- **执行内容**：快速测试（格式检查 + 静态分析 + 单元测试）
- **文件位置**：`.git/hooks/pre-commit`
- **作用**：防止提交有基础问题的代码

#### Pre-push Hook（推送前验证）
- **触发时机**：每次 `git push` 时
- **执行内容**：完整测试（包含构建检查、安全检查等）
- **文件位置**：`.git/hooks/pre-push`
- **作用**：防止推送有问题的代码到远程

### 2. 手动测试脚本

#### 快速测试（开发时使用）
```bash
./scripts/test-quick.sh
```
- 代码格式检查
- 静态分析
- 单元测试
- **耗时**：约 10-30 秒

#### 完整测试（推送前使用）
```bash
./scripts/test-all.sh
```
- 包含快速测试的所有内容
- 构建检查
- 安全检查
- **耗时**：约 1-2 分钟

#### 覆盖率报告
```bash
./scripts/test-coverage.sh
```
- 生成详细的测试覆盖率报告
- 输出 HTML 报告文件
- **耗时**：约 30-60 秒

## 🚀 使用方式

### 正常开发流程

```bash
# 1. 修改代码
vim internal/api/some_handler.go

# 2. 提交（自动运行快速测试）
git add .
git commit -m "feat: 添加新功能"
# ✅ pre-commit hook 自动运行 test-quick.sh

# 3. 推送（自动运行完整测试）
git push origin main
# ✅ pre-push hook 自动运行 test-all.sh
```

### 跳过验证（紧急情况）

```bash
# 跳过提交前验证
git commit --no-verify -m "紧急修复"

# 跳过推送前验证
git push --no-verify origin main
```

### 手动验证

```bash
# 开发过程中随时运行快速测试
./test-quick.sh

# 推送前手动运行完整测试
./test-all.sh

# 查看测试覆盖率
./test-coverage.sh
```

## ⚙️ 配置说明

### Git Hooks 工作原理

1. **Pre-commit**：
   - 在你执行 `git commit` 后、实际提交前运行
   - 如果测试失败，提交会被阻止
   - 必须修复错误后才能提交

2. **Pre-push**：
   - 在你执行 `git push` 后、实际推送前运行
   - 如果测试失败，推送会被阻止
   - 必须修复错误后才能推送

### 优势

✅ **防止低质量代码进入仓库**
✅ **及时发现编译和格式问题**
✅ **自动化流程，无需手动执行**
✅ **保护远程仓库代码质量**

### 注意事项

⚠️ **首次使用可能需要安装依赖**
```bash
go mod download
```

⚠️ **测试脚本必须在项目根目录**
- Git hooks 会在 `.git/` 目录执行
- 脚本使用相对路径 `./test-quick.sh`
- 确保在项目根目录执行 git 操作

⚠️ **测试失败时的处理**
1. 查看错误信息
2. 修复代码问题
3. 再次尝试提交/推送
4. 或使用 `--no-verify` 跳过（不推荐）

## 📊 测试结果示例

### 成功示例
```
======================================
⚡ 快速测试模式
======================================

[1/3] 代码格式检查...
✅ 格式检查通过

[2/3] 静态分析...
✅ 静态分析通过

[3/3] 单元测试...
✅ 单元测试通过

🎉 快速测试全部通过！
```

### 失败示例
```
======================================
⚡ 快速测试模式
======================================

[1/3] 代码格式检查...
❌ 代码格式错误，请运行: gofmt -w .

======================================
❌ 提交前验证失败
======================================

请修复上述错误后再提交。
如果需要跳过验证，使用: git commit --no-verify
```

## 🔧 自定义配置

### 修改测试脚本

编辑 `test-quick.sh` 或 `test-all.sh` 可以自定义测试内容：

```bash
# 添加额外的检查
echo "4. 自定义检查..."
if ! some-custom-check; then
    echo "❌ 自定义检查失败"
    exit 1
fi
```

### 禁用特定 Hook

```bash
# 暂时禁用 pre-commit
mv .git/hooks/pre-commit .git/hooks/pre-commit.disabled

# 重新启用
mv .git/hooks/pre-commit.disabled .git/hooks/pre-commit
```

## 📚 相关文档

- [AGENTS.md](./AGENTS.md) - 项目架构和开发指南
- [test-quick.sh](./test-quick.sh) - 快速测试脚本
- [test-all.sh](./test-all.sh) - 完整测试脚本
- [test-coverage.sh](./test-coverage.sh) - 覆盖率报告脚本
