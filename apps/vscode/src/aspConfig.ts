import * as vscode from "vscode";

export type LintRuleSeverity = "default" | "off" | "hint" | "info" | "warning" | "error";

export interface AspLspLintSettings {
  enabled: boolean;
  groups: {
    security: boolean;
    correctness: boolean;
    style: boolean;
  };
  rules: {
    "asp/unencoded-output": LintRuleSeverity;
    "asp/sql-concat": LintRuleSeverity;
    "vb/undeclared-var": LintRuleSeverity;
    "vb/missing-set": LintRuleSeverity;
    "vb/unreachable": LintRuleSeverity;
    "vb/on-error-resume-next": LintRuleSeverity;
    "vb/option-explicit": LintRuleSeverity;
    "vb/unused-var": LintRuleSeverity;
    "vb/goto-legacy": LintRuleSeverity;
  };
}

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
  html: {
    linkedEditing: boolean;
  };
  js: {
    jsdocTypeStyle: "typescript" | "jsdoc";
  };
  io: {
    networkMode: "auto" | "always" | "never";
    statTtlMs: number;
    diskCache: boolean;
  };
  workspace: {
    includes: string[];
    excludes: string[];
    respectGitIgnore: boolean;
  };
  lint: AspLspLintSettings;
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
    html: {
      linkedEditing: cfg.get<boolean>("html.linkedEditing", true),
    },
    js: {
      jsdocTypeStyle: cfg.get<"typescript" | "jsdoc">("js.jsdocTypeStyle", "typescript"),
    },
    io: {
      networkMode: cfg.get<"auto" | "always" | "never">("io.networkMode", "auto"),
      statTtlMs: cfg.get<number>("io.statTtlMs", 5000),
      diskCache: cfg.get<boolean>("io.diskCache", true),
    },
    workspace: {
      includes: cfg.get<string[]>("workspace.includes", ["**/*.{asp,asa,inc,vbs}"]),
      excludes: cfg.get<string[]>("workspace.excludes", []),
      respectGitIgnore: cfg.get<boolean>("workspace.respectGitIgnore", false),
    },
    lint: {
      enabled: cfg.get<boolean>("lint.enabled", true),
      groups: {
        security: cfg.get<boolean>("lint.groups.security", true),
        correctness: cfg.get<boolean>("lint.groups.correctness", true),
        style: cfg.get<boolean>("lint.groups.style", true),
      },
      rules: {
        "asp/unencoded-output": cfg.get<LintRuleSeverity>(
          "lint.rules.asp/unencoded-output",
          "default",
        ),
        "asp/sql-concat": cfg.get<LintRuleSeverity>("lint.rules.asp/sql-concat", "default"),
        "vb/undeclared-var": cfg.get<LintRuleSeverity>("lint.rules.vb/undeclared-var", "default"),
        "vb/missing-set": cfg.get<LintRuleSeverity>("lint.rules.vb/missing-set", "default"),
        "vb/unreachable": cfg.get<LintRuleSeverity>("lint.rules.vb/unreachable", "default"),
        "vb/on-error-resume-next": cfg.get<LintRuleSeverity>(
          "lint.rules.vb/on-error-resume-next",
          "default",
        ),
        "vb/option-explicit": cfg.get<LintRuleSeverity>("lint.rules.vb/option-explicit", "default"),
        "vb/unused-var": cfg.get<LintRuleSeverity>("lint.rules.vb/unused-var", "default"),
        "vb/goto-legacy": cfg.get<LintRuleSeverity>("lint.rules.vb/goto-legacy", "default"),
      },
    },
  };
}
