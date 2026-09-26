import {
  createEffect,
  createMemo,
  createSignal,
  For,
  onSettled,
  untrack,
  type Accessor,
} from "solid-js";
import type { JSX } from "@solidjs/web";
import {
  Virtualizer,
  elementScroll,
  observeElementOffset,
  observeElementRect,
  type VirtualizerOptions,
} from "@tanstack/virtual-core";
import { cn } from "../lib/utils";
type ListOptions = Pick<
  VirtualizerOptions<HTMLDivElement, Element>,
  "count" | "estimateSize" | "getItemKey" | "getScrollElement" | "overscan"
>;
/** Connect the framework-neutral virtualizer to Solid's tracked reads and lifecycle. */
export function createVirtualizer(options: Accessor<ListOptions>) {
  const [revision, setRevision] = createSignal(0);
  const resolved = () => ({
    ...options(),
    scrollToFn: elementScroll,
    observeElementRect,
    observeElementOffset,
    onChange: () => setRevision((value) => value + 1),
  });
  const instance = new Virtualizer<HTMLDivElement, Element>(untrack(resolved));
  createEffect(options, () => {
    instance.setOptions(resolved());
    instance._willUpdate();
    setRevision((value) => value + 1);
  });
  onSettled(() => {
    const dispose = instance._didMount();
    instance._willUpdate();
    return dispose;
  });
  return {
    getVirtualItems: () => {
      revision();
      return instance.getVirtualItems();
    },
    getTotalSize: () => {
      revision();
      return instance.getTotalSize();
    },
    scrollToIndex: instance.scrollToIndex,
    measureElement: instance.measureElement,
  };
}
export interface VirtualListProps<TItem> {
  className?: string;
  estimateSize: number | ((item: TItem, index: number) => number);
  gap?: number;
  getKey(item: TItem, index: number): string | number | bigint;
  itemClassName?: string;
  items: readonly TItem[];
  maxHeight: number | string;
  onVisibleItemsChange?(items: readonly TItem[]): void;
  overscan?: number;
  renderItem(item: TItem, index: number): JSX.Element;
  scrollToIndex?: number;
  threshold?: number;
}
/** Render only the visible portion of long lists while measuring variable-height rows. */
export function VirtualList<TItem>(props: VirtualListProps<TItem>): JSX.Element {
  let parent: HTMLDivElement | undefined;
  const virtualized = createMemo(() => props.items.length > (props.threshold ?? 40));
  const virtualizer = createVirtualizer(() => ({
    count: virtualized() ? props.items.length : 0,
    estimateSize: (index) =>
      (typeof props.estimateSize === "number"
        ? props.estimateSize
        : props.estimateSize(props.items[index], index)) + (props.gap ?? 8),
    getItemKey: (index) => String(props.getKey(props.items[index], index)),
    getScrollElement: () => parent ?? null,
    overscan: props.overscan ?? 6,
  }));
  const virtualItems = createMemo(() => virtualizer.getVirtualItems());
  const visible = createMemo(() =>
    virtualized()
      ? virtualItems()
          .map((item) => props.items[item.index])
          .filter((item) => item !== undefined)
      : props.items,
  );
  createEffect(visible, (items) => props.onVisibleItemsChange?.(items));
  createEffect(
    () => [props.scrollToIndex, props.items.length, virtualized()] as const,
    ([index, length, enabled]) => {
      if (enabled && index !== undefined && index >= 0 && length)
        virtualizer.scrollToIndex(Math.min(index, length - 1), { align: "auto" });
    },
  );
  return (
    <>
      {virtualized() ? (
        <div
          ref={(element) => {
            parent = element;
          }}
          class={cn(props.className, "overflow-auto pr-1")}
          style={{
            "max-height":
              typeof props.maxHeight === "number" ? `${props.maxHeight}px` : props.maxHeight,
          }}
        >
          <div class="relative w-full" style={{ height: `${virtualizer.getTotalSize()}px` }}>
            <For each={virtualItems()}>
              {(item) => {
                let element: HTMLDivElement | undefined;
                onSettled(() => {
                  if (element) virtualizer.measureElement(element);
                  return () => virtualizer.measureElement(null);
                });
                return (
                  <div
                    ref={(node) => {
                      element = node;
                    }}
                    data-index={item.index}
                    class={cn("absolute top-0 left-0 box-border w-full", props.itemClassName)}
                    style={{
                      "padding-bottom": `${props.gap ?? 8}px`,
                      transform: `translateY(${item.start}px)`,
                    }}
                  >
                    {props.renderItem(props.items[item.index], item.index)}
                  </div>
                );
              }}
            </For>
          </div>
        </div>
      ) : (
        <div class={props.className}>
          <For each={props.items}>{(item, index) => props.renderItem(item, index())}</For>
        </div>
      )}
    </>
  );
}
