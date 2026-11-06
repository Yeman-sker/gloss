# 如何添加功能

先判断属于哪一类，**能往上放就不要往下放**。

| 你想加 | 放哪 | 改 core？ |
|---|---|---|
| 新的翻译服务 | `providers/<名称>/` 新 Action | 否 |
| 新的模型功能（释义、改写、总结） | `providers/prompt/specs/<ID>/` 下写声明和提示词 | 否 |
| 新能力（不靠模型，或要特殊处理） | 新 Action，将来可以是插件 | 否 |
| 新的结果类型（如词性） | 新增 `BlockKind`，并在 `app/panel.go` 渲染 | 向后兼容的小改动 |
| 新外观或动效 | `app/` | 否 |
| 新的取词方式或平台 | `capture/helper/src/<平台>.rs` | 否 |
| 新的触发方式（OCR、输入框） | `app/`，最终生成 `core.Request` | 否 |

## 添加一个提示词功能（最常见）

翻译、释义、优化提示词、整理输出都是“提示词模板 + 模型 + 输出类型”，由 `providers/prompt` 统一运行。新功能只要加一个目录：

```text
providers/prompt/specs/<ID>/
  spec.json   ID、名字、用途、输出类型、能否替换、顺序、设置项（每项写 label，设置窗口用它做标题）
  system.md   系统提示词
  user.md     用户消息模板
```

模板变量：`{{text}}`、`{{surrounding}}`、`{{source_app}}`、`{{source_lang}}`、`{{target_lang}}`、`{{setting.<键>}}`。`{{#x}}…{{/x}}` 在 x 非空时输出，`{{^x}}…{{/x}}` 在 x 为空时输出。用到未声明的设置或未知变量时，加载就会报错。

1. 写好三个文件。`purpose` 要写清用户什么时候想用它，路由靠这句话区分功能。
2. `go test ./providers/prompt`：所有声明都会被加载和校验。
3. 新功能默认启用；用户的配置里有 `actions` 列表时，要把 ID 加进去才会出现。
4. 用真实服务试几次：`go run ./cmd/smoke -feature <ID>`。
5. 如果新功能应该被自动选中，在 `routing/testdata/samples.json` 加几条样例，并检查 `routing.Rules`。

功能拿不到 Key，也不能自己选服务：`prompt.Action` 只调用宿主给的 `Chat`，由宿主按 `action_settings.<ID>.service` 选择服务。

## 添加一个内置 Action

1. 复制 `providers/demo/`，改成 `providers/<名称>/`。
2. 实现 `ID`、`Name`、`Run`，响应 `ctx` 取消，用 `emit` 输出 Blocks。需要出现在面板的功能栏、被路由选择或有设置项时，再实现 `Describe`（见 [CORE.md](CORE.md)）。
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
