import type {
  WorkspaceFilesFile,
  WorkspaceFilesGlobStat,
  WorkspaceFilesPayload,
} from "../workspace-files-webview";
import type {
  GlobInputItem,
  GlobKind,
  Locale,
  Summary,
  TextKey,
  TreeRow,
} from "./workspace-files-types";

let nextGlobItemId = 0;

export function highlightRanges(text: string, normalizedQuery: string): Array<[number, number]> {
  const normalizedText = text.toLowerCase();
  const ranges: Array<[number, number]> = [];
  let cursor = 0;
  while (cursor < normalizedText.length) {
    const start = normalizedText.indexOf(normalizedQuery, cursor);
    if (start < 0) {
      break;
    }
    const end = start + normalizedQuery.length;
    ranges.push([start, end]);
    cursor = end;
  }
  return ranges;
}

export function isCollapsibleTreeRow(row: TreeRow): boolean {
  return row.kind === "folder" || row.kind === "root";
}

export function excludePatternForTreeRow(row: TreeRow): string | undefined {
  if (row.kind === "file") {
    return row.file.relativePath;
  }
  return row.kind === "folder" && row.detail ? `${row.detail}/**` : undefined;
}

export function visibleTreeRows(rows: TreeRow[], collapsedIds: ReadonlySet<string>): TreeRow[] {
  const visibleRows: TreeRow[] = [];
  let collapsedDepth: number | undefined;
  for (const row of rows) {
    if (collapsedDepth !== undefined) {
      if (row.depth > collapsedDepth) {
        continue;
      }
      collapsedDepth = undefined;
    }
    visibleRows.push(row);
    if (isCollapsibleTreeRow(row) && collapsedIds.has(row.id)) {
      collapsedDepth = row.depth;
    }
  }
  return visibleRows;
}

export function treeRows(payload: WorkspaceFilesPayload): TreeRow[] {
  const rows: TreeRow[] = [];
  for (const root of payload.roots) {
    if (root.files.length === 0) {
      continue;
    }
    const folderMatches = new Map<string, boolean>();
    for (const file of root.files) {
      if (!file.matchesFilter) {
        continue;
      }
      const parts = file.relativePath.split("/");
      for (let index = 0; index < parts.length - 1; index += 1) {
        folderMatches.set(parts.slice(0, index + 1).join("/"), true);
      }
    }
    rows.push({
      id: `root:${root.uri}`,
      kind: "root",
      depth: 0,
      label: root.displayPath ?? root.name,
      matchesFilter: root.files.some((file) => file.matchesFilter),
      detail: `${root.files.length}`,
    });
    const folderIds = new Set<string>();
    for (const file of [...root.files].sort((left, right) =>
      left.relativePath.localeCompare(right.relativePath),
    )) {
      const parts = file.relativePath.split("/");
      for (let index = 0; index < parts.length - 1; index += 1) {
        const folderPath = parts.slice(0, index + 1).join("/");
        const id = `folder:${root.uri}:${folderPath}`;
        if (!folderIds.has(id)) {
          folderIds.add(id);
          rows.push({
            id,
            kind: "folder",
            depth: index + 1,
            label: parts[index],
            matchesFilter: folderMatches.get(folderPath) === true,
            detail: folderPath,
          });
        }
      }
      rows.push({
        id: `file:${file.uri}`,
        kind: "file",
        depth: parts.length,
        label: parts.at(-1) ?? file.relativePath,
        matchesFilter: file.matchesFilter,
        detail: file.relativePath,
        file,
      });
    }
  }
  return rows;
}

export function summarizePayload(payload: WorkspaceFilesPayload): Summary {
  const folders = new Set<string>();
  let aspFiles = 0;
  let asaFiles = 0;
  let incFiles = 0;
  let latestModifiedMs = 0;
  for (const root of payload.roots) {
    for (const file of root.files) {
      const type = fileType(file);
      if (type === "ASP") {
        aspFiles += 1;
      } else if (type === "ASA") {
        asaFiles += 1;
      } else if (type === "INC") {
        incFiles += 1;
      }
      latestModifiedMs = Math.max(latestModifiedMs, file.mtimeMs);
      const parts = file.relativePath.split("/");
      for (let index = 0; index < parts.length - 1; index += 1) {
        folders.add(`${root.uri}:${parts.slice(0, index + 1).join("/")}`);
      }
    }
  }
  return { asaFiles, aspFiles, folders: folders.size, incFiles, latestModifiedMs };
}

export function globItems(globs: string[], kind: GlobKind): GlobInputItem[] {
  const items = globs.map((glob) => createGlobItem(kind, glob));
  return items.length > 0 ? items : [createGlobItem(kind, "")];
}

export function createGlobItem(kind: GlobKind, value: string): GlobInputItem {
  nextGlobItemId += 1;
  return { id: `${kind}:${nextGlobItemId}`, value };
}

export function globValues(items: GlobInputItem[]): string[] {
  return items.map((item) => item.value.trim()).filter((value) => value.length > 0);
}

export function globStatCount(
  stats: WorkspaceFilesGlobStat[] | undefined,
  index: number,
  value: string,
): number | undefined {
  const glob = value.trim();
  if (glob.length === 0) {
    return 0;
  }
  const stat = stats?.[index];
  return stat?.glob === glob ? stat.files : undefined;
}

export function globCountText(
  count: number | undefined,
  text: (key: TextKey, params?: Record<string, string | number>) => string,
): string {
  return count === undefined ? text("globPending") : text("fileCount", { count });
}

export function fileType(file: WorkspaceFilesFile): string {
  const extension = file.relativePath.split(".").at(-1)?.toUpperCase();
  return extension && extension !== file.relativePath.toUpperCase() ? extension : "ASP";
}

export function formatBytes(value: number): string {
  if (value < 1024) {
    return `${value} B`;
  }
  const units = ["KB", "MB", "GB"];
  let amount = value / 1024;
  for (const unit of units) {
    if (amount < 1024 || unit === units.at(-1)) {
      return `${amount.toFixed(amount >= 100 ? 0 : amount >= 10 ? 1 : 2)} ${unit}`;
    }
    amount /= 1024;
  }
  return `${value} B`;
}

export function formatDate(value: number, locale: Locale): string {
  return new Date(value).toLocaleString(locale);
}

export function formatDateShort(value: number, locale: Locale): string {
  return new Date(value).toLocaleDateString(locale, {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
}

export function formatNumber(value: number, locale: Locale): string {
  return new Intl.NumberFormat(locale).format(value);
}

export function emptyPayload(): WorkspaceFilesPayload {
  return {
    includeGlobs: ["**/*.{asp,asa,inc,vbs}"],
    excludeGlobs: [],
    globStats: {
      include: [{ glob: "**/*.{asp,asa,inc,vbs}", files: 0 }],
      exclude: [],
    },
    respectGitIgnore: false,
    roots: [],
    showUnmatched: true,
    stats: { files: 0, totalBytes: 0 },
  };
}
