# asp-lsp rust analyzer phase 1 introspection

## Goal
Implement only Phase 1 of the rust-analyzer style A-group LSP extensions for
asp-lsp: low-risk introspection methods and matching VS Code commands.

## Success Criteria
- Rust server handles `rust-analyzer/viewFileText`,
  `rust-analyzer/viewSyntaxTree`, `rust-analyzer/analyzerStatus`,
  `rust-analyzer/memoryUsage`, and `rust-analyzer/openServerLogs` or an
  equivalent execute-command route where appropriate.
- `server_capabilities().experimental` advertises the Phase 1 surface.
- VS Code client registers commands, sends the custom requests, and displays
  read-only virtual documents or the existing output channel.
- User-facing command labels are routed through VS Code NLS files.
- Stdio round-trip tests cover representative responses.
- Phase 2+ features are not implemented.

## Current Context
- Repository: `/Users/yottonoko/programs/asp-lsp`.
- The working tree already had edits in `crates/asp-ide/src/lib.rs` and
  `crates/asp-lsp-server/src/main.rs` adding an initial `analyzerStatus`
  path. Treat them as pre-existing work and integrate without reverting.
- Main implementation surfaces:
  `crates/asp-ide/src/lib.rs`,
  `crates/asp-lsp-server/src/main.rs`,
  `crates/asp-lsp-server/tests/stdio.rs`,
  `apps/vscode/src/extension.ts`,
  `apps/vscode/package.json`,
  `apps/vscode/package.nls.json`,
  `apps/vscode/package.nls.ja.json`.
- Delegated Codex thread: `019e8b6a-fd58-7952-b09a-c7b5f833cdfa`.

## Constraints
- Stop after Phase 1 and wait for user review before Phase 2.
- Do not stage, commit, push, or revert unrelated changes.
- Preserve UTF-16 LSP position/range semantics.
- Use existing parser/CST/IDE helpers; do not introduce regex-only symbol
  extraction.
- Use `pnpm`, not `npm`.

## Risks
- Custom method parameter shapes may drift from rust-analyzer conventions.
- Virtual document UI can become noisy if commands lack clear selection rules.
- Existing user edits in server files must be preserved.

## Approval Required
- No approval required for local non-destructive edits and tests.
- Approval required before destructive git operations, broad codemods,
  external publishing, or Phase 2+ implementation.

## Work Packets
- P1-impl: Implement Phase 1 server/client/test changes in the delegated Codex
  thread. Owner: Codex thread `019e8b6a-fd58-7952-b09a-c7b5f833cdfa`.
- P1-review: Review the delegated diff, resolve integration issues locally, and
  run focused checks. Owner: current orchestrator.

## Integration Policy
- Accept only changes tied to Phase 1 introspection.
- Reject or defer Phase 2+ behavior, unrelated cleanup, and speculative API
  expansion.
- Prefer repo-local style and existing request/command patterns.

## Verification
- Narrow: `cargo test -p asp-lsp-server`.
- Client: run the repository's relevant VS Code/package typecheck if available.
- Workflow artifact: run `verify_workflow.py` before final Phase 1 report.
- Full workspace checks are deferred unless Phase 1 changes prove broad enough
  to require them.

## Reusable Artifacts
- Keep this `.workflow/` directory as the run record.
- Do not create a reusable recipe until at least one full phase completes cleanly.
