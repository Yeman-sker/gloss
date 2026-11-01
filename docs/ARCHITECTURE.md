# 架构

## 一句话

**对一段文本执行一个动作，流式返回结构化结果，再由宿主统一渲染。** 翻译、释义、解释热梗都只是不同的动作。

## 流程

```text
快捷键 → 取词 → 执行 Action → 流式 Blocks → 渲染
 app     capture    core / providers       app
```

## 分层

```text
        main.go
           │
         app/  ← 唯一依赖 mygo，负责外观和交互
       ┌───┼─────────┐
       ▼   ▼         ▼
 capture/ providers/ (plugins，未来)
       │     │
       │     ▼
       │   core/  ← 只依赖标准库
       ▼
 Rust helper（独立进程）
```

依赖只能向下。`arch_test.go` 检查 core、capture 和 providers 的依赖方向，违反时测试失败。

## 轻量极简的含义

| 原则 | 具体做法 |
|---|---|
| 不用 WebView | mygo 用 GPU 自绘界面 |
| 不用 cgo | Go 主程序保持纯 Go |
| 平台代码隔离 | 取词放在独立 helper，一个进程只做一件事 |
| 功能不进 core | core 只有契约和引擎，功能都是 Action |
| 依赖要有理由 | core 零第三方依赖 |

目标是预算而不是口号。macOS 原型实测：内存占用 41–47MB，主程序 9MB，helper 约 331KB，空闲 CPU 0%（[数据](research/2026-11-01-macos-spike.md)）。明显超出时要说明原因。

## 各层职责

- **`core/`**：见 [CORE.md](CORE.md)。不知道窗口、平台和具体服务。
- **`providers/`**：内置 Action。将来和第三方插件使用同一种 Action 契约。
- **`capture/`**：提供"当前选中了什么"。平台差异都封装在 `capture/helper/src/<平台>.rs`。
- **`app/`**：把 Blocks 变成界面。品牌风格以后只改这一层。
- **`plugins/`**：官方插件，尚未开始。

## 跨进程协议

Go 向 helper 的 stdin 写一行请求，helper 向 stdout 回一行 JSON，协议说明在 `capture/helper/src/main.rs` 顶部。

## 决策

选型原因见 [decisions/](decisions/)。
