declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};

export const vscode = acquireVsCodeApi();

export const flowchartPaneResizeKeyboardStep = 16;
export const flowchartSourcePanelMinimumWidth = 280;
export const flowchartSourceActiveAnnotationName = "flowchartSourceActive";

export function vscodeThemeColor(name: string): string | undefined {
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(`--vscode-${name}`)
    .trim();
  return value || undefined;
}
