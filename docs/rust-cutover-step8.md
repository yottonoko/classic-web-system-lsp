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

## Release Matrix

The release workflow currently builds one native VSIX asset:

- `win32-x64`

Linux and macOS native VSIX assets are intentionally out of the release matrix
for now. Local package smoke still covers both the native VSIX command and the
no-native VSIX command, but tag release upload is limited to Windows x64.

If Linux or macOS release assets are restored later, use a runner that matches
the target architecture. `scripts/build-server.mjs --target` only changes the
copied destination name; it does not cross-compile the Rust executable.
