const VOID_ELEMENTS = new Set([
  "area",
  "base",
  "br",
  "col",
  "embed",
  "hr",
  "img",
  "input",
  "link",
  "meta",
  "param",
  "source",
  "track",
  "wbr",
]);

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function findOpeningTagStartAtGt(docText: string, gtIdx: number): number | undefined {
  let i = 0;
  let state: "html" | "asp" | "comment" | "script" = "html";
  let inTag = false;
  let tagStart = -1;
  let inQuote: '"' | "'" | null = null;

  while (i <= gtIdx) {
    if (state === "asp") {
      if (docText[i] === "%" && docText[i + 1] === ">") {
        if (i + 1 === gtIdx) {
          return undefined;
        }
        state = "html";
        i += 2;
        continue;
      }
      i++;
      continue;
    }

    if (state === "comment") {
      if (docText.startsWith("-->", i)) {
        if (i + 2 === gtIdx) {
          return undefined;
        }
        state = "html";
        i += 3;
        continue;
      }
      i++;
      continue;
    }

    if (state === "script") {
      if (docText[i] === "<" && docText[i + 1] === "%") {
        // Skip ASP islands inside client-side <script> blocks.
        let j = i + 2;
        while (j < gtIdx && !(docText[j] === "%" && docText[j + 1] === ">")) {
          j++;
        }
        if (j + 1 >= gtIdx) {
          return undefined;
        }
        i = j + 2;
        continue;
      }
      if (docText[i] === "<" && /^<\/script\b/i.test(docText.slice(i, i + 9))) {
        const closeGt = docText.indexOf(">", i + 8);
        if (closeGt === -1 || closeGt >= gtIdx) {
          return undefined;
        }
        state = "html";
        i = closeGt + 1;
        continue;
      }
      i++;
      continue;
    }

    // state === "html"
    if (docText[i] === "<" && docText[i + 1] === "%") {
      state = "asp";
      i += 2;
      continue;
    }

    if (!inTag && docText.startsWith("<!--", i)) {
      state = "comment";
      i += 4;
      continue;
    }

    if (!inTag) {
      if (docText[i] === "<") {
        inTag = true;
        tagStart = i;
        inQuote = null;
      }
      i++;
      continue;
    }

    // Inside an HTML tag
    const ch = docText[i];
    if (inQuote !== null) {
      if (ch === inQuote) {
        inQuote = null;
      }
      i++;
      continue;
    }

    if (ch === '"' || ch === "'") {
      inQuote = ch;
      i++;
      continue;
    }

    if (ch === ">") {
      if (i === gtIdx) {
        return tagStart;
      }
      const tagSlice = docText.slice(tagStart, i + 1);
      inTag = false;
      tagStart = -1;
      if (/^<script\b/i.test(tagSlice) && !/\/>\s*$/.test(tagSlice)) {
        state = "script";
      }
      i++;
      continue;
    }

    i++;
  }

  return undefined;
}

/**
 * Determines the HTML tag name to auto-close when `>` has just been inserted
 * at `offsetAfterGt` in a Classic ASP document.
 */
export function findAutoCloseTagName(docText: string, offsetAfterGt: number): string | undefined {
  if (offsetAfterGt <= 0 || offsetAfterGt > docText.length) {
    return undefined;
  }
  const gtIdx = offsetAfterGt - 1;
  if (docText[gtIdx] !== ">") {
    return undefined;
  }

  if (gtIdx >= 1 && (docText[gtIdx - 1] === "/" || docText[gtIdx - 1] === "%")) {
    return undefined;
  }
  if (gtIdx >= 2 && docText.slice(gtIdx - 2, gtIdx + 1) === "-->") {
    return undefined;
  }

  const tagStart = findOpeningTagStartAtGt(docText, gtIdx);
  if (tagStart === undefined) {
    return undefined;
  }

  const inner = docText.slice(tagStart + 1, gtIdx);
  if (
    inner.startsWith("/") ||
    inner.startsWith("!") ||
    inner.startsWith("?") ||
    inner.startsWith("%")
  ) {
    return undefined;
  }

  const nameMatch = /^([a-zA-Z][a-zA-Z0-9:-]*)(?=[\s/>]|$)/.exec(inner);
  if (!nameMatch) {
    return undefined;
  }
  const tagName = nameMatch[1];

  if (VOID_ELEMENTS.has(tagName.toLowerCase())) {
    return undefined;
  }

  const after = docText.slice(offsetAfterGt);
  const alreadyClosed = new RegExp(`^\\s*</${escapeRegExp(tagName)}\\s*>`, "i");
  if (alreadyClosed.test(after)) {
    return undefined;
  }

  return tagName;
}
