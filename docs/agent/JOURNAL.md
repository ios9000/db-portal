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
2026-07-06 | s01 | WU-000 | ckpt | ADR-008 accepted (WSL2+docker-ce). Provisioning blocked by disk incident: C: 99% full -> WSL2 E_FAIL (WSL1 boots OK, image fine). P: (local NTFS, 17GB free) is relocation target; 1.6GB orphaned Docker Desktop data found. Deleted own 369MB image. User decisions pending.
2026-07-06 | s01 | WU-000 | block | WSL2 on P: boots (disk was root cause) but NAT is dead (gateway unreachable; stale HNS/WinNAT suspected); elevated repair abandoned mid-flight. USER PIVOT: development moves to a dedicated cloud Linux VM. Local WSL track paused, superseded by ADR-009 (pending).
2026-07-06 | s01 | WU-000 | ckpt | ADR-009 accepted: dev moves to dedicated cloud VM (Ubuntu 24.04, 4vCPU/16GB/100GB). ADR-008 superseded. STATE rewritten as VM-first; WU-000 re-audit queued for first VM session. Repo pushed to GitHub.
2026-07-06 | s01 | WU-000 | ckpt | ADR-001 accepted by user (FastAPI + React/TS). VM 80.85.254.99 reachable; wdsvc key valid client-side (RSA-2048, no passphrase) but rejected for 5 common usernames -> blocked on username/authorized_keys from user.
