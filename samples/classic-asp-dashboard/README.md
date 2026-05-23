# Classic ASP Dashboard Sample

This sample is a static Classic ASP dashboard for checking editor features in
`asp-lsp`.

It intentionally mixes:

- ASP directives, include directives, expression blocks, and statement blocks
- VBScript classes, functions, arrays, dictionaries, `For Each`, `With`, and
  `Select Case`
- HTML regions, inline CSS, inline JavaScript, and `style` attributes
- Form and query-string access through `Request`
- Output encoding through `Server.HTMLEncode`
- `.inc` fragments for include resolution

Pages:

- `default.asp`: mixed dashboard with table, metrics, detail panel, CSS, and
  client JavaScript
- `customers.asp`: card-based customer browser with query-string filters
- `reports.asp`: report table with a server-side JScript block
- `settings.asp`: form post handling and `Session` access

Open the `samples/classic-asp-dashboard` directory in VS Code, then inspect the
`.asp` and `.inc` files for diagnostics, hover, completion, folding, document
symbols, formatting, include resolution, and cross-file navigation behavior.
