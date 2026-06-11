import { bench, describe } from "vitest";
import { parseAspDocument, updateAspParsedDocument } from "../src";
import { positionAt } from "../src/position";

const blocks = Array.from(
  { length: 1_000 },
  (_, index) => `<section data-index="${index}">
<%
Dim value${index}
value${index} = ${index}
If value${index} > 0 Then
  Response.Write value${index}
End If
%>
</section>`,
);
const source = blocks.join("\n");
const parsed = parseAspDocument("file:///bench/default.asp", source);
const editOffset = source.indexOf("Response.Write");
const edit = {
  range: { start: positionAt(source, editOffset), end: positionAt(source, editOffset) },
  rangeOffset: editOffset,
  rangeLength: 0,
  text: "' ",
};

describe("ASP parser", () => {
  bench("full parse", () => {
    parseAspDocument("file:///bench/default.asp", source);
  });

  bench("incremental parse", () => {
    updateAspParsedDocument(parsed, [edit]);
  });
});
