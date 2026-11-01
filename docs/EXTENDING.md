# 如何添加功能

先判断属于哪一类，**能往上放就不要往下放**。

| 你想加 | 放哪 | 改 core？ |
|---|---|---|
| 新的翻译服务 | `providers/<名称>/` 新 Action | 否 |
| 新能力（句子释义、热梗解释） | 新 Action，将来可以是插件 | 否 |
| 新的结果类型（如词性） | 新增 `BlockKind`，并在 `app/panel.go` 渲染 | 向后兼容的小改动 |
| 新外观或动效 | `app/` | 否 |
| 新的取词方式或平台 | `capture/helper/src/<平台>.rs` | 否 |
| 新的触发方式（OCR、输入框） | `app/`，最终生成 `core.Request` | 否 |

## 添加一个内置 Action

1. 复制 `providers/demo/`，改成 `providers/<名称>/`。
2. 实现 `ID`、`Name`、`Run`，响应 `ctx` 取消，用 `emit` 输出 Blocks。
3. 写测试：正常流式、取消、错误。不要访问真实网络。
4. 在 `app/app.go` 的 `Run` 中用 `engine.Register` 注册。
5. 把新包加入 `arch_test.go` 的 `forbidden`，确认它不依赖 app 和 mygo。
6. `make check`，并实际运行一次。
7. 更新 `docs/STATUS.md` 和 journal。

Agent 可以使用 `add-action` skill。

## 插件（规划中，尚未实现）

下面是目标设计，goja 未验证，实现前可能调整。

- 插件是一个目录：`manifest.json` 加 JS 入口。
- 插件提供的也是 Action，经 `Engine.Register` 注册，使用同样的 Blocks。
- 运行在内嵌 JS 引擎里，网络、存储和密钥都由宿主提供，并按 manifest 声明的权限和域名白名单开放。
- 内置服务也会迁移到同一套 API，确保 API 够用。

在插件系统完成之前，新功能都先做成 `providers/` 下的内置 Action。由于契约相同，以后迁移成本很低。

## 不要这样做

- 在 core 中判断 `if action == "translate"`。
- 让 Action 返回颜色、字号、HTML 或布局。
- 在 `app/` 中写某个服务的业务逻辑。
- 为单一功能修改 core 的公开契约。
