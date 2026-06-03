# Orchestration: asp-lsp rust analyzer a group all phases

## Execution Rules

- Keep the original objective intact.
- Ask for approval before risky, expensive, external, or destructive actions.
- Keep immediate blocking work local.
- Delegate only bounded, disjoint, materially useful packets.
- Integrate packet results before final verification.
- Keep Phase 1 diff intact and build on it.
- Stop and narrow scope if SSR or editing behavior begins to require broad parser
  infrastructure.

## Branching Rules
- If a feature cannot be implemented safely, add the smallest useful server
  response shape and a test that documents the limitation.
- If client/server method naming differs, prefer the rust-analyzer method name
  on the wire and `aspLsp.*` command names in VS Code.
- If a test reveals formatting or offset risk, reduce edit scope instead of
  adding broader recovery logic.

## Packet Prompts
- P2-nav: implement read-only matching brace and include parent/children.
- P3-edit: implement conservative edit-generating methods.
- P4-quality: add metadata enhancements to existing hover/code-action surfaces.
- P5-docs-ssr: add external docs and narrow SSR.

## Completion Audit
- Confirm all requested A-group bullets are represented.
- Confirm no Phase 2+ accidental omissions remain.
- Confirm no unrelated files are staged.
