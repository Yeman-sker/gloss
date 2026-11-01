# Agent 共享配置

`skills/` 是所有 Agent 共用的工作流，是唯一的正文来源。`.claude/skills/` 只是指向这里的入口，不用符号链接是为了兼容 Windows。

新增 skill 时：在这里写正文，再在 `.claude/skills/<名称>/SKILL.md` 添加入口。
