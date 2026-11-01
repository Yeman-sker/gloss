# 项目现状

> 新会话先读这里。这里只记录现状，历史放到 [journal](journal/)，规划在 [Linear](https://linear.app/yeman/team/GLO/overview)。

**最后更新：** 2026-11-01
**阶段：** 0 脚手架 → 1 Core 与真实翻译
**一句话：** 项目骨架和 core 契约已建好，弹窗能显示演示数据；还没有接入真实翻译服务。

## 平台状态

| 能力 | macOS | Windows | Linux |
|---|---|---|---|
| 构建主程序 | 已实测 | 原型交叉编译通过，脚手架未测 | 原型交叉编译通过，脚手架未测 |
| 取词 helper | 已实现（原型实测） | **未实现** | 未实现 |
| 弹窗与动画 | 已实测 | 原型动画跑过，焦点、材质和字体未完成验证 | 未测 |

## 已完成

- 技术验证：mygo + Rust helper 在 macOS 上可行，见 [macOS 验证](research/2026-11-01-macos-spike.md)。
- `core`：Action、Block、Engine（新请求取消旧请求），有单元测试。
- `providers/demo`：无需网络的演示 Action。
- `capture`：Go 客户端和 macOS helper。
- `app`：全局快捷键、不抢焦点的弹窗、流式淡入、高度弹簧动画。
- 分层检查 `arch_test.go`，以及文档和 Agent 配置。

## 尚待讨论的想法

初期想到的方向，还没有讨论，也不是承诺的计划。讨论后进入 [Linear](https://linear.app/yeman/team/GLO/overview)，并从这里删除。

- 接入 OpenAI 兼容服务，换掉演示数据。
- 增加一个传统翻译 API。
- 最简设置和菜单栏托盘（含退出入口）。
- 插件运行时：验证 goja 能否支持流式和异步。
- Windows 取词和手动验证。
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

## 开放问题

- 产品正式名称。
- 品牌视觉风格（有意延后）。
- 插件运行时最终用 goja 还是 wazero。
