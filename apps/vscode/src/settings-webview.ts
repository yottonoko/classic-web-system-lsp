import fs from "node:fs";
import path from "node:path";
import * as vscode from "vscode";
import {
  allSettingsMetadata,
  settingsCategoryLabel,
  valueStateForTarget,
  type AspSettingsLocale,
  type AspSettingsManifest,
  type SettingsMetadata,
  type SettingsTargetScope,
  type SettingsValueState,
} from "./settings-metadata";

export type AspSettingsWebviewThemeSetting = "auto" | "dark" | "light";

export interface AspSettingsWebviewPayload {
  categories: AspSettingsWebviewCategory[];
  locale: AspSettingsLocale;
  scopes: AspSettingsWebviewScopes;
  selectedScope: AspSettingsTarget;
  settings: SettingsMetadata[];
  theme: AspSettingsWebviewThemeSetting;
  values: Record<string, SettingsValueState>;
}

export interface AspSettingsWebviewCategory {
  id: string;
  label: string;
  total: number;
}

export interface AspSettingsWebviewScopes {
  folders: AspSettingsWorkspaceFolder[];
  workspaceAvailable: boolean;
  workspaceFolderAvailable: boolean;
}

export interface AspSettingsWorkspaceFolder {
  name: string;
  uri: string;
}

export interface AspSettingsTarget {
  folderUri?: string;
  scope: SettingsTargetScope;
}

interface SaveSettingsMessage {
  requestId: string;
  target: AspSettingsTarget;
  type: "saveSettings";
  updates: AspSettingsUpdate[];
}

interface ReloadSettingsMessage {
  requestId: string;
  target: AspSettingsTarget;
  type: "reloadSettings";
}

interface AspSettingsUpdate {
  key: string;
  reset?: boolean;
  value?: unknown;
}

interface AspSettingsHostMessage {
  error?: string;
  payload?: AspSettingsWebviewPayload;
  requestId?: string;
  type: "saveResult" | "settingsPayload";
}

type WebviewMessage = ReloadSettingsMessage | SaveSettingsMessage;

export function showAspSettingsWebview(
  context: vscode.ExtensionContext,
  locale: AspSettingsLocale,
  theme: AspSettingsWebviewThemeSetting,
): vscode.WebviewPanel {
  const webviewRoot = vscode.Uri.joinPath(context.extensionUri, "dist", "webview");
  const panel = vscode.window.createWebviewPanel(
    "aspLsp.settings",
    settingsHostText(locale, "panelTitle"),
    vscode.ViewColumn.Active,
    {
      enableScripts: true,
      retainContextWhenHidden: true,
      localResourceRoots: [webviewRoot],
    },
  );
  const initialTarget = defaultSettingsTarget();
  panel.webview.onDidReceiveMessage((message: WebviewMessage) => {
    if (message.type === "reloadSettings") {
      void postSettingsPayload(context, panel.webview, locale, theme, message.target, message.requestId);
    } else if (message.type === "saveSettings") {
      void saveSettings(context, panel.webview, locale, theme, message);
    }
  });
  panel.webview.html = settingsWebviewHtml(
    panel.webview,
    webviewRoot,
    settingsPayload(context, locale, theme, initialTarget),
    settingsHostText(locale, "panelTitle"),
    locale,
  );
  return panel;
}

function postSettingsPayload(
  context: vscode.ExtensionContext,
  webview: vscode.Webview,
  locale: AspSettingsLocale,
  theme: AspSettingsWebviewThemeSetting,
  target: AspSettingsTarget,
  requestId: string,
): Thenable<boolean> {
  return webview.postMessage({
    type: "settingsPayload",
    requestId,
    payload: settingsPayload(context, locale, theme, normalizeTarget(target)),
  } satisfies AspSettingsHostMessage);
}

async function saveSettings(
  context: vscode.ExtensionContext,
  webview: vscode.Webview,
  locale: AspSettingsLocale,
  theme: AspSettingsWebviewThemeSetting,
  message: SaveSettingsMessage,
): Promise<void> {
  const response: AspSettingsHostMessage = {
    type: "saveResult",
    requestId: message.requestId,
  };
  try {
    const target = normalizeTarget(message.target);
    const metadataByKey = new Map(
      settingsMetadata(context, locale).map((metadata) => [metadata.key, metadata]),
    );
    for (const update of message.updates) {
      const metadata = metadataByKey.get(update.key);
      if (!metadata) {
        throw new Error(settingsHostText(locale, "unknownSetting").replace("{key}", update.key));
      }
      await updateSetting(metadata, update, target);
    }
    response.payload = settingsPayload(context, locale, theme, target);
    void vscode.window.showInformationMessage(settingsHostText(locale, "saved"));
  } catch (error) {
    response.error = error instanceof Error ? error.message : String(error);
  }
  await webview.postMessage(response);
}

function updateSetting(
  metadata: SettingsMetadata,
  update: AspSettingsUpdate,
  target: AspSettingsTarget,
): Thenable<void> {
  const { configuration, section } = configurationForMetadata(metadata, target);
  return configuration.update(
    section,
    update.reset ? undefined : update.value,
    configurationTarget(target.scope),
    metadata.languageOverride ? true : false,
  );
}

function settingsPayload(
  context: vscode.ExtensionContext,
  locale: AspSettingsLocale,
  theme: AspSettingsWebviewThemeSetting,
  target: AspSettingsTarget,
): AspSettingsWebviewPayload {
  const normalizedTarget = normalizeTarget(target);
  const settings = settingsMetadata(context, locale);
  const categories = settingsCategories(settings, locale);
  const values = Object.fromEntries(
    settings.map((metadata) => [metadata.key, valueState(metadata, normalizedTarget)]),
  );
  return {
    categories,
    locale,
    scopes: settingsScopes(),
    selectedScope: normalizedTarget,
    settings,
    theme,
    values,
  };
}

function settingsMetadata(context: vscode.ExtensionContext, locale: AspSettingsLocale): SettingsMetadata[] {
  const manifest = readJsonFile<AspSettingsManifest>(path.join(context.extensionPath, "package.json"));
  const nlsFile = locale === "ja" ? "package.nls.ja.json" : "package.nls.json";
  const nls = readJsonFile<Record<string, string>>(path.join(context.extensionPath, nlsFile));
  return allSettingsMetadata(manifest, nls, locale);
}

function valueState(metadata: SettingsMetadata, target: AspSettingsTarget): SettingsValueState {
  const { configuration, section } = configurationForMetadata(metadata, target);
  const inspection = configuration.inspect(section);
  const effectiveValue = configuration.get(section, metadata.defaultValue);
  return valueStateForTarget(metadata, inspection, effectiveValue, target.scope);
}

function configurationForMetadata(
  metadata: SettingsMetadata,
  target: AspSettingsTarget,
): { configuration: vscode.WorkspaceConfiguration; section: string } {
  const folderUri = target.folderUri ? vscode.Uri.parse(target.folderUri) : undefined;
  const scope = metadata.languageOverride
    ? { uri: folderUri, languageId: "classic-asp" }
    : folderUri;
  if (metadata.key.startsWith("aspLsp.")) {
    return {
      configuration: vscode.workspace.getConfiguration("aspLsp", scope),
      section: metadata.section,
    };
  }
  return {
    configuration: vscode.workspace.getConfiguration(undefined, scope),
    section: metadata.section,
  };
}

function settingsCategories(
  settings: SettingsMetadata[],
  locale: AspSettingsLocale,
): AspSettingsWebviewCategory[] {
  const counts = new Map<string, number>();
  for (const metadata of settings) {
    counts.set(metadata.category, (counts.get(metadata.category) ?? 0) + 1);
  }
  return [...counts.entries()].map(([id, total]) => ({
    id,
    label: settingsCategoryLabel(id, locale),
    total,
  }));
}

function settingsScopes(): AspSettingsWebviewScopes {
  const folders =
    vscode.workspace.workspaceFolders?.map((folder) => ({
      name: folder.name,
      uri: folder.uri.toString(),
    })) ?? [];
  return {
    folders,
    workspaceAvailable: folders.length > 0,
    workspaceFolderAvailable: folders.length > 0,
  };
}

function defaultSettingsTarget(): AspSettingsTarget {
  return {
    folderUri: vscode.workspace.workspaceFolders?.[0]?.uri.toString(),
    scope: "global",
  };
}

function normalizeTarget(target: AspSettingsTarget): AspSettingsTarget {
  const folders = vscode.workspace.workspaceFolders ?? [];
  const fallbackFolderUri = folders[0]?.uri.toString();
  if (target.scope === "workspaceFolder") {
    return {
      folderUri:
        folders.find((folder) => folder.uri.toString() === target.folderUri)?.uri.toString() ??
        fallbackFolderUri,
      scope: "workspaceFolder",
    };
  }
  return {
    folderUri: target.folderUri ?? fallbackFolderUri,
    scope: target.scope === "workspace" ? "workspace" : "global",
  };
}

function configurationTarget(scope: SettingsTargetScope): vscode.ConfigurationTarget {
  if (scope === "workspaceFolder") {
    return vscode.ConfigurationTarget.WorkspaceFolder;
  }
  if (scope === "workspace") {
    return vscode.ConfigurationTarget.Workspace;
  }
  return vscode.ConfigurationTarget.Global;
}

function settingsWebviewHtml(
  webview: vscode.Webview,
  webviewRoot: vscode.Uri,
  payload: AspSettingsWebviewPayload,
  title: string,
  locale: AspSettingsLocale,
): string {
  const nonce = nonceString();
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "settings.js"));
  const payloadJson = JSON.stringify(payload).replaceAll("</", "<\\/");
  return `<!doctype html>
<html lang="${locale}">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${webview.cspSource} data:; style-src ${webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';">
  <title>${escapeHtml(title)}</title>
</head>
<body>
  <div id="root"></div>
  <script nonce="${nonce}">window.__ASP_LSP_SETTINGS__ = ${payloadJson};</script>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}

function readJsonFile<T>(fileName: string): T {
  return JSON.parse(fs.readFileSync(fileName, "utf8")) as T;
}

function nonceString(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let value = "";
  for (let index = 0; index < 32; index += 1) {
    value += chars[Math.floor(Math.random() * chars.length)];
  }
  return value;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function settingsHostText(
  locale: AspSettingsLocale,
  key: "panelTitle" | "saved" | "unknownSetting",
): string {
  const messages = {
    en: {
      panelTitle: "Classic ASP Settings",
      saved: "Classic ASP settings saved.",
      unknownSetting: "Unknown Classic ASP setting: {key}",
    },
    ja: {
      panelTitle: "Classic ASP 設定",
      saved: "Classic ASP 設定を保存しました。",
      unknownSetting: "不明な Classic ASP 設定です: {key}",
    },
  };
  return messages[locale][key] ?? messages.en[key];
}
