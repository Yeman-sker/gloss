# Gloss

轻量、现代、动效精致的跨平台桌面划词工具。当前做翻译，之后通过插件扩展到释义、热梗解释等。Gloss 是暂定代号。

这个项目由两位开发者和各自的 Agent 共同开发。**文档就是项目的记忆**：对话会消失，写进仓库的才算数。

## 开始工作前（按顺序）

1. 读 [docs/STATUS.md](docs/STATUS.md)：现在做到哪、有什么已知问题。
2. 读 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)：分层和依赖方向。
3. 如果任务来自某个 issue，到 [Linear](https://linear.app/yeman/team/GLO/overview) 读它；规划问题也以 Linear 为准。
4. 只读与任务相关的文档，索引在 [docs/README.md](docs/README.md)。

## 文档与规划

- **`docs/` 记录事实：** 现状、决定、原因、实测数据和已发生的工作。
- **[Linear（团队 GLO）](https://linear.app/yeman/team/GLO/overview) 管理规划：** 我们讨论计划后，在这里逐步打磨 issue 和开发安排。

Issue 来自讨论。未经用户同意，不要创建 issue 或调整规划；发现新问题或新想法时先告诉用户，讨论后再进入 Linear。详见 [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md#规划linear)。无法访问 Linear 时告诉用户，不要猜测规划内容。

## 目录

| 路径 | 职责 | 能依赖 |
|---|---|---|
| `core/` | Action、Block、流式引擎。产品的本体 | 只有标准库 |
| `providers/` | 内置 Action：`prompt`（声明驱动的四个功能）、`openai`（流式调用） | `core` |
| `capture/` | 读取其他应用的选中文本、写回原选区：Go 客户端和 Rust helper | 标准库 |
| `settings/` | 配置文件：产品设置和功能设置两层，Key 只在这里 | `core` |
| `memory/` | 本地使用记录（不存文本）、偏好提炼、统计 | `core` |
| `routing/` | 为一次取词选择功能：规则、Jev、记忆、默认 | `core`、`memory` |
| `app/` | mygo 界面层：快捷键、弹窗、视觉和动效，把各层接起来 | 以上全部，唯一可用 mygo 的包 |
| `cmd/` | 开发工具：`smoke`（真实服务冒烟）、`routebench`（规则与 Jev 对比） | 以上除 `app` |
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
make app     # 打包 gloss.app（登录时启动需要它）
```

更多见 [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)。

## 完成的定义

改动只有在满足以下条件后才算完成：

- `make check` 通过。界面改动已实际运行过；未运行的要明确说明。
- 文档同步（见下表）。
- 如果任务对应 Linear issue，其状态和验证结果已更新。

| 发生了什么 | 更新哪里 |
|---|---|
| 阶段、平台状态或已知问题变化 | `docs/STATUS.md`（必须保持最新） |
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
