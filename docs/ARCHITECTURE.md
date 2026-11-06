# 架构

## 一句话

**对一段文本执行一个动作，流式返回结构化结果，再由宿主统一渲染。** 翻译、释义、解释热梗都只是不同的动作。

## 流程

```text
快捷键 → 取词 → 路由 → 执行 Action → 流式 Blocks → 渲染 → 复制 / 替换原文
 app     capture  routing   core / providers        app      app + capture
                    ↑                                  │
                  memory  ←──────── 会话记录（不含文本）─┘
```

一次取词是一个**会话**（`app/session.go`）：来源快照固定不变，切换功能和重试都在同一快照上开启新的**执行**，只有最新一次执行的事件能显示。会话结束时，记录写进记忆。

## 分层

```text
              main.go
                 │
               app/  ← 唯一依赖 mygo，负责外观、交互和把各层接起来
   ┌──────┬──────┼────────┬─────────┐
   ▼      ▼      ▼        ▼         ▼
capture/ settings/ routing/ providers/ (plugins，未来)
   │      │      │  │       │
   │      │      │  ▼       │
   │      │      │ memory/  │
   │      ▼      ▼  ▼       ▼
   │          core/  ← 只依赖标准库
   ▼
 Rust helper（独立进程）
```

依赖只能向下。`arch_test.go` 检查 core、capture、providers、settings、memory、routing 的依赖方向，违反时测试失败。

## 轻量极简的含义

| 原则 | 具体做法 |
|---|---|
| 不用 WebView | mygo 用 GPU 自绘界面 |
| 不用 cgo | Go 主程序保持纯 Go |
| 平台代码隔离 | 取词放在独立 helper，一个进程只做一件事 |
| 功能不进 core | core 只有契约和引擎，功能都是 Action |
| 依赖要有理由 | core 零第三方依赖 |

目标是预算而不是口号。macOS 原型实测：内存占用 41–47MB，主程序 9MB，helper 约 331KB，空闲 CPU 0%（[数据](research/2026-11-01-macos-spike.md)）。0.1.0（2026-11-06 实测）：主程序 16.44 MB（字体占约 4 MB），空闲 37–40 MB，其中 mygo 框架本身约 35 MB（[数据](research/2026-11-06-memory.md)）。明显超出时要说明原因。

## 各层职责

- **`core/`**：见 [CORE.md](CORE.md)。不知道窗口、平台和具体服务。
- **`providers/`**：内置 Action。`providers/prompt` 用声明加提示词运行四个功能，`providers/openai` 只负责 OpenAI 兼容接口的流式调用。将来和第三方插件使用同一种 Action 契约。
- **`settings/`**：读写配置文件，按功能声明校验设置，Key 只在这里。`Update` 是唯一的写入口：改动后的整份文件先通过和读取相同的校验，再原子写入（0600），服务改名和删除会同步改掉所有引用。错误分类（`ErrNoService`、`MissingError`、`ErrInvalid`）让界面能把错误指向设置里对应的页面。见[决策 0008](decisions/0008-two-layer-settings.md)。
- **`memory/`**：本地使用记录、偏好提炼和统计。**`routing/`**：规则、Jev、偏好、最近选择、默认的选择顺序。见[决策 0009](decisions/0009-memory-and-routing.md)。
- **`capture/`**：提供“当前选中了什么”、来源信号，以及在目标没变时写回。平台差异都封装在 `capture/helper/src/<平台>.rs`。
- **`app/`**：把 Blocks 变成界面，还有设置窗口（`prefs.go`，包括首次运行引导，界面按功能声明生成）、错误到修复入口的映射（`fix.go`）和诊断日志（`logfile.go`）。品牌风格以后只改这一层。
- **`plugins/`**：官方插件，尚未开始。

## 跨进程协议

Go 向 helper 的 stdin 写一行 JSON 请求，helper 向 stdout 回一行 JSON，请求和响应用 `id` 对应。协议说明在 `capture/helper/src/main.rs` 顶部。Go 客户端给每个请求设超时，helper 超时、退出或回复无法解析时自动重启。

## 决策

选型原因见 [decisions/](decisions/)。
