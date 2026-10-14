# Codex support: revised implementation plan and results

Revised and executed on 2026-10-14. **Passive Codex tracking is implemented in this checkout.** User-level installation and an authenticated Codex session are still separate verification steps. See [PROJECT_GUIDE.md](../PROJECT_GUIDE.md) for the complete architecture and [README.md](../README.md#codex) for setup.

## 1. Revision and scope

The original proposal combined passive tracking, usage enrichment, and managed terminals. Inspection of the installed Codex CLI showed that these need different integration boundaries: version 0.162.1 reports hooks enabled and supports a shared daemon, so a process ID cannot reliably identify one session.

The implemented milestone observes Codex sessions using native hook IDs, keeps Claude behavior compatible, and makes missing telemetry visible. A separate read-only account quota view has now been added. Per-session usage enrichment and managed execution remain independent follow-up milestones below.

Acceptance criteria for this milestone:

- [x] Codex and Claude can coexist, including identical native IDs.
- [x] Prompts, replies, tools, permissions, interruptions, and endings update cards.
- [x] Repeated tool deliveries do not inflate stored activity when stable IDs are supplied.
- [x] Patch calls contribute multiple files to activity, collision detection, and statistics.
- [x] Installer previews changes, preserves unrelated hooks, backs up changed files, and is idempotent.
- [x] Existing databases migrate without losing titles or settings.
- [x] Provider filters and unknown-usage indicators appear in the UI.
- [x] Tests exercise ingestion through persistence, replay, and statistics.
- [x] A native Windows build and generated hook command work against an isolated daemon.
- [x] Observe authenticated local desktop hook delivery and verify stable conversation IDs across messages; full CLI lifecycle validation remains below.
- [ ] Verify desktop and IDE hook behavior separately; do not assume cloud coverage.

## 2. Integration evidence

Local inspection: Codex CLI 0.162.1; hooks feature enabled; shared-daemon and standalone modes; resume/fork commands available. The implementation does not infer minimum-version compatibility from that one installed version.

Codex documents lifecycle hooks, adjacent hooks.json configuration, command-handler trust, shell/patch/MCP tool events, and asynchronous handlers. SessionEnd is synchronous. Stop handlers require JSON output. Transcript formats are unstable. These are the basis for choosing hooks. [Official hooks documentation](https://learn.chatgpt.com/docs/hooks).

App Server exposes thread/turn/item events, token usage, and account-rate-limit interfaces; using those interfaces does not establish that TRK can attach to any existing conversation. [Official App Server documentation](https://learn.chatgpt.com/docs/app-server).

Non-interactive JSONL is another possible adapter for explicitly launched batch runs. [Official non-interactive documentation](https://learn.chatgpt.com/docs/non-interactive-mode).

## 3. Implemented work, in dependency order

### A. Provider identity and persistence

Files: internal/model, internal/store/store.go, internal/fleet/fleet.go.

Added provider and native-session fields to events and sessions. Codex's internal key is codex:<native-id>; existing Claude keys stay unchanged. Explicit Codex progress uses TRK_SESSION, then CODEX_THREAD_ID if available. Native hook identity wins over inherited environment identity.

Codex never participates in Claude's PID map. Directory attribution only selects a unique matching active Codex session. An ambiguous directory creates a fallback card instead of choosing another conversation.

Migration 6 adds provider metadata, usage-presence flags, and a nullable unique event key. Legacy events with hook/status-line sources are classified as Claude; other legacy sources are generic. Session classification uses retained historical evidence. Existing IDs, titles, settings, and Claude account data are preserved.

### B. Codex hook ingestion and normalization

Files: internal/cli/integrations.go, internal/codex/hook.go, internal/fleet/payload.go.

Added trk codex-hook. It reads bounded JSON stdin, requires native identity, sends best-effort telemetry, and does not return a blocking decision. Stop/SubagentStop emit an empty JSON object; other supported hooks emit no stdout.

Registered events: SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, PermissionRequest, Stop, Interrupt, SessionEnd, PreCompact, PostCompact, SubagentStart, SubagentStop. Claude-only events are not registered.

Normalization happens before payload trimming and storage, so replay consumes the same normalized schema. Tool IDs include turn identity. Deduplication hashes session, turn, tool-call ID, and event stage for pre/post deliveries; without stable identifiers, events are not guessed to be duplicates.

Structured exit codes and MCP error flags identify success/failure. Recognized shell transport headers can supply exit status; command output is not searched for error words. Unrecognized results remain unknown. Patch headers supply add/update/delete/move paths, capped at 200 distinct paths.

### C. State and historical calculations

Files: internal/derive, internal/fleet, internal/store/stats.go, internal/stats, internal/daemon.

Added interrupted state and closure of unresolved calls at interruption/end. Timestamp checks cover late starts, late permissions, and prompt/start events older than an ending. A resumed prompt can reactivate the same session. Completed subagents retain the existing counts.

Multi-file patches feed edit counts, churn rules, and cross-provider collisions. Interrupted sessions no longer generate active collision warnings. Identical tool IDs in different sessions do not collide in statistics.

Token-only status records are accepted without requiring a dollar cost. Cost, token, and model-time presence are tracked independently. Codex hook payloads do not overwrite Claude quotas or manufacture usage counters. Stats supports a provider query and includes that filter in its cache key.

### D. Safe installation and explicit progress

Files: internal/codexcfg, internal/cli/init.go, internal/cli/report.go.

Commands:

~~~sh
trk init --agent codex --dry-run
trk init --agent codex
trk init --agent codex --agents-md /chosen/path/AGENTS.md

trk codex start "Task" --steps 3
trk codex step "Implementing"
trk codex progress 2/3
trk codex blocked "Need a decision"
trk codex done "Completed"
~~~

The default destination is CODEX_HOME/hooks.json, falling back to ~/.codex/hooks.json. The installer does not edit config.toml or trust records. AGENTS.md changes require an explicit path.

It validates all requested edits before writing, preserves unrelated JSON fields/handlers and existing instructions, updates recognized TRK commands, creates exclusive backups, and uses atomic replacement per file. This is not a transaction across multiple files; backups support recovery after a later write failure. Dry runs do not write.

On Windows, the generated PowerShell command uses an encoded script to quote executable paths, forward UTF-8 stdin, and suppress startup progress. On Unix, it uses the quoted executable directly. Hook entries use a short timeout; SessionEnd is synchronous. After installation, review/trust the entries in Codex's /hooks UI.

### E. Dashboard behavior

Files: web/app.js, web/agents.js, web/index.html, web/stats.js, web/stats.html.

Cards identify their provider. The Fleet provider control filters cards; fleet-wide header and timeline summaries remain global. Stats filters historical results by provider. Codex drawers describe external ownership and offer a validated resume command to copy, without Claude terminal controls.

Live desktop evidence showed that automatic next-message suggestions run in separate forks with their own IDs. The adapter recognizes their exact app-generated prompt preamble and excludes them from fleet summaries, directory attribution, and statistics without deleting events. Blank fork cards wait for a prompt/tool before appearing. Real user forks remain distinct. This is a narrow compatibility heuristic: the observed hooks lack a structured purpose field, so changed app preambles may require an adapter update. Regression coverage includes existing summaries, restart replay, late prompt delivery, and multiple messages in one real session.

Missing cost/tokens appear as unavailable. Mixed totals are labeled partial, and model-time splits identify incomplete measurement. Unknown tool results are distinct from successful results. The Limits page stacks Codex above Claude, with both account sections visible together.

### F. Codex account quota snapshots (implemented follow-up)

The signed-in local Codex App Server successfully returned account windows through `account/rateLimits/read`. TRK now makes this read over an owned, bounded stdio connection without starting threads or turns. Requests from Limits viewers share a one-minute cache, with failed reads eligible for retry after 15 seconds. Browser disconnects do not cancel the shared bounded read. The UI displays all returned buckets, their real window durations, usage percentages, and reset timestamps; absent secondary windows stay unavailable. Stale readings are labeled after a failed refresh. No quota-reset or authentication-mutation methods are sent. Windows helpers are hidden; `TRK_CODEX_COMMAND` supports an explicit native executable path. Quota snapshots do not change Claude account data or create session cards.

Tests cover handshake ordering, legacy/multi-bucket response parsing, unknown versus zero values, failure redaction, timeout/child-process cleanup, concurrent cache requests, stale retention, and provider separation. A live read verified the supported interface. Historical Codex quota charts and per-session token/cost ingestion remain follow-up work.

## 4. Validation record

Executed locally:

| Check | Result |
|---|---|
| Go test suite | Passed, including existing Claude coverage and new Codex tests. |
| go vet ./... | Passed. |
| Windows binary build | Built bin/trk.exe. |
| Linux cross-build | Compiled successfully from Windows; not executed on Linux. |
| JavaScript syntax checks | Passed for changed frontend scripts. |
| Generated Windows hook smoke | Passed against an isolated localhost daemon, using config/data under ignored bin/. |
| Unicode round trip | Prompt/reply preserved through encoded PowerShell, HTTP, SQLite, and session response. |
| Installer repeat | Second installation leaves generated hooks unchanged. |
| Migration/replay | Populated legacy DB retains title/settings; mixed fleet survives replay. |
| Deduplication/outcomes | Repeated stable tool stages suppressed; failed and unknown results distinguished. |

The fixture in internal/codex/testdata is synthetic and contains no private conversation data. It covers lifecycle, failure, patch files, compaction, subagents, resume, and interruption. Its README records that it is not a captured authenticated session.

Native race detection was attempted with CGO enabled and could not build because gcc is absent from PATH. Unix PTY behavior and tagged Unix E2E tests are not validated by the Windows build. No user Codex configuration was changed to run these tests.

## 5. Remaining validation and limitations

1. With a freshly built binary, preview installation, install, restart Codex, and review/trust hooks. Exercise a prompt, a successful/failed tool, a multi-file patch, permission approval, interruption, resume, and ending. Compare observed payloads to the synthetic fixture and add sanitized differences.
2. Repeat in desktop and IDE environments that support local command hooks. Record the tested client versions. Cloud execution is not included in the compatibility claim.
3. Run the existing CI matrix, including race detection and Unix E2E/PTY coverage.
4. Telemetry remains best effort: disabled/untrusted hooks, timeout, daemon downtime, or abrupt process exit can lose observations. Async delivery timestamps reflect TRK receipt; timestamp safeguards do not promise total ordering across independently launched hook processes.
5. Shell activity does not imply arbitrary filesystem reads/edits. Only recognized patch/tool metadata contributes file paths.
6. Directory fallback is weaker than native identity. Supply TRK_SESSION for explicit progress in ambiguous workspaces.
7. Hook payloads may contain prompts, commands, and replies. Existing local retention and trimming apply; this integration does not add transcript ingestion or secret redaction.

## 6. Follow-up milestones

### Per-session usage enrichment and quota history

Choose a supported, explicitly connected App Server or batch source. Define ownership, reconnect/replay, usage identity, and cumulative-vs-delta semantics before implementation. Extend provider-specific account storage and per-metric freshness. Preserve unknown values when data is missing. Validate partial-counter baselines across time windows and restart recovery. Do not estimate billed spend from token counts without a separately specified pricing model.

Exit criteria: supported source, tested event fixtures, no double counting, no cross-provider quota contamination, and correct disconnected/unknown UI.

### Managed Codex execution (implemented follow-up)

Design a provider runner behind the terminal manager with argument arrays and explicit owned-session identity. Preserve shared-daemon ownership boundaries: stopping a TRK-owned client must not kill unrelated sessions. Validate launch/resume/fork, duplicate ownership, cancellation, and shutdown before enabling controls.

Implemented provider selection, standalone Codex launching, namespaced resume/fork arguments, hook-based root-terminal association, and Windows ConPTY. External Codex sessions remain observed and expose a copyable resume command. Native Windows tests cover terminal input/output, Unicode argv, resize, exit/stop, WebSocket attachment, root identity, and duplicate-resume rejection. Unix execution still needs platform CI coverage.

Exit criteria: ownership/reconnect tests, platform smoke tests, explicit unsupported states, and no Claude regressions.
