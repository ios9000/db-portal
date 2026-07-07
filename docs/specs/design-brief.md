# Claude Design Brief — "DB Portal" Hybrid Concept Prototype

Paste everything below the line into Claude Design as one prompt.

---

Design a clickable web-app prototype for **"DB Portal"** — an internal self-service portal where database administrators (DBAs) and less-technical application administrators run PostgreSQL maintenance operations (housekeeping, backup, vacuum, reindex, version upgrades) as one-click actions instead of running Ansible playbooks in a terminal. The prototype will be demoed to a client; it must tell one coherent story across all screens using the sample data below. Three personas use the same product: **Jonas** (app admin — wants safe, simple buttons), **Marta** (senior DBA — wants density, bulk actions, raw logs), **Priya** (team lead — wants approvals and audit oversight).

## Design system

- Clean internal-tools aesthetic in the style of Linear/Vercel. Light mode, Inter font, 8px spacing grid, 1200px content max-width, generous white space, subtle borders (#E5E7EB), 8px card radius.
- **Top nav bar (every screen):** left — logo mark + "DB Portal"; center-left — nav items `My Databases` · `Activity` (with a small blue "1" live badge) · `Approvals` · `Schedules`; right — user avatar with name.
- **Environment semantics (use redundant encoding, never color alone):** PROD = solid red badge labeled "PROD" + 3px red left border on its card/row; TEST = amber badge; DEV = gray badge.
- **Run-state colors:** running = blue with subtle pulse animation, succeeded = green ✓, failed = red ✕, succeeded-with-warnings = amber ⚠, waiting approval = purple, queued/skipped = gray.
- Buttons: primary = solid dark, destructive-confirm = red, secondary = outlined. Confirm buttons are always labeled with the consequence ("Run housekeeping on billing-test"), never "OK" or "Submit".

## Sample data (use exactly this on every screen)

- Logged-in user: **Jonas K.**, role App Administrator (screens 1–5); **Priya N.**, Team Lead (screen 7); **Marta S.**, DBA (screen 2).
- Instances: `billing-prod` (PROD, PostgreSQL 16.3, 412 GB, last backup 11h ago ✓, healthy), `billing-test` (TEST, 16.3, 38 GB, last backup 2d ago ⚠, healthy), `shop-prod` (PROD, 16.2, 280 GB, currently running a workflow).
- Operations: 🧹 Housekeeping ("Cleans up dead data and refreshes statistics" · ~15 min · Database stays online), 💾 Backup ("Full backup, verified after completion" · ~25 min · Online), 🔧 Vacuum, 📇 Rebuild indexes ("Brief table locks"), ⬆ Minor upgrade ("~3 min downtime — requires approval on PROD").
- Workflow: **"Monthly Maintenance"** = Backup → Vacuum → Rebuild indexes.
- Live run: **RUN-4821** · Monthly Maintenance · shop-prod · started by Jonas K. at 12:31 · elapsed 23m · "usually ~35m on this instance" · stage 2 of 3 (Backup done: 12m, 4.2 GB; Vacuum running: 64%, table `orders` 14 of 22; Reindex queued).
- Pending approval: Jonas requested **Minor upgrade 16.3 → 16.4 on billing-prod**, reason "CHG-10234 — security patch".

## Screens

**Screen 1 — My Databases (landing, card view — Jonas).** Toolbar under the nav: search input "Search instances…", environment filter pills `All / DEV / TEST / PROD`, and on the far right a segmented `Cards ⇄ Table` view toggle (Cards active). Below, a 3-column grid with exactly the three instance cards from the sample data. Card anatomy: health dot + bold instance name + environment badge; line 2 "PostgreSQL 16.3 · 412 GB"; line 3 "Last backup: 11h ago ✓" (amber ⚠ on billing-test); divider; button row `🧹 Housekeeping` `💾 Backup` `⋯ All operations`. The `shop-prod` card is the running variant: buttons replaced by a slim blue progress bar, "Monthly Maintenance — Stage 2/3 · Vacuum 64%", and a `View progress →` link. billing-prod and shop-prod carry the red PROD left border.

**Screen 2 — Fleet table (same page, Table toggle active — Marta, DBA).** Same toolbar with `Table` selected, plus a "Saved views" dropdown showing "My PROD". Dense table, ~8 rows (invent 5 more instances consistent in style: `crm-prod`, `crm-test`, `wms-prod`, `analytics-dev`, `hr-test`). Columns: ☑ checkbox, status dot, Instance, Env badge, PG version (one row shows a small blue "16.4 available" chip), Last backup, Last vacuum, Bloat %, and a live-run spinner on shop-prod. Three rows are check-selected, raising a sticky bottom action bar: "3 instances selected — `Run operation…` `Schedule…`" with a small strategy note "rolling, 2 at a time". This screen proves the same product serves power users.

**Screen 3 — Launch drawer (480px right slide-over above Screen 1, page dimmed).** Header: 🧹 icon + "Run Housekeeping". Target block restating the instance in large type: "billing-test" with amber TEST badge. Info chip strip: `~15 min` · `Database stays online` · `Last run: 32 days ago`. One-paragraph plain-language description ("Removes dead rows, refreshes planner statistics. Safe to run during business hours."). Collapsed accordion "Advanced parameters (DBA)". Optional field "Reason / ticket #". Footer: secondary `Schedule…`, primary `Run housekeeping on billing-test`.

**Screen 4 — Tier-3 confirmation modal (centered, for the PROD upgrade).** Title "⚠ Run Minor Upgrade on a PRODUCTION database". Summary rows: Operation "Minor upgrade 16.3 → 16.4"; Target "billing-prod" + PROD badge; Impact chip in red "⛔ ~3 min downtime (service restart)"; Duration "~8 min (last run on TEST: 7m 40s)"; Window warning "⚠ Outside maintenance window (Sat 02–06h)". Required "Reason / ticket" field filled with "CHG-10234 — security patch". A "To confirm, type the instance name" input showing partial entry `billing-pro` with helper text "(paste disabled)". Primary button disabled state, labeled `Request approval from DB team` (this op is approval-gated). Cancel as text button.

**Screen 5 — Run detail (RUN-4821, the hero screen).** Header: breadcrumb "← Activity", title "RUN-4821 · Monthly Maintenance · shop-prod [PROD badge]", meta line "Started by Jonas K. · 12:31 · elapsed 23m · usually ~35m on this instance", right-aligned `🔔 Notify me` toggle (on), `⏸ Pause` and `✖ Abort` outlined buttons. Hero: horizontal 3-node pipeline connected by arrows — node 1 green "✓ Backup · 12m · 4.2 GB", node 2 active/selected blue with circular progress "Vacuum · 64% · 11m", node 3 gray "Reindex · queued". Below: stage panel with "Running — processing table `orders` (14 of 22)" and a reassurance line "✓ Your database stays online during this step." Then a collapsible dark monospace log pane (5–6 timestamped Ansible task lines), with search field, `Follow ✓` pill, download icon. Bottom artifact strip: "💾 backup → s3://dbbk/shop-prod/2026-06-10.dump · 4.2 GB · verified". Also produce a **failed variant** of this screen: node 2 red "✕ Vacuum · failed at 11m", node 3 gray "skipped"; above the log a calm failure card: "Step 2: Vacuum did not complete. Your database is online and unchanged. The backup from Step 1 succeeded and is valid. The DB team has been notified — ref RUN-4821." with buttons `Resume from failed step` (primary) and `View technical details`.

**Screen 6 — Activity feed.** Filter chips row (Status, Environment, Operation, User, Date range). Section "Now running" with one live row (RUN-4821, inline blue progress bar). Below, history table: status chip, Run ID, Operation, Instance + env badge, Requester (avatar + name), Started, Duration, `⋯` row menu. Include one green succeeded row, one amber "succeeded with warnings", one red failed, and one purple "Waiting for approval" row (Jonas's upgrade request). `Export` button top right.

**Screen 7 — Approvals inbox (Priya, team lead).** Split view. Left: list of pending requests — one item: "Minor upgrade 16.3 → 16.4 · billing-prod · requested by Jonas K. · 2h ago" with purple dot. Right detail pane: the same summary block as Screen 4 (operation, target with PROD badge, impact, duration, window warning), requester's reason "CHG-10234 — security patch", a context line "Last backup on billing-prod: 11h ago ✓", risk tier label "Tier 3 — requires approval". Buttons: green `Approve & run`, outlined `Decline with comment`. Also produce the empty-state variant: checkmark illustration, "Nothing waiting — all clear."

## Clickable demo flow (wire these interactions)

1. Screen 1: click `🧹 Housekeeping` on billing-test → opens Screen 3 drawer; its primary button → toast "Started — you can close this page. We'll notify you." and the card gains a progress state.
2. Screen 1: click `⋯ All operations` → `Minor upgrade` on billing-prod → opens Screen 4 modal.
3. Screen 1: click `View progress →` on shop-prod → Screen 5.
4. `Cards ⇄ Table` toggle → Screen 2 and back.
5. Nav `Activity` → Screen 6; clicking RUN-4821 row → Screen 5; clicking the purple row → Screen 7 (as Priya).
6. Screen 7: `Approve & run` → the request moves to a green "Approved — queued" state.

## Out of scope (do not design)

No login page, no settings/admin/RBAC management screens, no schedule-creation form (the `Schedules` nav item exists but is not a screen), no mobile layouts, no dark mode variants, no workflow designer/canvas. Seven screens plus the two listed variants (failed run, empty approvals) — nothing more.
