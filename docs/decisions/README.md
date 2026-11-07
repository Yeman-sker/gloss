# 决策记录

| 编号 | 决定 | 状态 |
|---|---|---|
| [0001](0001-mygo-and-go.md) | 界面用 mygo 自绘，主程序用 Go | 已接受 |
| [0002](0002-actions-return-blocks.md) | Action 只返回结构化 Blocks | 已接受 |
| [0003](0003-capture-helper-process.md) | 取词放在独立的 Rust 进程 | 已接受 |
| [0004](0004-docs-as-memory.md) | 文档即记忆 | 已接受 |
| [0005](0005-docs-record-linear-plans.md) | 文档记录事实，Linear 管理规划 | 已接受 |
| [0006](0006-bundled-fonts.md) | 打包字体，不用系统字体 | 已接受，已接入 `app/` |
| [0007](0007-action-descriptor.md) | Action 描述、主结果和设置声明的最小契约 | 已接受，已接入 `core/` |
| [0008](0008-two-layer-settings.md) | 设置分两层，Key 放在 0600 的配置文件里 | 已接受，已接入 `settings/` |
| [0009](0009-memory-and-routing.md) | 记忆与自动路由的分层、存储和统计口径 | 已接受，已接入 `memory/`、`routing/` |
| [0010](0010-setting-label.md) | 设置声明增加 Label，界面用它做标题 | 已接受，已接入 `core/` |
| [0011](0011-ink-and-marker.md) | 设置界面的视觉语言：墨与荧光，圆角规则 | 已接受方向，待负责人确认手感 |
| [0012](0012-baked-icons.md) | 动态图标用烘焙帧，不在运行时跑引擎 | 已接受，已接入 `app/` |
| [0013](0013-descriptor-summary.md) | 功能描述增加面向用户的 Summary | 已接受，已接入 `core/` |
| [0014](0014-panel-press-and-plan.md) | 面板不猜手势，打开前先定框架 | 已接受，已接入 `app/`，等负责人上手确认 |
| [0015](0015-updates-and-signing.md) | 自动更新读私有 Release，改用固定的证书签名 | 已接受，已接入 `app/`，真实更新待下个版本验证 |
