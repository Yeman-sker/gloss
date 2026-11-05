# Gloss（暂定名）

轻量、现代、动效精致的跨平台桌面划词工具。选中文字，按下快捷键，在不打断当前工作的情况下得到翻译或解释。

> **状态：** 早期开发，已接入 OpenAI 兼容接口，尚未对真实服务验证。进度见 [docs/STATUS.md](docs/STATUS.md)。

## 快速开始

需要 Go 1.27.3+ 和 Rust。目前只支持 macOS，Windows 开发已暂停。

先创建配置文件 `~/Library/Application Support/Gloss/config.json`（字段和其他平台路径见[开发约定](docs/DEVELOPMENT.md#翻译服务配置)）：

```json
{
  "api_key": "你的Key",
  "model": "模型名称"
}
```

然后运行：

```bash
make run
```

在任意应用中选中文字，按 `Option+Shift+D`，按 `Esc` 关闭。首次使用要在系统设置中授予辅助功能权限。

## 文档

- [项目现状](docs/STATUS.md)：从这里开始
- [开发规划（Linear）](https://linear.app/yeman/team/GLO/overview)
- [架构](docs/ARCHITECTURE.md)
- [Core 是什么](docs/CORE.md)
- [如何添加功能](docs/EXTENDING.md)
- [开发约定](docs/DEVELOPMENT.md)
- [文档索引](docs/README.md)

Agent 请从 [AGENTS.md](AGENTS.md) 开始。
