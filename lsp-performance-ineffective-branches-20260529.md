# 2026-05-29 LSP 高速化で効果がなかった実装まとめ

## 対象範囲

この資料は、2026-05-29 の LSP 高速化ループで試し、ベンチマーク上の有効な改善が確認できなかったため `main` に merge しなかった branch をまとめたもの。

- 資料の作成言語: 日本語 Markdown
- 対象実装の作成言語: TypeScript
- 対象 branch: `origin/main` に merge されていない `origin/codex/perf-*20260529*`
- 対象外: Rust 実験 branch、空で終わった branch、local-only branch
- 判定方針: 関連 benchmark が改善したものだけ `main` に merge し、改善しなかったものは branch を残して `main` に戻した

## 全体傾向

効果がなかった理由は主に次の 3 種類。

1. V8 の `sort`、`map`、`filter`、`slice` などの組み込み処理が十分速く、手書き loop や merge 処理が負けた。
2. cache や fast path のために追加した分岐、lookup、allocation のコストが、削減できた処理より大きかった。
3. benchmark fixture では最適化対象の条件があまり発生せず、狙った処理削減より常時通る判定コストが目立った。

## Branch 別まとめ

| Branch | やったこと | 効果がなかった理由 | 作成言語 |
| --- | --- | --- | --- |
| `origin/codex/perf-asp-close-boundary-skip-20260529-0539` | ASP region scan の `%>` 探索を現在 boundary 内に絞った。 | `indexOf` 呼び出しと state 判定の追加コストが勝ち、huge parse が遅くなった。 | TypeScript |
| `origin/codex/perf-asp-close-fast-path-20260529-0413` | 単純な ASP close-region scan 用の fast path を追加した。 | 追加分岐が既存 scanner path に勝てず、parse time が改善しなかった。 | TypeScript |
| `origin/codex/perf-asp-close-state-starter-20260529-0442` | scanner state starter が出るまで ASP close scan を短絡した。 | starter 探索のコストが増え、scan 削減分を上回った。 | TypeScript |
| `origin/codex/perf-asp-scanner-charcode-20260529-0405` | ASP scanner の文字比較を `charCodeAt` に置き換えた。 | この workload では既存の string 比較より速くならなかった。 | TypeScript |
| `origin/codex/perf-find-tag-end-charcode-20260529-0449` | tag-end scan を `charCodeAt` ベースにした。 | 支配的な hot path ではなく、end-to-end benchmark の改善につながらなかった。 | TypeScript |
| `origin/codex/perf-html-scan-jump-20260529-0549` | HTML text scan で次の `<` まで直接 jump するようにした。 | `indexOf` による jump 戦略が benchmark fixture では parse cost を増やした。 | TypeScript |
| `origin/codex/perf-build-regions-merge-20260529-0554` | region sort を手書き merge に置き換えた。 | 実データの region 配列では native `sort` のほうが速かった。 | TypeScript |
| `origin/codex/perf-cst-node-merge-20260529-0613` | CST node sort を手書き merge に置き換えた。 | custom merge が V8 の native sort に勝てず、parse time が悪化した。 | TypeScript |
| `origin/codex/perf-directive-one-pass-20260529-0611` | ASP directive 抽出を 1 pass の手書き loop にした。 | 既存の配列処理 path より手書き loop が遅く、huge parse が悪化した。 | TypeScript |
| `origin/codex/perf-lazy-source-map-20260529-0129` | source-map lookup の sort を必要になるまで遅延した。 | lazy state 管理のコストが、回避できた sort 処理を上回った。 | TypeScript |
| `origin/codex/perf-mask-no-newline-fast-path-20260529-0419` | 改行を含まない line mask 用の fast path を追加した。 | 分岐追加に見合う virtual document 処理削減が出なかった。 | TypeScript |
| `origin/codex/perf-diagnostics-virtual-batch-20260529-0151` | diagnostics 用 virtual document を batch 生成した。 | 既存の lazy path の利点が消え、悪化またはほぼ横ばいになった。 | TypeScript |
| `origin/codex/perf-cache-lower-token-20260529-0522` | token の lowercase text を cache した。 | WeakMap/cache lookup の overhead が lowercase 省略分より大きかった。 | TypeScript |
| `origin/codex/perf-vb-keyword-probe-20260529-0552` | VBScript keyword 候補だけ lowercase する probe を入れた。 | 候補判定の分岐が増え、tokenizer/parse time が改善しなかった。 | TypeScript |
| `origin/codex/perf-vb-statement-end-reuse-20260529-0543` | VBScript statement-end 判定結果を再利用した。 | helper 呼び出しと引数 plumbing の増加が、再利用による削減を上回った。 | TypeScript |
| `origin/codex/perf-vb-string-unquote-fast-path-20260529-0547` | VBScript string unquote に fast path を追加した。 | 一部の狭い case は改善したが、large/parse benchmark 全体では悪化した。 | TypeScript |
| `origin/codex/perf-vb-stack-top-index-20260529-0622` | `stack.at(-1)` を直接 index access に置き換えた。 | 変更が小さすぎて benchmark noise を超えず、測定上はやや悪化した。 | TypeScript |
| `origin/codex/perf-parent-class-loop-20260529-0615` | parent class lookup の reverse array 作成を loop に置き換えた。 | allocation 削減が効かず、測定 path は遅くなった。 | TypeScript |
| `origin/codex/perf-doc-comment-probe-20260529-0326` | doc comment lookup を binary search にした。 | 既存の forward cursor のほうが実データの並びに合っていた。 | TypeScript |
| `origin/codex/perf-doc-comment-empty-20260529-0619` | 空の doc-comment token collection を共有した。 | ほとんど allocation が減らず、分岐 overhead だけが増えた。 | TypeScript |
| `origin/codex/perf-type-annotation-comment-scan-20260529-0253` | type-annotation comment scan の allocation を減らした。 | type annotation scan は測定 workload の支配的コストではなかった。 | TypeScript |
| `origin/codex/perf-type-annotation-loop-20260529-0517` | annotation scan を手書き loop にした。 | benchmark では既存実装に負けた。 | TypeScript |
| `origin/codex/perf-cache-type-annotations-20260529-0238` | parsed type annotation を cache した。 | cache invalidation と lookup のコストが、再 parse 削減分より大きかった。 | TypeScript |
| `origin/codex/perf-option-explicit-snapshot-20260529-0242` | `Option Explicit` 判定を snapshot text から行うようにした。 | 元の判定がすでに軽く、snapshot path は benchmark を動かさなかった。 | TypeScript |
| `origin/codex/perf-symbol-snapshot-walk-20260529-0240` | snapshot node を使って symbol collection した。 | snapshot の構築と参照が overhead になり、symbol time が減らなかった。 | TypeScript |
| `origin/codex/perf-snapshot-token-pass-20260529-0509` | snapshot token collection を 1 pass にした。 | 配列処理削減より、追加分岐と bookkeeping が重かった。 | TypeScript |
| `origin/codex/perf-flatten-vb-nodes-20260529-0507` | VBScript node flatten 時の配列生成を減らした。 | 再帰と配列の形が V8 で既存実装より速くならなかった。 | TypeScript |
| `origin/codex/perf-summary-public-symbols-20260529-0434` | summary 用 public-symbol walk を再利用した。 | summary path は benchmark 上の hot path ではなく、全体時間に効かなかった。 | TypeScript |
| `origin/codex/perf-best-visible-symbol-20260529-0211` | best visible symbol 探索で sort を避けた。 | generated benchmark では同名 candidate が少なく、効果が出にくかった。 | TypeScript |
| `origin/codex/perf-first-visible-symbol-20260529-0300` | first-visible-symbol lookup path を追加した。 | visibility lookup の分岐が増え、unused-symbol/analysis time が改善しなかった。 | TypeScript |
| `origin/codex/perf-resolve-visible-symbol-20260529-0535` | visible symbol resolve を早期終了するようにした。 | large/huge benchmark の両方で悪化した。 | TypeScript |
| `origin/codex/perf-unique-symbol-visible-20260529-0200` | unique symbol の visibility check を短絡した。 | index/probe overhead が、まれな短絡で得られる削減を上回った。 | TypeScript |
| `origin/codex/perf-scope-lookup-cache-20260529-0249` | offset ごとの scope lookup を cache した。 | Map 操作が、この path の lookup 再計算より高くついた。 | TypeScript |
| `origin/codex/perf-lower-name-reuse-20260529-0519` | lowercase 済み name を再利用した。 | lowercasing は測定された symbol path 周辺の支配的コストではなかった。 | TypeScript |
| `origin/codex/perf-vb-name-sets-20260529-0459` | よく使う VBScript name set を再利用した。 | Set 再利用でも access overhead を相殺できるほど analysis time が減らなかった。 | TypeScript |
| `origin/codex/perf-runtime-entrypoint-set-20260529-0455` | runtime entry-point name の set を再利用した。 | runtime entry-point check は hot path ではなく、benchmark に効かなかった。 | TypeScript |
| `origin/codex/perf-identifier-case-loop-20260529-0453` | identifier-case diagnostics の allocation を減らした。 | diagnostics time は noise 範囲または悪化で、有効な改善にならなかった。 | TypeScript |
| `origin/codex/perf-vb-context-root-replace-20260529-0606` | root VBScript context 置換時の配列生成を減らした。 | `benchmark:change` の final diagnostics と cache update timing が悪化した。 | TypeScript |
| `origin/codex/perf-include-reuse-non-html-20260529-0607` | 非 HTML edit で include references を再利用した。 | background mode は少し改善したが、foreground final diagnostics が悪化した。 | TypeScript |
| `origin/codex/perf-edit-history-append-20260529-0609` | edit-history append の spread/slice を手書き append に置き換えた。 | 手書き append は速くならず、cache update time が悪化した。 | TypeScript |
| `origin/codex/perf-zero-delta-incremental-20260529-0143` | same-length incremental edit の range shift を省略した。 | 条件判定と既存 path 維持のコストが、回避できた shift 処理より大きかった。 | TypeScript |

## 今後の注意点

- built-in array/string operation を置き換える前に、debug-step や profiler で本当に支配的か確認する。
- cache layer は hit rate と削減できる処理量が benchmark 上で見える場合だけ入れる。
- 同名 symbol、特殊 delimiter、annotation の多発などを狙う場合は、その条件を強く踏む fixture を先に追加する。
