---
name: handoff
description: 结束一次工作、提交前或用户说收尾时使用：更新项目状态和工作日志，让下一个会话能直接接手。
---

# 收尾交接

目标：另一位开发者或一个全新的 Agent 只读 `docs/STATUS.md` 就能接手。

1. 运行 `make check`，记录结果。界面改动要实际运行；没运行就写明。
2. 更新 `docs/STATUS.md`：
   - 最后更新日期、阶段、一句话现状
   - 已进入 Linear 的想法从"尚待讨论的想法"中删除
   - 更新平台状态表，只写实际验证过的
   - 更新已知问题
   - 删除过时内容，历史放进 journal
3. 如果任务对应 Linear issue，切换状态并写明验证结果和遗留。新发现的问题告诉用户，讨论后再进入 Linear。无法访问时告诉用户。
4. 向 `docs/journal/YYYY-MM.md` 追加一条：结果、验证、遗留。
5. 检查是否还需要更新：决策记录、`ARCHITECTURE.md`、`CORE.md`、`EXTENDING.md`、`research/`。
6. 向用户说明改了什么、验证了什么、还有什么没做。用户未要求时不要提交。
