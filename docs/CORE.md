# Core 是什么

Core 是产品的本体，也就是**不依赖任何窗口和平台**、脱离界面也能完整测试的部分。代码全部在 `core/core.go`，只依赖标准库。

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
| `Request` | 文本加 `Context`（来源应用、上下文、语言）。解释句子和热梗需要上下文，所以一开始就带上 |
| `Block` | 结果的一段，由 `index` 标识，`delta` 追加文本 |
| `BlockKind` | 语义类型：`text`、`phonetic`、`definition`、`example`、`tag`。只表达含义，不表达外观 |
| `Engine` | 同一时间只运行一个 Action |
| `Event` | Engine 发给界面的 `Delta`、`Done` 或 `Error`，带有 run 编号 |

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
