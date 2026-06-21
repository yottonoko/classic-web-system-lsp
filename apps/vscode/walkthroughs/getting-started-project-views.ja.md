# プロジェクト表示を使う

Classic ASP project は include file や共有 VBScript routine に依存していることが多いです。Classic ASP LSP には、すべての file を手で追わなくても構造を確認できる view があります。

- Project glob files: workspace analysis に含める file を preview し、include / exclude glob を調整できます。
- Current file graph: 現在の file と include、declaration、reference、assignment、call のつながりを表示します。
- Folder graph / workspace graph: include が多い project で、より広い範囲を確認します。
- Current file flowchart: active な ASP file の control flow を読みます。
- Excel export: 共有しやすい workbook として現在の file analysis を出力します。

これらは Command Palette で `Classic ASP` と検索して開けます。`.asp`, `.asa`, `.inc` file では、editor title や Explorer の右クリック menu に出る command もあります。
