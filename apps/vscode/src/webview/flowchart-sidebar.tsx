import { createSignal, createMemo, createEffect } from "solid-js";
import { type JSX } from "@solidjs/web";
import { webviewStyle } from "./webview-dom-types";
import { HighlightedCodeView } from "./highlighted-code-view";
import { VirtualList } from "./virtual-list";
import { cn } from "../lib/utils";
import { vscode } from "./flowchart-runtime";
import { EmptyText } from "./flowchart-primitives";
import { highlightFlowchartOutputFragment } from "./flowchart-source-highlight";
import {
  clamp,
  detailParts,
  flowchartNodeHint,
  flowchartNodeKindLabel,
  flowchartNodeLinkHint,
  flowchartNodeLinkStyle,
  flowchartNodeVisualStyle,
  flowchartSectionHint,
  flowchartSwatchStyle,
} from "./flowchart-model";
import type { FlowchartLocale, FlowchartThemePalette, WebviewTheme } from "./flowchart-types";
import type {
  AspFlowchartInclude,
  AspFlowchartLabelMode,
  AspFlowchartNode,
  AspFlowchartOutputFragment,
  AspFlowchartSection,
  AspFlowchartTarget,
} from "../protocol-types";
export function SidebarAccordionSection(props: {
  children: JSX.Element;
  count?: number;
  hint?: string;
  text(key: string): string;
  title: string;
}): JSX.Element {
  const [open, setOpen] = createSignal(true);
  const headerTitle = createMemo(() =>
    detailParts(props.title, props.hint, props.text(open() ? "collapseSection" : "expandSection")),
  );
  return (
    <section class="mb-4 min-w-0 overflow-hidden rounded border border-[#263140] bg-[#101820]">
      <div class="flex items-center gap-2">
        <button
          aria-expanded={open() == null ? undefined : open() ? "true" : "false"}
          aria-label={headerTitle()}
          class="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left hover:bg-[#172131]"
          type="button"
          onClick={() => setOpen((current) => !current)}
        >
          <span class="h-6 w-6 shrink-0 rounded border border-[#334255] text-center text-xs leading-6 text-[#c4d4e8]">
            {open() ? "▾" : "▸"}
          </span>
          <span class="min-w-0 flex-1 truncate text-xs font-semibold uppercase tracking-wide text-[#9fb0c5]">
            {props.title}
          </span>
          {typeof props.count === "number" ? (
            <span class="shrink-0 rounded border border-[#334255] px-1.5 py-0.5 text-[11px] text-[#9fb0c5]">
              {props.count}
            </span>
          ) : null}
        </button>
        {props.hint ? (
          <div class="shrink-0 pr-2">
            <FlowchartHint hint={props.hint} label={props.title} />
          </div>
        ) : null}
      </div>
      {open() ? <div class="border-t border-[#263140] p-2">{props.children}</div> : null}
    </section>
  );
}
interface TooltipPosition {
  left: number;
  top: number;
  maxWidth: number;
}
export function FlowchartHint(props: { hint: string; label: string }): JSX.Element {
  const triggerRef = { current: null } as {
    current: HTMLSpanElement | null;
  };
  const tooltipRef = { current: null } as {
    current: HTMLSpanElement | null;
  };
  const [visible, setVisible] = createSignal(false);
  const [position, setPosition] = createSignal<TooltipPosition>();
  const showTooltip = () => {
    setVisible(true);
  };
  const hideTooltip = () => {
    setVisible(false);
    setPosition(undefined);
  };
  createEffect(
    () => [visible()],
    () => {
      if (!visible()) {
        return undefined;
      }
      const updatePosition = (): void => {
        setPosition(tooltipPositionFor(triggerRef.current, tooltipRef.current));
      };
      updatePosition();
      window.addEventListener("resize", updatePosition);
      window.addEventListener("scroll", updatePosition, true);
      return () => {
        window.removeEventListener("resize", updatePosition);
        window.removeEventListener("scroll", updatePosition, true);
      };
    },
  );
  return (
    <span class="group relative inline-flex shrink-0 items-center">
      <span
        ref={(element) => (triggerRef.current = element)}
        tabindex={0}
        aria-label={`${props.label}: ${props.hint}`}
        class="inline-grid h-3.5 w-3.5 cursor-help place-items-center rounded-full border border-[#405068] text-[10px] leading-none text-[#8d98a8] outline-none hover:border-[#89ddff] hover:text-[#d7dde8] focus:border-[#89ddff] focus:text-[#d7dde8]"
        onBlur={hideTooltip}
        onFocus={showTooltip}
        onPointerEnter={showTooltip}
        onPointerLeave={hideTooltip}
      >
        ?
      </span>
      {visible() ? (
        <span
          ref={(element) => (tooltipRef.current = element)}
          role="tooltip"
          class={cn(
            "pointer-events-none fixed z-[1000] rounded-md border border-[#405068] bg-[#0d1117] px-2 py-1.5 text-[11px] leading-[1.35] whitespace-normal text-[#d7dde8] shadow-[0_10px_24px_rgb(0_0_0_/_35%)]",
            position() ? "visible" : "invisible",
          )}
          style={webviewStyle({
            left: position()?.left ?? -9999,
            top: position()?.top ?? -9999,
            maxWidth: position()?.maxWidth ?? tooltipMaximumWidth(),
          })}
        >
          {props.hint}
        </span>
      ) : null}
    </span>
  );
}
function tooltipPositionFor(
  element: HTMLElement | null,
  tooltip: HTMLElement | null,
): TooltipPosition | undefined {
  if (!element || !element.isConnected) {
    return undefined;
  }
  const margin = 12;
  const gap = 6;
  const rect = element.getBoundingClientRect();
  if (!Number.isFinite(rect.left) || !Number.isFinite(rect.top)) {
    return undefined;
  }
  const maxWidth = tooltipMaximumWidth();
  const tooltipRect = tooltip?.isConnected ? tooltip.getBoundingClientRect() : undefined;
  const tooltipWidth = Math.min(tooltipRect?.width ?? maxWidth, maxWidth);
  const tooltipHeight = tooltipRect?.height ?? 80;
  const left = clamp(
    rect.left + rect.width / 2 - tooltipWidth / 2,
    margin,
    Math.max(margin, window.innerWidth - tooltipWidth - margin),
  );
  const belowTop = rect.bottom + gap;
  const aboveTop = rect.top - gap - tooltipHeight;
  const top =
    belowTop + tooltipHeight + margin <= window.innerHeight || aboveTop < margin
      ? clamp(belowTop, margin, Math.max(margin, window.innerHeight - tooltipHeight - margin))
      : aboveTop;
  return { left, top, maxWidth };
}
function tooltipMaximumWidth(): number {
  const margin = 12;
  return Math.max(160, Math.min(280, window.innerWidth - margin * 2));
}
export function IncludeList(props: {
  includes: AspFlowchartInclude[];
  labelMode: AspFlowchartLabelMode;
  text(key: string): string;
  uri: string;
}): JSX.Element {
  return (
    <>
      {" "}
      {props.includes.length === 0 ? (
        <EmptyText>{props.text("emptyIncludes")}</EmptyText>
      ) : (
        <VirtualList
          className="grid gap-2"
          estimateSize={84}
          getKey={(include, index) => `${include.mode}:${include.path}:${index}`}
          items={props.includes}
          maxHeight={280}
          overscan={8}
          renderItem={(include) => (
            <div class="min-w-0 overflow-hidden rounded border border-[#263140] bg-[#101820] p-2">
              <div class="flex min-w-0 items-start justify-between gap-2">
                <button
                  class="min-w-0 flex-1 text-left text-sm font-medium text-[#8ec7ff] hover:underline disabled:cursor-not-allowed disabled:text-[#7b8796] disabled:no-underline"
                  disabled={!include.exists || !include.resolvedUri}
                  title={detailParts(props.text("openFlowchart"), include.path, include.actualPath)}
                  type="button"
                  onClick={() =>
                    include.resolvedUri &&
                    vscode.postMessage({
                      type: "openIncludeFlowchart",
                      uri: include.resolvedUri,
                      labelMode: props.labelMode,
                    })
                  }
                >
                  <span class="block overflow-hidden text-ellipsis whitespace-nowrap">
                    {include.path}
                  </span>
                </button>
                {include.exists === false ? (
                  <span class="text-xs text-[#ffb4a8]">{props.text("missing")}</span>
                ) : null}
              </div>
              <div class="mt-1 flex min-w-0 items-center justify-between gap-2 text-xs text-[#9fb0c5]">
                <span class="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
                  {include.mode}
                </span>
                <button
                  class="shrink-0 text-[#c4d4e8] hover:text-white hover:underline"
                  title={detailParts(props.text("openDirective"), include.path)}
                  type="button"
                  onClick={() =>
                    vscode.postMessage({ type: "openRange", uri: props.uri, range: include.range })
                  }
                >
                  {props.text("openDirective")}
                </button>
              </div>
              {include.actualPath ? (
                <div
                  class="mt-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#7d8ca1]"
                  title={include.actualPath}
                >
                  {include.actualPath}
                </div>
              ) : null}
            </div>
          )}
        />
      )}
    </>
  );
}
export function FlowSection(props: {
  locale: FlowchartLocale;
  nodes: AspFlowchartNode[];
  selected: boolean;
  section: AspFlowchartSection;
  shouldAutoOpen: boolean;
  theme: WebviewTheme;
  themePalette: FlowchartThemePalette;
  text(key: string): string;
  activeSearchNodeId?: string;
  matchedNodeIds: Set<string>;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenTarget(target: AspFlowchartTarget): void;
  onSelect(): void;
  onSelectNode(node: AspFlowchartNode): void;
}): JSX.Element {
  const visibleNodes = createMemo(() =>
    props.nodes.filter((node) => node.kind !== "start" && node.kind !== "end"),
  );
  const [open, setOpen] = createSignal(false);
  const sectionHint = createMemo(() =>
    flowchartSectionHint(props.section, props.text, props.locale),
  );
  const headerTitle = createMemo(() =>
    detailParts(
      props.section.label,
      sectionHint(),
      props.text(open() ? "collapseSection" : "expandSection"),
    ),
  );
  createEffect(
    () => [props.shouldAutoOpen],
    () => {
      if (props.shouldAutoOpen) {
        setOpen(true);
      }
    },
  );
  const activeNodeIndex = createMemo(() =>
    visibleNodes().findIndex((node) => node.id === props.activeSearchNodeId),
  );
  return (
    <div
      class={cn(
        "mb-3 min-w-0 overflow-hidden rounded border bg-[#101820]",
        props.selected ? "border-[#6fb6ff]" : "border-[#263140]",
      )}
    >
      <div class="flex items-center gap-2 border-b border-[#263140] px-2 py-1.5">
        <button
          aria-expanded={open() == null ? undefined : open() ? "true" : "false"}
          aria-label={headerTitle()}
          class="h-6 w-6 shrink-0 rounded border border-[#334255] text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white"
          type="button"
          onClick={() => setOpen((current) => !current)}
        >
          {open() ? "▾" : "▸"}
        </button>
        <button
          aria-label={detailParts(
            props.text("selectFlowchart"),
            props.section.label,
            sectionHint(),
          )}
          class="min-w-0 flex-1 truncate text-left text-xs font-semibold text-[#9fb0c5] hover:text-[#f1f5f9]"
          type="button"
          onClick={props.onSelect}
        >
          <span>{props.section.label}</span>
        </button>
        <FlowchartHint hint={sectionHint()} label={props.section.label} />
        <button
          class="shrink-0 rounded border border-[#334255] px-2 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#6fb6ff] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
          disabled={!props.section.range}
          title={props.text("openCode")}
          type="button"
          onClick={() => props.section.range && props.onOpenCode(props.section.range)}
        >
          {props.text("code")}
        </button>
      </div>
      {open() ? (
        <div class="p-2">
          {visibleNodes().length === 0 ? (
            <EmptyText>{props.text("emptySection")}</EmptyText>
          ) : (
            <VirtualList
              className="grid gap-1"
              estimateSize={(node) => (node.links?.length ? 76 : 42)}
              getKey={(node) => node.id}
              items={visibleNodes()}
              maxHeight={360}
              overscan={10}
              scrollToIndex={activeNodeIndex() >= 0 ? activeNodeIndex() : undefined}
              renderItem={(node) => {
                const isSearchMatch = props.matchedNodeIds.has(node.id);
                const isActiveSearchMatch = props.activeSearchNodeId === node.id;
                const nodeHint = flowchartNodeHint(node, props.text, props.locale);
                const nodeStyle = flowchartNodeVisualStyle(props.themePalette, node.kind);
                return (
                  <div
                    class={cn(
                      "rounded px-1 py-1 hover:bg-[#223044]",
                      isActiveSearchMatch
                        ? "bg-[#17324a] ring-1 ring-[#7dd3fc]"
                        : isSearchMatch && "bg-[#2b2b18] ring-1 ring-[#f6c177]",
                    )}
                  >
                    <div class="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-1">
                      <button
                        class="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap px-1 py-1 text-left text-xs text-[#d9e0ea] hover:text-white"
                        title={detailParts(props.text("openFlowchart"), node.label, nodeHint)}
                        type="button"
                        onClick={() => props.onSelectNode(node)}
                      >
                        <span
                          class="mr-2 rounded border px-1 py-0.5 text-[10px]"
                          style={webviewStyle(flowchartSwatchStyle(nodeStyle))}
                          title={flowchartNodeKindLabel(node.kind, props.locale)}
                        >
                          {flowchartNodeKindLabel(node.kind, props.locale)}
                        </span>
                        <span title={node.label}>{node.label}</span>
                      </button>
                      <button
                        class="mr-1 shrink-0 rounded border border-[#3b4a5f] px-1.5 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
                        disabled={!node.range}
                        title={props.text("openCode")}
                        type="button"
                        onClick={() => node.range && props.onOpenCode(node.range)}
                      >
                        {props.text("code")}
                      </button>
                    </div>
                    {node.links?.length ? (
                      <div class="ml-1 mt-1 flex flex-wrap gap-1">
                        {node.links.map((link) => {
                          const linkStyle = flowchartNodeLinkStyle(link, props.themePalette);
                          return (
                            <button
                              class="max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded border px-1.5 py-0.5 text-[11px] hover:text-white"
                              disabled={!link.target}
                              style={webviewStyle(flowchartSwatchStyle(linkStyle))}
                              title={flowchartNodeLinkHint(link, props.text, props.locale)}
                              type="button"
                              onClick={() => link.target && props.onOpenTarget(link.target)}
                            >
                              {link.label}
                            </button>
                          );
                        })}
                      </div>
                    ) : null}
                    {node.outputFragments?.length ? (
                      <FlowchartOutputFragments
                        fragments={node.outputFragments}
                        theme={props.theme}
                      />
                    ) : null}
                  </div>
                );
              }}
            />
          )}
        </div>
      ) : null}
    </div>
  );
}
export function FlowchartOutputFragments(props: {
  fragments: readonly AspFlowchartOutputFragment[];
  theme: WebviewTheme;
}): JSX.Element {
  return (
    <div class="ml-1 mt-1 grid gap-1">
      {props.fragments.map((fragment) => {
        const code = highlightFlowchartOutputFragment(
          fragment.text,
          fragment.language,
          props.theme,
        );
        return (
          <div class="min-w-0 overflow-hidden rounded border border-[#263140] bg-[#0c1117]">
            <div class="border-b border-[#263140] px-2 py-0.5 text-[10px] uppercase tracking-wide text-[#9fb0c5]">
              {fragment.language}
            </div>
            <HighlightedCodeView
              code={code}
              class="asp-lsp-output-fragment max-h-32 overflow-auto p-2 text-[11px] leading-4"
            />
          </div>
        );
      })}
    </div>
  );
}
