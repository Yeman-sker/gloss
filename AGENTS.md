# Gloss

轻量、现代、动效精致的跨平台桌面划词工具。当前做翻译，之后通过插件扩展到释义、热梗解释等。Gloss 是暂定代号。

这个项目由两位开发者和各自的 Agent 共同开发。**文档就是项目的记忆**：对话会消失，写进仓库的才算数。

## 开始工作前（按顺序）

1. 读 [docs/STATUS.md](docs/STATUS.md)：现在做到哪、下一步是什么、有什么已知问题。
2. 读 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)：分层和依赖方向。
3. 只读与任务相关的文档，索引在 [docs/README.md](docs/README.md)。

## 目录

| 路径 | 职责 | 能依赖 |
|---|---|---|
| `core/` | Action、Block、流式引擎。产品的本体 | 只有标准库 |
| `providers/` | 内置 Action 的实现 | `core` |
| `capture/` | 读取其他应用的选中文本：Go 客户端和 Rust helper | 标准库 |
| `app/` | mygo 界面层：快捷键、弹窗、视觉和动效 | 以上全部，唯一可用 mygo 的包 |
| `plugins/` | 官方插件（尚未开始） | 插件 API |
| `docs/` | 项目记忆 | |
| `.agents/skills/` | 共享工作流，`.claude/skills/` 只是入口 | |

## 铁律

1. **core 不认识界面。** 不得依赖 mygo、app、capture。`arch_test.go` 会检查。
2. **Action 只返回数据，不带 UI。** 所有视觉和动效由 `app/` 统一负责。
3. **新功能优先做成 Action 或插件。** 只有多个功能都用得上且插件做不到，才改 core。改 core 的公开契约必须写决策记录。
4. **守住轻量。** 新增依赖要说明理由，涉及体积或内存的改动要写实测数据。不引入 WebView，不使用 cgo。
5. **不写未验证的结论。** 文档和汇报要区分已实测、推测和未测，并写明平台。

## 常用命令

```bash
make check   # 完成改动前必须通过：格式、vet、测试、helper 构建
make test    # 只跑测试
make run     # 构建并运行，默认快捷键 Option/Alt+Shift+D
```

更多见 [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)。

## 完成的定义

改动只有在满足以下条件后才算完成：

- `make check` 通过。界面改动已实际运行过；未运行的要明确说明。
- 文档同步（见下表）。

| 发生了什么 | 更新哪里 |
|---|---|
| 进度、下一步或已知问题变化 | `docs/STATUS.md`（必须保持最新） |
| 一次工作结束 | `docs/journal/YYYY-MM.md` 追加一条 |
| 做了难以反悔的决定 | `docs/decisions/` 新增记录 |
| 分层或契约变化 | `docs/ARCHITECTURE.md`、`docs/CORE.md` |
| 扩展方式变化 | `docs/EXTENDING.md` |
| 做了实验或调研 | `docs/research/` |

收尾时使用 `handoff` skill；Agent 不支持 skill 时直接阅读 `.agents/skills/handoff/SKILL.md`。

## 协作边界

- 用户未要求时不要提交或推送。
- 不要回滚你没有改动的内容：它可能是另一位开发者或其他 Agent 的工作。
- 发现文档与代码不符时，以代码为准，并顺手修正文档。
