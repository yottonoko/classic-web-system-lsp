# Rust Cutover Step 8 Evidence

Step 8 cutover/package audit notes for `codex/revolution`.

## Scope

This checkpoint covers VS Code client launch-path cutover, VSIX layout smoke
checks, no-native packaging behavior, and legacy TypeScript fallback cleanup.

## Launch Path

The VS Code client now resolves the Rust server in this order:

1. `aspLsp.server.path` external binary setting
2. bundled VSIX binary at `server/bin/<platform>-<arch>/asp-lsp-server`
3. development binary at `../../target/release/asp-lsp-server`

This keeps platform VSIX installs on the bundled Rust stdio server while making
the no-native VSIX usable when an external `asp-lsp-server` binary is configured.

## Package Smoke

Native VSIX command:

```sh
pnpm run package:vsix --out /private/tmp/classic-asp-lsp-native-step8.vsix
```

Native VSIX layout check:

```sh
unzip -l /private/tmp/classic-asp-lsp-native-step8.vsix \
  | rg "extension/server/bin/.*/asp-lsp-server|extension/server/sidecar/(dist/sidecar.js|package.json)"
```

Result:

- `extension/server/bin/darwin-arm64/asp-lsp-server` present
- `extension/server/sidecar/dist/sidecar.js` present
- `extension/server/sidecar/package.json` present

No-native VSIX command:

```sh
pnpm run package:vsix:no-native --out /private/tmp/classic-asp-lsp-no-native-step8.vsix
```

No-native VSIX layout checks:

```sh
unzip -l /private/tmp/classic-asp-lsp-no-native-step8.vsix \
  | rg "extension/server/sidecar/(dist/sidecar.js|package.json)"

unzip -l /private/tmp/classic-asp-lsp-no-native-step8.vsix \
  | rg "extension/server/bin/.*/asp-lsp-server"
```

Result:

- `extension/server/sidecar/dist/sidecar.js` present
- `extension/server/sidecar/package.json` present
- `extension/server/bin/.*/asp-lsp-server` absent
- `extension/package.json` contributes `aspLsp.server.path`
- `extension/dist/server-path.js` contains the configured-path error branch

## Legacy Cleanup

- VS Code backend status handling now accepts only Rust backend status.
- The `typescript-fallback` status label and `status.backend.typescript`
  localizer key were removed from the extension source.
- The stale `.vscodeignore` entry for `server/language-server/node_modules/**`
  was removed.

## Remaining Cutover Risk

The release workflow currently builds native VSIX assets for:

- `linux-x64`
- `darwin-arm64`
- `win32-x64`

The client computes its bundled server directory from the runtime
`process.platform` and `process.arch`, so `darwin-x64` is not covered by the
current release matrix. Adding that target needs a build-server change that
actually cross-compiles or runs on an x64 macOS runner; the current
`scripts/build-server.mjs --target` path only changes the copied destination
name.
