import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";
import { findAutoCloseTagName } from "../src/autoCloseTag.js";
import { classifyLspMethod, FrameParser } from "../src/wasmRunner.js";

const extensionRoot = path.resolve(import.meta.dirname, "..");

describe("classifyLspMethod", () => {
  it("classifies lifecycle and document sync methods as Control", () => {
    for (const method of [
      "initialize",
      "initialized",
      "shutdown",
      "exit",
      "textDocument/didOpen",
      "textDocument/didChange",
      "textDocument/didClose",
      "textDocument/didSave",
      "workspace/didChangeConfiguration",
      "$/setTrace",
    ]) {
      expect(classifyLspMethod(method)).toBe("Control");
    }
  });

  it("classifies expensive analysis requests as Heavy", () => {
    for (const method of [
      "textDocument/semanticTokens/full",
      "textDocument/inlayHint",
      "textDocument/documentSymbol",
      "textDocument/rename",
      "textDocument/formatting",
      "workspace/symbol",
    ]) {
      expect(classifyLspMethod(method)).toBe("Heavy");
    }
  });

  it("classifies cross-file reference and CodeLens requests as Refs", () => {
    for (const method of ["textDocument/codeLens", "codeLens/resolve", "textDocument/references"]) {
      expect(classifyLspMethod(method)).toBe("Refs");
    }
  });

  it("classifies latency-sensitive requests as Interactive", () => {
    for (const method of [
      "textDocument/completion",
      "textDocument/hover",
      "textDocument/signatureHelp",
      "textDocument/definition",
      "asp/toggleLineComment",
      "asp/toggleBlockComment",
    ]) {
      expect(classifyLspMethod(method)).toBe("Interactive");
    }
  });
});

describe("FrameParser", () => {
  it("parses complete, split, batched, and multi-byte UTF-8 LSP frames", () => {
    const parser = new FrameParser();
    const msg1 = JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize" });
    const msg2 = JSON.stringify({ jsonrpc: "2.0", method: "initialized", text: "日本語✨" });

    const frame1 = Buffer.concat([
      Buffer.from(`Content-Length: ${Buffer.byteLength(msg1, "utf8")}\r\n\r\n`, "ascii"),
      Buffer.from(msg1, "utf8"),
    ]);
    const frame2 = Buffer.concat([
      Buffer.from(`Content-Length: ${Buffer.byteLength(msg2, "utf8")}\r\n\r\n`, "ascii"),
      Buffer.from(msg2, "utf8"),
    ]);

    const combined = Buffer.concat([frame1, frame2]);
    // Split mid-header and mid-body
    expect(parser.push(combined.subarray(0, 10))).toEqual([]);
    const firstBatch = parser.push(combined.subarray(10, frame1.length + 12));
    expect(firstBatch.map((b) => b.toString("utf8"))).toEqual([msg1]);

    const secondBatch = parser.push(combined.subarray(frame1.length + 12));
    expect(secondBatch.map((b) => b.toString("utf8"))).toEqual([msg2]);
  });
});

describe("findAutoCloseTagName", () => {
  it("returns tag name for open HTML tags including tags with ASP attribute expressions", () => {
    const simple = "<div>";
    expect(findAutoCloseTagName(simple, simple.length)).toBe("div");

    const withAttr = '<span class="<%=cls%>">';
    expect(findAutoCloseTagName(withAttr, withAttr.length)).toBe("span");

    const serverScript = '<script language="VBScript" runat="server">';
    expect(findAutoCloseTagName(serverScript, serverScript.length)).toBe("script");
  });

  it("skips self-closing, ASP, comment, closing, declaration, and void tags", () => {
    for (const sample of [
      "<br/>",
      "<% Response.Write x %>",
      "<!-- comment -->",
      "</div>",
      "<!DOCTYPE html>",
      '<?xml version="1.0"?>',
      "<br>",
      "<IMG src='a.png'>",
      '<input type="text">',
      "<wbr>",
    ]) {
      expect(findAutoCloseTagName(sample, sample.length)).toBeUndefined();
    }
  });

  it("skips > inside ASP blocks, HTML comments, script bodies, and attribute quotes", () => {
    const inAsp = "<% If a > 0 Then %>";
    expect(findAutoCloseTagName(inAsp, 9)).toBeUndefined();

    const inComment = "<!-- <div> -->";
    expect(findAutoCloseTagName(inComment, 10)).toBeUndefined();

    const inScript = '<script runat="server">If a > 0 Then</script>';
    expect(findAutoCloseTagName(inScript, 29)).toBeUndefined();

    const inAttrQuote = '<div title="a>b">';
    expect(findAutoCloseTagName(inAttrQuote, 14)).toBeUndefined();
    expect(findAutoCloseTagName(inAttrQuote, inAttrQuote.length)).toBe("div");
  });

  it("skips already-closed tags", () => {
    const doc = "<div></div>";
    expect(findAutoCloseTagName(doc, 5)).toBeUndefined();
  });
});

describe("lint settings in package.json", () => {
  const pkg = JSON.parse(fs.readFileSync(path.join(extensionRoot, "package.json"), "utf8"));
  const props = pkg.contributes.configuration.properties;

  it("declares lint enabled, groups, and all 9 rule overrides", () => {
    expect(props["aspLsp.lint.enabled"]).toMatchObject({ type: "boolean", default: true });
    expect(props["aspLsp.html.linkedEditing"]).toMatchObject({ type: "boolean", default: true });
    expect(props["aspLsp.js.jsdocTypeStyle"]).toMatchObject({
      type: "string",
      default: "typescript",
      enum: ["typescript", "jsdoc"],
    });
    const commands = pkg.contributes.commands as Array<{ command: string; title: string }>;
    expect(commands.some((c) => c.command === "aspLsp.clearCache")).toBe(true);
    expect(commands.some((c) => c.command === "aspLsp.clearIndexCache")).toBe(true);
    expect(props["aspLsp.cache.maxAgeDays"]).toMatchObject({
      type: "number",
      default: 14,
      minimum: 0,
    });
    expect(props["aspLsp.cache.maxSizeMb"]).toMatchObject({
      type: "number",
      default: 256,
      minimum: 0,
    });
    expect(pkg.contributes.configurationDefaults["[classic-asp]"]["editor.linkedEditing"]).toBe(
      true,
    );
    for (const group of ["security", "correctness", "style"]) {
      expect(props[`aspLsp.lint.groups.${group}`]).toMatchObject({
        type: "boolean",
        default: true,
      });
    }
    const rules = [
      "asp/unencoded-output",
      "asp/sql-concat",
      "vb/undeclared-var",
      "vb/missing-set",
      "vb/unreachable",
      "vb/on-error-resume-next",
      "vb/option-explicit",
      "vb/unused-var",
      "vb/goto-legacy",
    ];
    for (const rule of rules) {
      expect(props[`aspLsp.lint.rules.${rule}`]).toMatchObject({
        type: "string",
        enum: ["default", "off", "hint", "info", "warning", "error"],
        default: "default",
      });
    }
  });
});

describe("classic-asp.tmLanguage.json #asp-vb-script-tag", () => {
  const grammar = JSON.parse(
    fs.readFileSync(path.join(extensionRoot, "syntaxes", "classic-asp.tmLanguage.json"), "utf8"),
  );

  it("includes #asp-vb-script-tag before text.html.basic and embeds #asp-vbscript", () => {
    const includes = grammar.patterns.map((p: { include: string }) => p.include);
    const vbTagIdx = includes.indexOf("#asp-vb-script-tag");
    const htmlIdx = includes.indexOf("text.html.basic");
    expect(vbTagIdx).toBeGreaterThanOrEqual(0);
    expect(htmlIdx).toBeGreaterThan(vbTagIdx);

    const rule = grammar.repository["asp-vb-script-tag"];
    expect(rule.contentName).toBe("source.vbscript.embedded.asp");
    expect(rule.patterns).toEqual([{ include: "#asp-vbscript" }]);

    const beginPattern = rule.begin.replace(/^\(\?i\)/, "");
    const re = new RegExp(beginPattern, "i");
    expect(re.test('<script language="vbscript">')).toBe(true);
    expect(re.test('<script language="vb">')).toBe(true);
    expect(re.test("<script language='vbs'>")).toBe(true);
    expect(re.test('<script type="text/vbscript">')).toBe(true);
    expect(re.test('<script runat="server">')).toBe(true);
    expect(re.test('<SCRIPT LANGUAGE="VBScript" RUNAT="Server">')).toBe(true);

    expect(re.test('<script language="javascript">')).toBe(false);
    expect(re.test('<script language="jscript" runat="server">')).toBe(false);
    expect(re.test('<script type="text/javascript">')).toBe(false);
    expect(re.test('<script src="app.js">')).toBe(false);
  });
});
