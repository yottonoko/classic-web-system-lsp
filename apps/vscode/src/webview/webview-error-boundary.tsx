import { Errored } from "solid-js";
import type { JSX } from "@solidjs/web";
/** Keep an unexpected view failure readable while preserving the extension host. */
export function WebviewErrorBoundary(props: { children: JSX.Element; title: string }): JSX.Element {
  return (
    <Errored
      fallback={(error) => (
        <main
          style={{
            "min-height": "100vh",
            padding: "24px",
            background: "var(--vscode-editor-background, #101419)",
            color: "var(--vscode-editor-foreground, #d9e0ea)",
            "font-family": "var(--vscode-font-family), system-ui, sans-serif",
          }}
        >
          <section
            style={{
              "max-width": "760px",
              border: "1px solid var(--vscode-inputValidation-errorBorder, #7f3434)",
              background: "var(--vscode-inputValidation-errorBackground, #291416)",
              padding: "16px",
            }}
          >
            <h1
              style={{
                margin: "0 0 8px",
                "font-size": "16px",
                color: "var(--vscode-errorForeground, #ffd2cc)",
              }}
            >
              {props.title}
            </h1>
            <pre style={{ margin: 0, "white-space": "pre-wrap" }}>
              {error() instanceof Error ? (error() as Error).message : String(error())}
            </pre>
          </section>
        </main>
      )}
    >
      {props.children}
    </Errored>
  );
}
