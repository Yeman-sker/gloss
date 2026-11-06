# Core 是什么

Core 是产品的本体，也就是**不依赖任何窗口和平台**、脱离界面也能完整测试的部分。代码在 `core/core.go` 和 `core/describe.go`，只依赖标准库。

## 只做四件事

1. **定义 Action：** 一个能力是什么样的。
2. **定义 Block：** 结果长什么样。
3. **管理执行：** 注册、启动、流式传递结果。
4. **保证取消：** 新请求立刻终止旧请求，旧结果不再送达。

## 不属于 core

快捷键、窗口、动画、颜色、取词、网络请求细节、某个翻译服务、配置界面。判断标准：**如果这个东西换一个界面或平台就要变，它就不属于 core。**

## 契约

```go
type Action interface {
    ID() string
    Name() string
    Run(ctx context.Context, req Request, emit Emit) error
}

type Emit func(index int, kind BlockKind, delta string)
```

| 概念 | 说明 |
|---|---|
| `Request` | 文本、`Context` 和 `Settings`（宿主按功能声明校验过的设置值） |
| `Context` | `SourceApp`（bundle id）、`Surrounding`（选区连同前后各约 300 字，有上限）、`TargetLang`、`WindowTitle`（只给路由，不发给模型）、`Editable`（三态：yes、no、unknown） |
| `Block` | 结果的一段，由 `index` 标识，`delta` 追加文本 |
| `BlockKind` | 语义类型：`text`、`markdown`、`phonetic`、`definition`、`example`、`tag`。只表达含义，不表达外观 |
| `Engine` | 同一时间只运行一个 Action；`Descriptors()` 列出所有功能的描述 |
| `Event` | Engine 发给界面的 `Delta`、`Done` 或 `Error`，带有 run 编号 |

## 描述（可选）

Action 可以再实现 `Describer`，返回 `Descriptor`，见[决策 0007](decisions/0007-action-descriptor.md)：

| 字段 | 说明 |
|---|---|
| `ID`、`Name` | 稳定的用途名（`translate`）和界面上的名字 |
| `Purpose` | 一句用途描述，路由用它 |
| `Output` | 主结果（index 0）的类型，`text` 或 `markdown`，整个执行中不变 |
| `Replaceable` | 结果适合替换原文。能否安全写回由宿主在运行时判断 |
| `Settings` | 设置项：键、类型（`text`、`bool`、`choice`、`number`）、默认值、选项、说明 |

没实现 `Describer` 的 Action 由 `core.Describe` 补上默认值：输出 `text`，不可替换，没有设置。

## 完成语义

- `EventDone`：结果完整，可以复制或替换原文。
- `EventError`：不能当成完整结果，哪怕之前已经输出了一部分。
- 面板的动画什么时候结束是 `app/` 的事，与这两个事件无关。

## Action 必须遵守

- `ctx` 结束后尽快返回。
- 只通过 `emit` 输出，不接触界面。
- 失败时返回 error，不要把错误信息当作结果输出。

## Sink 必须遵守

- 不能阻塞，也不能重新调用 Engine。Sink 在 Engine 的锁内执行，这样才能保证取消后不再有事件送达。
- 在 Action 的 goroutine 上执行，界面要自行转到主线程。

## 为什么 Action 不能带 UI

这样所有功能都能共享同一套动效和品牌风格，第三方插件也破坏不了体验。见[决策 0002](decisions/0002-actions-return-blocks.md)。

## 修改 core 的门槛

新增 `BlockKind` 这类向后兼容的改动，只需更新本文档。修改 `Action`、`Emit` 或 `Event` 这类破坏性改动，要先写决策记录。
