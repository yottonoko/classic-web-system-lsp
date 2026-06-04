import type { AspLocale } from "./types";

export type LocalizeParams = Record<string, string | number>;

export interface AspLocalizer {
  locale: AspLocale;
  t(key: LocalizeKey, params?: LocalizeParams): string;
}

export type LocalizeKey =
  | "parser.missingAspClose"
  | "vb.doc.detail.parameter"
  | "vb.doc.detail.cref"
  | "vb.doc.detail.attribute"
  | "vb.doc.detail.closingTag"
  | "vb.doc.detail.tag"
  | "vb.doc.annotation.type.detail"
  | "vb.doc.annotation.type.documentation"
  | "vb.doc.annotation.param.detail"
  | "vb.doc.annotation.param.documentation"
  | "vb.doc.annotation.returns.detail"
  | "vb.doc.annotation.returns.documentation"
  | "vb.doc.annotation.member.detail"
  | "vb.doc.annotation.member.documentation"
  | "vb.doc.heading.remarks"
  | "vb.doc.heading.parameters"
  | "vb.doc.heading.returns"
  | "vb.doc.heading.value"
  | "vb.doc.heading.exceptions"
  | "vb.doc.heading.seeAlso"
  | "vb.doc.heading.example"
  | "vb.doc.typeNote"
  | "vb.builtin.request.detail"
  | "vb.builtin.request.documentation"
  | "vb.builtin.response.detail"
  | "vb.builtin.session.detail"
  | "vb.builtin.application.detail"
  | "vb.builtin.server.detail"
  | "vb.builtin.asperror.detail"
  | "vb.builtin.optionExplicit.detail"
  | "vb.hover.builtin.request"
  | "vb.hover.builtin.response"
  | "vb.hover.builtin.session"
  | "vb.hover.builtin.application"
  | "vb.hover.builtin.server"
  | "vb.hover.builtin.asperror"
  | "vb.symbol.owner"
  | "vb.symbol.scope"
  | "vb.completion.builtinLabel"
  | "vb.completion.syntaxSnippet"
  | "vb.completion.syntaxKeyword"
  | "vb.completion.definedIn"
  | "vb.completion.memberDocumentation"
  | "vb.diagnostic.undeclared"
  | "vb.diagnostic.unusedParameter"
  | "vb.diagnostic.unusedSymbol"
  | "vb.diagnostic.initializedDeclaration"
  | "vb.diagnostic.typedDeclaration"
  | "vb.diagnostic.invalidCallSyntax"
  | "vb.diagnostic.missingThen"
  | "vb.diagnostic.missingIfCondition"
  | "vb.diagnostic.invalidIfCondition"
  | "vb.diagnostic.missingEndIf"
  | "vb.diagnostic.setScalar"
  | "vb.diagnostic.objectNeedsSet"
  | "vb.diagnostic.typeMismatch"
  | "vb.diagnostic.unknownCall"
  | "vb.diagnostic.argumentCountMismatch"
  | "vb.diagnostic.missingMember"
  | "vb.diagnostic.identifierCase"
  | "server.unknownCommand"
  | "server.include.unresolved"
  | "server.include.caseMismatch"
  | "server.include.currentDocument"
  | "server.include.cycle"
  | "server.workspaceIndex.truncated"
  | "server.completion.html.detail"
  | "server.completion.html.documentation"
  | "server.completion.css.detail"
  | "server.completion.css.documentation"
  | "server.quickfix.declareDim"
  | "server.quickfix.splitInitializedDim"
  | "server.quickfix.fixVbscriptCallSyntax"
  | "server.quickfix.splitMultiDim"
  | "server.quickfix.removeUnusedDeclaration"
  | "server.quickfix.addSet"
  | "server.quickfix.removeSet"
  | "server.quickfix.annotateType"
  | "server.quickfix.renameIdentifierCase"
  | "server.quickfix.createMissingInclude"
  | "server.quickfix.generateVbscriptDocumentation"
  | "server.refactor.extractVbscriptVariable"
  | "server.codeAction.organizeJavascriptImports"
  | "server.codeLens.reference"
  | "server.codeLens.references"
  | "server.codeLens.include";

const en: Record<LocalizeKey, string> = {
  "parser.missingAspClose": "Classic ASP block is missing a closing %> delimiter.",
  "vb.doc.detail.parameter": "VBScript XML documentation parameter",
  "vb.doc.detail.cref": "VBScript XML documentation cref",
  "vb.doc.detail.attribute": "VBScript XML documentation attribute",
  "vb.doc.detail.closingTag": "VBScript XML documentation closing tag",
  "vb.doc.detail.tag": "VBScript XML documentation tag",
  "vb.doc.annotation.type.detail": "VBScript type annotation",
  "vb.doc.annotation.type.documentation":
    "Declares VBScript type metadata for a variable or constant: `' @type name As Type`.",
  "vb.doc.annotation.param.detail": "VBScript parameter type annotation",
  "vb.doc.annotation.param.documentation":
    "Declares VBScript type metadata for a parameter: `' @param [procedure.]name As Type`.",
  "vb.doc.annotation.returns.detail": "VBScript return type annotation",
  "vb.doc.annotation.returns.documentation":
    "Declares VBScript return type metadata: `' @returns [procedure] Type`.",
  "vb.doc.annotation.member.detail": "VBScript member type annotation",
  "vb.doc.annotation.member.documentation":
    "Declares type metadata for a dynamic or external member: `' @member Type.member As Type`.",
  "vb.doc.heading.remarks": "Remarks",
  "vb.doc.heading.parameters": "Parameters",
  "vb.doc.heading.returns": "Returns",
  "vb.doc.heading.value": "Value",
  "vb.doc.heading.exceptions": "Exceptions",
  "vb.doc.heading.seeAlso": "See also",
  "vb.doc.heading.example": "Example",
  "vb.doc.typeNote":
    "XML documentation is descriptive only. Use `' @type`, `' @param ... As ...`, or `' @returns ...` annotations for VBScript type metadata.",
  "vb.builtin.request.detail": "Classic ASP Request object",
  "vb.builtin.request.documentation":
    "Reads client request values such as QueryString, Form, Cookies, and ServerVariables.",
  "vb.builtin.response.detail": "Classic ASP Response object",
  "vb.builtin.session.detail": "Classic ASP Session object",
  "vb.builtin.application.detail": "Classic ASP Application object",
  "vb.builtin.server.detail": "Classic ASP Server object",
  "vb.builtin.asperror.detail": "Classic ASP error object",
  "vb.builtin.optionExplicit.detail": "Require explicit variable declarations",
  "vb.hover.builtin.request": "Classic ASP Request object. Reads values sent by the client.",
  "vb.hover.builtin.response":
    "Classic ASP Response object. Writes output and controls the HTTP response.",
  "vb.hover.builtin.session": "Classic ASP Session object. Stores per-user state.",
  "vb.hover.builtin.application": "Classic ASP Application object. Stores application-wide state.",
  "vb.hover.builtin.server":
    "Classic ASP Server object. Creates COM objects, maps paths, and encodes values.",
  "vb.hover.builtin.asperror": "Classic ASP error object returned by Server.GetLastError.",
  "vb.symbol.owner": " of {owner}",
  "vb.symbol.scope": " in {scope}",
  "vb.completion.builtinLabel": "built-in",
  "vb.completion.syntaxSnippet": "VBScript syntax snippet",
  "vb.completion.syntaxKeyword": "VBScript syntax keyword",
  "vb.completion.definedIn": "Defined in {uri}.",
  "vb.completion.memberDocumentation": "{kind} {type}.{member}{suffix}",
  "vb.diagnostic.undeclared": "'{name}' is not declared under Option Explicit.",
  "vb.diagnostic.unusedParameter": "Parameter '{name}' is never used.",
  "vb.diagnostic.unusedSymbol": "'{name}' is declared but never used.",
  "vb.diagnostic.initializedDeclaration":
    "VBScript {keyword} declarations cannot include initializers.",
  "vb.diagnostic.typedDeclaration": "VBScript {keyword} declarations cannot include As types.",
  "vb.diagnostic.invalidCallSyntax": "VBScript call syntax is invalid for '{name}'.",
  "vb.diagnostic.missingThen": "VBScript If statements must include Then.",
  "vb.diagnostic.missingIfCondition": "VBScript If statements must include a condition.",
  "vb.diagnostic.invalidIfCondition": "VBScript If condition syntax is invalid.",
  "vb.diagnostic.missingEndIf": "VBScript multiline If block is missing End If.",
  "vb.diagnostic.setScalar": "Set assigns an object reference, but '{name}' receives {type}.",
  "vb.diagnostic.objectNeedsSet": "Object assignment to '{name}' should use Set.",
  "vb.diagnostic.typeMismatch": "Type mismatch: '{name}' is {expected}, but assigned {actual}.",
  "vb.diagnostic.unknownCall": "Call target '{name}' is not known.",
  "vb.diagnostic.argumentCountMismatch":
    "Argument count mismatch for '{name}': expected {expected}, got {actual}.",
  "vb.diagnostic.missingMember": "Type '{type}' has no member '{member}'.",
  "vb.diagnostic.identifierCase":
    "Identifier '{name}' should be '{expectedName}' for {style} casing.",
  "server.unknownCommand": "Unknown command: {command}",
  "server.include.unresolved": "Include file '{path}' could not be resolved.",
  "server.include.caseMismatch":
    "Include path '{path}' differs from the file system casing '{actualPath}'.",
  "server.include.currentDocument": "Include file references the current document.",
  "server.include.cycle": "Include cycle detected: {cycle}.",
  "server.workspaceIndex.truncated":
    "Classic ASP workspace index stopped at {maxFiles} files. Increase aspLsp.workspace.maxIndexFiles to index more files.",
  "server.completion.html.detail": "HTML completion",
  "server.completion.html.documentation": "Completion provided by vscode-html-languageservice.",
  "server.completion.css.detail": "CSS completion",
  "server.completion.css.documentation": "Completion provided by vscode-css-languageservice.",
  "server.quickfix.declareDim": "Declare {name} with Dim",
  "server.quickfix.splitInitializedDim": "Split initialized Dim declaration",
  "server.quickfix.fixVbscriptCallSyntax": "Fix VBScript call syntax",
  "server.quickfix.splitMultiDim": "Split Dim declarations",
  "server.quickfix.removeUnusedDeclaration": "Remove unused declaration {name}",
  "server.quickfix.addSet": "Use Set for object assignment to {name}",
  "server.quickfix.removeSet": "Remove Set from scalar assignment to {name}",
  "server.quickfix.annotateType": "Annotate {name} as {type}",
  "server.quickfix.renameIdentifierCase": "Rename {name} to {expectedName}",
  "server.quickfix.createMissingInclude": "Create missing include {path}",
  "server.quickfix.generateVbscriptDocumentation": "Generate VBScript documentation",
  "server.refactor.extractVbscriptVariable": "Extract VBScript variable",
  "server.codeAction.organizeJavascriptImports": "Organize JavaScript imports",
  "server.codeLens.reference": "{count} reference",
  "server.codeLens.references": "{count} references",
  "server.codeLens.include": "include {name}",
};

const ja: Record<LocalizeKey, string> = {
  "parser.missingAspClose": "Classic ASP ブロックに閉じ区切り %> がありません。",
  "vb.doc.detail.parameter": "VBScript XML ドキュメントコメントの parameter",
  "vb.doc.detail.cref": "VBScript XML ドキュメントコメントの cref",
  "vb.doc.detail.attribute": "VBScript XML ドキュメントコメントの attribute",
  "vb.doc.detail.closingTag": "VBScript XML ドキュメントコメントの closing tag",
  "vb.doc.detail.tag": "VBScript XML ドキュメントコメントの tag",
  "vb.doc.annotation.type.detail": "VBScript type annotation",
  "vb.doc.annotation.type.documentation":
    "VBScript の変数または定数の型メタデータを宣言します: `' @type name As Type`。",
  "vb.doc.annotation.param.detail": "VBScript parameter type annotation",
  "vb.doc.annotation.param.documentation":
    "VBScript の parameter 型メタデータを宣言します: `' @param [procedure.]name As Type`。",
  "vb.doc.annotation.returns.detail": "VBScript return type annotation",
  "vb.doc.annotation.returns.documentation":
    "VBScript の戻り値型メタデータを宣言します: `' @returns [procedure] Type`。",
  "vb.doc.annotation.member.detail": "VBScript member type annotation",
  "vb.doc.annotation.member.documentation":
    "dynamic member または external member の型メタデータを宣言します: `' @member Type.member As Type`。",
  "vb.doc.heading.remarks": "補足",
  "vb.doc.heading.parameters": "パラメーター",
  "vb.doc.heading.returns": "戻り値",
  "vb.doc.heading.value": "値",
  "vb.doc.heading.exceptions": "例外",
  "vb.doc.heading.seeAlso": "関連項目",
  "vb.doc.heading.example": "例",
  "vb.doc.typeNote":
    "XML ドキュメントコメントは説明用です。VBScript の型メタデータには `' @type`、`' @param ... As ...`、`' @returns ...` 注釈を使ってください。",
  "vb.builtin.request.detail": "Classic ASP Request オブジェクト",
  "vb.builtin.request.documentation":
    "QueryString、Form、Cookies、ServerVariables など、クライアントから送られた値を読み取ります。",
  "vb.builtin.response.detail": "Classic ASP Response オブジェクト",
  "vb.builtin.session.detail": "Classic ASP Session オブジェクト",
  "vb.builtin.application.detail": "Classic ASP Application オブジェクト",
  "vb.builtin.server.detail": "Classic ASP Server オブジェクト",
  "vb.builtin.asperror.detail": "Classic ASP error オブジェクト",
  "vb.builtin.optionExplicit.detail": "明示的な変数宣言を必須にします",
  "vb.hover.builtin.request":
    "Classic ASP Request オブジェクト。クライアントから送られた値を読み取ります。",
  "vb.hover.builtin.response":
    "Classic ASP Response オブジェクト。出力を書き込み、HTTP レスポンスを制御します。",
  "vb.hover.builtin.session": "Classic ASP Session オブジェクト。ユーザーごとの状態を保存します。",
  "vb.hover.builtin.application":
    "Classic ASP Application オブジェクト。アプリケーション全体の状態を保存します。",
  "vb.hover.builtin.server":
    "Classic ASP Server オブジェクト。COM オブジェクトの作成、パス解決、値のエンコードを行います。",
  "vb.hover.builtin.asperror": "Server.GetLastError が返す Classic ASP error オブジェクトです。",
  "vb.symbol.owner": " ({owner} のメンバー)",
  "vb.symbol.scope": " ({scope} 内)",
  "vb.completion.builtinLabel": "built-in",
  "vb.completion.syntaxSnippet": "VBScript 構文スニペット",
  "vb.completion.syntaxKeyword": "VBScript 構文キーワード",
  "vb.completion.definedIn": "{uri} で定義されています。",
  "vb.completion.memberDocumentation": "{type}.{member}{suffix} の {kind}",
  "vb.diagnostic.undeclared": "'{name}' は Option Explicit のもとで宣言されていません。",
  "vb.diagnostic.unusedParameter": "パラメーター '{name}' は使われていません。",
  "vb.diagnostic.unusedSymbol": "'{name}' は宣言されていますが使われていません。",
  "vb.diagnostic.initializedDeclaration": "VBScript の {keyword} 宣言には初期値を含められません。",
  "vb.diagnostic.typedDeclaration": "VBScript の {keyword} 宣言には As 型指定を含められません。",
  "vb.diagnostic.invalidCallSyntax": "'{name}' の VBScript 呼び出し構文が無効です。",
  "vb.diagnostic.missingThen": "VBScript の If statement には Then が必要です。",
  "vb.diagnostic.missingIfCondition": "VBScript の If statement には条件式が必要です。",
  "vb.diagnostic.invalidIfCondition": "VBScript の If 条件式の構文が無効です。",
  "vb.diagnostic.missingEndIf": "VBScript の multiline If block に End If がありません。",
  "vb.diagnostic.setScalar":
    "Set はオブジェクト参照を代入しますが、'{name}' は {type} を受け取っています。",
  "vb.diagnostic.objectNeedsSet": "'{name}' へのオブジェクト代入には Set が必要です。",
  "vb.diagnostic.typeMismatch":
    "型が一致しません: '{name}' は {expected} ですが、{actual} が代入されています。",
  "vb.diagnostic.unknownCall": "呼び出し先 '{name}' は不明です。",
  "vb.diagnostic.argumentCountMismatch":
    "'{name}' の引数の数が一致しません: 期待値 {expected}、実際 {actual}。",
  "vb.diagnostic.missingMember": "型 '{type}' にメンバー '{member}' はありません。",
  "vb.diagnostic.identifierCase":
    "識別子 '{name}' は {style} casing の '{expectedName}' にしてください。",
  "server.unknownCommand": "不明なコマンドです: {command}",
  "server.include.unresolved": "include file '{path}' を解決できません。",
  "server.include.caseMismatch":
    "include path '{path}' は file system 上の大文字小文字 '{actualPath}' と一致していません。",
  "server.include.currentDocument": "include file が現在のドキュメントを参照しています。",
  "server.include.cycle": "include の循環を検出しました: {cycle}。",
  "server.workspaceIndex.truncated":
    "Classic ASP workspace index は {maxFiles} ファイルで停止しました。さらに index するには aspLsp.workspace.maxIndexFiles を増やしてください。",
  "server.completion.html.detail": "HTML 補完",
  "server.completion.html.documentation": "vscode-html-languageservice による補完です。",
  "server.completion.css.detail": "CSS 補完",
  "server.completion.css.documentation": "vscode-css-languageservice による補完です。",
  "server.quickfix.declareDim": "{name} を Dim で宣言",
  "server.quickfix.splitInitializedDim": "初期化つき Dim 宣言を分割",
  "server.quickfix.fixVbscriptCallSyntax": "VBScript の呼び出し構文を修正",
  "server.quickfix.splitMultiDim": "Dim 宣言を分割",
  "server.quickfix.removeUnusedDeclaration": "未使用の宣言 {name} を削除",
  "server.quickfix.addSet": "{name} への object assignment に Set を使う",
  "server.quickfix.removeSet": "{name} への scalar assignment から Set を削除",
  "server.quickfix.annotateType": "{name} に {type} の型注釈を追加",
  "server.quickfix.renameIdentifierCase": "{name} を {expectedName} に変更",
  "server.quickfix.createMissingInclude": "不足している include {path} を作成",
  "server.quickfix.generateVbscriptDocumentation": "VBScript documentation を生成",
  "server.refactor.extractVbscriptVariable": "VBScript 変数に抽出",
  "server.codeAction.organizeJavascriptImports": "JavaScript import を整理",
  "server.codeLens.reference": "{count} 件の参照",
  "server.codeLens.references": "{count} 件の参照",
  "server.codeLens.include": "{name} を include",
};

export function createLocalizer(locale: AspLocale | undefined): AspLocalizer {
  const resolved = locale === "ja" ? "ja" : "en";
  return {
    locale: resolved,
    t(key, params = {}) {
      const template = (resolved === "ja" ? ja : en)[key] ?? en[key] ?? key;
      return template.replace(/\{([A-Za-z0-9_]+)\}/g, (_, name: string) =>
        String(params[name] ?? `{${name}}`),
      );
    },
  };
}
