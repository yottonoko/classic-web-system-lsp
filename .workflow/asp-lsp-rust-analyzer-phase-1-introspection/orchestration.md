# Orchestration: asp-lsp rust analyzer phase 1 introspection

## Execution Rules

- Keep the original objective intact.
- Ask for approval before risky, expensive, external, or destructive actions.
- Keep immediate blocking work local.
- Delegate only bounded, disjoint, materially useful packets.
- Integrate packet results before final verification.
- Do not stage, commit, push, or revert unrelated changes.
- Treat existing edits in `crates/asp-ide/src/lib.rs` and
  `crates/asp-lsp-server/src/main.rs` as pre-existing work.

## Branching Rules
- If the delegated thread implements Phase 2+ features, discard or revert only
  those new delegated changes after inspecting the diff.
- If tests fail because of pre-existing unrelated failures, capture the exact
  failure and run a narrower command that proves the touched behavior.
- If custom request parameter shape is ambiguous, choose the smallest shape that
  matches existing client/server request helpers and document it in the review.

## Packet Prompts
- P1-impl was sent to Codex thread
  `019e8b6a-fd58-7952-b09a-c7b5f833cdfa`.
- Required packet output:
  - changed file list
  - implemented custom methods and client commands
  - validation commands and results
  - remaining risks

## Completion Audit
- Confirm Phase 1-only scope.
- Confirm NLS/localization coverage for client-facing strings.
- Confirm stdio tests cover request round-trips.
- Confirm no git stage/commit/push was performed.
