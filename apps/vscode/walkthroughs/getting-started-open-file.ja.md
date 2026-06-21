# Classic ASP ファイルを開く

Classic ASP LSP は、VS Code で対応ファイルを開くと動きます。

- `.asp`, `.asa`, `.inc` は `classic-asp` language mode です。
- `.vbs` は `vbscript` language mode です。

VS Code が初めての場合は、左の Activity Bar から Explorer を開き、ファイルを選択します。エディター右下の Status Bar に表示される language mode が `classic-asp` または `vbscript` になっているか確認してください。正しい mode になると、diagnostics、completion、hover、definition、references、formatting、CodeLens、inlay hints などが使えます。

`.inc` ファイルは include fragment として扱います。完全な HTML document であるとは仮定しません。
