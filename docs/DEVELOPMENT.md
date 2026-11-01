# 开发约定

## 环境

- Go 1.27.3+（mygo 要求）
- Rust stable
- macOS 运行时要给启动程序的终端授予辅助功能权限

## 命令

| 命令 | 作用 |
|---|---|
| `make check` | 完成改动前必须通过 |
| `make test` | Go 和 Rust 测试 |
| `make build` | 构建到 `bin/` |
| `make run` | 构建并运行 |

结束程序：在终端按 Ctrl+C。

## 规划（Linear）

[Linear 的 GLO 团队](https://linear.app/yeman/team/GLO/overview) 是规划的地方，`docs/` 是记录的地方。

1. **先讨论：** 开发者和 Agent 一起讨论计划。
2. **再落到 Linear：** 把讨论结果写成 issue，用户同意后 Agent 才能创建或调整。
3. **逐步打磨：** 随着理解加深，持续调整 issue 的范围、顺序和拆分。
4. **事后记录：** 工作完成后，把事实写回 `docs/`。

| 状态 | 含义 |
|---|---|
| Backlog | 想做，还没排期 |
| Todo | 已决定要做 |
| In Progress | 有人正在做 |
| In Review | 代码已完成，等待评审或合并 |
| Done | 已合并且验证通过 |
| Canceled / Duplicate | 不做或重复，写明原因 |

约定：

- 按 issue 开工时，先把状态切到 In Progress 并指派给自己，避免撞车。
- 工作中发现的新问题先告诉用户，不要自行建 issue，也不要顺手扩大范围。
- 关闭 issue 前写验证结果：运行了什么，在什么平台，什么没验证。
- 长期有效的结论写进 `docs/`，不要只留在 issue 里。

## 协作

- `main` 始终可构建。功能在分支上开发，优先使用 Linear 生成的分支名；没有时用 `glo-123-short-title`。
- 提交信息说明为什么改，用 `feat:`、`fix:`、`docs:`、`refactor:`、`test:`、`chore:` 开头，有 issue 时在末尾写 `GLO-123`。
- 文档和代码放在同一个提交里。

## 代码风格

- 遵循 `gofmt` 和相邻代码的写法。
- 注释说明原因，不重复代码。
- 平台相关代码只放在 `capture/helper/src/<平台>.rs` 和必要的 `app/` 中。
- 宁可少量重复，也不提前抽象。

## 测试

- `core` 和 `providers`：单元测试，不访问真实网络。
- `app`：可以用 `ui.NewTester` 测试视图，动效和焦点要实际运行。
- 未在某个平台实际运行过，就不要声称它在该平台可用。
- **测试安全：** 不要关闭用户已打开的文档，也不要向其窗口发送按键。测试 Cmd/Ctrl+C 兜底前先备份剪贴板。

## 决策记录

文件命名 `docs/decisions/NNNN-短标题.md`，内容为背景、决定、理由、代价。已接受的记录不改结论；想法变了就新增一条，并在旧记录顶部标注"已被 NNNN 取代"。

## 工作日志

追加到 `docs/journal/YYYY-MM.md`，格式：

```markdown
## YYYY-MM-DD 做了什么（作者或 Agent）
- 结果：
- 验证：运行了什么，在什么平台
- 遗留：
```

## 升级 mygo

mygo 还没到 1.0，API 可能变动。升级使用 `.agents/skills/mygo-maintenance`，并只修改 `app/`。
