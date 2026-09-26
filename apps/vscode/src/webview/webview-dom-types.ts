/** Native DOM events with the element type retained for shared input handlers. */
export type WebviewEvent<TEvent extends Event, TElement extends EventTarget> = TEvent & {
  currentTarget: TElement;
};

/** Mutable element reference shared by DOM integration helpers. */
export interface WebviewRef<T> {
  current: T;
}

/** CSS values produced by the graph and code-highlighting models. */
export type WebviewStyle = Record<string, string | number | undefined>;

/** Normalize computed CSS lengths and property names before applying them to DOM elements. */
export function webviewStyle(
  values: WebviewStyle | undefined,
): import("@solidjs/web").JSX.CSSProperties {
  if (!values) return {};
  const result: Record<string, string | number | undefined> = {};
  for (const [name, value] of Object.entries(values)) {
    const property = name.startsWith("--")
      ? name
      : name.replace(/[A-Z]/g, (letter) => "-" + letter.toLowerCase());
    const unitless =
      property.startsWith("--") ||
      /^(opacity|z-index|order|flex|flex-grow|flex-shrink|font-weight|line-height|scale|stroke-width|fill-opacity|stroke-opacity|grid-row|grid-column)$/.test(
        property,
      );
    result[property] = typeof value === "number" && !unitless ? `${value}px` : value;
  }
  return result as import("@solidjs/web").JSX.CSSProperties;
}
