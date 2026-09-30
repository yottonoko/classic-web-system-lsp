const sqlContinuationWord =
  /(?:^|[^A-Za-z0-9_])(select|distinct|top|from|where|and|or|join|on|by|set|into|update|values|having|like|in|not|as)$/i;

/**
 * Reports whether a space typed at the end of `linePrefix` continues SQL text
 * inside a VBScript string literal, so completion can be opened there. The
 * server still decides whether the literal really is SQL.
 */
export function shouldSuggestSqlAfterSpace(linePrefix: string): boolean {
  if (!linePrefix.endsWith(" ")) {
    return false;
  }
  let insideString = false;
  for (const character of linePrefix) {
    if (character === '"') {
      insideString = !insideString;
    } else if (character === "'" && !insideString) {
      // The rest of the line is a VBScript comment.
      return false;
    }
  }
  return insideString && sqlContinuationWord.test(linePrefix.slice(0, -1));
}
