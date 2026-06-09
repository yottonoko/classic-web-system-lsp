export function sourceUriIdentityKey(uri: string): string {
  if (!uri.toLowerCase().startsWith("file:")) {
    return uri;
  }
  try {
    const parsed = new URL(uri);
    if (parsed.protocol.toLowerCase() !== "file:") {
      return uri;
    }
    const host = parsed.hostname ? `//${parsed.hostname.toLowerCase()}` : "";
    const pathname = decodeURIComponent(parsed.pathname).replace(/\\/g, "/");
    const withoutLeadingDriveSlash = pathname.replace(/^\/([A-Za-z]:)(?=\/|$)/, "$1");
    const suffix = `${parsed.search}${parsed.hash}`;
    const key = `${host}${withoutLeadingDriveSlash}${suffix}`;
    const windowsLike =
      host.length > 0 ||
      /^[A-Za-z]:(?:\/|$)/.test(withoutLeadingDriveSlash) ||
      /^\/[A-Za-z]:(?:\/|$)/.test(pathname);
    return windowsLike ? key.toLowerCase() : key;
  } catch {
    return uri;
  }
}

export function sameSourceUri(left: string, right: string): boolean {
  return sourceUriIdentityKey(left) === sourceUriIdentityKey(right);
}
