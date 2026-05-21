# asp-lsp

Classic ASP language server and VS Code extension.

The implementation treats `.asp`, `.asa`, and `.inc` files as mixed documents:

- Classic ASP server script regions: `<% %>`, `<%= %>`, `<%@ %>`, and `<script runat="server">`
- HTML regions delegated to `vscode-html-languageservice`
- CSS regions delegated to `vscode-css-languageservice`
- client JavaScript regions delegated to the TypeScript language service
- VBScript server regions handled by the built-in v1 analyzer

## Commands

```sh
pnpm install
pnpm run typecheck
pnpm run lint
pnpm run test
pnpm run build
```

Run the standalone language server:

```sh
pnpm --filter @asp-lsp/language-server run start -- --stdio
```

Open the VS Code extension from `apps/vscode` during extension development.
