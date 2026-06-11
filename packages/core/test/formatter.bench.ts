import { bench, describe } from "vitest";
import { formatAspDocument, parseAspDocument } from "../src";

const body = Array.from(
  { length: 2_000 },
  (_, index) => `If value${index}>0 Then
Response.Write value${index}
End If`,
).join("\n");
const parsed = parseAspDocument("file:///bench/formatter.asp", `<%\n${body}\n%>`);

describe("ASP formatter", () => {
  bench("2,000-line VBScript block", () => {
    formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
  });
});
