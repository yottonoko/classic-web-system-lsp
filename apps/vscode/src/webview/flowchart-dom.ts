import { useLayoutEffect, useRef, useState } from "react";
import type React from "react";
import type { AspFlowchartNode } from "@asp-lsp/core";
import { clamp, flowchartNodeHint, mermaidId } from "./flowchart-model";
import type { FlowchartPayload } from "./flowchart-types";

export function attachSvgNodeHandlers(
  container: HTMLDivElement,
  payload: FlowchartPayload,
  text: (key: string) => string,
  onOpenContextMenu: (node: AspFlowchartNode, event: MouseEvent) => void,
  onHoverNode: (nodeId: string | undefined) => void,
  onOpenFlowchart: (node: AspFlowchartNode) => void,
): void {
  const locale = payload.locale ?? "en";
  const elementsByNodeId = svgElementsByFlowchartNodeId(container, payload.nodes);
  for (const node of payload.nodes) {
    for (const element of elementsByNodeId.get(node.id) ?? []) {
      const hint = flowchartNodeHint(node, text, locale);
      element.setAttribute("aria-label", hint);
      element.querySelector("title")?.remove();
      element.style.cursor = "pointer";
      element.addEventListener("mouseenter", () => onHoverNode(node.id));
      element.addEventListener("mouseleave", () => onHoverNode(undefined));
      element.addEventListener("click", () => {
        onOpenFlowchart(node);
      });
      element.addEventListener("contextmenu", (event) => onOpenContextMenu(node, event));
    }
  }
}

export function syncSvgSearchHighlights(
  container: HTMLDivElement,
  viewport: HTMLDivElement,
  payload: FlowchartPayload,
  matchedNodeIds: Set<string>,
  activeNodeId: string | undefined,
): void {
  for (const element of container.querySelectorAll<SVGGElement>(
    ".asp-lsp-flowchart-match, .asp-lsp-flowchart-active",
  )) {
    element.classList.remove("asp-lsp-flowchart-match", "asp-lsp-flowchart-active");
  }
  let activeElement: SVGGElement | undefined;
  const elementsByNodeId = svgElementsByFlowchartNodeId(container, payload.nodes);
  for (const node of payload.nodes) {
    if (!matchedNodeIds.has(node.id) && node.id !== activeNodeId) {
      continue;
    }
    for (const element of elementsByNodeId.get(node.id) ?? []) {
      if (matchedNodeIds.has(node.id)) {
        element.classList.add("asp-lsp-flowchart-match");
      }
      if (node.id === activeNodeId) {
        element.classList.add("asp-lsp-flowchart-active");
        activeElement = element;
      }
    }
  }
  if (activeElement) {
    scrollFlowchartElementIntoViewport(activeElement, viewport);
  }
}

export function scrollFlowchartElementIntoViewport(
  element: SVGGraphicsElement,
  viewport: HTMLElement,
): void {
  const elementRect = element.getBoundingClientRect();
  const viewportRect = viewport.getBoundingClientRect();
  const nextLeft =
    viewport.scrollLeft +
    elementRect.left +
    elementRect.width / 2 -
    viewportRect.left -
    viewport.clientWidth / 2;
  const nextTop =
    viewport.scrollTop +
    elementRect.top +
    elementRect.height / 2 -
    viewportRect.top -
    viewport.clientHeight / 2;
  viewport.scrollTo({
    left: Math.max(0, nextLeft),
    top: Math.max(0, nextTop),
  });
}

export function clampedContextMenuPosition(x: number, y: number): { left: number; top: number } {
  const margin = 8;
  const estimatedWidth = 180;
  const estimatedHeight = 120;
  return {
    left: clamp(x, margin, Math.max(margin, window.innerWidth - estimatedWidth - margin)),
    top: clamp(y, margin, Math.max(margin, window.innerHeight - estimatedHeight - margin)),
  };
}

export function serializedFlowchartSvg(container: HTMLDivElement | null): string | undefined {
  const svgElement = container?.querySelector<SVGSVGElement>("svg");
  if (!svgElement) {
    return undefined;
  }
  const clone = svgElement.cloneNode(true) as SVGSVGElement;
  if (!clone.getAttribute("xmlns")) {
    clone.setAttribute("xmlns", "http://www.w3.org/2000/svg");
  }
  if (!clone.getAttribute("xmlns:xlink")) {
    clone.setAttribute("xmlns:xlink", "http://www.w3.org/1999/xlink");
  }
  return `${new XMLSerializer().serializeToString(clone)}\n`;
}

export function svgElementsByFlowchartNodeId(
  container: HTMLDivElement,
  nodes: readonly AspFlowchartNode[],
): Map<string, SVGGElement[]> {
  const nodeMatches = new Map(nodes.map((node) => [node.id, [] as SVGGElement[]]));
  const fallbackMatches = new Map(nodes.map((node) => [node.id, [] as SVGGElement[]]));
  const nodeIds = nodes.map((node) => ({ id: node.id, mermaidId: mermaidId(node.id) }));
  for (const element of container.querySelectorAll<SVGGElement>("g[id]")) {
    for (const node of nodeIds) {
      if (!svgElementIdContainsMermaidNodeId(element.id, node.mermaidId)) {
        continue;
      }
      const matches = element.classList.contains("node") ? nodeMatches : fallbackMatches;
      matches.get(node.id)?.push(element);
    }
  }
  return new Map<string, SVGGElement[]>(
    nodes.map((node): [string, SVGGElement[]] => {
      const preferred = nodeMatches.get(node.id) ?? [];
      return [node.id, preferred.length > 0 ? preferred : (fallbackMatches.get(node.id) ?? [])];
    }),
  );
}

function svgElementIdContainsMermaidNodeId(elementId: string, mermaidNodeId: string): boolean {
  const index = elementId.indexOf(mermaidNodeId);
  if (index < 0) {
    return false;
  }
  const before = index === 0 ? "" : elementId[index - 1];
  const after = elementId[index + mermaidNodeId.length] ?? "";
  return isMermaidIdBoundary(before) && isMermaidIdBoundary(after);
}

function isMermaidIdBoundary(value: string): boolean {
  return !value || !/[A-Za-z0-9_]/.test(value);
}

export function useElementSize<TElement extends HTMLElement>(): [
  React.RefObject<TElement | null>,
  { width: number; height: number },
] {
  const ref = useRef<TElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });

  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) {
      return undefined;
    }

    const updateSize = () => {
      const { width, height } = element.getBoundingClientRect();
      const nextSize = {
        width: Math.max(1, Math.floor(width)),
        height: Math.max(1, Math.floor(height)),
      };
      setSize((currentSize) =>
        currentSize.width === nextSize.width && currentSize.height === nextSize.height
          ? currentSize
          : nextSize,
      );
    };

    updateSize();
    const observer = new ResizeObserver(updateSize);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  return [ref, size];
}
