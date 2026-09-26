package lspserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

const duplicateProcedureParameterHoverSource = `<%
''' <summary>First Same documentation.</summary>
''' <param name="value">First value documentation.</param>
Function Same(ByVal value)
  Response.Write value
End Function
''' <summary>Second Same documentation.</summary>
''' <param name="other">Second other documentation.</param>
Function Same(ByVal other)
  Response.Write other
End Function
%>`

const duplicateProcedureParameterHoverSameNameSource = `<%
''' <summary>First Same documentation.</summary>
''' <param name="value">First value documentation.</param>
Function Same(ByVal value)
  Response.Write value
End Function
''' <summary>Second Same documentation.</summary>
''' <param name="value">Second value documentation.</param>
Function Same(ByVal value)
  Response.Write value
End Function
%>`

func TestParameterHoverUsesEnclosingDuplicateRootProcedure(t *testing.T) {
	const uri = "file:///tmp/duplicate-procedure-parameter-hover.asp"
	server, document := testServerWithVBScriptDocument(uri, duplicateProcedureParameterHoverSource)
	for _, testCase := range []struct {
		name   string
		needle string
		want   string
		reject string
	}{
		{name: "first procedure same-named parameter", needle: "Response.Write value", want: "First value documentation.", reject: "Second other documentation."},
		{name: "second procedure distinct parameter", needle: "Response.Write other", want: "Second other documentation.", reject: "First value documentation."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			offset := strings.Index(duplicateProcedureParameterHoverSource, testCase.needle)
			if offset < 0 {
				t.Fatalf("parameter reference %q not found", testCase.needle)
			}
			offset += len("Response.Write ")
			hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want parameter hover", hover)
			}
			text := duplicateProcedureHoverText(t, hover)
			if !strings.Contains(text, testCase.want) || strings.Contains(text, testCase.reject) {
				t.Fatalf("hover = %q, want %q without %q", text, testCase.want, testCase.reject)
			}
		})
	}
}

func TestParameterHoverUsesEnclosingDuplicateRootProcedureWithSameNamedParameters(t *testing.T) {
	const uri = "file:///tmp/duplicate-procedure-same-parameter-hover.asp"
	server, document := testServerWithVBScriptDocument(uri, duplicateProcedureParameterHoverSameNameSource)
	for _, testCase := range []struct {
		name       string
		occurrence int
		want       string
		reject     string
	}{
		{name: "first procedure", occurrence: 0, want: "First value documentation.", reject: "Second value documentation."},
		{name: "second procedure", occurrence: 1, want: "Second value documentation.", reject: "First value documentation."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			offset := duplicateProcedureParameterOccurrenceOffset(duplicateProcedureParameterHoverSameNameSource, "Response.Write value", testCase.occurrence)
			if offset < 0 {
				t.Fatalf("parameter reference occurrence %d not found", testCase.occurrence)
			}
			offset += len("Response.Write ")
			hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want parameter hover", hover)
			}
			text := duplicateProcedureHoverText(t, hover)
			if !strings.Contains(text, testCase.want) || strings.Contains(text, testCase.reject) {
				t.Fatalf("hover = %q, want %q without %q", text, testCase.want, testCase.reject)
			}
		})
	}
}

func TestParameterHoverDoesNotShareLocalShadowAcrossDuplicateProcedures(t *testing.T) {
	const uri = "file:///tmp/duplicate-procedure-local-shadow-hover.asp"
	const source = `<%
Function Same(ByVal value)
  Dim value
  Response.Write value
End Function
Function Same(ByVal value)
  Response.Write value
End Function
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	firstOffset := duplicateProcedureParameterOccurrenceOffset(source, "Response.Write value", 0) + len("Response.Write ")
	secondOffset := duplicateProcedureParameterOccurrenceOffset(source, "Response.Write value", 1) + len("Response.Write ")
	firstHover, ok := server.hover(uri, document.PositionAt(firstOffset)).(*lsp.Hover)
	if !ok || firstHover == nil || !strings.Contains(duplicateProcedureHoverText(t, firstHover), "(local) Dim value As Variant") {
		t.Fatalf("first duplicate hover = %#v, want local shadow", firstHover)
	}
	secondHover, ok := server.hover(uri, document.PositionAt(secondOffset)).(*lsp.Hover)
	if !ok || secondHover == nil || !strings.Contains(duplicateProcedureHoverText(t, secondHover), "ByVal value As Variant") {
		t.Fatalf("second duplicate hover = %#v, want parameter hover", secondHover)
	}
}

func TestParameterHoverUsesEnclosingDuplicateProcedureTypeAnnotation(t *testing.T) {
	const uri = "file:///tmp/duplicate-procedure-parameter-type-hover.asp"
	const source = `<%
' @param value As String
Function Same(ByVal value)
  Response.Write value
End Function
' @param value As Number
Function Same(ByVal value)
  Response.Write value
End Function
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for occurrence, want := range []string{"ByVal value As String", "ByVal value As Number"} {
		offset := duplicateProcedureParameterOccurrenceOffset(source, "Response.Write value", occurrence) + len("Response.Write ")
		hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
		if !ok || hover == nil || !strings.Contains(duplicateProcedureHoverText(t, hover), want) {
			t.Fatalf("duplicate parameter occurrence %d hover = %#v, want %s", occurrence, hover, want)
		}
	}
}

func TestParameterHoverDoesNotBorrowTypeFromDuplicateProcedure(t *testing.T) {
	const uri = "file:///tmp/duplicate-procedure-missing-parameter-type-hover.asp"
	const source = `<%
Function Same(ByVal value)
  Response.Write value
End Function
' @param value As Number
Function Same(ByVal value)
  Response.Write value
End Function
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	firstOffset := duplicateProcedureParameterOccurrenceOffset(source, "Response.Write value", 0) + len("Response.Write ")
	secondOffset := duplicateProcedureParameterOccurrenceOffset(source, "Response.Write value", 1) + len("Response.Write ")
	firstHover, ok := server.hover(uri, document.PositionAt(firstOffset)).(*lsp.Hover)
	if !ok || firstHover == nil || !strings.Contains(duplicateProcedureHoverText(t, firstHover), "ByVal value As Variant") {
		t.Fatalf("first duplicate without type hover = %#v, want Variant", firstHover)
	}
	secondHover, ok := server.hover(uri, document.PositionAt(secondOffset)).(*lsp.Hover)
	if !ok || secondHover == nil || !strings.Contains(duplicateProcedureHoverText(t, secondHover), "ByVal value As Number") {
		t.Fatalf("second duplicate with type hover = %#v, want Number", secondHover)
	}
}

func duplicateProcedureParameterOccurrenceOffset(source, needle string, occurrence int) int {
	start := 0
	for index := 0; index <= occurrence; index++ {
		found := strings.Index(source[start:], needle)
		if found < 0 {
			return -1
		}
		start += found
		if index == occurrence {
			return start
		}
		start += len(needle)
	}
	return -1
}

func duplicateProcedureHoverText(t *testing.T, hover *lsp.Hover) string {
	t.Helper()
	contents, ok := hover.Contents.(lsp.MarkupContent)
	if !ok {
		t.Fatalf("hover contents = %#v, want markdown content", hover.Contents)
	}
	return contents.Value
}

func TestParameterHoverUsesEnclosingDuplicateClassProcedure(t *testing.T) {
	const uri = "file:///tmp/duplicate-class-procedure-parameter-hover.asp"
	const source = `<%
Class Widget
''' <param name="value">First class value documentation.</param>
Public Function Same(ByVal value)
  Response.Write value
End Function
''' <param name="other">Second class other documentation.</param>
Public Function Same(ByVal other)
  Response.Write other
End Function
End Class
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, testCase := range []struct {
		name   string
		needle string
		want   string
		reject string
	}{
		{name: "first class procedure same-named parameter", needle: "Response.Write value", want: "First class value documentation.", reject: "Second class other documentation."},
		{name: "second class procedure distinct parameter", needle: "Response.Write other", want: "Second class other documentation.", reject: "First class value documentation."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			offset := strings.Index(source, testCase.needle)
			if offset < 0 {
				t.Fatalf("parameter reference %q not found", testCase.needle)
			}
			offset += len("Response.Write ")
			hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want parameter hover", hover)
			}
			text := duplicateProcedureHoverText(t, hover)
			if !strings.Contains(text, testCase.want) || strings.Contains(text, testCase.reject) {
				t.Fatalf("hover = %q, want %q without %q", text, testCase.want, testCase.reject)
			}
		})
	}
}

func TestParameterHoverUsesEnclosingDuplicatePropertyAccessor(t *testing.T) {
	const uri = "file:///tmp/duplicate-property-parameter-hover.asp"
	const source = `<%
Class Widget
''' <param name="value">First property value documentation.</param>
Property Get Same(ByVal value)
  Response.Write value
End Property
''' <param name="other">Second property other documentation.</param>
Property Get Same(ByVal other)
  Response.Write other
End Property
End Class
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, testCase := range []struct {
		name   string
		needle string
		want   string
		reject string
	}{
		{name: "first property same-named parameter", needle: "Response.Write value", want: "First property value documentation.", reject: "Second property other documentation."},
		{name: "second property distinct parameter", needle: "Response.Write other", want: "Second property other documentation.", reject: "First property value documentation."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			offset := strings.Index(source, testCase.needle)
			if offset < 0 {
				t.Fatalf("parameter reference %q not found", testCase.needle)
			}
			offset += len("Response.Write ")
			hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want parameter hover", hover)
			}
			text := duplicateProcedureHoverText(t, hover)
			if !strings.Contains(text, testCase.want) || strings.Contains(text, testCase.reject) {
				t.Fatalf("hover = %q, want %q without %q", text, testCase.want, testCase.reject)
			}
		})
	}
}

func TestStdioParameterHoverUsesEnclosingDuplicateRootProcedure(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "duplicate-procedure-parameter-hover.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, duplicateProcedureParameterHoverSource)

	document := core.NewTextDocument(uri, "classic-asp", 1, duplicateProcedureParameterHoverSource)
	for _, testCase := range []struct {
		name   string
		needle string
		want   string
		reject string
	}{
		{name: "first procedure same-named parameter", needle: "Response.Write value", want: "First value documentation.", reject: "Second other documentation."},
		{name: "second procedure distinct parameter", needle: "Response.Write other", want: "Second other documentation.", reject: "First value documentation."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			offset := strings.Index(duplicateProcedureParameterHoverSource, testCase.needle)
			if offset < 0 {
				t.Fatalf("parameter reference %q not found", testCase.needle)
			}
			offset += len("Response.Write ")
			response := client.request("textDocument/hover", map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"position":     positionAt(duplicateProcedureParameterHoverSource, offset),
			})
			if response.Result == nil || mustJSONText(t, response.Result) == "null" {
				t.Fatalf("stdio hover result = %s, want parameter hover", mustJSONText(t, response.Result))
			}
			text := hoverMarkdownValue(t, response.Result)
			if !strings.Contains(text, testCase.want) || strings.Contains(text, testCase.reject) {
				t.Fatalf("stdio hover = %q, want %q without %q", text, testCase.want, testCase.reject)
			}
			var hover lsp.Hover
			mustDecodeResult(t, response.Result, &hover)
			if hover.Range == nil {
				t.Fatal("stdio parameter hover range is nil")
			}
			wantRange := lsp.Range{Start: document.PositionAt(offset), End: document.PositionAt(offset + len(strings.TrimPrefix(testCase.needle, "Response.Write ")))}
			if *hover.Range != wantRange {
				t.Fatalf("stdio parameter hover range = %#v, want %#v", *hover.Range, wantRange)
			}
		})
	}
}

func TestStdioParameterHoverUsesEnclosingDuplicateRootProcedureWithSameNamedParameters(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "duplicate-procedure-same-parameter-hover.asp"))
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, duplicateProcedureParameterHoverSameNameSource)

	for _, testCase := range []struct {
		name       string
		occurrence int
		want       string
		reject     string
	}{
		{name: "first procedure", occurrence: 0, want: "First value documentation.", reject: "Second value documentation."},
		{name: "second procedure", occurrence: 1, want: "Second value documentation.", reject: "First value documentation."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			offset := duplicateProcedureParameterOccurrenceOffset(duplicateProcedureParameterHoverSameNameSource, "Response.Write value", testCase.occurrence)
			if offset < 0 {
				t.Fatalf("parameter reference occurrence %d not found", testCase.occurrence)
			}
			offset += len("Response.Write ")
			response := client.request("textDocument/hover", map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"position":     positionAt(duplicateProcedureParameterHoverSameNameSource, offset),
			})
			if response.Result == nil || mustJSONText(t, response.Result) == "null" {
				t.Fatalf("stdio hover result = %s, want parameter hover", mustJSONText(t, response.Result))
			}
			text := hoverMarkdownValue(t, response.Result)
			if !strings.Contains(text, testCase.want) || strings.Contains(text, testCase.reject) {
				t.Fatalf("stdio hover = %q, want %q without %q", text, testCase.want, testCase.reject)
			}
		})
	}
}
