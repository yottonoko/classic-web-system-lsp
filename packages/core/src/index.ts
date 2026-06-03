import { clearAspParseCaches } from "./parser";
import { clearPositionCaches } from "./position";
import { clearVbscriptCaches } from "./vbscript";
import { clearVirtualDocumentCaches } from "./virtual-documents";

export * from "./types";
export * from "./parser";
export * from "./virtual-documents";
export * from "./vbscript";
export * from "./formatter";
export * from "./localize";
export * from "./comments";

export function clearAspCoreCaches(): void {
  clearAspParseCaches();
  clearPositionCaches();
  clearVbscriptCaches();
  clearVirtualDocumentCaches();
}
