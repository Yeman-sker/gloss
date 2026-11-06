# Arc 取不到选中文字：排查和菜单路径

**日期：** 2026-11-06 **平台：** macOS（Darwin 27，Apple M4），Arc 1.172.1，Chrome、Chromium（本机版本） **作者：** Claude Code

负责人报告：Gloss 在 Arc 里拿不到选中的文字，Safari 正常。

**结论先写：** Arc 里失败的确切原因还没有查明，因为没有向负责人正在用的 Arc 发过按键或按菜单。修复是在 Cmd+C 之前先按应用自己的 Copy 菜单项，这条路在 Chrome、Chromium、TextEdit 上实测可用；Arc 上它存在且可用（只读确认），但有没有修好要负责人实测。

## 怎么测

- **负责人的 Arc 和 Safari：** 只读。探针只输出角色、属性名、错误码和长度，不输出文字，不设置任何属性，不发按键，不按菜单。
- **主动测试：** 只对自己开的实例。Chrome 和 Chromium 用临时 profile 打开一张自己的测试页，页面加载时用 JS 选中一段文字；TextEdit 是单独的新实例和自己的文档。按键和菜单只发给这些进程。
- **剪贴板：** 主动测试前后核对全部类型和数据的哈希，一致（实测 `98490b97dcdf` 前后相同）。

探针脚本没有进仓库，只用于这次排查。

## 日志（已实测）

诊断日志里 Safari 4 次全部走 `copy`（254–382ms），说明 AX 在网页正文上本来就取不到，只是 Cmd+C 救了回来。全文件没有一条成功记录来自 Arc（`company.thebrowser.Browser`）。`no selection` 共 15 条，当时的日志不记来源应用，所以**不能确定它们都来自 Arc**。这条缺口已补，见“改动”。

## AX 在 Chromium 上（已实测，Chrome）

| 做法 | 结果 |
|---|---|
| 什么都不设置 | `AXFocusedUIElement` 返回 -25212（没有值）：默认不建无障碍树 |
| 设置 `AXManualAccessibility`（helper 原来的唤醒） | -25205（不支持）。这是 Electron 的开关，**对 Chromium 无效** |
| 设置 `AXEnhancedUserInterface` | 返回 -25208，但实际生效。1 秒时还读不到，3 秒时读到：焦点是 `AXWebArea`，`AXSelectedText` 是完整选区，`AXSelectedTextMarkerRange` 也可用 |
| 再设回 false | 500ms 后又读不到 |

Arc 只读结果和 Chrome 一致：`AXManualAccessibility` -25205，`AXEnhancedUserInterface` 当前为 0。Arc 和 Safari 都在后台时 `AXFocusedUIElement` 都是 -25212，所以这一条不能单独说明 Arc 前台时的情况。

**不用 `AXEnhancedUserInterface`：** 第一次要等 1–3 秒，而且会让浏览器长期维护整个页面的无障碍树。一直开着的性能代价和对窗口动画的影响未测（后者是推测）。

## 复制的三种触发方式（已实测）

单位 ms，按时间顺序。测的是从发出到剪贴板变化。

| 方式 | Chrome | Chromium |
|---|---|---|
| 按 Copy 菜单项（`AXPress`） | 19, 4, 3, 3, 2, 3, 3, 3 | 4, 2, 2, 3, 2, 2, 3, 3 |
| Cmd+C 只发给目标进程 | 82, 10, 9, 10, 12, 12, 12, 7 | 40, 7, 8, 8, 7, 10, 10, 10 |

另有一次 Chrome 刚启动后的第一次 Cmd+C 用了 712ms，超过 helper 当时 600ms 的等待，同一轮随后的菜单项是 24ms。只出现过一次，**不是 Arc 问题的解释**：多次采样后 Cmd+C 在 Chromium 上是正常的。

没有选区时（Chrome）：菜单项仍然显示可用，按了不写剪贴板，Cmd+C 也不写。所以 Chromium 上不能用菜单项的 `AXEnabled` 判断有没有选区。TextEdit 没有选区时 Copy 是禁用的。

## 谁在前台（已实测）

`front_pid` 先问系统的 `AXFocusedApplication`，取不到时用窗口列表第一个 layer 0 的窗口。

| 前台应用 | `AXFocusedApplication`（20 次） | 最终选中目标 |
|---|---|---|
| Chrome | 全部失败：-25212 共 19 次，-25204 共 1 次 | 20/20，靠窗口列表 |
| Chromium | 全部失败：-25212 共 19 次，-25204 共 1 次 | 20/20，靠窗口列表 |
| TextEdit | 成功 19 次，-25204 共 1 次 | 20/20 |

所以 Chromium 内核的应用在前台时，发给谁完全取决于窗口列表。负责人这台机器的窗口列表紧跟前台应用的是两个 `Cua Driver` 窗口（1920×1080，layer 0，alpha 1），后面是台前调度（`WindowManager`）的缩略窗口。Arc 的主窗口也是 layer 0（只读确认），它在前台时理应排第一，**但 Arc 在前台时的列表没有采到**。现在每次失败都记录选中的应用，如果选错了会直接看到。

## Arc 的 Copy 菜单项（只读）

| 应用 | 位置 | 可用 | 查找耗时（从第一个菜单找起） |
|---|---|---|---|
| Arc | 第 4 个菜单 Edit › Copy | 是 | 20ms，66 项 |
| Safari | 第 4 个菜单 编辑 › 拷贝 | 否（当时在后台） | 43ms，76 项 |
| Claude 桌面端 | 第 4 个菜单 Edit › Copy | 是 | 10ms，49 项 |

## 改动

`capture/helper/src/macos.rs` 的取词顺序变成：

1. AX 的 `AXSelectedText`，没有变化。
2. **目标应用的 Copy 菜单项。** 按按键等效值（Command+C，没有其他修饰键）查找，不受语言影响；Edit 所在的第 4 个菜单先找，查找限时 150ms。禁用就跳过。按下后最多等 150ms。
3. Cmd+C 只发给目标进程，等 600ms，和以前一样。

复制前保存、复制后恢复剪贴板的做法不变。菜单路径不用等用户松开快捷键，也不经过按键，所以不受面板是键盘窗口影响，网页也看不到按键。

失败时日志写明应用和试过什么，从不含文字，例如：

```
capture: nothing selected: com.google.Chrome: no focus (-25212); Copy pressed, Cmd+C sent, the clipboard did not change
```

`wake` 的注释原来说 Chromium 也靠它，已更正。

## 改动后的端到端结果（已实测，真实 helper）

单位 ms，从收到请求到回复，首次较慢。

| 场景 | 结果 |
|---|---|
| Chrome，有选区 | `menu`：107.6, 46.3, 38.8, 43.9, 41.9，文字完整 |
| Chromium，有选区 | `menu`：73.3, 32.7, 27.4, 27.6, 27.3，文字完整 |
| Chrome，没有选区 | `no_selection`，5 次都如此，原因如上例 |
| TextEdit，有选区 | `ax`：108.0, 5.3，没有退化 |
| TextEdit，强制跳过 AX | `menu`：85.8, 28.5, 25.0 |
| TextEdit，没有选区 | `no_selection`，“Copy disabled, Cmd+C sent” |

推算（未单独计时）：Chromium 上没有选区时，提示要等 150 + 600ms，比以前多约 150ms。

## 未测

- **Arc 本身：** 修复是否生效，以及原来到底卡在哪一步。下次在 Arc 里试，日志会直接说明。
- **Safari：** 没有对负责人的 Safari 主动测试。菜单不可用时走的还是原来的 Cmd+C；可用时走菜单。
- 按菜单项时菜单栏标题是否闪一下：这个环境没有屏幕录制权限，看不到。
- 面板是键盘窗口时的菜单路径（按理不受影响，推测）。
- Electron 应用（含 GLO-9 的 Claude 桌面端）、终端、Google Docs 这类自己处理复制的网页。

## 发现但没有改的

- **迟到的复制会留在剪贴板里。** Cmd+C 超过等待时间才生效时，helper 已经放弃，不再恢复，用户的剪贴板内容被选中文字替换。是从代码读出来的，上面 712ms 那次的情形会触发；菜单路径减少了它，但只走 Cmd+C 的应用仍可能发生。放弃后继续盯剪贴板又可能盖掉用户自己刚复制的内容，所以没有改。
- **Chromium 内核的应用里没有写回凭证。** 实测 `editable=unknown`、`token=0`，因为没有焦点元素：面板不能替换原文，也拿不到前后文。要改只能开 `AXEnhancedUserInterface`，代价见上。
