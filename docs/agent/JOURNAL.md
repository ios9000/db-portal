# Journal — append-only

One line per event, newest LAST (append at bottom). Never edit old lines.

Format:
`YYYY-MM-DD | session-# | WU | event | note (+evidence for WU completions)`

Events: `start` (session began), `ckpt` (checkpoint), `done` (WU completed — include
verification evidence), `block` (blocked, see STATE.md), `compact` (compaction observed),
`fix` (protocol/docs discrepancy repaired), `meta` (rule/strategy change).

---

2026-07-06 | s01 | — | meta | Repo created; agent-docs package v1 (STRATEGY, PROTOCOL, STATE, JOURNAL, BACKLOG, ROADMAP, DECISIONS, ARCHITECTURE, VISION) authored from research corpus + workshop decisions D1–D7.
2026-07-06 | s01 | WU-000 | ckpt | Audit done: node22/npm10, py3.14.2(+pip/venv), git2.52, gh2.91; docker ABSENT (Desktop uninstalled 05-13), WSL2 distro-less; no just/make/uv. ADR-007 npm-scripts accepted; blocked on ADR-008 container runtime (user).
