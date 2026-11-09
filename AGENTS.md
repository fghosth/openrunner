# AGENTS.md

## Agent skills

### Issue tracker

Issues live as local markdown files under `.scratch/<feature>/` in this repo. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`), label string equals role name. See `docs/agents/triage-labels.md`.

### 金标准对照

改算法/指标/公式时必须先读 `docs/agents/golden-checks.md`。要点：

- 参照实现只在 `scripts/goldenlib.py`，**一个公式一份**；
- 参照自己要有解析真值自测（`make ref-test`，已进 `make test` 与 CI）；
- dump 测试必须调用生产函数本身，不许重拼算式；
- 与 Go 的有意偏离要量出数字写进文档，不许靠容差或抄袭对齐。

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
