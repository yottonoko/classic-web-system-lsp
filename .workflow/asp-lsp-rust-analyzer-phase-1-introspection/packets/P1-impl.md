# Packet P1-impl

## Objective
Implement only Phase 1 introspection extensions for asp-lsp.

## Context
The repository already has a partial `analyzerStatus` implementation in
`crates/asp-ide/src/lib.rs` and `crates/asp-lsp-server/src/main.rs`. Preserve
and integrate it.

## Files / Sources
- `crates/asp-ide/src/lib.rs`
- `crates/asp-lsp-server/src/main.rs`
- `crates/asp-lsp-server/tests/stdio.rs`
- `apps/vscode/src/extension.ts`
- `apps/vscode/package.json`
- `apps/vscode/package.nls.json`
- `apps/vscode/package.nls.ja.json`

## Ownership
Codex thread `019e8b6a-fd58-7952-b09a-c7b5f833cdfa`.

## Do
- Add `rust-analyzer/viewFileText`, `rust-analyzer/viewSyntaxTree`,
  `rust-analyzer/analyzerStatus`, `rust-analyzer/memoryUsage`, and
  `rust-analyzer/openServerLogs` or equivalent execute-command handling where
  appropriate.
- Add VS Code commands and NLS labels.
- Add stdio round-trip tests.
- Run focused verification.

## Do Not
- Do not implement Phase 2+ features.
- Do not stage, commit, push, or revert unrelated changes.
- Do not replace CST-backed behavior with regex-only extraction.

## Expected Output
- Changed files.
- Implemented methods and client commands.
- Verification commands and results.
- Remaining risks.

## Verification
- Prefer `cargo test -p asp-lsp-server`.
- Run a relevant client typecheck if available.
