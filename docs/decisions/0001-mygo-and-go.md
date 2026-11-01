# 0001 界面用 mygo 自绘，主程序用 Go

**日期：** 2026-11-01 **状态：** 已接受

**背景：** 目标是极轻量、动效精致、跨平台、风格统一。曾考虑 Tauri + WebView。负责人明确不用 WebView，选择 mygo。

**决定：** 用 mygo 的 `ui` 包自绘界面。mygo 是 Go 框架，所以 core 和界面都用 Go。

**理由：**
- GPU 自绘，能做完全自定义的品牌风格，也不受各平台 WebView 差异影响。
- 提供 Panel、弹簧动画、全局快捷键等所需能力。
- macOS 上实测达标，见[验证报告](../research/2026-11-01-macos-spike.md)。

**代价：**
- mygo 未到 1.0，贡献者很少。用分层隔离：只有 `app/` 依赖它，万一更换不影响 core。
- Windows 只跑过部分验证，见 [Windows 验证](../research/2026-11-01-windows-spike.md)。
