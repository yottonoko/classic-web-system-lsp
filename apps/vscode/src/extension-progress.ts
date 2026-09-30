import * as vscode from "vscode";
import {
  CloseAction,
  ErrorAction,
  type ErrorHandler,
  type LanguageClient,
} from "vscode-languageclient/node";
import type { AspFlowchartLocale } from "./flowchart-webview";
import {
  progressTasksForActiveDocument,
  referenceProgressLabel,
  type ActiveProgressDocument,
} from "./reference-progress";
import { uriTextForVSCode } from "./uri-encoding";
import type {
  AspLspProgressTask,
  AspLspProgressTaskState,
  AspLspServerStatusKind,
} from "./protocol-types";
import type { ExtensionMessageArgs, ExtensionMessageKey } from "./extension-localization";
import { aggregateProgressValues, progressPercentage, progressValueText } from "./progress-values";

const maxCrashRestartCount = 4;
const crashRestartWindowMs = 3 * 60 * 1000;
const cancelProgressTaskServerCommand = "aspLsp.server.cancelProgressTask";

type ServerStatusKind = AspLspServerStatusKind;
type ProgressTaskState = AspLspProgressTaskState;

interface ProgressTask extends AspLspProgressTask {
  source: "server" | "extension";
}

interface ProgressQuickPickItem extends vscode.QuickPickItem {
  task?: ProgressTask;
}

interface ServerProgressReporterRegistration {
  id: number;
  startedAt: number;
  exactLabels: ReadonlySet<string>;
  labelPrefixes: readonly string[];
  progress: vscode.Progress<{ increment?: number; message?: string }>;
  claimedTaskId?: string;
  lastMessage?: string;
  lastPercentage: number;
}

export interface ExtensionProgressDependencies {
  getClient: () => LanguageClient | undefined;
  getStatusBarItem: () => vscode.StatusBarItem | undefined;
  isDeactivating: () => boolean;
  isManualRestarting: () => boolean;
  /** Called instead of the client's default message once automatic restarts stop. */
  onServerCrashLimit?: (crash: { count: number; minutes: number }) => void;
  localize: () => (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string;
  locale: () => AspFlowchartLocale;
  baseNameFromPath: (value: string | undefined) => string | undefined;
  baseNameFromUri: (value: string | undefined) => string | undefined;
}

export interface ExtensionProgressController {
  updateStatusBar(): void;
  handleServerStatusNotification(params: unknown): void;
  resetServerState(): void;
  resetCrashRestartHistory(): void;
  clearExtensionProgressTasks(): void;
  withServerTaskProgress<T>(
    options: vscode.ProgressOptions,
    selector: { exactLabels?: readonly string[]; labelPrefixes?: readonly string[] },
    operation: (
      progress: vscode.Progress<{ increment?: number; message?: string }>,
      token: vscode.CancellationToken,
    ) => Thenable<T> | Promise<T>,
  ): Promise<T>;
  beginExtensionProgressTask(
    kind: Exclude<ServerStatusKind, "idle">,
    label: string,
    options?: {
      detail?: string;
      current?: number;
      total?: number;
      cancellable?: boolean;
    },
  ): {
    update(update: Partial<Pick<ProgressTask, "label" | "detail" | "current" | "total">>): void;
    end(): void;
  };
  progressDetailFromUriText(uriText: string): string;
  showProgressDetails(): Promise<void>;
  createLanguageClientErrorHandler(): ErrorHandler;
}

export function createProgressController(
  dependencies: ExtensionProgressDependencies,
): ExtensionProgressController {
  const {
    getClient,
    getStatusBarItem,
    isDeactivating,
    isManualRestarting,
    onServerCrashLimit,
    localize,
    locale,
    baseNameFromPath,
    baseNameFromUri,
  } = dependencies;
  let serverStatusKind: ServerStatusKind = "idle";
  let serverProgressTasks: ProgressTask[] = [];
  let serverProgress: { current: number; total: number } | undefined;
  let extensionProgressTaskSequence = 0;
  const extensionProgressTasks = new Map<string, ProgressTask>();
  let serverProgressReporterSequence = 0;
  const serverProgressReporters = new Map<number, ServerProgressReporterRegistration>();
  const claimedServerProgressTaskIds = new Map<string, number>();
  let crashRestartTimestamps: number[] = [];

  function updateStatusBar(): void {
    const statusBarItem = getStatusBarItem();
    if (!statusBarItem) {
      return;
    }
    const localizer = localize();
    const tasks = activeProgressTasks();
    const primaryTask = primaryProgressTask(tasks);
    const progressText =
      progressValueText(progressFromTask(primaryTask)) ||
      progressSummaryText(tasks) ||
      progressValueText(serverProgress);
    const activeKind = activeStatusKind(tasks);
    if (activeKind === "loading") {
      statusBarItem.text = `$(sync~spin) ${progressStatusText(
        "status.loading.text",
        "status.progress.loadingStatusText",
        primaryTask,
        localizer,
      )}${progressText}`;
      statusBarItem.tooltip = progressTooltip(
        localizer("status.loading.tooltip"),
        tasks,
        localizer,
      );
      return;
    }
    if (activeKind === "analyzing") {
      statusBarItem.text = `$(loading~spin) ${progressStatusText(
        "status.analyzing.text",
        "status.progress.analyzingStatusText",
        primaryTask,
        localizer,
      )}${progressText}`;
      statusBarItem.tooltip = progressTooltip(
        localizer("status.analyzing.tooltip"),
        tasks,
        localizer,
      );
      return;
    }
    statusBarItem.text = "$(code) ASP LSP";
    statusBarItem.tooltip = localizer("status.tooltip");
  }

  function handleServerStatusNotification(params: unknown): void {
    const status = (params as { status?: unknown } | undefined)?.status;
    if (status !== "idle" && status !== "loading" && status !== "analyzing") {
      return;
    }
    serverStatusKind = status;
    serverProgress = progressFromStatusNotification(params);
    serverProgressTasks = progressTasksFromStatusNotification(params);
    reportServerProgressTasks(serverProgressTasks);
    updateStatusBar();
  }

  function clearServerProgressState(): void {
    serverProgress = undefined;
    serverProgressTasks = [];
  }

  function activeProgressTasks(): ProgressTask[] {
    const visibleServerProgressTasks = progressTasksForActiveDocument(
      serverProgressTasks,
      activeProgressDocument(),
    );
    const extensionFallbackTasks = [...extensionProgressTasks.values()].filter(
      (task) => !visibleServerProgressTasks.some((serverTask) => serverTask.label === task.label),
    );
    return [...visibleServerProgressTasks, ...extensionFallbackTasks];
  }

  function activeProgressDocument(): ActiveProgressDocument | undefined {
    const document = vscode.window.activeTextEditor?.document;
    if (!document) {
      return undefined;
    }
    return {
      uri: document.uri.toString(),
      version: document.version,
      label: baseNameFromPath(document.fileName) ?? document.uri.toString(),
    };
  }

  function activeStatusKind(tasks: ProgressTask[]): ServerStatusKind {
    if (tasks.some((task) => task.kind === "analyzing")) {
      return "analyzing";
    }
    if (tasks.some((task) => task.kind === "loading")) {
      return "loading";
    }
    if (serverProgressTasks.length > 0) {
      return "idle";
    }
    return serverStatusKind;
  }

  function progressSummaryText(tasks: ProgressTask[]): string | undefined {
    const progress = aggregateProgress(tasks);
    return progressValueText(progress) || undefined;
  }

  function progressStatusText(
    fallbackKey: ExtensionMessageKey,
    taskKey: ExtensionMessageKey,
    task: ProgressTask | undefined,
    localizer: (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string,
  ): string {
    if (!task) {
      return localizer(fallbackKey);
    }
    const label = progressTaskDisplayLabel(task.label, localizer);
    const detail = progressStatusBarDetail(task.detail);
    return localizer(taskKey, { task: detail ? `${label}: ${detail}` : label });
  }

  function primaryProgressTask(tasks: ProgressTask[]): ProgressTask | undefined {
    return [...tasks].sort(
      (left, right) =>
        progressTaskStatusPriority(right) - progressTaskStatusPriority(left) ||
        right.updatedAt - left.updatedAt ||
        right.startedAt - left.startedAt,
    )[0];
  }

  function progressTaskStatusPriority(task: ProgressTask): number {
    if (task.label.startsWith("references.")) {
      return 20;
    }
    return task.label.startsWith("excel.") ? 10 : 0;
  }

  function aggregateProgress(
    tasks: ProgressTask[],
  ): { current: number; total: number } | undefined {
    const measurable = tasks.filter(
      (task) => typeof task.current === "number" || typeof task.total === "number",
    );
    if (measurable.length === 0) {
      return undefined;
    }
    return aggregateProgressValues(
      measurable.map((task) => ({ current: task.current ?? 0, total: task.total ?? 0 })),
    );
  }

  function progressFromTask(
    task: ProgressTask | undefined,
  ): { current: number; total: number } | undefined {
    return task && typeof task.current === "number" && typeof task.total === "number"
      ? { current: task.current, total: task.total }
      : undefined;
  }

  function progressStatusBarDetail(detail: string | undefined): string | undefined {
    if (!detail) {
      return undefined;
    }
    const normalized = detail.replace(/\s+/g, " ").trim();
    if (normalized.length <= 72) {
      return normalized;
    }
    return `${normalized.slice(0, 34)}...${normalized.slice(-35)}`;
  }

  function withServerTaskProgress<T>(
    options: vscode.ProgressOptions,
    selector: { exactLabels?: readonly string[]; labelPrefixes?: readonly string[] },
    operation: (
      progress: vscode.Progress<{ increment?: number; message?: string }>,
      token: vscode.CancellationToken,
    ) => Thenable<T> | Promise<T>,
  ): Promise<T> {
    return Promise.resolve(
      vscode.window.withProgress(options, async (progress, token) => {
        const registration = registerServerProgressReporter(progress, selector);
        try {
          return await operation(progress, token);
        } finally {
          unregisterServerProgressReporter(registration.id);
        }
      }),
    );
  }

  function registerServerProgressReporter(
    progress: vscode.Progress<{ increment?: number; message?: string }>,
    selector: { exactLabels?: readonly string[]; labelPrefixes?: readonly string[] },
  ): ServerProgressReporterRegistration {
    const registration: ServerProgressReporterRegistration = {
      id: ++serverProgressReporterSequence,
      startedAt: Date.now(),
      exactLabels: new Set(selector.exactLabels ?? []),
      labelPrefixes: selector.labelPrefixes ?? [],
      progress,
      lastPercentage: 0,
    };
    serverProgressReporters.set(registration.id, registration);
    return registration;
  }

  function unregisterServerProgressReporter(id: number): void {
    const registration = serverProgressReporters.get(id);
    if (!registration) {
      return;
    }
    if (registration.claimedTaskId) {
      claimedServerProgressTaskIds.delete(registration.claimedTaskId);
    }
    serverProgressReporters.delete(id);
  }

  function clearServerProgressReporters(): void {
    claimedServerProgressTaskIds.clear();
    serverProgressReporters.clear();
  }

  function reportServerProgressTasks(tasks: readonly ProgressTask[]): void {
    for (const registration of serverProgressReporters.values()) {
      if (!registration.claimedTaskId) {
        continue;
      }
      const task = tasks.find((candidate) => candidate.id === registration.claimedTaskId);
      if (task) {
        reportServerProgressTask(registration, task);
      }
    }
    for (const task of tasks) {
      if (claimedServerProgressTaskIds.has(task.id)) {
        continue;
      }
      const registration = bestServerProgressReporter(task);
      if (!registration) {
        continue;
      }
      registration.claimedTaskId = task.id;
      claimedServerProgressTaskIds.set(task.id, registration.id);
      reportServerProgressTask(registration, task);
    }
  }

  function bestServerProgressReporter(
    task: ProgressTask,
  ): ServerProgressReporterRegistration | undefined {
    const candidates = [...serverProgressReporters.values()].filter(
      (registration) =>
        !registration.claimedTaskId &&
        task.updatedAt >= registration.startedAt &&
        serverProgressReporterMatchScore(registration, task) > 0,
    );
    return candidates.sort(
      (left, right) =>
        serverProgressReporterMatchScore(right, task) -
          serverProgressReporterMatchScore(left, task) || left.id - right.id,
    )[0];
  }

  function serverProgressReporterMatchScore(
    registration: ServerProgressReporterRegistration,
    task: ProgressTask,
  ): number {
    if (registration.exactLabels.has(task.label)) {
      return 2;
    }
    return registration.labelPrefixes.some((prefix) => task.label.startsWith(prefix)) ? 1 : 0;
  }

  function reportServerProgressTask(
    registration: ServerProgressReporterRegistration,
    task: ProgressTask,
  ): void {
    const localizer = localize();
    const label = progressTaskDisplayLabel(task.label, localizer);
    const detail = task.detail || task.activeItems?.join(", ");
    const value = progressValueText(progressFromTask(task));
    const message = `${label}${detail ? `: ${detail}` : ""}${value}`;
    const percentage = progressPercentage(progressFromTask(task));
    const increment = Math.max(0, percentage - registration.lastPercentage);
    if (message === registration.lastMessage && increment === 0) {
      return;
    }
    registration.progress.report({ message, increment: increment > 0 ? increment : undefined });
    registration.lastMessage = message;
    registration.lastPercentage = Math.max(registration.lastPercentage, percentage);
  }

  function progressTooltip(
    fallback: string,
    tasks: ProgressTask[],
    localizer: (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string,
  ): string {
    if (tasks.length === 0) {
      return fallback;
    }
    return tasks
      .map((task) => {
        const progress = progressValueText(
          typeof task.current === "number" && typeof task.total === "number"
            ? { current: task.current, total: task.total }
            : undefined,
        ).trim();
        const state =
          task.state === "cancelling" ? ` ${localizer("status.progress.cancelling")}` : "";
        const detail = task.detail ? ` - ${task.detail}` : "";
        const label = progressTaskDisplayLabel(task.label, localizer);
        return `${label}${progress ? ` ${progress}` : ""}${state}${detail}`;
      })
      .join("\n");
  }

  function progressFromStatusNotification(
    params: unknown,
  ): { current: number; total: number } | undefined {
    const progress = (params as { progress?: unknown } | undefined)?.progress;
    if (!progress || typeof progress !== "object") {
      return undefined;
    }
    const current = (progress as { current?: unknown }).current;
    const total = (progress as { total?: unknown }).total;
    return typeof current === "number" && typeof total === "number"
      ? { current, total }
      : undefined;
  }

  function progressTasksFromStatusNotification(params: unknown): ProgressTask[] {
    const tasks = (params as { tasks?: unknown } | undefined)?.tasks;
    if (!Array.isArray(tasks)) {
      return [];
    }
    return tasks.flatMap((task) => {
      const parsed = progressTaskFromStatusNotification(task);
      return parsed ? [parsed] : [];
    });
  }

  function progressTaskFromStatusNotification(task: unknown): ProgressTask | undefined {
    if (!task || typeof task !== "object") {
      return undefined;
    }
    const record = task as Record<string, unknown>;
    const id = record.id;
    const kind = record.kind;
    const label = record.label;
    if (
      typeof id !== "string" ||
      (kind !== "loading" && kind !== "analyzing") ||
      typeof label !== "string"
    ) {
      return undefined;
    }
    const state = progressTaskState(record.state);
    const activeItems = Array.isArray(record.activeItems)
      ? record.activeItems.filter((item): item is string => typeof item === "string")
      : undefined;
    const startedAt = typeof record.startedAt === "number" ? record.startedAt : Date.now();
    return {
      id,
      kind,
      label,
      detail: typeof record.detail === "string" ? record.detail : undefined,
      current: typeof record.current === "number" ? record.current : undefined,
      total: typeof record.total === "number" ? record.total : undefined,
      activeItems,
      cancellable: record.cancellable === true,
      state,
      startedAt,
      updatedAt: typeof record.updatedAt === "number" ? record.updatedAt : startedAt,
      documentUri: progressDocumentUri(record.documentUri),
      documentVersion:
        typeof record.documentVersion === "number" ? record.documentVersion : undefined,
      source: "server",
    };
  }

  function progressTaskState(value: unknown): ProgressTaskState {
    switch (value) {
      case "cancelling":
      case "completed":
      case "cancelled":
      case "failed":
      case "stale":
        return value;
      default:
        return "running";
    }
  }

  function progressDocumentUri(value: unknown): string | undefined {
    if (typeof value !== "string") {
      return undefined;
    }
    try {
      return vscode.Uri.parse(uriTextForVSCode(value)).toString();
    } catch {
      return undefined;
    }
  }

  function beginExtensionProgressTask(
    kind: Exclude<ServerStatusKind, "idle">,
    label: string,
    options: {
      detail?: string;
      current?: number;
      total?: number;
      cancellable?: boolean;
    } = {},
  ): {
    update(update: Partial<Pick<ProgressTask, "label" | "detail" | "current" | "total">>): void;
    end(): void;
  } {
    const id = `extension-${++extensionProgressTaskSequence}`;
    const startedAt = Date.now();
    extensionProgressTasks.set(id, {
      id,
      kind,
      label,
      detail: options.detail,
      current: options.current,
      total: options.total,
      cancellable: options.cancellable === true,
      state: "running",
      startedAt,
      updatedAt: startedAt,
      source: "extension",
    });
    updateStatusBar();
    return {
      update(update) {
        const task = extensionProgressTasks.get(id);
        if (!task) {
          return;
        }
        if (update.detail !== undefined) {
          task.detail = update.detail;
        }
        if (update.label !== undefined) {
          task.label = update.label;
        }
        if (update.current !== undefined) {
          task.current = update.current;
        }
        if (update.total !== undefined) {
          task.total = update.total;
        }
        task.updatedAt = Date.now();
        updateStatusBar();
      },
      end() {
        extensionProgressTasks.delete(id);
        updateStatusBar();
      },
    };
  }

  async function showProgressDetails(): Promise<void> {
    const localizer = localize();
    const quickPick = vscode.window.createQuickPick<ProgressQuickPickItem>();
    quickPick.title = localizer("status.progress.title");
    quickPick.placeholder = localizer("status.progress.placeholder");
    quickPick.matchOnDescription = true;
    quickPick.matchOnDetail = true;
    const refresh = (): void => {
      quickPick.items = progressQuickPickItems(localizer);
    };
    refresh();
    const refreshTimer = setInterval(refresh, 500);
    quickPick.onDidTriggerItemButton(async (event) => {
      const task = event.item.task;
      if (!task || !task.cancellable || task.state === "cancelling") {
        return;
      }
      await cancelProgressTask(task);
      refresh();
    });
    quickPick.onDidHide(() => {
      clearInterval(refreshTimer);
      quickPick.dispose();
    });
    quickPick.show();
  }

  function progressQuickPickItems(
    localizer: (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string,
  ): ProgressQuickPickItem[] {
    const tasks = activeProgressTasks();
    const taskItems = tasks.map((task): ProgressQuickPickItem => {
      const progress = progressValueText(
        typeof task.current === "number" && typeof task.total === "number"
          ? { current: task.current, total: task.total }
          : undefined,
      ).trim();
      const state =
        task.state === "cancelling" ? ` ${localizer("status.progress.cancelling")}` : "";
      const activeItems =
        task.activeItems && task.activeItems.length > 0
          ? `\n${localizer("status.progress.active")}: ${task.activeItems.join(", ")}`
          : "";
      return {
        label: `${progressTaskDisplayLabel(task.label, localizer)}${progress ? ` ${progress}` : ""}${state}`,
        description: task.detail,
        detail: activeItems || undefined,
        task,
        buttons:
          task.cancellable && task.state !== "cancelling"
            ? [
                {
                  iconPath: new vscode.ThemeIcon("close"),
                  tooltip: localizer("status.progress.cancel"),
                },
              ]
            : undefined,
      };
    });
    return [
      ...(taskItems.length > 0
        ? taskItems
        : [{ label: localizer("status.progress.none"), alwaysShow: true }]),
    ];
  }

  function progressTaskDisplayLabel(
    label: string,
    localizer: (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string,
  ): string {
    const referenceLabel = referenceProgressLabel(label, locale());
    if (referenceLabel) {
      return referenceLabel;
    }
    const excelGraphStageKey = excelGraphProgressStageLabelKey(label);
    if (excelGraphStageKey) {
      return `${localizer("status.progress.excelGraph")}: ${localizer(excelGraphStageKey)}`;
    }
    const key = progressTaskDisplayLabelKey(label);
    return key ? localizer(key) : label;
  }

  function excelGraphProgressStageLabelKey(label: string): ExtensionMessageKey | undefined {
    const match = /^excel\.graph\.(?:document|folder|workspace)\.(.+)$/.exec(label);
    switch (match?.[1]) {
      case "collectDocuments":
        return "status.progress.graphLoadDocuments";
      case "filterDocuments":
      case "prepareDocuments":
        return "status.progress.graphPrepareDocuments";
      case "indexDeclarations":
      case "indexMembers":
      case "indexTypes":
        return "status.progress.graphIndexDocuments";
      case "addFiles":
      case "addDeclarations":
        return "status.progress.graphAddStructure";
      case "resolveIncludes":
        return "status.progress.graphResolveIncludes";
      case "linkMembers":
      case "linkReferences":
      case "linkUnresolved":
        return "status.progress.graphAddUsages";
      default:
        return undefined;
    }
  }

  function progressTaskDisplayLabelKey(label: string): ExtensionMessageKey | undefined {
    switch (label) {
      case "workspace.diagnostics":
        return "status.progress.workspaceDiagnostics";
      case "workspace.diagnostics.indexed":
        return "status.progress.workspaceDiagnosticsIndexed";
      case "workspace.diagnostics.openDocuments":
        return "status.progress.workspaceDiagnosticsOpenDocuments";
      case "diagnostics":
        return "status.progress.diagnostics";
      case "diagnostics.include":
        return "status.progress.diagnosticsInclude";
      case "diagnostics.syntax":
        return "status.progress.diagnosticsSyntax";
      case "diagnostics.projectFast":
        return "status.progress.diagnosticsProjectFast";
      case "diagnostics.project":
        return "status.progress.diagnosticsProject";
      case "document.analysis":
        return "status.progress.documentAnalysis";
      case "document.analysis.incremental":
        return "status.progress.documentAnalysisIncremental";
      case "document.analysis.parse":
        return "status.progress.documentAnalysisParse";
      case "document.analysis.cache":
        return "status.progress.documentAnalysisCache";
      case "document.analysis.ready":
        return "status.progress.documentAnalysisReady";
      case "workspace.index":
        return "status.progress.workspaceIndex";
      case "workspace.index.failed":
        return "status.progress.workspaceIndexFailed";
      case "workspace.index.waitDocuments":
        return "status.progress.workspaceIndexWaitDocuments";
      case "workspace.index.parseFiles":
        return "status.progress.workspaceIndexParseFiles";
      case "workspace.index.readCache":
        return "status.progress.workspaceIndexReadCache";
      case "workspace.index.catalog":
        return "status.progress.workspaceIndexCatalog";
      case "workspace.index.scanRoot":
        return "status.progress.workspaceIndexScanRoot";
      case "workspace.index.scanFiles":
        return "status.progress.workspaceIndexScanFiles";
      case "workspace.index.writeCache":
        return "status.progress.workspaceIndexWriteCache";
      case "workspace.index.includeGraph":
        return "status.progress.workspaceIndexIncludeGraph";
      case "workspace.index.finalize":
        return "status.progress.workspaceIndexFinalize";
      case "workspace.previewFiles":
        return "status.progress.workspacePreviewFiles";
      case "flowchart.build":
        return "status.progress.flowchart";
      case "flowchart.loadDocument":
        return "status.progress.flowchartLoadDocument";
      case "flowchart.hydrateDocument":
        return "status.progress.flowchartHydrateDocument";
      case "flowchart.collectIncludes":
        return "status.progress.flowchartCollectIncludes";
      case "flowchart.indexDocuments":
        return "status.progress.flowchartIndexDocuments";
      case "flowchart.canonicalizeSymbols":
        return "status.progress.flowchartCanonicalizeSymbols";
      case "flowchart.buildPayload":
        return "status.progress.flowchartBuildPayload";
      case "navigationGraph.build":
        return "status.progress.navigationGraph";
      case "navigationGraph.collectDocuments":
        return "status.progress.navigationGraphCollectDocuments";
      case "navigationGraph.resolveIncludes":
        return "status.progress.navigationGraphResolveIncludes";
      case "navigationGraph.extract":
        return "status.progress.navigationGraphExtract";
      case "navigationGraph.buildPayload":
        return "status.progress.navigationGraphBuildPayload";
      case "graph.document":
        return "status.progress.graphDocument";
      case "graph.folder":
        return "status.progress.graphFolder";
      case "graph.workspace":
        return "status.progress.graphWorkspace";
      case "graph.workspaceIndex":
        return "status.progress.graphWorkspaceIndex";
      case "graph.openDocuments":
        return "status.progress.graphOpenDocuments";
      case "graph.prepareDocuments":
        return "status.progress.graphPrepareDocuments";
      case "graph.loadDocuments":
        return "status.progress.graphLoadDocuments";
      case "graph.collectIncludes":
        return "status.progress.graphCollectIncludes";
      case "graph.prefetchIncludes":
        return "status.progress.graphPrefetchIncludes";
      case "graph.resolveIncludes":
        return "status.progress.graphResolveIncludes";
      case "graph.collectRelatedIncludes":
        return "status.progress.graphCollectRelatedIncludes";
      case "graph.checkRelatedIncludes":
        return "status.progress.graphCheckRelatedIncludes";
      case "graph.collectIncomingIncludes":
        return "status.progress.graphCollectIncomingIncludes";
      case "graph.findIncomingIncludes":
        return "status.progress.graphFindIncomingIncludes";
      case "graph.reverseIncludeIndex":
        return "status.progress.graphReverseIncludeIndex";
      case "graph.filterIncomingIncludes":
        return "status.progress.graphFilterIncomingIncludes";
      case "graph.indexDocuments":
        return "status.progress.graphIndexDocuments";
      case "graph.spillIndexes":
        return "status.progress.graphSpillIndexes";
      case "graph.canonicalizeSymbols":
        return "status.progress.graphCanonicalizeSymbols";
      case "graph.addStructure":
        return "status.progress.graphAddStructure";
      case "graph.addUsages":
        return "status.progress.graphAddUsages";
      case "graph.finalize":
        return "status.progress.graphFinalize";
      case "excel.graph":
        return "status.progress.excelGraph";
      case "excel.normalizeGraph":
        return "status.progress.excelNormalizeGraph";
      case "excel.analysisContext":
        return "status.progress.excelAnalysisContext";
      case "excel.analysisSummary":
        return "status.progress.excelAnalysisSummary";
      case "excel.sheet":
        return "status.progress.excelSheet";
      case "excel.chooseFile":
        return "status.progress.excelChooseFile";
      case "excel.sheets":
        return "status.progress.excelSheets";
      case "excel.workbook":
        return "status.progress.excelWorkbook";
      case "excel.file":
        return "status.progress.excelFile";
      case "excel.fileSheet":
        return "status.progress.excelFileSheet";
      case "excel.fileRows":
        return "status.progress.excelFileRows";
      case "excel.fileCommit":
        return "status.progress.excelFileCommit";
      case "excel.export":
      case "excel.write":
        return "status.progress.excel";
      default:
        return undefined;
    }
  }

  function progressDetailFromUriText(uriText: string): string {
    return baseNameFromUri(uriText) ?? uriText;
  }

  async function cancelProgressTask(task: ProgressTask): Promise<void> {
    if (task.source === "extension") {
      const current = extensionProgressTasks.get(task.id);
      if (current) {
        current.state = "cancelling";
        current.updatedAt = Date.now();
        updateStatusBar();
      }
      return;
    }
    await getClient()?.sendRequest("workspace/executeCommand", {
      command: cancelProgressTaskServerCommand,
      arguments: [{ id: task.id }],
    });
  }

  function createLanguageClientErrorHandler(): ErrorHandler {
    return {
      error(_error, _message, count) {
        if (count && count <= 3) {
          return { action: ErrorAction.Continue };
        }
        return { action: ErrorAction.Shutdown };
      },
      closed() {
        serverStatusKind = "idle";
        clearServerProgressState();
        clearServerProgressReporters();
        updateStatusBar();
        if (isDeactivating() || isManualRestarting()) {
          return { action: CloseAction.DoNotRestart, handled: true };
        }
        crashRestartTimestamps.push(Date.now());
        if (crashRestartTimestamps.length <= maxCrashRestartCount) {
          return { action: CloseAction.Restart };
        }
        const elapsedMs =
          crashRestartTimestamps[crashRestartTimestamps.length - 1] - crashRestartTimestamps[0];
        if (elapsedMs <= crashRestartWindowMs) {
          if (onServerCrashLimit) {
            const count = crashRestartTimestamps.length;
            crashRestartTimestamps = [];
            onServerCrashLimit({ count, minutes: crashRestartWindowMs / 60_000 });
            return { action: CloseAction.DoNotRestart, handled: true };
          }
          return {
            action: CloseAction.DoNotRestart,
            message:
              "The Classic ASP Language Server crashed 5 times in the last 3 minutes. The server will not be restarted. See the output for more information.",
          };
        }
        crashRestartTimestamps.shift();
        return { action: CloseAction.Restart };
      },
    };
  }

  return {
    updateStatusBar,
    handleServerStatusNotification,
    resetServerState() {
      serverStatusKind = "idle";
      clearServerProgressState();
      clearServerProgressReporters();
      updateStatusBar();
    },
    resetCrashRestartHistory() {
      crashRestartTimestamps = [];
    },
    clearExtensionProgressTasks() {
      extensionProgressTasks.clear();
      updateStatusBar();
    },
    withServerTaskProgress,
    beginExtensionProgressTask,
    progressDetailFromUriText,
    showProgressDetails,
    createLanguageClientErrorHandler,
  };
}
