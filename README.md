# Gloss（暂定名）

轻量、现代、动效精致的跨平台桌面划词工具。选中文字，按下快捷键，在不打断当前工作的情况下得到翻译或解释。

> **状态：** 正在打磨 0.1.0，只支持 macOS。进度见 [docs/STATUS.md](docs/STATUS.md)。

## 安装（0.1.0，macOS）

0.1.0 只提供本地构建的 .app，没有 Apple 签名和公证。需要 Go 1.27.3+ 和 Rust。

```bash
make app
```

1. 把 `build/darwin-arm64/Gloss.app` 拖进“应用程序”。
2. 打开 Gloss。它只在菜单栏显示图标，不占 Dock。第一次打开会弹出设置窗口：添加一个 OpenAI 兼容的模型服务，填入接口地址、Key 和模型名，点“测试连接”确认。
3. 在“系统设置 › 隐私与安全性 › 辅助功能”里允许 Gloss，用来读取选中的文字。
4. 在任意应用中选中文字，按 `Option+Shift+D`，按 `Esc` 关闭。快捷键可以在设置里改。

本机构建的 .app 没有隔离标记，可以直接打开。如果 .app 是从别人那里或网上下载的，macOS 会拦截未公证的应用：在“系统设置 › 隐私与安全性”底部点“仍要打开”（较早的 macOS 也可以在 Finder 里右键 › 打开）。这一条是 macOS 的通用行为，还没有用下载的 Gloss 实测过。

设置都保存在 `~/Library/Application Support/Gloss/config.json`（权限 0600，只有当前用户能读），也可以在设置的“通用”页用编辑器打开它。

## 开发

```bash
make run     # 构建并直接运行，不打包
make check   # 提交前必须通过
```

## 文档

- [项目现状](docs/STATUS.md)：从这里开始
- [开发规划（Linear）](https://linear.app/yeman/team/GLO/overview)
- [架构](docs/ARCHITECTURE.md)
- [Core 是什么](docs/CORE.md)
- [如何添加功能](docs/EXTENDING.md)
- [开发约定](docs/DEVELOPMENT.md)
- [文档索引](docs/README.md)

Agent 请从 [AGENTS.md](AGENTS.md) 开始。
