# Understand Inlay Hints and CodeLens

Inlay hints are small inline labels shown inside code. They are not part of the file text. Classic ASP LSP can show labels such as parameter names, inferred variable types, function return types, implicit `ByRef`, and scope markers. Use them when old VBScript code is hard to read because calls omit parameter names or variables are declared far from their use.

CodeLens items are clickable links shown above code. Classic ASP LSP uses CodeLens to show reference counts for procedures, classes, members, and include relationships. Click a CodeLens item to open the reference list.

You can adjust these features in Settings:

- `aspLsp.inlayHints.parameterNames`
- `aspLsp.inlayHints.variableTypes`
- `aspLsp.inlayHints.functionReturnTypes`
- `aspLsp.inlayHints.implicitByRef`
- `aspLsp.inlayHints.scopeMarkers.*`
- `aspLsp.codeLens.references`
- `aspLsp.codeLens.includes`

In VS Code, open Settings from the gear icon or the Command Palette, then search for `aspLsp inlay hints` or `aspLsp CodeLens`.
