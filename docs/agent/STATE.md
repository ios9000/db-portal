# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-001R — Backend rework Python → Go (ADR-010; user vetoed Python for
  single-binary deployment). WU-002 queues after it.
- **Status:** Go 1.26.4 + golangci-lint 2.12.2 installed on VM; docs re-architected;
  scaffold swap in progress. WU-001 (Python scaffold) was done at bbae15c — its gate
  rigor carries over, its Python content gets removed.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main (docs only, no code yet)

## Next action (be exact)

Finish WU-001R per its BACKLOG entry, ON THE VM: remove Python backend files; write Go
scaffold (go.mod toolchain-pinned, cmd/portal, internal/version + test, .golangci.yml v2);
rewire root `check:be`/`fmt:be`, pre-commit hook Go branch, CI (setup-go + golangci-lint,
drop uv); prove hook rejection with a bad .go; `npm run check` green; push; CI green;
flip WU-001R → done; STATE → WU-002.

## Blocked / needs user

- (nothing) — O-1 (dump artifact storage): only matters at WU-012; mock OK there,
  minio WU-035.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- VM toolchain (audited): Docker 29.6.1, Compose v5.3.0, node 22.23.1, Python 3.12.3,
  uv 0.11.27, git 2.43. Passwordless sudo; docker group active after next login.
- GitHub: private repo `ios9000/db-portal`; VM pushes via write-enabled deploy key
  (`~/.ssh/dbportal_deploy`, Host github.com stanza in VM ssh config).
- Research corpus stays on the workstation (`P:/Projects/db-portal-research/`),
  reference-only (ADR-006); key extracts already in docs/. Need a research file on the
  VM? Ask the user to copy it over.
- Windows-era forensics (WSL E_FAIL, dead NAT, disk incident): JOURNAL 2026-07-06 +
  ADR-008. Do not resurrect the WSL track.

## Checkpoint log (last 3, newest first)

- 2026-07-06 — WU-000 done (VM audit evidence in BACKLOG/JOURNAL); active → WU-001.
- 2026-07-06 — VM bootstrapped (docker/node/uv), deploy key added, repo cloned on VM.
- 2026-07-06 — Pivot to cloud VM (ADR-009); repo pushed to GitHub.
