# asp-lsp rust analyzer a group all phases

## Goal
Implement all remaining rust-analyzer style A-group extensions for asp-lsp,
building on the existing Phase 1 introspection diff.

## Success Criteria
- Phase 2: read-only navigation supports `rust-analyzer/matchingBrace`,
  `experimental/parentModule`, and child include navigation.
- Phase 3: editing methods support `experimental/joinLines`,
  `experimental/onEnter`, and `experimental/moveItem` with conservative
  Classic ASP/VBScript edits.
- Phase 4: existing hover/code-action surfaces expose snippet text edit,
  code-action group, and hover action metadata without regressing standard LSP.
- Phase 5: external docs works for built-in ASP/VBScript symbols; SSR has a
  deliberately narrow first version for simple identifier replacement.
- Rust server capabilities advertise implemented custom methods only.
- VS Code client has commands/request plumbing and NLS labels.
- Stdio tests cover every custom method group.

## Current Context
- Repository: `/Users/yottonoko/programs/asp-lsp`.
- Phase 1 diff is already present but not staged or committed.
- Existing workflow artifact:
  `.workflow/asp-lsp-rust-analyzer-phase-1-introspection`.
- Keep all new edits integrated in the current working tree.

## Constraints
- Do not revert existing uncommitted Phase 1 changes.
- Avoid Phase scope creep beyond the A-group request.
- Preserve UTF-16/LSP ranges.
- Do not add regex-only symbol extraction when CST/symbol/folding/include
  helpers can be used.
- Use `pnpm`, not `npm`.

## Risks
- Editing features can damage ASP server regions if ranges are too broad.
- SSR can grow too large; first version must stay narrow.
- rust-analyzer experimental method shapes are not standard LSP, so tests must
  pin asp-lsp's accepted request/response shape.

## Approval Required
- No extra approval for local source edits and tests.
- Approval required before destructive git operations, external publishing, or
  push.

## Work Packets
- P2-nav: matching brace and include parent/children navigation.
- P3-edit: join lines, on enter, move item.
- P4-quality: snippet edit metadata, code action groups, hover actions.
- P5-docs-ssr: external docs and narrow SSR.
- P6-final: integration review, verification, commit if appropriate.

## Integration Policy
- Prefer small helper methods on `Ide`, routed through `handle_request`.
- Client commands must be NLS-backed and tolerate missing active editor/server.
- Reject broad AST matching or formatting rewrites not needed for A-group.

## Verification
- Narrow Rust stdio tests for each method group.
- `cargo test -p asp-lsp-server`.
- `pnpm --filter classic-asp-lsp run typecheck`.
- `pnpm --filter classic-asp-lsp run lint`.
- `git diff --check`.
- Workflow verifier.

## Reusable Artifacts
- This run directory records the all-phase implementation.
