import type { WorkspaceFilesFile } from "../workspace-files-webview";

export type TreeRow =
  | {
      id: string;
      kind: "root";
      depth: number;
      label: string;
      matchesFilter: boolean;
      detail?: string;
    }
  | {
      id: string;
      kind: "folder";
      depth: number;
      label: string;
      matchesFilter: boolean;
      detail?: string;
    }
  | {
      id: string;
      kind: "file";
      depth: number;
      label: string;
      matchesFilter: boolean;
      detail?: string;
      file: WorkspaceFilesFile;
    };

export type Summary = {
  aspFiles: number;
  asaFiles: number;
  folders: number;
  incFiles: number;
  latestModifiedMs: number;
};

export type GlobInputItem = {
  id: string;
  value: string;
};

export type GlobKind = "exclude" | "include";

export type TreeContextMenu = {
  x: number;
  y: number;
  pattern: string;
};

export type Locale = "en" | "ja";
export type TextKey =
  | "action.addGlob"
  | "action.excludePattern"
  | "action.export"
  | "action.removeGlob"
  | "action.saveSettings"
  | "analysisOverview"
  | "currentFilters"
  | "empty"
  | "excludeGlobs"
  | "fileCount"
  | "files"
  | "filters"
  | "folder"
  | "foldersScanned"
  | "fullPath"
  | "globPending"
  | "includeGlobs"
  | "lastModified"
  | "lastScanned"
  | "name"
  | "noSelection"
  | "none"
  | "open"
  | "previewFailed"
  | "projectRoot"
  | "relativePath"
  | "respectGitIgnore"
  | "search"
  | "selectedFile"
  | "settingsSaveFailed"
  | "settingsSaved"
  | "showUnmatched"
  | "size"
  | "savingSettings"
  | "title"
  | "totalSize"
  | "type"
  | "workspace";
