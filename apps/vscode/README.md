# Classic ASP LSP

VS Code extension package for the Classic ASP language server.

It registers `.asp`, `.asa`, and `.inc` files with the `classic-asp` language id, `.vbs` files with the `vbscript` language id, and starts the Go `asp-lsp-go` stdio server.

Standalone `.vbs` files are included by the default `aspLsp.workspace.includes` glob. The `aspLsp.defaultLanguage` setting controls server-side script blocks in Classic ASP documents; it does not change the language mode of a `.vbs` file.

For local Extension Development Hosts, build the server first with `go build -o bin/asp-lsp-go ./cmd/asp-lsp-go` from the repository root. If the server binary is missing, the extension reports its expected path and the build command in the VS Code error notification.

## License

Classic ASP LSP is dual-licensed under either the MIT License or the Apache License, Version 2.0, at your option.

The VSIX includes `LICENSE.txt`, `LICENSE-MIT`, and `LICENSE-APACHE`.
Third-party license texts and notices are listed in
`third_party_licenses/INDEX.md` inside the VSIX.
