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
2026-07-06 | s01 | WU-000 | done | VM primary env live: wdsvc55@80.85.254.99 Ubuntu 24.04 4vCPU/15GB/96GB. Evidence: git 2.43.0, Docker 29.6.1, Compose v5.3.0 (hello-world OK), node v22.23.1, npm 10.9.8, Python 3.12.3, uv 0.11.27. Deploy key (write) registered; clone at ~/db-portal via ssh.github.com:443 (port 22 egress blocked). ADR-001+007+009 all accepted. Active -> WU-001.
2026-07-06 | s01 | — | block | VM #1 (80.85.254.99) decommissioned by user post-setup; its write deploy key REVOKED on GitHub. Getting-ready process codified as infra/bootstrap-vm.sh (idempotent). Awaiting new VM details (IP/user/key).
2026-07-06 | s01 | — | ckpt | VM #2 ready: root@80.209.240.36 "206610", Ubuntu 24.04.4, 8vCPU/31GB/387GB. bootstrap-vm.sh reran cleanly (identical toolchain: Docker 29.6.1, Compose v5.3.0, node 22.23.1, Py 3.12.3, uv 0.11.27). Deploy key registered, clone at /root/db-portal, Claude Code 2.1.202. No blockers; WU-001 ready.
2026-07-06 | s01 | WU-001 | done | Scaffold landed (bbae15c). Evidence: local `npm run check` EXIT:0; GitHub Actions run SUCCESS on clean runner; pre-commit blocked bad .py then reverted. Delta: Vite 2026 template uses oxlint not eslint (hook+docs adapted). Active -> WU-002.
2026-07-06 | s01 | WU-001R | done | Backend now Go 1.26.4 (4122ac7). Evidence: npm run check CHECK-EXIT:0 (golangci-lint 2.12.2 clean, fmt-diff clean, go test -race pass); CI SUCCESS on clean runner; hook blocked bad .go; 0 *.py remain. gcc added to bootstrap (race needs cgo). Active -> WU-002.
2026-07-06 | s01 | WU-002 | done | Compose dev env (44325db). Evidence: up --wait -> postgres+mailpit (healthy); db-reset idempotent x2; TCP psql creds-ok; mailpit HTTP 200. Active -> WU-003 (Go chassis).
2026-07-06 | s01 | — | meta | SESSION END. s01 delivered: docs package v1, WU-000/001/001R/002 (4 WUs done, all with CI/runtime evidence), ADR-001..010, 3 env migrations (WSL dead-end -> VM#1 -> VM#2), bootstrap-vm.sh, stack pivot Python->Go. Compactions: 0. Next session: WU-003 cold-start — MEASURE time-to-first-productive-edit vs the 5-min target (STRATEGY §8).
2026-07-07 | s02 | WU-003 | done | Go chassis (aee80cc). Evidence: npm run check green (lint 0 issues, go test -race pass; DB-backed migrate round-trip ran on compose PG); curl /healthz -> 200 {"db":"ok","status":"ok"}; portal migrate status/up/down/up clean; SIGTERM -> graceful exit 0. Cold-start note: STATE->first code edit ~4 min (5-min target met). Active -> WU-004 (frontend shell; needs design-brief extract on VM, see STATE).
2026-07-07 | s02 | — | fix | Cold-start measured precisely by user: 3m42s STATE->first productive edit (beats 5-min target; journal line above said ~4 min). STRATEGY §8 metric holds.
2026-07-07 | s02 | WU-004 | done | Frontend shell (2304b9c). Evidence: npm run check green (oxlint, tsc strict, vitest 18/18); npm run build clean; live check Vite:5173 served DB Portal shell + proxied /healthz -> Go:8080 -> 200 {"db":"ok","status":"ok"}. EnvBadge 3/3 + RunStatus 5/5 variants tested; redundant encoding (label+color) throughout. Caveat: token hexes are placeholders — brief extract not on VM (icebox: reconcile; gh CLI missing on VM too). Active -> WU-005 (ExecutionAdapter + MockEngine).
