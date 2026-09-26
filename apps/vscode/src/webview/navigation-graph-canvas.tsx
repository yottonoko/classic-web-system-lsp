import { createEffect, createMemo, createSignal, For, onSettled, type Accessor } from "solid-js";
import type { JSX } from "@solidjs/web";
import type {
  NavigationFlowEdge,
  NavigationFlowGroup,
  NavigationFlowNode,
} from "./navigation-graph-layout";
/** Imperative viewport operations used by the graph toolbar. */
export interface NavigationViewportController {
  fitView(options?: {
    nodes?: {
      id: string;
    }[];
    padding?: number;
    maxZoom?: number;
    duration?: number;
    includeHiddenNodes?: boolean;
  }): void;
}
interface NavigationGraphCanvasProps {
  nodes: NavigationFlowNode[];
  edges: NavigationFlowEdge[];
  groups: NavigationFlowGroup[];
  locale: "en" | "ja";
  onController(controller: NavigationViewportController): void;
  onClear(): void;
  renderNode(node: Accessor<NavigationFlowNode>): JSX.Element;
  renderEdge(edge: Accessor<NavigationFlowEdge>, zoom: Accessor<number>): JSX.Element;
}
/** Solid SVG viewport with explicit directed paths, keyboard controls and a navigable overview. */
export function NavigationGraphCanvas(props: NavigationGraphCanvasProps): JSX.Element {
  let container: HTMLDivElement | undefined;
  let svg: SVGSVGElement | undefined;
  const [size, setSize] = createSignal({ width: 1, height: 1 });
  const [view, setView] = createSignal({ x: 0, y: 0, zoom: 1 });
  let drag:
    | {
        x: number;
        y: number;
        view: ReturnType<typeof view>;
        moved: boolean;
      }
    | undefined;
  let suppressClick = false;
  const bounds = createMemo(() => graphBounds(props.nodes, props.edges, props.groups));
  const text = (en: string, ja: string) => (props.locale === "ja" ? ja : en);
  const controller: NavigationViewportController = {
    fitView(options = {}) {
      const ids = options.nodes ? new Set(options.nodes.map((node) => node.id)) : undefined;
      const nodes = ids ? props.nodes.filter((node) => ids.has(node.id)) : props.nodes;
      if (!nodes.length) return;
      const rect = graphBounds(
        nodes,
        props.edges.filter((edge) => !ids || (ids.has(edge.source) && ids.has(edge.target))),
        ids ? [] : props.groups,
      );
      const padding = options.padding ?? 0.05;
      const zoom = Math.max(
        0.02,
        Math.min(
          options.maxZoom ?? 1.1,
          size().width / (rect.width * (1 + padding * 2)),
          size().height / (rect.height * (1 + padding * 2)),
        ),
      );
      setView({
        zoom,
        x: size().width / 2 - (rect.x + rect.width / 2) * zoom,
        y: size().height / 2 - (rect.y + rect.height / 2) * zoom,
      });
    },
  };
  const zoomAt = (factor: number, x = size().width / 2, y = size().height / 2) => {
    const current = view();
    const zoom = Math.max(0.02, Math.min(2.2, current.zoom * factor));
    setView({
      zoom,
      x: x - ((x - current.x) * zoom) / current.zoom,
      y: y - ((y - current.y) * zoom) / current.zoom,
    });
  };
  onSettled(() => {
    props.onController(controller);
    if (!container || !svg) return;
    const observer = new ResizeObserver(([entry]) =>
      setSize({ width: entry.contentRect.width, height: entry.contentRect.height }),
    );
    observer.observe(container);
    const wheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = svg!.getBoundingClientRect();
      zoomAt(Math.exp(-event.deltaY * 0.002), event.clientX - rect.left, event.clientY - rect.top);
    };
    const element = svg;
    element.addEventListener("wheel", wheel, { passive: false });
    return () => {
      observer.disconnect();
      element.removeEventListener("wheel", wheel);
    };
  });
  const geometryKey = createMemo(() =>
    props.nodes
      .map(
        (node) =>
          `${node.id}:${node.position.x}:${node.position.y}:${node.style.width}:${node.style.height}`,
      )
      .join(";"),
  );
  createEffect(
    () => [geometryKey(), size()] as const,
    () => controller.fitView(),
  );
  const orderedEdges = createMemo(() =>
    [...props.edges].sort((a, b) => (a.zIndex ?? 0) - (b.zIndex ?? 0)),
  );
  return (
    <div
      class="navigation-graph-viewport"
      ref={(element) => {
        container = element;
      }}
    >
      <svg
        ref={(element) => {
          svg = element;
        }}
        class="navigation-graph-svg"
        aria-label={text("Page transitions", "ページ遷移")}
        tabindex="0"
        onPointerDown={(event) => {
          if (
            event.button !== 0 ||
            (event.target instanceof Element && event.target.closest('button, [role="button"]'))
          )
            return;
          event.currentTarget.setPointerCapture(event.pointerId);
          drag = { x: event.clientX, y: event.clientY, view: view(), moved: false };
        }}
        onPointerMove={(event) => {
          if (!drag) return;
          const dx = event.clientX - drag.x,
            dy = event.clientY - drag.y;
          if (Math.abs(dx) + Math.abs(dy) > 3) drag.moved = true;
          setView({ ...drag.view, x: drag.view.x + dx, y: drag.view.y + dy });
        }}
        onPointerUp={() => {
          suppressClick = drag?.moved ?? false;
          drag = undefined;
        }}
        onPointerCancel={() => {
          drag = undefined;
        }}
        onClick={(event) => {
          if (suppressClick) {
            suppressClick = false;
            return;
          }
          if (!(event.target instanceof Element && event.target.closest('button, [role="button"]')))
            props.onClear();
        }}
        onKeyDown={(event) => {
          if (event.target !== event.currentTarget) return;
          if (event.key === "+" || event.key === "=") {
            event.preventDefault();
            zoomAt(1.25);
          }
          if (event.key === "-") {
            event.preventDefault();
            zoomAt(0.8);
          }
          if (event.key === "0") {
            event.preventDefault();
            controller.fitView();
          }
          if (event.key === "Escape") props.onClear();
          const delta = {
            ArrowLeft: [60, 0],
            ArrowRight: [-60, 0],
            ArrowUp: [0, 60],
            ArrowDown: [0, -60],
          }[event.key];
          if (delta) {
            event.preventDefault();
            setView((current) => ({
              ...current,
              x: current.x + delta[0],
              y: current.y + delta[1],
            }));
          }
        }}
      >
        <g
          class="navigation-graph-transform"
          transform={`translate(${view().x} ${view().y}) scale(${view().zoom})`}
        >
          <For each={props.groups}>
            {(group) => (
              <g class="navigation-component-group">
                <rect x={group.x} y={group.y} width={group.width} height={group.height} rx="14" />
                <text x={group.x + 20} y={group.y + 27}>
                  {group.isolated
                    ? text("Pages without transitions", "つながりのないページ")
                    : group.label}{" "}
                  · {group.nodeCount} {text("pages", "ページ")} / {group.edgeCount}{" "}
                  {text("transitions", "遷移")}
                </text>
              </g>
            )}
          </For>
          <For each={orderedEdges()} keyed={(edge) => edge.id}>
            {(edge) => props.renderEdge(edge, () => view().zoom)}
          </For>
          <For each={props.nodes} keyed={(node) => node.id}>
            {(node) => (
              <foreignObject
                x={node().position.x}
                y={node().position.y}
                width={node().style.width}
                height={node().style.height}
                class={node().className}
                data-node-id={node().id}
              >
                {props.renderNode(node)}
              </foreignObject>
            )}
          </For>
        </g>
      </svg>
      <div class="navigation-viewport-controls">
        <button type="button" aria-label={text("Zoom in", "拡大")} onClick={() => zoomAt(1.25)}>
          +
        </button>
        <button type="button" aria-label={text("Zoom out", "縮小")} onClick={() => zoomAt(0.8)}>
          −
        </button>
        <button
          type="button"
          aria-label={text("Fit graph", "グラフ全体を表示")}
          onClick={() => controller.fitView()}
        >
          □
        </button>
        <span>{Math.round(view().zoom * 100)}%</span>
      </div>
      <svg
        class="navigation-minimap"
        viewBox={`${bounds().x} ${bounds().y} ${bounds().width} ${bounds().height}`}
        aria-label={text("Graph overview", "グラフの全体図")}
        onClick={(event) => {
          const matrix = event.currentTarget.getScreenCTM();
          if (!matrix) return;
          const point = new DOMPoint(event.clientX, event.clientY).matrixTransform(
            matrix.inverse(),
          );
          setView((current) => ({
            ...current,
            x: size().width / 2 - point.x * current.zoom,
            y: size().height / 2 - point.y * current.zoom,
          }));
        }}
      >
        <For each={props.edges}>
          {(edge) => (
            <path d={edge.data.path} fill="none" stroke="var(--navigation-edge)" stroke-width="3" />
          )}
        </For>
        <For each={props.nodes}>
          {(node) => (
            <rect
              x={node.position.x}
              y={node.position.y}
              width={node.style.width}
              height={node.style.height}
              fill={`var(--navigation-${node.data.node.kind})`}
            />
          )}
        </For>
        <rect
          class="navigation-minimap-view"
          x={-view().x / view().zoom}
          y={-view().y / view().zoom}
          width={size().width / view().zoom}
          height={size().height / view().zoom}
        />
      </svg>
    </div>
  );
}
function graphBounds(
  nodes: NavigationFlowNode[],
  edges: NavigationFlowEdge[] = [],
  groups: NavigationFlowGroup[] = [],
): {
  x: number;
  y: number;
  width: number;
  height: number;
} {
  if (!nodes.length) return { x: 0, y: 0, width: 720, height: 520 };
  let x = Infinity,
    y = Infinity,
    right = -Infinity,
    bottom = -Infinity;
  for (const node of nodes) {
    x = Math.min(x, node.position.x);
    y = Math.min(y, node.position.y);
    right = Math.max(right, node.position.x + node.style.width);
    bottom = Math.max(bottom, node.position.y + node.style.height);
  }
  for (const group of groups) {
    x = Math.min(x, group.x);
    y = Math.min(y, group.y);
    right = Math.max(right, group.x + group.width);
    bottom = Math.max(bottom, group.y + group.height);
  }
  for (const edge of edges) {
    for (const point of edge.data.points ?? []) {
      x = Math.min(x, point.x);
      y = Math.min(y, point.y);
      right = Math.max(right, point.x);
      bottom = Math.max(bottom, point.y);
    }
    if (edge.data.labelX !== undefined && edge.data.labelY !== undefined) {
      x = Math.min(x, edge.data.labelX - 70);
      right = Math.max(right, edge.data.labelX + 70);
      y = Math.min(y, edge.data.labelY - 14);
      bottom = Math.max(bottom, edge.data.labelY + 14);
    }
  }
  return { x: x - 35, y: y - 35, width: right - x + 70, height: bottom - y + 70 };
}
