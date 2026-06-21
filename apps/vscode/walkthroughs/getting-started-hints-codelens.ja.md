# Inlay Hints と CodeLens を知る

Inlay Hints（インレイヒント）は、コードの中に表示される小さな補助 label です。ファイル本文に書き込まれる文字ではありません。Classic ASP LSP では、parameter name、推論した variable type、function return type、暗黙の `ByRef`、scope marker などを表示できます。古い VBScript で、call の parameter 名が省略されていたり、variable 宣言と使用箇所が離れていたりするときに読みやすくなります。

CodeLens（コードレンズ）は、procedure や class の上に出る clickable link です。Classic ASP LSP では、function、sub、class、member、include relationship の reference count などを表示します。CodeLens をクリックすると reference list を開けます。

これらは Settings で調整できます。

- `aspLsp.inlayHints.parameterNames`
- `aspLsp.inlayHints.variableTypes`
- `aspLsp.inlayHints.functionReturnTypes`
- `aspLsp.inlayHints.implicitByRef`
- `aspLsp.inlayHints.scopeMarkers.*`
- `aspLsp.codeLens.references`
- `aspLsp.codeLens.includes`

VS Code では、左下の gear icon または Command Palette から Settings を開き、`aspLsp inlay hints` や `aspLsp CodeLens` を検索してください。
