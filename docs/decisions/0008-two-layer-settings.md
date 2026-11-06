# 0008 设置分两层，Key 放在权限为 0600 的配置文件里

**日期：** 2026-11-05 **状态：** 已接受，已接入 `settings/`（GLO-25）

**背景：** 功能、记忆、路由都需要配置，以后插件也要。负责人要求设置分产品和功能两层，Key 只在产品层。

**决定：**

- 新包 `settings/` 读写 `~/Library/Application Support/Gloss/config.json`（其他平台按 `os.UserConfigDir`）。只依赖标准库和 `core`。
- **产品层**：`services`（多个模型服务，各有 `base_url`、`api_key`、`model`）、`default_service`、`shortcut`、`target_lang`、`launch_at_login`、`memory`、`routing`、`actions`（启用哪些功能及顺序）。
- **功能层**：`action_settings.<功能ID>`，只能填该功能声明过的设置项，外加 `service` 和 `model` 两个保留键（选择服务和模型）。在这里写 `api_key` 或 `base_url` 会报错：Key 只能放在 `services`。
- 读取严格校验：未知字段、类型错误、超出选项的值都报错，错误写明文件、行列和字段名，**从不引用字段的值**（可能是 Key）。读取时把文件权限收紧到 0600。
- 旧格式（顶层 `api_key`/`base_url`/`model`）继续可用，读成名为 `main` 的服务。
- 托盘开关（自动选择、记忆、登录时启动）通过 `settings.Set` 改文件本身，文件是唯一的事实来源。程序每 2 秒检查修改时间，改完不用重启。
- 功能拿不到 Key：`prompt.Action` 只调用宿主给的 `Chat` 函数，由 `app/` 按功能 ID 取服务。

**理由：** 实测前只有负责人一个用户，手改 JSON 足够；设置界面（GLO-26）放到实测后。Key 存文件而不是钥匙串：不用 cgo，也不必为钥匙串单独写 helper 命令；代价是明文，用 0600 限制其他用户读取。

**代价：**

- Key 是明文。如果以后分发给别人，应改存钥匙串（需要 helper 支持），届时新增决策。
- 托盘写回文件时键会按字母排序，用户手写的顺序会变（内容不变）。
- 严格校验意味着拼错字段名会报错，而不是被静默忽略。这是有意的。
