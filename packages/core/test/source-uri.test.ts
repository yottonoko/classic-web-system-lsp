import { describe, expect, it } from "vitest";
import { sameSourceUri, sourceUriIdentityKey } from "../src";

describe("sourceUriIdentityKey", () => {
  it.each([
    {
      name: "normalizes Windows drive file URI case and escaping",
      uri: "file:///C:/Site/Default.asp",
      key: "c:/site/default.asp",
      sameAs: "file:///c%3A/site/default.asp",
    },
    {
      name: "normalizes UNC host and path case",
      uri: "file://Server/Share/Folder/Default.asp",
      key: "//server/share/folder/default.asp",
      sameAs: "file://server/share/folder/default.asp",
    },
    {
      name: "decodes percent escaped POSIX path segments without folding case",
      uri: "file:///Users/yottonoko/My%20Site/Default.asp",
      key: "/Users/yottonoko/My Site/Default.asp",
      sameAs: "file:///Users/yottonoko/My Site/Default.asp",
    },
    {
      name: "preserves query and hash as part of the identity",
      uri: "file:///C:/Site/Default.asp?view=1#runtime-global",
      key: "c:/site/default.asp?view=1#runtime-global",
      sameAs: "file:///c:/site/default.asp?view=1#runtime-global",
    },
    {
      name: "leaves non-file URIs unchanged",
      uri: "untitled:Untitled-1",
      key: "untitled:Untitled-1",
      sameAs: "untitled:Untitled-1",
    },
  ])("$name", ({ uri, key, sameAs }) => {
    expect(sourceUriIdentityKey(uri)).toBe(key);
    expect(sourceUriIdentityKey(uri)).toBe(key);
    expect(sameSourceUri(uri, sameAs)).toBe(true);
  });

  it("does not collapse file URI identities with different query or hash", () => {
    expect(sameSourceUri("file:///C:/site/default.asp?view=1", "file:///C:/site/default.asp")).toBe(
      false,
    );
    expect(
      sameSourceUri("file:///C:/site/default.asp#runtime-global", "file:///C:/site/default.asp"),
    ).toBe(false);
  });
});
