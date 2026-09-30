import { describe, expect, it } from "vitest";
import { shouldSuggestSqlAfterSpace } from "../src/sql-suggest";

describe("shouldSuggestSqlAfterSpace", () => {
  it("opens completion after a SQL keyword inside a string literal", () => {
    expect(shouldSuggestSqlAfterSpace('sql = "SELECT * FROM ')).toBe(true);
    expect(shouldSuggestSqlAfterSpace('sql = sql & " and ')).toBe(true);
    expect(shouldSuggestSqlAfterSpace('conn.Execute "UPDATE ')).toBe(true);
    expect(shouldSuggestSqlAfterSpace('sql = "a = \'" & x & "\' ORDER BY ')).toBe(true);
  });

  it("stays quiet outside string literals, in comments, and after other words", () => {
    expect(shouldSuggestSqlAfterSpace("Select Case from ")).toBe(false);
    expect(shouldSuggestSqlAfterSpace('title = "Hello" And ')).toBe(false);
    expect(shouldSuggestSqlAfterSpace("' sql = \"SELECT * FROM ")).toBe(false);
    expect(shouldSuggestSqlAfterSpace('sql = "SELECT name ')).toBe(false);
    expect(shouldSuggestSqlAfterSpace('sql = "platform ')).toBe(false);
    expect(shouldSuggestSqlAfterSpace('sql = "SELECT * FROM')).toBe(false);
  });
});
