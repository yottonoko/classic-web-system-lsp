import React, { useEffect, useMemo, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";

export interface VirtualListProps<TItem> {
  className?: string;
  estimateSize: number | ((item: TItem, index: number) => number);
  gap?: number;
  getKey(item: TItem, index: number): React.Key;
  itemClassName?: string;
  items: readonly TItem[];
  maxHeight: number;
  onVisibleItemsChange?(items: readonly TItem[]): void;
  overscan?: number;
  renderItem(item: TItem, index: number): React.ReactNode;
  scrollToIndex?: number;
  threshold?: number;
}

const defaultVirtualListThreshold = 40;
const defaultVirtualListOverscan = 6;
const defaultVirtualListGap = 8;

export function VirtualList<TItem>({
  className,
  estimateSize,
  gap = defaultVirtualListGap,
  getKey,
  itemClassName,
  items,
  maxHeight,
  onVisibleItemsChange,
  overscan = defaultVirtualListOverscan,
  renderItem,
  scrollToIndex,
  threshold = defaultVirtualListThreshold,
}: VirtualListProps<TItem>): React.ReactElement {
  const parentRef = useRef<HTMLDivElement>(null);
  const shouldVirtualize = items.length > threshold;
  const virtualizer = useVirtualizer({
    count: shouldVirtualize ? items.length : 0,
    estimateSize: (index) => estimatedItemSize(items[index], index, estimateSize) + gap,
    getItemKey: (index) => {
      const item = items[index];
      return item === undefined ? String(index) : String(getKey(item, index));
    },
    getScrollElement: () => parentRef.current,
    overscan,
  });

  const virtualItems = virtualizer.getVirtualItems();
  const visibleItemsKey = shouldVirtualize
    ? virtualItems.map((item) => item.key).join("\u0000")
    : items.map((item, index) => String(getKey(item, index))).join("\u0000");
  const visibleItems = useMemo(
    () =>
      shouldVirtualize
        ? virtualItems
            .map((item) => items[item.index])
            .filter((item): item is TItem => item !== undefined)
        : items,
    [items, shouldVirtualize, visibleItemsKey],
  );

  useEffect(() => {
    onVisibleItemsChange?.(visibleItems);
  }, [onVisibleItemsChange, visibleItems]);

  useEffect(() => {
    if (!shouldVirtualize || scrollToIndex === undefined || scrollToIndex < 0) {
      return;
    }
    virtualizer.scrollToIndex(Math.min(scrollToIndex, items.length - 1), { align: "auto" });
  }, [items.length, scrollToIndex, shouldVirtualize, virtualizer]);

  if (!shouldVirtualize) {
    return (
      <div className={className}>
        {items.map((item, index) => (
          <React.Fragment key={getKey(item, index)}>{renderItem(item, index)}</React.Fragment>
        ))}
      </div>
    );
  }

  return (
    <div
      ref={parentRef}
      className={className}
      style={{ maxHeight, overflow: "auto", paddingRight: 4 }}
    >
      <div
        style={{
          height: `${virtualizer.getTotalSize()}px`,
          position: "relative",
          width: "100%",
        }}
      >
        {virtualItems.map((virtualItem) => {
          const item = items[virtualItem.index];
          if (item === undefined) {
            return null;
          }
          return (
            <div
              key={virtualItem.key}
              ref={virtualizer.measureElement}
              data-index={virtualItem.index}
              className={itemClassName}
              style={{
                boxSizing: "border-box",
                left: 0,
                paddingBottom: gap,
                position: "absolute",
                top: 0,
                transform: `translateY(${virtualItem.start}px)`,
                width: "100%",
              }}
            >
              {renderItem(item, virtualItem.index)}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function estimatedItemSize<TItem>(
  item: TItem | undefined,
  index: number,
  estimateSize: number | ((item: TItem, index: number) => number),
): number {
  if (typeof estimateSize === "number" || item === undefined) {
    return typeof estimateSize === "number" ? estimateSize : 48;
  }
  return estimateSize(item, index);
}
