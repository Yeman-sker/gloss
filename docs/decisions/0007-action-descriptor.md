# 0007 Action 描述、主结果和设置声明的最小契约

**日期：** 2026-11-05 **状态：** 已接受，已接入 `core/`（GLO-27）

**背景：** 输出、提示词功能、设置、路由都要读同一套关于“功能”的信息。如果各自定义，会出现四份互相重叠的“插件契约”。

**决定：**

- `core.Action` 不变（`ID`、`Name`、`Run`）。新增可选接口 `core.Describer`，返回 `core.Descriptor`：
  - `ID`：稳定的用途名（`translate`），不是服务名。
  - `Name`：界面上的名字；`Purpose`：一句用途描述，给路由（规则说明和 Jev 的选项描述）用。
  - `Output`：主结果的 `BlockKind`，`text` 或新增的 `markdown`。一个功能只有一个主结果 Block（index 0），类型固定，delta 只追加。Markdown 的结构留给 `app/` 渲染，不拆成多个 core Block。
  - `Replaceable`：结果**适合**替换原文。当前目标能否**安全写回**由宿主和 helper 运行时判断，不由功能声明。
  - `Settings`：设置项声明（键、类型 `text`/`bool`/`choice`/`number`、默认值、选项、说明），由 `Setting.Check` 统一校验。
- 没实现 `Describer` 的 Action 由 `core.Describe` 补默认值（`Output: text`，不可替换，无设置），旧 Action 不用改。
- 完成语义写进 `EventType` 注释：`EventDone` 表示结果完整、可以复制或替换；`EventError` 不能当作完整结果。面板动画结束是 `app/` 自己的事，与之无关。
- `Request` 新增 `Settings map[string]string`：宿主按声明校验、补默认值后传入，功能只能读到自己的设置。`Context` 新增 `WindowTitle`（只给路由，不发模型）和三态 `Editable`。

**理由：** 都是向后兼容的新增，旧 Action 和测试不用改。描述放在 Action 自己身上，插件以后也走同一条路。“适合替换”和“能否写回”分开，避免功能声明一个平台做不到的能力。

**代价：**

- `markdown` 是第一个让 `app/` 换渲染器的 `BlockKind`。以后新增结构化类型时，`app/` 也要相应支持。
- 只有一个主结果 Block，像“音标 + 释义 + 例句”这类多 Block 输出暂时只有翻译以外的旧路径在用，新功能先不支持。
