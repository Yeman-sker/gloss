# 项目现状

> 新会话先读这里。这里只记录现状，历史放到 [journal](journal/)，规划在 [Linear](https://linear.app/yeman/team/GLO/overview)。

**最后更新：** 2026-11-05
**阶段：** 1 打磨 macOS 基础功能 → 2 macOS 本地实测（负责人连续几天日常使用）
**一句话：** “macOS 本地实测”项目的代码已全部完成（GLO-26 设置界面按计划留到实测后），四个功能、记忆和自动路由都在模拟服务上跑通；真实服务、Jev 和负责人常用应用里的取词与写回还没验证，等负责人实测。

## 当前重点

**只做 macOS。** Windows 开发自 2026-11-05 起暂停（负责人决定），Linux 也不考虑。代码继续保持跨平台写法，但不再为 Windows 和 Linux 做专门工作和验证。

进入本地实测阶段前，先把三块基础功能打磨好：

| 环节 | 现状 | 以后 |
|---|---|---|
| 取词 | 划词：AX，失败时用 Cmd+C 兜底并完整恢复剪贴板；带来源应用、窗口标题、可编辑三态和前后文 | OCR 等其他取词方式 |
| Action | 翻译、释义、优化提示词、整理输出，都由 `providers/prompt` 的声明驱动 | 插件 |
| 输出 | 墨色面板：文本、流式 Markdown、长文阅读、复制、替换原文 | 插件只描述内容，界面仍由 `app/` 渲染 |

实测时要有四个功能：划词翻译、提示词优化、通俗释义、Agent 输出优化。此外还要有两个新模块：

- **记忆：** 在本地记录每次使用，提炼出用户在不同情境下的偏好。
- **自动路由：** 在取词和 Action 之间，根据当前信号和记忆自动选择功能，用规则加 Jev 判断，猜错时可以一键改正，改正结果写回记忆。

设置分两层：产品设置（模型服务和 Key、快捷键、记忆、路由等）和功能设置（每个功能或插件声明自己的设置项，界面由宿主统一生成）。

拆解和顺序见 Linear 项目 [macOS 本地实测](https://linear.app/yeman/project/macos-本地实测-decd5658cbd1)（GLO-7 至 GLO-31）。截至 2026-11-05，除 GLO-26（设置界面，Backlog）外代码都已完成，状态为 In Review；需要负责人实测的验收项写在各 issue 和[实测清单](research/2026-11-05-local-trial.md)里。

Gloss 定位为 Bob 的替代品，两者不会同时运行，所以不处理快捷键冲突。

## 平台状态（macOS）

| 能力 | 状态 |
|---|---|
| 构建主程序、`make app` 打包 | 已实测（.app 生成，未从 .app 运行） |
| 取词 helper：超时重启、错误分类、来源信号、剪贴板完整恢复 | TextEdit 已实测；浏览器、终端、Electron 未测 |
| 写回原选区（AX 优先，否则粘贴；写前校验目标） | TextEdit 已实测，含面板里的端到端替换；其他应用未测 |
| 墨色面板、功能栏、Markdown、长文阅读 | 模拟服务已实测（截图核对） |
| 四个功能（翻译、释义、优化提示词、整理输出） | 模拟服务已实测，真实服务未测 |
| 记忆和统计命令 | 已实测（隔离的 HOME，记录不含文本）；`stats`/`recent` 只读不裁剪，`label` 与运行中的 Gloss 共用文件锁（单测） |
| 规则路由 | 自写样例和本地运行已实测；Jev 未测（没有 Key） |
| 配置文件（两层、热加载、托盘开关） | 单测覆盖；热加载和托盘开关未实机点过 |
| 托盘和退出 | 已实测（早期）；新增菜单项未实机点过 |

**Windows 暂停时的状态：** 本仓库只有交叉检查（`go vet`、`go build`），Windows 取词 helper 未实现。Linear 上 GLO-5（Windows 构建和触发）和 GLO-6（Windows 实机验证）标为 Done，GLO-4（Windows 取词 helper）为 In Progress，三项都由 Silco 负责，但截至 2026-11-05 相关代码和验证记录都没有进入本仓库。早期验证见 [Windows 验证](research/2026-11-01-windows-spike.md)。Linux 只跑过 `go vet`。

## 已完成

- 技术验证：mygo + Rust helper 在 macOS 上可行，见 [macOS 验证](research/2026-11-01-macos-spike.md)。
- `core`：Action、Block、Engine（新请求取消旧请求），有单元测试。
- `providers/openai`：OpenAI 兼容接口的流式调用，只用标准库，有单元测试。服务返回的错误里会隐去 Key 片段。
- `providers/prompt`：用声明和提示词运行四个功能，翻译的提示词与之前一致。配置方式见[开发约定](DEVELOPMENT.md#配置)。
- `settings`、`memory`、`routing`：两层配置（[0008](decisions/0008-two-layer-settings.md)），本地记忆和路由（[0009](decisions/0009-memory-and-routing.md)），都有单元测试。
- 公共契约：`core.Descriptor`、`markdown` 主结果、设置声明、完成语义（[0007](decisions/0007-action-descriptor.md)）。
- `providers/demo`：不再注册，保留作新 Action 的模板。
- `capture`：Go 客户端（请求 id、超时、自动重启）和 macOS helper（来源信号、剪贴板完整恢复、写回凭证）。实测见[取词与写回](research/2026-11-05-capture-and-writeback.md)。
- `app`：全局快捷键、不抢焦点的墨色面板（透明窗口内自绘）、打包字体（[0006](decisions/0006-bundled-fonts.md)）、胶囊展开、匀速流式、点击复制、选中文字、拖动钉住、错误重试；会话与迟到结果隔离；功能栏（显示当前功能和选择来源，一键切换即改正）；Markdown 与长文阅读布局（原文折叠、跟随到底、回到底部）；复制和替换按钮；托盘里的自动选择、记忆、清空记忆、登录时启动、打开配置文件、退出。
- 边际审查（pi，codex/gpt-6.1-sol，xhigh）：第一轮 9 个问题、复审修复时又发现的 9 个问题都已修，见 [journal](journal/2026-11.md)。
- 命令：`gloss stats|recent|label`，`cmd/smoke`（真实服务冒烟和延迟），`cmd/routebench`（规则与 Jev 对比）。
- 分层检查 `arch_test.go`，以及文档和 Agent 配置。

## 尚待讨论的想法

初期想到的方向，还没有讨论，也不是承诺的计划。讨论后进入 [Linear](https://linear.app/yeman/team/GLO/overview)，并从这里删除。

- 增加一个传统翻译 API。
- 插件运行时：验证 goja 能否支持流式和异步。
- 产品名和品牌风格。

## 进行中

谁在做什么，看 [Linear 的 In Progress](https://linear.app/yeman/team/GLO/overview)。

## 已知问题

这里记录已确认的事实。要不要修、何时修，讨论后在 Linear 规划。

| 问题 | 影响 | 状态 |
|---|---|---|
| 真实模型服务、Jev 都没测过（本机没有 Key） | 首字延迟、Jev 延迟和区分效果未知 | 等负责人实测（GLO-7、GLO-21） |
| 在 Claude 桌面端选中多段文字时，有时提示“先选中一段文字”（负责人录屏） | 取词失败 | 未复现；已加 `AXManualAccessibility`、AX 超时，修了 Cmd 键卡住；兜底 Cmd+C 改为只发给目标进程（面板是键盘窗口时全局 Cmd+C 会进面板，可能是原因之一，未证实），待实测（GLO-9） |
| Cmd+C 兜底曾让系统认为 Cmd 一直按着（实测 HID 标志），旧 helper 也有 | 下次取词多等 400ms，可能影响用户输入 | 已修，TextEdit 实测 |
| 点过面板后，面板会成为键盘窗口（Gloss 不激活），全局发出的 Cmd+V 进了面板，替换没落地（TextEdit 实测） | 面板里的替换失败 | 已修：AX 优先，按键只发给目标进程；面板替换在 TextEdit 实测成功。按进程发键在沙盒或特殊应用里是否被处理，未测 |
| AX 写入是否进入各应用的撤销栈 | 替换后可能无法 Cmd+Z | TextEdit 可撤销；其他应用未测 |
| “最近一次明确选择”的回退是全局的：本地运行时手动选过一次“优化提示词”后，规则判断不了的英文句子都走了它 | 路由可能被一次选择带偏 | 观察中，看实测数据 |
| 只读环境里的英文单词规则一律判翻译，用户可能想看释义 | 这类情况不会交给 Jev 或记忆 | 观察中 |
| 中文粗体不显示粗（只打包了一档字重，见 0006） | Markdown 的强调在中文里看不出来 | 待讨论 |
| Markdown 代码块的“Copy”按钮是 mygo 自带的英文 | 与界面语言不一致 | 待讨论 |
| 程序约 14.2MB（此前 13.9MB），模拟服务跑完四个功能后 footprint 46MB（macOS 实测），绘制后峰值来源未查明 | 偏离“守住轻量” | 待讨论 |
| 面板交互仍有抖动、不够丝滑（macOS 160Hz，负责人实测）；录屏里点击时有 57–82ms 的帧间隔，原因未查明 | 手感未达预期 | 元素生命周期已修，加了帧间隔日志，卡顿原因仍未查明（GLO-10） |
| mygo 只在应用激活时跟踪鼠标，而 Gloss 从不激活。macOS 上面板用私有 API `SetsCursorInBackground` 自己设置鼠标样式 | 系统升级后可能失效，失效时只是样式不变 | 观察中 |
| 面板未实测：关闭动画、点外部关闭后的拖选变淡、单词大字号、音标块、甩动惯性、已打开时滑到新位置、减弱动效、1 倍屏、托盘新菜单项、配置热加载、从 .app 运行 | 可能有问题 | 待验证 |

## 开放问题

- 产品正式名称。
- 品牌视觉风格（有意延后）。
- 插件运行时最终用 goja 还是 wazero。
