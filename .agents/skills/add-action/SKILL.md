---
name: add-action
description: 添加新的翻译服务或文本能力时使用，例如接入 DeepL、OpenAI 兼容接口、句子释义、热梗解释。
---

# 添加 Action

先读 `docs/CORE.md` 和 `docs/EXTENDING.md`。

1. 确认不需要修改 core。如果觉得需要，先和用户讨论并写决策记录。
2. 以 `providers/demo/` 为模板创建 `providers/<名称>/`。
3. 实现 `core.Action`：
   - 响应 `ctx` 取消，及时返回
   - 用 `emit` 输出语义 Block，不输出样式
   - 错误通过返回值传递，不要把错误作为结果输出
4. 写测试，覆盖正常流式、取消和错误。网络用 `httptest`，不访问真实服务，不在代码或测试中写入真实 Key。
5. 在 `app/app.go` 注册，把新包加入 `arch_test.go` 的 `forbidden`。
6. 运行 `make check`，再实际运行一次，确认弹窗效果。
7. 使用 `handoff` skill 更新文档。
