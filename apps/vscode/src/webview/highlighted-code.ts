/** Source annotations use one-based, inclusive line numbers. */
export interface CodeAnnotation {
  name: string;
  query: string;
  fromLineNumber: number;
  toLineNumber: number;
  data?: Record<string, unknown>;
}

/** Framework-independent output of the embedded-language source lexer. */
export interface HighlightedCode {
  value: string;
  code: string;
  lang: string;
  meta: string;
  themeName: string;
  style: Record<string, string>;
  tokens: (string | [string, string?, number?])[];
  annotations: CodeAnnotation[];
}

/** Split colored tokens without losing blank lines, CRLF boundaries, or indentation. */
export function highlightedCodeLines(code: HighlightedCode): { text: string; color?: string }[][] {
  const lines: { text: string; color?: string }[][] = [[]];
  let previousCarriageReturn = false;
  for (const token of code.tokens) {
    let text = typeof token === "string" ? token : token[0];
    if (text.length === 0) continue;
    if (previousCarriageReturn && text.startsWith("\n")) text = text.slice(1);
    previousCarriageReturn = text.endsWith("\r");
    const color = typeof token === "string" ? undefined : token[1];
    const parts = text.split(/\r\n|\r|\n/);
    parts.forEach((part, index) => {
      if (index > 0) lines.push([]);
      if (part) lines[lines.length - 1].push({ text: part, color });
    });
  }
  return lines;
}
