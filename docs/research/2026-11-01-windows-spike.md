# Windows 技术验证（未完成）

**日期：** 2026-11-01 **执行：** 负责人的 Windows 机器上的 Codex
**结论：** 没有得出结论。负责人决定先推进 core，以后再做 Windows。

> 本文依据 Codex 输出摘要的照片整理。完整的报告和代码留在那台 Windows 机器上，未进入本仓库。

## 结果

- Go 无 cgo release 构建通过，固定文本动画运行了三轮。
- Codex 写的 Rust helper 独立测试时两次以 `0xC0000005`（非法内存访问）崩溃，根因未定位。推测是 helper 实现问题，未确认。
- `App.Quit()` 被关闭拦截阻止。
- 自动截图与目标窗口不符，焦点、应用取词、真实材质和字体均**未完成验证**。
- 进程清理有异常：一个 helper PID 能枚举到但无法终止，未查明。

## 源码分析（未实机验证）

依据 mygo v0.7.4 源码：

- `Panel`：置顶，不进任务栏和 Alt+Tab。
- `ShowInactive`：显示时不激活。点击弹窗是否抢焦点未知。
- `VibrancyGlass`：在 build 22621 及以上映射为 Acrylic，更低版本没有材质。
- `CornerRadius`：Win11 使用系统圆角，会忽略数值。

## 重新验证时注意

- 优先让人工按快捷键操作，自动化操作桌面不可靠。
- `Alt+Shift` 是 Windows 默认切换输入法的组合键，快捷键可能要换。
- 用 UI Automation + Ctrl+C 兜底实现取词；发 Ctrl+C 前要处理仍被按住的修饰键。
