/**
 * Produces URI text accepted by VS Code while preserving valid percent escapes.
 *
 * Older server and cache data may contain UNC file URIs serialized as `file:////server/share`.
 * VS Code rejects those because the empty-authority path starts with `//`.
 */
export function uriTextForVSCode(uriText: string): string {
  const normalized = normalizeFileUriSlashes(uriText);
  return normalized.replace(/(?:%[0-9a-f]{2})+|%/giu, (encodedRun) => {
    if (encodedRun === "%") {
      return "%25";
    }
    try {
      decodeURIComponent(encodedRun);
      return encodedRun;
    } catch {
      return encodedRun.replaceAll("%", "%25");
    }
  });
}

function normalizeFileUriSlashes(uriText: string): string {
  const match = /^file:\/{4,}([^/?#]*)(.*)$/iu.exec(uriText);
  if (!match) {
    return uriText;
  }
  const [, firstSegment, suffix] = match;
  if (!firstSegment) {
    return `file:///${suffix}`;
  }
  return /^[a-z]:$/iu.test(firstSegment)
    ? `file:///${firstSegment}${suffix}`
    : `file://${firstSegment}${suffix}`;
}
