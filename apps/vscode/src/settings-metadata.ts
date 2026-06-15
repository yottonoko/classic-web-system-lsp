export type AspSettingsLocale = "en" | "ja";
export type SettingsTargetScope = "global" | "workspace" | "workspaceFolder";
export type SettingsValueType = "array" | "boolean" | "enum" | "json" | "number" | "object" | "string";
export type SettingsPreviewKind =
  | "cache"
  | "code"
  | "excel"
  | "flowchart"
  | "general"
  | "graph"
  | "iis"
  | "memory"
  | "network"
  | "workspace";

export interface AspSettingsManifest {
  contributes?: {
    configuration?: {
      properties?: Record<string, AspSettingsSchema>;
    };
  };
}

export interface AspSettingsSchema {
  additionalProperties?: unknown;
  default?: unknown;
  description?: string;
  enum?: string[];
  items?: unknown;
  minimum?: number;
  properties?: Record<string, AspSettingsSchema>;
  tags?: string[];
  type?: string | string[];
}

export interface SettingsMetadata {
  category: string;
  defaultValue: unknown;
  description: string;
  enumValues?: string[];
  key: string;
  languageOverride: boolean;
  minimum?: number;
  nullable: boolean;
  objectProperties?: string[];
  previewKind: SettingsPreviewKind;
  section: string;
  tags: string[];
  title: string;
  type: SettingsValueType;
}

export interface SettingsInspection {
  defaultLanguageValue?: unknown;
  defaultValue?: unknown;
  globalLanguageValue?: unknown;
  globalValue?: unknown;
  workspaceFolderLanguageValue?: unknown;
  workspaceFolderValue?: unknown;
  workspaceLanguageValue?: unknown;
  workspaceValue?: unknown;
}

export interface SettingsValueState {
  defaultValue: unknown;
  effectiveValue: unknown;
  inheritedFrom: string;
  inheritedValue: unknown;
  targetDefined: boolean;
  targetValue: unknown;
}

const categoryLabels: Record<AspSettingsLocale, Record<string, string>> = {
  en: {
    cache: "Cache",
    codeLens: "CodeLens",
    debug: "Debug",
    diagnostics: "Diagnostics",
    editor: "Classic ASP editor",
    excel: "Excel export",
    flowchart: "Flowchart",
    format: "Formatting",
    general: "General",
    graph: "Graph",
    iis: "IIS",
    iisExpress: "IIS Express",
    inlayHints: "Inlay hints",
    incremental: "Incremental",
    javascript: "JavaScript/JScript",
    memory: "Memory",
    network: "Network",
    rename: "Rename",
    styleExtraction: "Style extraction",
    vbscript: "VBScript",
    webview: "Webview",
    workspace: "Workspace",
  },
  ja: {
    cache: "キャッシュ",
    codeLens: "CodeLens",
    debug: "デバッグ",
    diagnostics: "診断",
    editor: "Classic ASP エディター",
    excel: "Excel 出力",
    flowchart: "フローチャート",
    format: "整形",
    general: "基本",
    graph: "グラフ",
    iis: "IIS",
    iisExpress: "IIS Express",
    inlayHints: "インレイヒント",
    incremental: "差分解析",
    javascript: "JavaScript/JScript",
    memory: "メモリ",
    network: "ネットワーク",
    rename: "リネーム",
    styleExtraction: "style 抽出",
    vbscript: "VBScript",
    webview: "webview",
    workspace: "ワークスペース",
  },
};

export function buildAspSettingsMetadata(
  manifest: AspSettingsManifest,
  nls: Record<string, string>,
  locale: AspSettingsLocale,
): SettingsMetadata[] {
  const properties = manifest.contributes?.configuration?.properties ?? {};
  return Object.entries(properties)
    .filter(([key]) => key.startsWith("aspLsp."))
    .map(([key, schema]) => metadataFromAspSchema(key, schema, nls, locale));
}

export function classicAspLanguageSettingsMetadata(locale: AspSettingsLocale): SettingsMetadata[] {
  const text = standardSettingText[locale];
  const formatterId = "classic-asp-lsp";
  return [
    standardSetting("editor.defaultFormatter", "string", text.defaultFormatter, formatterId),
    standardSetting("editor.formatOnSave", "boolean", text.formatOnSave, false),
    standardSetting("editor.formatOnType", "boolean", text.formatOnType, false),
    standardSetting("editor.formatOnPaste", "boolean", text.formatOnPaste, false),
    standardSetting("editor.tabSize", "number", text.tabSize, 2, { minimum: 1 }),
    standardSetting("editor.insertSpaces", "boolean", text.insertSpaces, true),
    standardSetting("editor.detectIndentation", "boolean", text.detectIndentation, true),
    standardSetting("editor.wordWrap", "enum", text.wordWrap, "off", {
      enumValues: ["off", "on", "wordWrapColumn", "bounded"],
    }),
    standardSetting("editor.rulers", "array", text.rulers, []),
    standardSetting("editor.inlayHints.enabled", "enum", text.inlayHintsEnabled, "on", {
      enumValues: ["on", "off", "offUnlessPressed", "onUnlessPressed"],
    }),
    standardSetting("files.encoding", "string", text.filesEncoding, "utf8"),
    standardSetting("files.autoGuessEncoding", "boolean", text.autoGuessEncoding, false),
    standardSetting("files.eol", "enum", text.filesEol, "\n", {
      enumValues: ["\n", "\r\n", "auto"],
    }),
  ];
}

export function allSettingsMetadata(
  manifest: AspSettingsManifest,
  nls: Record<string, string>,
  locale: AspSettingsLocale,
): SettingsMetadata[] {
  return [
    ...buildAspSettingsMetadata(manifest, nls, locale),
    ...classicAspLanguageSettingsMetadata(locale),
  ];
}

export function settingsCategoryLabel(category: string, locale: AspSettingsLocale): string {
  return categoryLabels[locale][category] ?? titleFromKey(category);
}

export function valueStateForTarget(
  metadata: SettingsMetadata,
  inspection: SettingsInspection | undefined,
  effectiveValue: unknown,
  targetScope: SettingsTargetScope,
): SettingsValueState {
  const defaultValue =
    languageValue(metadata, inspection, "default") ??
    inspection?.defaultValue ??
    metadata.defaultValue;
  const globalValue = languageValue(metadata, inspection, "global");
  const workspaceValue = languageValue(metadata, inspection, "workspace");
  const workspaceFolderValue = languageValue(metadata, inspection, "workspaceFolder");
  const targetValue =
    targetScope === "global"
      ? globalValue
      : targetScope === "workspace"
        ? workspaceValue
        : workspaceFolderValue;
  const inheritedValue =
    targetScope === "global"
      ? defaultValue
      : targetScope === "workspace"
        ? globalValue ?? defaultValue
        : workspaceValue ?? globalValue ?? defaultValue;
  const inheritedFrom =
    targetScope === "global"
      ? "default"
      : targetScope === "workspace"
        ? globalValue === undefined
          ? "default"
          : "global"
        : workspaceValue !== undefined
          ? "workspace"
          : globalValue !== undefined
            ? "global"
            : "default";
  return {
    defaultValue,
    effectiveValue,
    inheritedFrom,
    inheritedValue,
    targetDefined: targetValue !== undefined,
    targetValue,
  };
}

function metadataFromAspSchema(
  key: string,
  schema: AspSettingsSchema,
  nls: Record<string, string>,
  locale: AspSettingsLocale,
): SettingsMetadata {
  const section = key.slice("aspLsp.".length);
  const category = categoryForAspSetting(key);
  const normalizedType = normalizedValueType(schema);
  return {
    category,
    defaultValue: schema.default,
    description: localizedDescription(schema.description, nls),
    enumValues: schema.enum,
    key,
    languageOverride: false,
    minimum: schema.minimum,
    nullable: schemaTypeList(schema).includes("null"),
    objectProperties: schema.properties ? Object.keys(schema.properties) : undefined,
    previewKind: previewKindForSetting(key),
    section,
    tags: schema.tags ?? [],
    title: `${settingsCategoryLabel(category, locale)}: ${titleFromKey(section)}`,
    type: normalizedType,
  };
}

function standardSetting(
  key: string,
  type: SettingsValueType,
  text: { description: string; title: string },
  defaultValue: unknown,
  options: { enumValues?: string[]; minimum?: number } = {},
): SettingsMetadata {
  return {
    category: "editor",
    defaultValue,
    description: text.description,
    enumValues: options.enumValues,
    key,
    languageOverride: true,
    minimum: options.minimum,
    nullable: false,
    previewKind: "code",
    section: key,
    tags: [],
    title: text.title,
    type,
  };
}

function localizedDescription(value: string | undefined, nls: Record<string, string>): string {
  const match = value?.match(/^%(.+)%$/);
  return match ? (nls[match[1] ?? ""] ?? value ?? "") : (value ?? "");
}

function normalizedValueType(schema: AspSettingsSchema): SettingsValueType {
  if (schema.enum) {
    return "enum";
  }
  const types = schemaTypeList(schema).filter((type) => type !== "null");
  const type = types[0] ?? "string";
  if (type === "array" || type === "boolean" || type === "number" || type === "string") {
    return type;
  }
  if (type === "object") {
    return "object";
  }
  return "json";
}

function schemaTypeList(schema: AspSettingsSchema): string[] {
  if (Array.isArray(schema.type)) {
    return schema.type;
  }
  return schema.type ? [schema.type] : [];
}

function categoryForAspSetting(key: string): string {
  const section = key.slice("aspLsp.".length);
  if (!section.includes(".")) {
    return "general";
  }
  return section.split(".")[0] ?? "general";
}

function previewKindForSetting(key: string): SettingsPreviewKind {
  if (
    key.startsWith("aspLsp.format.") ||
    key.startsWith("aspLsp.vbscript.") ||
    key.startsWith("aspLsp.javascript.") ||
    key.startsWith("aspLsp.inlayHints.") ||
    key.startsWith("aspLsp.codeLens.") ||
    key.startsWith("aspLsp.styleExtraction.") ||
    key === "aspLsp.defaultLanguage" ||
    key === "aspLsp.checkJs"
  ) {
    return "code";
  }
  if (key.startsWith("aspLsp.graph.")) {
    return "graph";
  }
  if (key.startsWith("aspLsp.flowchart.")) {
    return "flowchart";
  }
  if (key.startsWith("aspLsp.excel.")) {
    return "excel";
  }
  if (key.startsWith("aspLsp.workspace.")) {
    return "workspace";
  }
  if (key.startsWith("aspLsp.cache.")) {
    return "cache";
  }
  if (key.startsWith("aspLsp.network.")) {
    return "network";
  }
  if (key.startsWith("aspLsp.memory.")) {
    return "memory";
  }
  if (key.startsWith("aspLsp.iis.")) {
    return "iis";
  }
  if (key.startsWith("aspLsp.iisExpress.")) {
    return "iis";
  }
  return "general";
}

function languageValue(
  metadata: SettingsMetadata,
  inspection: SettingsInspection | undefined,
  scope: "default" | "global" | "workspace" | "workspaceFolder",
): unknown {
  if (!metadata.languageOverride) {
    if (scope === "default") {
      return inspection?.defaultValue;
    }
    if (scope === "global") {
      return inspection?.globalValue;
    }
    if (scope === "workspace") {
      return inspection?.workspaceValue;
    }
    return inspection?.workspaceFolderValue;
  }
  if (scope === "default") {
    return inspection?.defaultLanguageValue ?? inspection?.defaultValue;
  }
  if (scope === "global") {
    return inspection?.globalLanguageValue;
  }
  if (scope === "workspace") {
    return inspection?.workspaceLanguageValue;
  }
  return inspection?.workspaceFolderLanguageValue;
}

function titleFromKey(key: string): string {
  return key
    .split(".")
    .map((part) =>
      part
        .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
        .replace(/^./, (char) => char.toUpperCase()),
    )
    .join(" / ");
}

const standardSettingText: Record<
  AspSettingsLocale,
  Record<
    | "autoGuessEncoding"
    | "defaultFormatter"
    | "detectIndentation"
    | "filesEncoding"
    | "filesEol"
    | "formatOnPaste"
    | "formatOnSave"
    | "formatOnType"
    | "inlayHintsEnabled"
    | "insertSpaces"
    | "rulers"
    | "tabSize"
    | "wordWrap",
    { description: string; title: string }
  >
> = {
  en: {
    autoGuessEncoding: {
      description: "Guess the file encoding when opening Classic ASP documents.",
      title: "Classic ASP editor: Files / Auto Guess Encoding",
    },
    defaultFormatter: {
      description: "Default formatter used for Classic ASP documents.",
      title: "Classic ASP editor: Editor / Default Formatter",
    },
    detectIndentation: {
      description: "Let VS Code detect indentation from Classic ASP file contents.",
      title: "Classic ASP editor: Editor / Detect Indentation",
    },
    filesEncoding: {
      description: "Text encoding used by VS Code when opening Classic ASP files.",
      title: "Classic ASP editor: Files / Encoding",
    },
    filesEol: {
      description: "End-of-line sequence used by VS Code for Classic ASP files.",
      title: "Classic ASP editor: Files / EOL",
    },
    formatOnPaste: {
      description: "Format pasted Classic ASP content automatically.",
      title: "Classic ASP editor: Editor / Format On Paste",
    },
    formatOnSave: {
      description: "Format Classic ASP files automatically on save.",
      title: "Classic ASP editor: Editor / Format On Save",
    },
    formatOnType: {
      description: "Format Classic ASP content automatically while typing.",
      title: "Classic ASP editor: Editor / Format On Type",
    },
    inlayHintsEnabled: {
      description: "VS Code inlay hint display mode for Classic ASP files.",
      title: "Classic ASP editor: Editor / Inlay Hints",
    },
    insertSpaces: {
      description: "Insert spaces instead of tab characters in Classic ASP files.",
      title: "Classic ASP editor: Editor / Insert Spaces",
    },
    rulers: {
      description: "Column rulers shown in Classic ASP files.",
      title: "Classic ASP editor: Editor / Rulers",
    },
    tabSize: {
      description: "Tab width used by the VS Code editor for Classic ASP files.",
      title: "Classic ASP editor: Editor / Tab Size",
    },
    wordWrap: {
      description: "Word wrapping mode for Classic ASP files.",
      title: "Classic ASP editor: Editor / Word Wrap",
    },
  },
  ja: {
    autoGuessEncoding: {
      description: "Classic ASP document を開くときに file encoding を推測します。",
      title: "Classic ASP エディター: Files / Auto Guess Encoding",
    },
    defaultFormatter: {
      description: "Classic ASP document に使う既定 formatter です。",
      title: "Classic ASP エディター: Editor / Default Formatter",
    },
    detectIndentation: {
      description: "Classic ASP file の内容から indentation を VS Code に推測させます。",
      title: "Classic ASP エディター: Editor / Detect Indentation",
    },
    filesEncoding: {
      description: "Classic ASP file を VS Code が開くときの text encoding です。",
      title: "Classic ASP エディター: Files / Encoding",
    },
    filesEol: {
      description: "Classic ASP file に使う改行コードです。",
      title: "Classic ASP エディター: Files / EOL",
    },
    formatOnPaste: {
      description: "Classic ASP content を paste したときに自動で整形します。",
      title: "Classic ASP エディター: Editor / Format On Paste",
    },
    formatOnSave: {
      description: "Classic ASP file を保存したときに自動で整形します。",
      title: "Classic ASP エディター: Editor / Format On Save",
    },
    formatOnType: {
      description: "Classic ASP content の入力中に自動で整形します。",
      title: "Classic ASP エディター: Editor / Format On Type",
    },
    inlayHintsEnabled: {
      description: "Classic ASP file での VS Code inlay hint 表示方法です。",
      title: "Classic ASP エディター: Editor / Inlay Hints",
    },
    insertSpaces: {
      description: "Classic ASP file で tab 文字ではなく space を挿入します。",
      title: "Classic ASP エディター: Editor / Insert Spaces",
    },
    rulers: {
      description: "Classic ASP file に表示する column ruler です。",
      title: "Classic ASP エディター: Editor / Rulers",
    },
    tabSize: {
      description: "Classic ASP file に使う VS Code editor の tab width です。",
      title: "Classic ASP エディター: Editor / Tab Size",
    },
    wordWrap: {
      description: "Classic ASP file の word wrap mode です。",
      title: "Classic ASP エディター: Editor / Word Wrap",
    },
  },
};
