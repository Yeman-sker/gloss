# 项目现状

> 新会话先读这里。这里只记录现状，历史放到 [journal](journal/)，规划在 [Linear](https://linear.app/yeman/team/GLO/overview)。

**最后更新：** 2026-11-01
**阶段：** 0 脚手架 → 1 Core 与真实翻译
**一句话：** 已接入 OpenAI 兼容接口，服务配置从用户配置目录的 JSON 文件读取；完整链路只对本地模拟服务实测过，还没有对真实服务验证。

## 平台状态

| 能力 | macOS | Windows | Linux |
|---|---|---|---|
| 构建主程序 | 已实测 | 原型交叉编译通过，脚手架未测 | 原型交叉编译通过，脚手架未测 |
| 取词 helper | 已实现（原型实测） | **未实现** | 未实现 |
| 弹窗与动画 | 已实测 | 原型动画跑过，焦点、材质和字体未完成验证 | 未测 |
| 流式翻译 | 模拟服务已实测，真实服务未测 | 未测 | 未测 |
| 配置文件 | 已实测 | 路径逻辑只有交叉检查（`go vet`、`go build`），未实机运行 | 同 Windows |

## 已完成

- 技术验证：mygo + Rust helper 在 macOS 上可行，见 [macOS 验证](research/2026-11-01-macos-spike.md)。
- `core`：Action、Block、Engine（新请求取消旧请求），有单元测试。
- `providers/openai`：OpenAI 兼容接口的流式翻译，只用标准库，有单元测试。配置方式见[开发约定](DEVELOPMENT.md#翻译服务配置)。
- `providers/demo`：不再注册，保留作新 Action 的模板。
- `capture`：Go 客户端和 macOS helper。
- `app`：全局快捷键、不抢焦点的弹窗、流式淡入、高度弹簧动画；读取配置文件，缺文件或缺字段时在面板说明。
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
| 再次触发时弹窗先以上次高度出现，再过渡到新高度 | 观感 | 待修 |
| Cmd+C 兜底只恢复纯文本剪贴板 | 可能丢失图片等内容 | 必须在发布前修复 |
| macOS 查询前台应用要启动 `lsappinfo`，约 20ms | 取词变慢 | 待改用 NSWorkspace |
| 原型曾有一次 AX 和 Cmd+C 都失败，原因未确认 | 取词可靠性 | 观察中 |
| 没有托盘，只能从终端结束程序 | 无法作为日常应用使用 | 待讨论 |
| 与 Bob 共用 `Alt+Shift+D`，Gloss 不报冲突（macOS，用户实测） | Bob 运行时用户看到的是 Bob 的结果 | 待讨论 |
| 等待首个译文期间面板没有加载提示 | 真实服务延迟较高时，译文区是空白的 | 待讨论 |

## 开放问题

- 产品正式名称。
- 品牌视觉风格（有意延后）。
- 插件运行时最终用 goja 还是 wazero。
