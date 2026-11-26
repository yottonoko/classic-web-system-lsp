import * as vscode from "vscode";

/**
 * Shared Classic ASP / VBScript LSP feature settings passed to the standalone
 * `classic-asp-lsp` server via `initializationOptions.aspLsp` and
 * `workspace/didChangeConfiguration`.
 *
 * Mirrors `editors/vscode/src/aspConfig.ts` in `mixin-lang`.
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
  io: {
    networkMode: "auto" | "always" | "never";
    statTtlMs: number;
    diskCache: boolean;
  };
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
    io: {
      networkMode: cfg.get<"auto" | "always" | "never">("io.networkMode", "auto"),
      statTtlMs: cfg.get<number>("io.statTtlMs", 5000),
      diskCache: cfg.get<boolean>("io.diskCache", true),
    },
  };
}
