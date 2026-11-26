import * as vscode from "vscode";

/**
 * Shared Classic ASP / VBScript LSP feature settings passed to
 * `mixin lsp --asp-only` via `initializationOptions.aspLsp` and
 * `workspace/didChangeConfiguration`.
 *
 * Mirrors `editors/vscode/src/aspConfig.ts` in `mixin-lang` so settings stay
 * consistent between Mixin's own extension and this standalone extension.
 */
export interface AspLspSettings {
  codeLens: {
    references: boolean;
    includes: boolean;
  };
  inlayHints: {
    variableTypes: boolean;
    returnTypes: boolean;
    parameterNames: boolean;
    scopeMarkers: boolean;
  };
  onTypeFormatting: boolean;
}

export function readAspLspSettings(section = "aspLsp"): AspLspSettings {
  const cfg = vscode.workspace.getConfiguration(section);
  return {
    codeLens: {
      references: cfg.get<boolean>("codeLens.references", true),
      includes: cfg.get<boolean>("codeLens.includes", true),
    },
    inlayHints: {
      variableTypes: cfg.get<boolean>("inlayHints.variableTypes", true),
      returnTypes: cfg.get<boolean>("inlayHints.returnTypes", true),
      parameterNames: cfg.get<boolean>("inlayHints.parameterNames", true),
      scopeMarkers: cfg.get<boolean>("inlayHints.scopeMarkers", true),
    },
    onTypeFormatting: cfg.get<boolean>("onTypeFormatting", true),
  };
}
