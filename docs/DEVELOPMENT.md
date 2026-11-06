# 开发约定

## 环境

- Go 1.27.3+（mygo 要求）
- Rust stable
- macOS 运行时要给启动程序的终端授予辅助功能权限

## 命令

| 命令 | 作用 |
|---|---|
| `make check` | 完成改动前必须通过 |
| `make test` | Go 和 Rust 测试 |
| `make build` | 构建到 `bin/` |
| `make run` | 构建并运行 |
| `make helper` | 构建 helper，放到 `bin/` 和 `resources/darwin/`。`go tool mygo dev` 打包时用后者，也不会自己构建 helper：改了 helper 要跑这一步 |
| `make app` | 打包 `build/darwin-arm64/Gloss.app`，helper 放进 `Contents/Resources`。“登录时启动”需要从 .app 运行 |
| `make icons` | 把 `design/icons/` 烘焙成 `app/icons/*.svgz`，把 `design/icon/gloss-tray-icon.svg` 导出成 `app/tray.png`，需要 Node 和 Chrome。结果已入库，只有改图标时才跑，见[决策 0012](decisions/0012-baked-icons.md) |
| `go run . stats [天数]` | 记忆里的路由统计（改正率、已确认准确率、未知比例、回退率、耗时） |
| `go run . recent [条数]`、`go run . label <会话> right\|wrong` | 查看最近的会话（不含文本），给会话标注对错 |
| `go run ./cmd/smoke` | 用配置里的真实服务测各功能的首字和总耗时，以及错误 Key、连不上时的提示 |
| `go run ./cmd/routebench` | 用固定样例比较规则和 Jev，测 Jev 延迟（需要 Jev 的 Key） |

结束程序：点托盘图标选“退出”，或在终端按 Ctrl+C。

调试面板卡顿：`GLOSS_FRAMELOG=1 ./bin/gloss`，动画期间超过 25ms 的帧间隔会写进日志。

调试取词：看 `~/Library/Logs/Gloss/gloss.log`。成功时写走了哪条路（`ax`、`menu`、`copy`）和来源应用；取不到时写来源应用和试过什么，从不含文字。如果写的不是你正在用的应用，就是前台判断错了。

## 配置

平时在设置窗口（托盘 › 设置…，或再次打开 Gloss）里改：每项改动先按下面的规则完整校验，通过后才以 0600 权限原子写入，不合法的改动不会写坏文件，错误显示在对应的那一项下面。也可以手动编辑用户配置目录下的 `Gloss/config.json`，不要放进仓库；设置 › 通用 › 配置文件 › 在编辑器中打开，会在文件不存在时生成模板。改完不用重启：程序每 2 秒检查一次，功能每次运行时读取最新的服务配置。设计见[决策 0008](decisions/0008-two-layer-settings.md)。

| 平台 | 路径 |
|---|---|
| macOS | `~/Library/Application Support/Gloss/config.json` |
| Windows | `%AppData%\Gloss\config.json` |
| Linux | `$XDG_CONFIG_HOME/Gloss/config.json`，默认 `~/.config/Gloss/config.json` |

```json
{
  "services": {
    "main": {"base_url": "https://api.openai.com/v1", "api_key": "你的Key", "model": "模型名称"},
    "jev": {"base_url": "https://api.typesafe.ai/v1", "api_key": "Jev的Key", "model": "jev-latest"}
  },
  "default_service": "main",
  "target_lang": "zh-Hans",
  "shortcut": "Alt+Shift+D",
  "launch_at_login": false,
  "memory": {"enabled": true, "retention_days": 90, "max_records": 5000},
  "routing": {"enabled": true, "jev_service": "jev", "wait_ms": 800, "min_confidence": 0.6, "send_window_title": false},
  "actions": ["translate", "explain", "prompt", "agent"],
  "action_settings": {
    "translate": {"target_lang": "en"},
    "explain": {"depth": "详细", "model": "另一个模型"},
    "agent": {"service": "main"}
  }
}
```

| 字段 | 说明 |
|---|---|
| `services` | 模型服务。`api_key` 和 `model` 必填，缺少时面板会说明是哪个服务缺什么。`base_url` 写到版本号为止，默认 `https://api.openai.com/v1`。不内置默认模型 |
| `default_service` | 功能没指定服务时用哪个。只有一个服务时可以省略 |
| `shortcut` | 全局快捷键，默认 `Alt+Shift+D` |
| `memory` | 是否记录使用、保留天数、最多条数。托盘可以开关，设置里可以清空 |
| `routing` | 是否自动选择功能；`jev_service` 为空时只用规则和记忆；`wait_ms` 是等 Jev 的上限；`min_confidence` 是采用 Jev 结果的最低把握；`send_window_title` 是否把窗口标题发给 Jev |
| `actions` | 启用哪些功能及顺序，第一个是默认功能 |
| `action_settings.<ID>` | 该功能声明的设置项，以及 `service`、`model`。不能在这里写 Key |

旧格式（顶层的 `api_key`、`base_url`、`model`、`target_lang`）仍然可用，会当作名为 `main` 的服务。

拼错字段名、类型不对、值不在选项里都会报错，错误信息写明行列和字段，但不会引用值。文件是明文 Key，程序读取时会把权限设为 600。

## 诊断日志

macOS 上在 `~/Library/Logs/Gloss/gloss.log`（权限 0600），满 1 MB 时轮换，只留一个旧文件 `gloss.log.1`。日志只记事件、耗时、错误类别和来源应用，不记选中的文字、结果、窗口标题和 Key（`app/logfile.go` 的 `errKind`）。设置 › 通用 › 诊断日志 › 显示日志。

## 发布

目前发到私有仓库的 GitHub Release：ad-hoc 签名，不公证，没有自动更新。

1. 改版本号：`app/prefs.go` 的 `Version` 和 `mygo.json`。
2. `make check`，同步文档，提交到 main 并推送。
3. 工作区干净时运行 `make app`，产物是 `build/darwin-arm64/gloss-<版本>-arm64.dmg`。
4. 打 tag 并发布：`git tag -a v<版本> -m "Gloss <版本>"`，`git push origin v<版本>`，`gh release create v<版本> <dmg> --verify-tag --title "Gloss <版本>" --notes-file <说明>`。
5. 用 `gh release download` 下载回来，核对校验和。

试下载版之前：

- **先停掉开发版。** `mygo dev` 的 `Gloss Dev` 和正式版单实例锁不同，能同时运行，都报告 `ready` 而不提示冲突，但抢同一个快捷键，共用配置和记忆文件。
- **Gatekeeper。** 浏览器下载的文件带隔离属性，装好后运行 `xattr -dr com.apple.quarantine /Applications/Gloss.app`。`gh release download` 下载的不带。
- **辅助功能权限。** ad-hoc 签名的每个构建身份不同。如果开关开着却提示没有权限，把列表里的 Gloss 删掉重新添加（推测，未测）。

## 规划（Linear）

[Linear 的 GLO 团队](https://linear.app/yeman/team/GLO/overview) 是规划的地方，`docs/` 是记录的地方。

1. **先讨论：** 开发者和 Agent 一起讨论计划。
2. **再落到 Linear：** 把讨论结果写成 issue，用户同意后 Agent 才能创建或调整。
3. **逐步打磨：** 随着理解加深，持续调整 issue 的范围、顺序和拆分。
4. **事后记录：** 工作完成后，把事实写回 `docs/`。

| 状态 | 含义 |
|---|---|
| Backlog | 想做，还没排期 |
| Todo | 已决定要做 |
| In Progress | 有人正在做 |
| In Review | 代码已完成，等待评审或合并 |
| Done | 已合并且验证通过 |
| Canceled / Duplicate | 不做或重复，写明原因 |

约定：

- 按 issue 开工时，先把状态切到 In Progress 并指派给自己，避免撞车。
- 工作中发现的新问题先告诉用户，不要自行建 issue，也不要顺手扩大范围。
- 关闭 issue 前写验证结果：运行了什么，在什么平台，什么没验证。
- 长期有效的结论写进 `docs/`，不要只留在 issue 里。

## 协作

- `main` 始终可构建。功能在分支上开发，优先使用 Linear 生成的分支名；没有时用 `glo-123-short-title`。
- 提交信息说明为什么改，用 `feat:`、`fix:`、`docs:`、`refactor:`、`test:`、`chore:` 开头，有 issue 时在末尾写 `GLO-123`。
- 文档和代码放在同一个提交里。

## 代码风格

- 遵循 `gofmt` 和相邻代码的写法。
- 注释说明原因，不重复代码。
- 平台相关代码只放在 `capture/helper/src/<平台>.rs` 和必要的 `app/` 中。
- 宁可少量重复，也不提前抽象。

## 测试

- `core` 和 `providers`：单元测试，不访问真实网络。
- `app`：可以用 `ui.NewTester` 测试视图，动效和焦点要实际运行。
- 未在某个平台实际运行过，就不要声称它在该平台可用。
- **测试安全：** 不要关闭用户已打开的文档，也不要向其窗口发送按键。测试 Cmd/Ctrl+C 兜底前先备份剪贴板。

## 决策记录

文件命名 `docs/decisions/NNNN-短标题.md`，内容为背景、决定、理由、代价。已接受的记录不改结论；想法变了就新增一条，并在旧记录顶部标注"已被 NNNN 取代"。

## 工作日志

追加到 `docs/journal/YYYY-MM.md`，格式：

```markdown
## YYYY-MM-DD 做了什么（作者或 Agent）
- 结果：
- 验证：运行了什么，在什么平台
- 遗留：
```

## 升级 mygo

mygo 还没到 1.0，API 可能变动。升级使用 `.agents/skills/mygo-maintenance`，并只修改 `app/`。

## 字体

`app/fonts/` 由 `scripts/subset_fonts.py <下载的字体目录> <输出目录>` 生成，选择依据见 [0006](decisions/0006-bundled-fonts.md)。
