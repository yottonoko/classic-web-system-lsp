import { createMemo, createEffect } from "solid-js";
import { type JSX } from "@solidjs/web";
import { type WebviewEvent } from "./webview-dom-types";
import { HighlightedCodeView } from "./highlighted-code-view";
import type { CodeAnnotation, HighlightedCode } from "./highlighted-code";
import { cn } from "../lib/utils";
import {
  flowchartPaneResizeKeyboardStep,
  flowchartSourceActiveAnnotationName,
  vscodeThemeColor,
} from "./flowchart-runtime";
import { EmptyText } from "./flowchart-primitives";
import { shouldScrollFlowchartSource } from "./flowchart-source-scroll";
import { highlightFlowchartSourceWithSetting } from "./flowchart-source-highlight";
import { clamp } from "./flowchart-model";
import type {
  FlowchartSourceActiveKind,
  FlowchartSourceHighlight,
  FlowchartSourceRange,
  FlowchartSourceScrollTarget,
  InfoPanelPosition,
  WebviewTheme,
  WebviewThemeSetting,
} from "./flowchart-types";
import type { AspFlowchartNode, AspFlowchartSection } from "../protocol-types";
export function FlowchartPaneResizeHandle(props: {
  className?: string;
  label: string;
  maxWidth: number;
  minWidth: number;
  onWidthChange(width: number): void;
  position: InfoPanelPosition;
  width: number;
}): JSX.Element {
  const updateWidth = (nextWidth: number) =>
    props.onWidthChange(clamp(nextWidth, props.minWidth, props.maxWidth));
  const handlePointerDown = (event: WebviewEvent<PointerEvent, HTMLDivElement>) => {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = props.width;
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    const handlePointerMove = (moveEvent: PointerEvent) => {
      moveEvent.preventDefault();
      const deltaX = moveEvent.clientX - startX;
      updateWidth(props.position === "left" ? startWidth + deltaX : startWidth - deltaX);
    };
    const stopResize = () => {
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", stopResize);
      window.removeEventListener("pointercancel", stopResize);
    };
    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", stopResize);
    window.addEventListener("pointercancel", stopResize);
  };
  const handleKeyDown = (event: WebviewEvent<KeyboardEvent, HTMLDivElement>) => {
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      updateWidth(
        props.width +
          (props.position === "left"
            ? -flowchartPaneResizeKeyboardStep
            : flowchartPaneResizeKeyboardStep),
      );
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      updateWidth(
        props.width +
          (props.position === "left"
            ? flowchartPaneResizeKeyboardStep
            : -flowchartPaneResizeKeyboardStep),
      );
    } else if (event.key === "Home") {
      event.preventDefault();
      updateWidth(props.minWidth);
    } else if (event.key === "End") {
      event.preventDefault();
      updateWidth(props.maxWidth);
    }
  };
  return (
    <div
      role="separator"
      tabindex={0}
      aria-label={props.label}
      aria-orientation="vertical"
      aria-valuemin={props.minWidth}
      aria-valuemax={props.maxWidth}
      aria-valuenow={props.width}
      title={props.label}
      class={cn(
        "relative z-10 w-[9px] justify-self-center cursor-col-resize bg-transparent outline-none before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-[#263140] hover:bg-[#172131] focus:bg-[#172131] focus:before:bg-[#7dd3fc]",
        props.className ?? "order-2",
      )}
      onKeyDown={handleKeyDown}
      onPointerDown={handlePointerDown}
    />
  );
}
export function FlowchartSourcePanel(props: {
  activeLabel?: string;
  className?: string;
  highlights: readonly FlowchartSourceHighlight[];
  nodes: readonly AspFlowchartNode[];
  scrollTarget?: FlowchartSourceScrollTarget;
  sourceText?: string;
  text(key: string): string;
  theme: WebviewTheme;
  themeRevision: number;
  themeSetting?: WebviewThemeSetting;
  onHoverNode(nodeId: string | undefined): void;
  onOpenCode(range: FlowchartSourceRange): void;
  onSelectNode(node: AspFlowchartNode): void;
}): JSX.Element {
  const preRef = { current: null } as {
    current: (HTMLPreElement | null) | null;
  };
  const consumedScrollKeysRef = { current: new Set<string>() };
  const highlightedCode = createMemo(() => {
    void props.themeRevision;
    return props.sourceText
      ? highlightFlowchartSourceWithSetting(
          props.sourceText,
          props.theme,
          props.themeSetting,
          vscodeThemeColor,
        )
      : undefined;
  });
  const scrollLineNumber = createMemo(() =>
    flowchartSourceFirstLineNumber(props.scrollTarget?.ranges),
  );
  const highlightedCodeWithActiveRange = createMemo(() => {
    const code = highlightedCode();
    return code ? flowchartHighlightedCodeWithSourceHighlights(code, props.highlights) : undefined;
  });
  const selectSourceLineNode = (lineNumber: number) => {
    const node = flowchartNodeForSourceLine(props.nodes, lineNumber);
    if (node) {
      props.onSelectNode(node);
    }
  };
  const handleSourceCodeClick = (event: WebviewEvent<MouseEvent, HTMLElement>) => {
    const lineNumber = sourceLineNumberFromEvent(event);
    if (lineNumber !== undefined) {
      selectSourceLineNode(lineNumber);
    }
  };
  const handleSourceCodeMouseMove = (event: WebviewEvent<MouseEvent, HTMLElement>) => {
    const lineNumber = sourceLineNumberFromEvent(event);
    props.onHoverNode(
      lineNumber === undefined
        ? undefined
        : flowchartNodeForSourceLine(props.nodes, lineNumber)?.id,
    );
  };
  const handleSourceCodeDoubleClick = (event: WebviewEvent<MouseEvent, HTMLElement>) => {
    const lineNumber = sourceLineNumberFromEvent(event);
    const node =
      lineNumber === undefined ? undefined : flowchartNodeForSourceLine(props.nodes, lineNumber);
    if (node?.range) {
      props.onOpenCode(node.range);
    }
  };
  const sourceLineClass = (lineNumber: number) => {
    const annotation = highlightedCodeWithActiveRange()
      ?.annotations.slice()
      .reverse()
      .find(
        (item) =>
          item.name === flowchartSourceActiveAnnotationName &&
          item.fromLineNumber <= lineNumber &&
          item.toLineNumber >= lineNumber,
      );
    return annotation
      ? `${flowchartSourceActiveBlockClassName(flowchartSourceAnnotationKind(annotation))} ${flowchartSourceActiveLineClassName(flowchartSourceAnnotationKind(annotation))}`
      : "";
  };
  createEffect(
    () => [highlightedCodeWithActiveRange(), scrollLineNumber(), props.scrollTarget],
    () => {
      if (!props.scrollTarget || !scrollLineNumber() || !preRef.current) {
        return;
      }
      if (!shouldScrollFlowchartSource(consumedScrollKeysRef.current, props.scrollTarget)) {
        return;
      }
      const target = props.scrollTarget;
      const line = scrollLineNumber()!;
      const frame = window.requestAnimationFrame(() => {
        const pre = preRef.current;
        if (pre && scrollSourceLineIntoView(pre, line)) {
          consumedScrollKeysRef.current.add(target.key);
        }
      });
      return () => window.cancelAnimationFrame(frame);
    },
  );
  return (
    <aside class={cn("flex min-h-0 min-w-0 flex-col bg-[#101820]", props.className)}>
      <header class="border-b border-[#263140] px-3 py-2">
        <div class="flex min-w-0 items-center gap-2">
          <div class="min-w-0 flex-1 truncate text-xs font-semibold uppercase tracking-wide text-[#9fb0c5]">
            {props.text("source")}
          </div>
          {props.activeLabel ? (
            <div
              class="min-w-0 flex-1 truncate text-right text-[11px] text-[#c4d4e8]"
              title={props.activeLabel}
            >
              {props.activeLabel}
            </div>
          ) : null}
        </div>
      </header>
      <div
        class="min-h-0 flex-1 overflow-hidden"
        onClick={handleSourceCodeClick}
        onDblClick={handleSourceCodeDoubleClick}
        onMouseLeave={() => props.onHoverNode(undefined)}
        onMouseMove={handleSourceCodeMouseMove}
      >
        {props.sourceText ? (
          highlightedCodeWithActiveRange() ? (
            <HighlightedCodeView
              ref={(element) => {
                preRef.current = element;
              }}
              code={highlightedCodeWithActiveRange()!}
              lineClass={sourceLineClass}
              class="asp-lsp-source-code h-full overflow-auto bg-[#0c1117] p-3 text-xs leading-5"
            />
          ) : (
            <pre
              ref={(element) => (preRef.current = element)}
              class="asp-lsp-source-code h-full overflow-auto bg-[#0c1117] p-3 text-xs leading-5 text-[#d9e0ea]"
            >
              <FlowchartSourcePlainText sourceText={props.sourceText} />
            </pre>
          )
        ) : (
          <EmptyText>{props.text("sourceEmpty")}</EmptyText>
        )}
      </div>
    </aside>
  );
}
function FlowchartSourcePlainText(props: { sourceText: string }): JSX.Element {
  return (
    <>
      {props.sourceText.split(/\r\n|\r|\n/).map((line, index) => (
        <span class="asp-lsp-source-line" data-source-line={index + 1}>
          {line}
        </span>
      ))}
    </>
  );
}
function sourceLineNumberFromEvent(
  event: WebviewEvent<MouseEvent, HTMLElement>,
): number | undefined {
  const target = event.target instanceof Element ? event.target : undefined;
  const line = target?.closest<HTMLElement>("[data-source-line]");
  return sourceLineNumber(line?.dataset.sourceLine);
}
function sourceLineNumber(value: string | undefined): number | undefined {
  const lineNumber = value ? Number(value) : NaN;
  return Number.isInteger(lineNumber) && lineNumber > 0 ? lineNumber : undefined;
}
function flowchartNodeForSourceLine(
  nodes: readonly AspFlowchartNode[],
  lineNumber: number,
): AspFlowchartNode | undefined {
  const sourceLine = lineNumber - 1;
  return nodes
    .filter(
      (
        node,
      ): node is AspFlowchartNode & {
        range: FlowchartSourceRange;
      } =>
        Boolean(
          node.range &&
          node.kind !== "start" &&
          node.kind !== "end" &&
          flowchartRangeContainsSourceLine(node.range, sourceLine),
        ),
    )
    .sort(
      (left, right) =>
        flowchartSourceRangeLength(left.range) - flowchartSourceRangeLength(right.range),
    )[0];
}
function flowchartRangeContainsSourceLine(range: FlowchartSourceRange, line: number): boolean {
  return range.start.line <= line && line <= flowchartSourceHighlightEndLine(range);
}
function flowchartSourceHighlightEndLine(range: FlowchartSourceRange): number {
  return range.end.character === 0 && range.end.line > range.start.line
    ? range.end.line - 1
    : range.end.line;
}
function flowchartHighlightedCodeWithSourceHighlights(
  code: HighlightedCode,
  highlights: readonly FlowchartSourceHighlight[],
): HighlightedCode {
  const annotations = code.annotations.filter(
    (annotation) => annotation.name !== flowchartSourceActiveAnnotationName,
  );
  for (const highlight of flowchartSourceHighlightsByPriority(highlights)) {
    for (const range of highlight.ranges) {
      annotations.push(flowchartSourceActiveAnnotation(range, highlight.kind));
    }
  }
  return { ...code, annotations };
}
function flowchartSourceActiveAnnotation(
  range: FlowchartSourceRange,
  kind: FlowchartSourceActiveKind,
): CodeAnnotation {
  return {
    name: flowchartSourceActiveAnnotationName,
    query: kind,
    fromLineNumber: range.start.line + 1,
    toLineNumber: flowchartSourceHighlightEndLine(range) + 1,
    data: { kind },
  };
}
function flowchartSourceFirstLineNumber(
  ranges: readonly FlowchartSourceRange[] | undefined,
): number | undefined {
  return ranges?.[0] ? ranges[0].start.line + 1 : undefined;
}
function flowchartSourceAnnotationKind(annotation: CodeAnnotation): FlowchartSourceActiveKind {
  const kind = annotation.data?.kind;
  return kind === "hover" || kind === "selection" || kind === "section" ? kind : "section";
}
function flowchartSourceActiveBlockClassName(kind: FlowchartSourceActiveKind): string {
  return `asp-lsp-source-active-block asp-lsp-source-active-block--${kind}`;
}
function flowchartSourceActiveLineClassName(kind: FlowchartSourceActiveKind): string {
  return `asp-lsp-source-active-line asp-lsp-source-active-line--${kind}`;
}
function scrollSourceLineIntoView(container: HTMLElement, lineNumber: number): boolean {
  const line = container.querySelector<HTMLElement>(`[data-source-line="${lineNumber}"]`);
  if (!line) {
    return false;
  }
  const containerRect = container.getBoundingClientRect();
  const lineRect = line.getBoundingClientRect();
  const isAbove = lineRect.top < containerRect.top;
  const isBelow = lineRect.bottom > containerRect.bottom;
  if (!isAbove && !isBelow) {
    return true;
  }
  const nextTop =
    container.scrollTop +
    lineRect.top +
    lineRect.height / 2 -
    containerRect.top -
    container.clientHeight / 2;
  container.scrollTo({ top: Math.max(0, nextTop), behavior: "smooth" });
  return true;
}
export function flowchartSourceHighlights(
  selectedSection: AspFlowchartSection | undefined,
  selectedSectionNodes: readonly AspFlowchartNode[],
  allNodes: readonly AspFlowchartNode[],
  hoveredNodeId: string | undefined,
  selectedNodeId: string | undefined,
): FlowchartSourceHighlight[] {
  const highlights: FlowchartSourceHighlight[] = [];
  const nodesById = flowchartNodesById(allNodes);
  const sectionRanges = flowchartSourceRangesForSection(selectedSection, selectedSectionNodes);
  if (sectionRanges.length > 0) {
    highlights.push({ kind: "section", label: selectedSection?.label, ranges: sectionRanges });
  }
  const selectedNode = flowchartNodeById(nodesById, selectedNodeId);
  if (selectedNode?.range) {
    highlights.push({ kind: "selection", label: selectedNode.label, ranges: [selectedNode.range] });
  }
  const hoveredNode = flowchartNodeById(nodesById, hoveredNodeId);
  if (hoveredNode?.range) {
    highlights.push({ kind: "hover", label: hoveredNode.label, ranges: [hoveredNode.range] });
  }
  return highlights;
}
export function flowchartPrimarySourceHighlight(
  highlights: readonly FlowchartSourceHighlight[],
): FlowchartSourceHighlight | undefined {
  return [...highlights].sort(
    (left, right) =>
      flowchartSourceHighlightPriority(right.kind) - flowchartSourceHighlightPriority(left.kind),
  )[0];
}
function flowchartSourceHighlightsByPriority(
  highlights: readonly FlowchartSourceHighlight[],
): FlowchartSourceHighlight[] {
  return [...highlights].sort(
    (left, right) =>
      flowchartSourceHighlightPriority(left.kind) - flowchartSourceHighlightPriority(right.kind),
  );
}
function flowchartSourceHighlightPriority(kind: FlowchartSourceActiveKind): number {
  switch (kind) {
    case "hover":
      return 3;
    case "selection":
      return 2;
    case "section":
      return 1;
  }
}
function flowchartNodesById(nodes: readonly AspFlowchartNode[]): Map<string, AspFlowchartNode> {
  return new Map(nodes.map((node) => [node.id, node]));
}
function flowchartNodeById(
  nodesById: Map<string, AspFlowchartNode>,
  id: string | undefined,
): AspFlowchartNode | undefined {
  return id ? nodesById.get(id) : undefined;
}
function flowchartSourceRangesForSection(
  section: AspFlowchartSection | undefined,
  nodes: readonly AspFlowchartNode[],
): FlowchartSourceRange[] {
  if (!section) {
    return [];
  }
  if (section.kind !== "topLevel") {
    return section.range ? [section.range] : [];
  }
  return mergeFlowchartSourceRanges(
    nodes
      .filter((node) => node.kind !== "start" && node.kind !== "end")
      .map((node) => node.range)
      .filter((range): range is FlowchartSourceRange => Boolean(range)),
  );
}
function mergeFlowchartSourceRanges(
  ranges: readonly FlowchartSourceRange[],
): FlowchartSourceRange[] {
  const sorted = [...ranges].sort(compareFlowchartSourceRangeStart);
  const merged: FlowchartSourceRange[] = [];
  for (const range of sorted) {
    const current = merged.at(-1);
    if (!current || !flowchartSourceRangesTouchOrOverlap(current, range)) {
      merged.push(cloneFlowchartSourceRange(range));
      continue;
    }
    current.end = laterFlowchartPosition(current.end, range.end);
  }
  return merged;
}
function cloneFlowchartSourceRange(range: FlowchartSourceRange): FlowchartSourceRange {
  return {
    start: { ...range.start },
    end: { ...range.end },
  };
}
function compareFlowchartSourceRangeStart(
  left: FlowchartSourceRange,
  right: FlowchartSourceRange,
): number {
  return compareFlowchartPosition(left.start, right.start);
}
function flowchartSourceRangesTouchOrOverlap(
  left: FlowchartSourceRange,
  right: FlowchartSourceRange,
): boolean {
  return (
    compareFlowchartPosition(right.start, left.end) <= 0 || right.start.line <= left.end.line + 1
  );
}
function laterFlowchartPosition(
  left: FlowchartSourceRange["end"],
  right: FlowchartSourceRange["end"],
): FlowchartSourceRange["end"] {
  return compareFlowchartPosition(left, right) >= 0 ? left : right;
}
function compareFlowchartPosition(
  left: FlowchartSourceRange["start"],
  right: FlowchartSourceRange["start"],
): number {
  return left.line === right.line ? left.character - right.character : left.line - right.line;
}
function flowchartSourceRangeLength(range: NonNullable<AspFlowchartNode["range"]>): number {
  return (
    (range.end.line - range.start.line) * 1000000 + range.end.character - range.start.character
  );
}
