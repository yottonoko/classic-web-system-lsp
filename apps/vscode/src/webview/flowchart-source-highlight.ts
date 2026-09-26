import type { HighlightedCode } from "./highlighted-code";
import type { AspFlowchartOutputLanguage } from "../protocol-types";
import type { WebviewTheme, WebviewThemeSetting } from "./flowchart-types";
import type { VsCodeColorLookup } from "./flowchart-theme";

const vbscriptKeywords = wordSet([
  "and",
  "as",
  "byref",
  "byval",
  "call",
  "case",
  "class",
  "const",
  "dim",
  "do",
  "each",
  "else",
  "elseif",
  "empty",
  "end",
  "error",
  "exit",
  "explicit",
  "false",
  "for",
  "function",
  "get",
  "if",
  "in",
  "is",
  "let",
  "loop",
  "mod",
  "new",
  "next",
  "not",
  "nothing",
  "on",
  "option",
  "or",
  "private",
  "property",
  "public",
  "redim",
  "rem",
  "resume",
  "select",
  "set",
  "step",
  "sub",
  "then",
  "to",
  "true",
  "until",
  "wend",
  "while",
  "with",
  "xor",
]);
const vbscriptBuiltins = wordSet([
  "array",
  "cbool",
  "cdate",
  "cdbl",
  "chr",
  "cint",
  "clng",
  "createobject",
  "cstr",
  "date",
  "document",
  "err",
  "eval",
  "execute",
  "filter",
  "formatcurrency",
  "formatdatetime",
  "instr",
  "isarray",
  "isdate",
  "isempty",
  "isnull",
  "isnumeric",
  "join",
  "lbound",
  "lcase",
  "left",
  "len",
  "mid",
  "msgbox",
  "replace",
  "request",
  "response",
  "right",
  "round",
  "scriptengine",
  "server",
  "session",
  "split",
  "strcomp",
  "string",
  "time",
  "trim",
  "ubound",
  "ucase",
  "write",
]);
const javascriptKeywords = wordSet([
  "async",
  "await",
  "break",
  "case",
  "catch",
  "class",
  "const",
  "continue",
  "debugger",
  "default",
  "delete",
  "do",
  "else",
  "export",
  "extends",
  "false",
  "finally",
  "for",
  "from",
  "function",
  "get",
  "if",
  "import",
  "in",
  "instanceof",
  "let",
  "new",
  "null",
  "of",
  "return",
  "set",
  "static",
  "super",
  "switch",
  "this",
  "throw",
  "true",
  "try",
  "typeof",
  "undefined",
  "var",
  "void",
  "while",
  "with",
  "yield",
]);

interface HighlightPalette {
  attribute: string;
  background: string;
  builtin: string;
  comment: string;
  constant: string;
  foreground: string;
  function: string;
  identifier: string;
  keyword: string;
  number: string;
  operator: string;
  punctuation: string;
  property: string;
  string: string;
  tag: string;
}

type HighlightTokens = HighlightedCode["tokens"];
type MarkupLanguage = "html" | "css" | "javascript";

interface HighlightState {
  blockComment: boolean;
  htmlComment: boolean;
  htmlTag: boolean;
  htmlTagQuote?: string;
  language: MarkupLanguage;
  pendingTag?: string;
  quote?: string;
  serverRegion: boolean;
}

const palettes: Record<WebviewTheme, HighlightPalette> = {
  dark: {
    attribute: "#d2a8ff",
    background: "#0c1117",
    builtin: "#ffa657",
    comment: "#8b949e",
    constant: "#79c0ff",
    foreground: "#c9d1d9",
    function: "#d2a8ff",
    identifier: "#c9d1d9",
    keyword: "#ff7b72",
    number: "#79c0ff",
    operator: "#ff7b72",
    punctuation: "#8b949e",
    property: "#79c0ff",
    string: "#a5d6ff",
    tag: "#7ee787",
  },
  light: {
    attribute: "#8250df",
    background: "#ffffff",
    builtin: "#953800",
    comment: "#6e7781",
    constant: "#0550ae",
    foreground: "#24292f",
    function: "#8250df",
    identifier: "#24292f",
    keyword: "#cf222e",
    number: "#0550ae",
    operator: "#cf222e",
    punctuation: "#6e7781",
    property: "#0550ae",
    string: "#0a3069",
    tag: "#116329",
  },
};

/** Builds self-contained Classic ASP, HTML, CSS, and JavaScript highlighting. */
export function highlightFlowchartSource(sourceText: string, theme: WebviewTheme): HighlightedCode {
  return highlightFlowchartSourceWithSetting(sourceText, theme, theme);
}

export function highlightFlowchartSourceWithSetting(
  sourceText: string,
  theme: WebviewTheme,
  setting: WebviewThemeSetting | undefined,
  color: VsCodeColorLookup = () => undefined,
): HighlightedCode {
  const palette = highlightPaletteForSetting(theme, setting, color);
  const tokens: HighlightTokens = [];
  const state = initialHighlightState("html");
  const lines = sourceText.split("\n");
  lines.forEach((line, index) => {
    highlightLine(line, state, palette, tokens);
    if (index < lines.length - 1) {
      tokens.push("\n");
    }
  });
  return {
    value: sourceText,
    code: sourceText,
    annotations: [],
    tokens,
    lang: "asp",
    meta: "",
    themeName: `asp-lsp-${theme}`,
    style: { color: palette.foreground, background: palette.background, colorScheme: theme },
  };
}

export function highlightPaletteForSetting(
  theme: WebviewTheme,
  setting: WebviewThemeSetting | undefined,
  color: VsCodeColorLookup,
): HighlightPalette {
  const fallback = palettes[theme];
  if (setting === "light" || setting === "dark") {
    return fallback;
  }
  const resolved = (name: string, fallbackColor: string) => color(name) ?? fallbackColor;
  const foreground = resolved("editor-foreground", fallback.foreground);
  return {
    ...fallback,
    background: resolved("editor-background", fallback.background),
    foreground,
    identifier: foreground,
    punctuation: foreground,
    operator: foreground,
    comment: resolved("editorCodeLens-foreground", fallback.comment),
    keyword: resolved("symbolIcon-keywordForeground", fallback.keyword),
    string: resolved("symbolIcon-stringForeground", fallback.string),
    number: resolved("symbolIcon-numberForeground", fallback.number),
    constant: resolved("symbolIcon-constantForeground", fallback.constant),
    function: resolved("symbolIcon-functionForeground", fallback.function),
    property: resolved("symbolIcon-propertyForeground", fallback.property),
    builtin: resolved("symbolIcon-methodForeground", fallback.builtin),
    tag: resolved("symbolIcon-classForeground", fallback.tag),
    attribute: resolved("symbolIcon-fieldForeground", fallback.attribute),
  };
}

/** Highlights a typed response-output fragment without requiring external grammars. */
export function highlightFlowchartOutputFragment(
  sourceText: string,
  language: AspFlowchartOutputLanguage,
  theme: WebviewTheme,
): HighlightedCode {
  const palette = palettes[theme];
  const tokens: HighlightTokens = [];
  if (language === "html") {
    highlightHtml(sourceText, initialHighlightState("html"), palette, tokens);
  } else if (language === "css") {
    highlightCss(sourceText, initialHighlightState("css"), palette, tokens);
  } else if (language === "javascript") {
    highlightJavascript(sourceText, initialHighlightState("javascript"), palette, tokens);
  } else {
    pushToken(tokens, sourceText, palette.foreground);
  }
  return {
    value: sourceText,
    code: sourceText,
    annotations: [],
    tokens,
    lang: language,
    meta: "",
    themeName: `asp-lsp-${theme}`,
    style: { color: palette.foreground, background: palette.background, colorScheme: theme },
  };
}

function highlightLine(
  line: string,
  state: HighlightState,
  palette: HighlightPalette,
  tokens: HighlightTokens,
): void {
  let index = 0;
  while (index < line.length) {
    const delimiter = state.serverRegion ? "%>" : "<%";
    const delimiterIndex = line.indexOf(delimiter, index);
    const end = delimiterIndex === -1 ? line.length : delimiterIndex;
    const source = line.slice(index, end);
    if (state.serverRegion) {
      highlightVbscript(source, palette, tokens);
    } else {
      highlightClientSource(source, state, palette, tokens);
    }
    if (delimiterIndex === -1) {
      break;
    }
    const fullDelimiter =
      !state.serverRegion && line.startsWith("<%=", delimiterIndex) ? "<%=" : delimiter;
    pushToken(tokens, fullDelimiter, palette.keyword);
    state.serverRegion = !state.serverRegion;
    index = delimiterIndex + fullDelimiter.length;
  }
}

function highlightClientSource(
  source: string,
  state: HighlightState,
  palette: HighlightPalette,
  tokens: HighlightTokens,
): void {
  if (state.language === "css") {
    const closing = findEmbeddedClosingTag(source, "style", state);
    highlightCss(closing === -1 ? source : source.slice(0, closing), state, palette, tokens);
    if (closing !== -1) {
      highlightHtml(source.slice(closing), state, palette, tokens);
    }
    return;
  }
  if (state.language === "javascript") {
    const closing = findEmbeddedClosingTag(source, "script", state);
    highlightJavascript(closing === -1 ? source : source.slice(0, closing), state, palette, tokens);
    if (closing !== -1) {
      highlightHtml(source.slice(closing), state, palette, tokens);
    }
    return;
  }
  highlightHtml(source, state, palette, tokens);
}

function highlightHtml(
  source: string,
  state: HighlightState,
  palette: HighlightPalette,
  tokens: HighlightTokens,
): void {
  let index = 0;
  while (index < source.length) {
    if (state.htmlComment) {
      const close = source.indexOf("-->", index);
      const end = close === -1 ? source.length : close + 3;
      pushToken(tokens, source.slice(index, end), palette.comment);
      state.htmlComment = close === -1;
      index = end;
      continue;
    }
    if (state.htmlTag) {
      const close = source.indexOf(">", index);
      const end = close === -1 ? source.length : close + 1;
      state.htmlTagQuote = highlightHtmlTagContinuation(
        source.slice(index, end),
        state.htmlTagQuote,
        palette,
        tokens,
      );
      state.htmlTag = close === -1;
      if (!state.htmlTag) {
        applyHtmlTagLanguage(state, state.pendingTag);
        state.pendingTag = undefined;
        state.htmlTagQuote = undefined;
      }
      index = end;
      continue;
    }
    if (source.startsWith("<!--", index)) {
      const close = source.indexOf("-->", index + 4);
      const end = close === -1 ? source.length : close + 3;
      pushToken(tokens, source.slice(index, end), palette.comment);
      state.htmlComment = close === -1;
      index = end;
      continue;
    }
    if (source[index] !== "<") {
      const next = source.indexOf("<", index);
      pushToken(
        tokens,
        source.slice(index, next === -1 ? source.length : next),
        palette.foreground,
      );
      index = next === -1 ? source.length : next;
      continue;
    }
    const close = source.indexOf(">", index + 1);
    const tagSource = source.slice(index, close === -1 ? source.length : close + 1);
    const tag = highlightHtmlTag(tagSource, palette, tokens);
    if (close === -1) {
      state.htmlTag = true;
      state.pendingTag = tag;
      state.htmlTagQuote = unclosedQuote(tagSource);
      return;
    }
    applyHtmlTagLanguage(state, tag);
    index = close + 1;
    if (state.language !== "html" && index < source.length) {
      highlightClientSource(source.slice(index), state, palette, tokens);
      return;
    }
  }
}

function highlightHtmlTagContinuation(
  source: string,
  initialQuote: string | undefined,
  palette: HighlightPalette,
  tokens: HighlightTokens,
): string | undefined {
  let index = 0;
  let quote = initialQuote;
  while (index < source.length) {
    if (quote) {
      const result = consumeQuotedContinuation(source, index, quote);
      pushToken(tokens, source.slice(index, result.end), palette.string);
      quote = result.closed ? undefined : quote;
      index = result.end;
      continue;
    }
    const character = source[index] ?? "";
    if (/\s/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /\s/.test(value));
      tokens.push(source.slice(index, end));
      index = end;
    } else if (character === '"' || character === "'") {
      const end = consumeQuotedString(source, index, character, false).end;
      pushToken(tokens, source.slice(index, end), palette.string);
      quote = source[end - 1] === character ? undefined : character;
      index = end;
    } else if (/[A-Za-z_:]/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /[\w:.-]/.test(value));
      pushToken(tokens, source.slice(index, end), palette.attribute);
      index = end;
    } else {
      pushToken(tokens, character, character === "=" ? palette.operator : palette.punctuation);
      index += 1;
    }
  }
  return quote;
}

function applyHtmlTagLanguage(state: HighlightState, tag: string | undefined): void {
  if (tag === "script" || tag === "style") {
    state.language = tag === "script" ? "javascript" : "css";
  } else if (tag === "/script" || tag === "/style") {
    state.language = "html";
    state.blockComment = false;
    state.quote = undefined;
  }
}

function unclosedQuote(source: string): string | undefined {
  let quote: string | undefined;
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index];
    if (quote === character) {
      quote = undefined;
    } else if (!quote && (character === '"' || character === "'")) {
      quote = character;
    }
  }
  return quote;
}

function highlightHtmlTag(
  source: string,
  palette: HighlightPalette,
  tokens: HighlightTokens,
): string | undefined {
  const match = /^(<\/?)([A-Za-z][\w:-]*)/.exec(source);
  if (!match) {
    pushToken(tokens, source, palette.punctuation);
    return undefined;
  }
  pushToken(tokens, match[1] ?? "", palette.punctuation);
  pushToken(tokens, match[2] ?? "", palette.tag);
  let index = match[0].length;
  while (index < source.length) {
    const character = source[index] ?? "";
    if (/\s/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /\s/.test(value));
      tokens.push(source.slice(index, end));
      index = end;
    } else if (character === '"' || character === "'") {
      const close = source.indexOf(character, index + 1);
      const end = close === -1 ? source.length : close + 1;
      pushToken(tokens, source.slice(index, end), palette.string);
      index = end;
    } else if (/[A-Za-z_:]/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /[\w:.-]/.test(value));
      pushToken(tokens, source.slice(index, end), palette.attribute);
      index = end;
    } else {
      pushToken(tokens, character, character === "=" ? palette.operator : palette.punctuation);
      index += 1;
    }
  }
  const name = (match[2] ?? "").toLowerCase();
  return (match[1] ?? "").includes("/") ? `/${name}` : name;
}

function highlightVbscript(
  source: string,
  palette: HighlightPalette,
  tokens: HighlightTokens,
): void {
  highlightProgrammingLanguage(
    source,
    initialHighlightState("html"),
    palette,
    tokens,
    vbscriptKeywords,
    vbscriptBuiltins,
    "vb",
  );
}

function highlightJavascript(
  source: string,
  state: HighlightState,
  palette: HighlightPalette,
  tokens: HighlightTokens,
): void {
  highlightProgrammingLanguage(source, state, palette, tokens, javascriptKeywords, new Set(), "js");
}

function highlightProgrammingLanguage(
  source: string,
  state: HighlightState,
  palette: HighlightPalette,
  tokens: HighlightTokens,
  keywords: ReadonlySet<string>,
  builtins: ReadonlySet<string>,
  language: "vb" | "js",
): void {
  let index = 0;
  while (index < source.length) {
    if (state.blockComment) {
      const close = source.indexOf("*/", index);
      const end = close === -1 ? source.length : close + 2;
      pushToken(tokens, source.slice(index, end), palette.comment);
      state.blockComment = close === -1;
      index = end;
      continue;
    }
    if (state.quote) {
      const result = consumeQuotedContinuation(source, index, state.quote);
      pushToken(tokens, source.slice(index, result.end), palette.string);
      state.quote = result.closed ? undefined : state.quote;
      index = result.end;
      continue;
    }
    const character = source[index] ?? "";
    if (/\s/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /\s/.test(value));
      tokens.push(source.slice(index, end));
      index = end;
    } else if ((language === "vb" && character === "'") || source.startsWith("//", index)) {
      pushToken(tokens, source.slice(index), palette.comment);
      return;
    } else if (source.startsWith("/*", index)) {
      const close = source.indexOf("*/", index + 2);
      const end = close === -1 ? source.length : close + 2;
      pushToken(tokens, source.slice(index, end), palette.comment);
      state.blockComment = close === -1;
      index = end;
    } else if (character === '"' || character === "'" || (language === "js" && character === "`")) {
      const result = consumeQuotedString(source, index, character, language === "vb");
      pushToken(tokens, source.slice(index, result.end), palette.string);
      if (!result.closed && language === "js") {
        state.quote = character;
      }
      index = result.end;
    } else if (/[A-Za-z_$]/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /[\w$]/.test(value));
      const word = source.slice(index, end);
      const normalized = word.toLowerCase();
      const remainder = source.slice(end);
      const next = remainder.match(/^\s*(.)/)?.[1];
      const previous = source.slice(0, index).match(/\.\s*$/);
      const color = keywords.has(normalized)
        ? palette.keyword
        : builtins.has(normalized)
          ? palette.builtin
          : previous
            ? palette.property
            : next === "(" || /^\s*=\s*(?:async\s*)?(?:\([^)]*\)|[\w$]+)\s*=>/.test(remainder)
              ? palette.function
              : palette.identifier;
      pushToken(tokens, word, color);
      index = end;
    } else if (/\d/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /[\dA-Fa-f.xX&]/.test(value));
      pushToken(tokens, source.slice(index, end), palette.number);
      index = end;
    } else {
      pushToken(
        tokens,
        character,
        /[=+*/%<>!-]/.test(character) ? palette.operator : palette.punctuation,
      );
      index += 1;
    }
  }
}

function highlightCss(
  source: string,
  state: HighlightState,
  palette: HighlightPalette,
  tokens: HighlightTokens,
): void {
  let index = 0;
  while (index < source.length) {
    if (state.blockComment) {
      const close = source.indexOf("*/", index);
      const end = close === -1 ? source.length : close + 2;
      pushToken(tokens, source.slice(index, end), palette.comment);
      state.blockComment = close === -1;
      index = end;
      continue;
    }
    if (state.quote) {
      const result = consumeQuotedContinuation(source, index, state.quote);
      pushToken(tokens, source.slice(index, result.end), palette.string);
      state.quote = result.closed ? undefined : state.quote;
      index = result.end;
      continue;
    }
    const character = source[index] ?? "";
    if (/\s/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /\s/.test(value));
      tokens.push(source.slice(index, end));
      index = end;
    } else if (source.startsWith("/*", index)) {
      const close = source.indexOf("*/", index + 2);
      const end = close === -1 ? source.length : close + 2;
      pushToken(tokens, source.slice(index, end), palette.comment);
      state.blockComment = close === -1;
      index = end;
    } else if (character === '"' || character === "'") {
      const result = consumeQuotedString(source, index, character, false);
      pushToken(tokens, source.slice(index, result.end), palette.string);
      state.quote = result.closed ? undefined : character;
      index = result.end;
    } else if (/[#.@A-Za-z_-]/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /[\w#.@-]/.test(value));
      const word = source.slice(index, end);
      const color = /^(#|\.|@)/.test(word)
        ? palette.tag
        : word.startsWith("--") || /^\s*:/.test(source.slice(end))
          ? palette.property
          : palette.constant;
      pushToken(tokens, word, color);
      index = end;
    } else if (/\d/.test(character)) {
      const end = consumeWhile(source, index + 1, (value) => /[\w.%]/.test(value));
      pushToken(tokens, source.slice(index, end), palette.number);
      index = end;
    } else {
      pushToken(
        tokens,
        character,
        /[{}:;,()>+~*]/.test(character) ? palette.punctuation : palette.operator,
      );
      index += 1;
    }
  }
}

function findEmbeddedClosingTag(
  source: string,
  tag: "script" | "style",
  state: HighlightState,
): number {
  let blockComment = state.blockComment;
  let quote = state.quote;
  for (let index = 0; index < source.length; index += 1) {
    if (blockComment) {
      if (source.startsWith("*/", index)) {
        blockComment = false;
        index += 1;
      }
    } else if (quote) {
      if (source[index] === "\\") {
        index += 1;
      } else if (source[index] === quote) {
        quote = undefined;
      }
    } else if (source.startsWith("/*", index)) {
      blockComment = true;
      index += 1;
    } else if (source[index] === '"' || source[index] === "'" || source[index] === "`") {
      quote = source[index];
    } else if (source.slice(index).toLowerCase().startsWith(`</${tag}`)) {
      return index;
    }
  }
  return -1;
}

function consumeQuotedString(
  source: string,
  start: number,
  quote: string,
  doubledQuotes: boolean,
): { closed: boolean; end: number } {
  let index = start + 1;
  while (index < source.length) {
    if (source[index] === "\\" && !doubledQuotes) {
      index += 2;
    } else if (source[index] !== quote) {
      index += 1;
    } else if (doubledQuotes && source[index + 1] === quote) {
      index += 2;
    } else {
      return { closed: true, end: index + 1 };
    }
  }
  return { closed: false, end: source.length };
}

function consumeQuotedContinuation(
  source: string,
  start: number,
  quote: string,
): { closed: boolean; end: number } {
  let index = start;
  while (index < source.length) {
    if (source[index] === "\\") {
      index += 2;
    } else if (source[index] === quote) {
      return { closed: true, end: index + 1 };
    } else {
      index += 1;
    }
  }
  return { closed: false, end: source.length };
}

function initialHighlightState(language: MarkupLanguage): HighlightState {
  return {
    blockComment: false,
    htmlComment: false,
    htmlTag: false,
    language,
    serverRegion: false,
  };
}

function consumeWhile(
  source: string,
  start: number,
  predicate: (value: string) => boolean,
): number {
  let index = start;
  while (index < source.length && predicate(source[index] ?? "")) {
    index += 1;
  }
  return index;
}

function pushToken(tokens: HighlightTokens, value: string, color: string): void {
  if (value) {
    tokens.push([value, color]);
  }
}

function wordSet(words: readonly string[]): ReadonlySet<string> {
  return new Set(words.map((word) => word.toLowerCase()));
}
