# Classic ASP LSP

VS Code extension package for the Classic ASP language server.

It registers `.asp`, `.asa`, and `.inc` files with the `classic-asp` language id, `.vbs` files with the `vbscript` language id, and starts the Go `asp-lsp-go` stdio server.

Standalone `.vbs` files are included by the default `aspLsp.workspace.includes` glob. The `aspLsp.defaultLanguage` setting controls server-side script blocks in Classic ASP documents; it does not change the language mode of a `.vbs` file.

For local Extension Development Hosts, build the server first with `go build -o bin/asp-lsp-go ./cmd/asp-lsp-go` from the repository root. If the server binary is missing, the extension reports its expected path and the build command in the VS Code error notification.

## Features

- Completion, hover, signature help, go to definition, references, rename, and document highlights for VBScript and JScript server code, including symbols declared in `#include` files.
- HTML, CSS, inline `style=""`, and client JavaScript support inside Classic ASP documents through the bundled Go ports of the HTML, CSS, and TypeScript language services.
- Diagnostics for server script, embedded languages, and broken includes, with quick fixes and XML documentation comment generation.
- Formatting for whole documents, ranges, and on type. Classic ASP server regions are preserved while the surrounding HTML, CSS, and JavaScript are formatted.
- Semantic highlighting, folding, document and workspace symbols, selection ranges, inlay hints, CodeLens, call hierarchy, and linked tag editing.
- Project views: navigation graphs for a file, folder, or workspace, per-file flowcharts with Mermaid export, Excel analysis export, and debug log analysis.

## Commands

| Command                                                            | Purpose                                              |
| ------------------------------------------------------------------ | ---------------------------------------------------- |
| Classic ASP: Restart Language Server                               | Restart the server process.                          |
| Classic ASP: Reindex Workspace                                     | Rescan the workspace files.                          |
| Classic ASP: Clear Process Cache and Analysis Database             | Drop in-memory and on-disk analysis results.         |
| Classic ASP: Open Output                                           | Show the server log.                                 |
| Classic ASP: Show Progress Details                                 | List running analysis tasks and cancel them.         |
| Classic ASP: Show Current File / Folder / Project Navigation Graph | Open the page navigation graph.                      |
| Classic ASP: Show Current File Flowchart                           | Open the flowchart for the active file.              |
| Classic ASP: Export Current File Analysis to Excel                 | Write the analysis of the active file to a workbook. |
| Classic ASP: Analyze Debug Log                                     | Inspect a server debug log.                          |

## Troubleshooting

- The status bar item shows loading and analysis progress. Select it to see the running tasks.
- If the server stops after repeated crashes, the notification offers **Restart Server** and **Open Output**. The output channel contains the failure that ended the process.
- A failure inside the embedded HTML or CSS services is logged as a warning and skipped for that request; the server keeps running.
- Formatting settings are bounded: indent sizes up to 32, wrap lengths up to 10000, and `maxPreserveNewLines` up to 100. Out-of-range values are clamped.

## License

Classic ASP LSP is dual-licensed under either the MIT License or the Apache License, Version 2.0, at your option.

The VSIX includes `LICENSE.txt`, `LICENSE-MIT`, and `LICENSE-APACHE`.
Third-party license texts and notices are listed in
`third_party_licenses/INDEX.md` inside the VSIX.
