import { createMemo, For } from "solid-js";
import type { JSX } from "@solidjs/web";
import { highlightedCodeLines, type HighlightedCode } from "./highlighted-code";
import { webviewStyle } from "./webview-dom-types";

/** Render source tokens and line annotations with stable, selectable DOM lines. */
export function HighlightedCodeView(props: {
  code: HighlightedCode;
  class?: string;
  ref?: (element: HTMLPreElement) => void;
  lineClass?: (lineNumber: number) => string;
}): JSX.Element {
  const tokenCode = createMemo(() => props.code, {
    equals: (left, right) => left.tokens === right.tokens,
  });
  const lines = createMemo(() => highlightedCodeLines(tokenCode()));
  return (
    <pre ref={props.ref} class={props.class} style={webviewStyle(props.code.style)}>
      <code>
        <For each={lines()}>
          {(tokens, index) => (
            <span
              class={`asp-lsp-source-line ${props.lineClass?.(index() + 1) ?? ""}`}
              data-source-line={index() + 1}
            >
              <For each={tokens}>
                {(token) => <span style={{ color: token.color }}>{token.text}</span>}
              </For>
            </span>
          )}
        </For>
      </code>
    </pre>
  );
}
