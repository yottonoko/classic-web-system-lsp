package lspserver

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

const incompleteProcedureHeaderSource = `<%
Function Root(Optional ByVal value = "fallback", ByRef second
  Response.Write value
End Function
Class Widget
  Public Sub Method(ByRef item
    Response.Write item
  End Sub
  Public Property Get Value(Optional ByVal index
    Response.Write index
  End Property
End Class
Dim widget
Set widget = New Widget
widget.Value("x")
Root("x", "y")
%>`

func TestIncompleteProcedureHeadersPreserveParametersForHoverAndSignatureHelp(t *testing.T) {
	const uri = "file:///tmp/incomplete-procedure-header.asp"
	server, document := testServerWithVBScriptDocument(uri, incompleteProcedureHeaderSource)
	for _, testCase := range []struct {
		name   string
		needle string
		length int
		want   string
	}{
		{name: "root function parameter", needle: "Response.Write value", length: len("value"), want: "ByVal value As Variant"},
		{name: "class sub parameter", needle: "Response.Write item", length: len("item"), want: "ByRef item As Variant"},
		{name: "property getter parameter", needle: "Response.Write index", length: len("index"), want: "ByVal index As Variant"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			offset := strings.Index(incompleteProcedureHeaderSource, testCase.needle) + strings.Index(testCase.needle, " ") + 1
			hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want parameter hover", hover)
			}
			text := hoverMarkdownValueForTest(t, hover)
			if !strings.Contains(text, testCase.want) {
				t.Fatalf("hover = %q, want %q", text, testCase.want)
			}
			if hover.Range == nil || *hover.Range != document.Range(offset, offset+testCase.length) {
				t.Fatalf("hover range = %#v, want %v", hover.Range, document.Range(offset, offset+testCase.length))
			}
		})
	}

	rootCall := strings.Index(incompleteProcedureHeaderSource, `Root("x", "y")`)
	rootHelp := server.signatureHelp(uri, document.PositionAt(rootCall+len(`Root("`)))
	if rootHelp == nil || len(rootHelp.Signatures) != 1 || !strings.Contains(rootHelp.Signatures[0].Label, "ByVal value") {
		t.Fatalf("root signature help = %#v, want recovered parameter list", rootHelp)
	}
	if len(rootHelp.Signatures[0].Parameters) != 2 {
		t.Fatalf("root signature parameters = %#v, want two parameters", rootHelp.Signatures[0].Parameters)
	}

	propertyCall := strings.Index(incompleteProcedureHeaderSource, `widget.Value("x")`)
	propertyHelp := server.signatureHelp(uri, document.PositionAt(propertyCall+len(`widget.Value("`)))
	if propertyHelp == nil || len(propertyHelp.Signatures) != 1 || !strings.Contains(propertyHelp.Signatures[0].Label, "Property Get Value") {
		t.Fatalf("property signature help = %#v, want recovered accessor signature", propertyHelp)
	}
	if len(propertyHelp.Signatures[0].Parameters) != 1 {
		t.Fatalf("property signature parameters = %#v, want one parameter", propertyHelp.Signatures[0].Parameters)
	}
}

func TestIncompleteProcedureHeaderColonDoesNotSwallowFollowingStatement(t *testing.T) {
	const uri = "file:///tmp/incomplete-procedure-header-colon.asp"
	source := `<%
Function Broken(ByVal value: Dim following
  Response.Write following
End Function
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	offset := strings.Index(source, "Response.Write following") + len("Response.Write ")
	hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
	if !ok || hover == nil {
		t.Fatalf("following-statement hover = %#v, want local declaration hover", hover)
	}
	text := hoverMarkdownValueForTest(t, hover)
	if !strings.Contains(text, "(local) Dim following As Variant") {
		t.Fatalf("following-statement hover = %q, want local Dim declaration", text)
	}
}

func TestStdioIncompleteProcedureHeaderPreservesParameterHover(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(root + "/incomplete-parameter.asp")
	source := `<%
Function Root(ByVal value
  Response.Write value
End Function
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	openClassicASPDocument(t, client, uri, source)
	offset := strings.Index(source, "Response.Write value") + len("Response.Write ")
	response := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, offset),
	})
	if response.Result == nil || mustJSONText(t, response.Result) == "null" {
		t.Fatalf("stdio hover result = %s, want recovered parameter hover", mustJSONText(t, response.Result))
	}
	if text := hoverMarkdownValue(t, response.Result); !strings.Contains(text, "ByVal value As Variant") {
		t.Fatalf("stdio hover = %q, want recovered parameter metadata", text)
	}
}

func TestMultilineProcedureHeadersPreserveParameterHoverAndSignatureHelp(t *testing.T) {
	const uri = "file:///tmp/multiline-procedure-header.asp"
	const source = `<%
Function Multi( _
  Optional ByVal first = Lookup("a,b)"), _
  ByRef second _
)
  Response.Write first
  Response.Write second
End Function
Class Widget
  Public Sub Method( _
    ByVal item, _
    ByRef other _
  )
    Response.Write item
    Response.Write other
  End Sub
  Public Property Get Value( _
    Optional ByVal index = Lookup(1, 2) _
  )
    Response.Write index
  End Property
  Public Property Let Assigned( _
    ByVal key, _
    Optional ByRef scalar = Lookup("fallback") _
  )
    Response.Write scalar
  End Property
  Public Property Set AssignedObject( _
    ByVal key, _
    ByRef objectValue _
  )
    Response.Write objectValue
  End Property
End Class
Dim widget
Set widget = New Widget
Multi("x", "y")
widget.Value("x")
widget.Assigned("x", "y")
widget.AssignedObject("x", objectValue)
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, testCase := range []struct {
		name   string
		needle string
		want   string
	}{
		{name: "root function first parameter", needle: "Response.Write first", want: "ByVal first As Variant"},
		{name: "root function second parameter", needle: "Response.Write second", want: "ByRef second As Variant"},
		{name: "class sub first parameter", needle: "Response.Write item", want: "ByVal item As Variant"},
		{name: "class sub second parameter", needle: "Response.Write other", want: "ByRef other As Variant"},
		{name: "property getter parameter", needle: "Response.Write index", want: "ByVal index As Variant"},
		{name: "property setter parameter", needle: "Response.Write scalar", want: "ByRef scalar As Variant"},
		{name: "property object setter parameter", needle: "Response.Write objectValue", want: "ByRef objectValue As Variant"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			nameStart := strings.LastIndex(testCase.needle, " ") + 1
			nameLength := len(testCase.needle) - nameStart
			offset := strings.Index(source, testCase.needle) + nameStart
			hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want parameter hover", hover)
			}
			text := hoverMarkdownValueForTest(t, hover)
			if !strings.Contains(text, testCase.want) {
				t.Fatalf("hover = %q, want %q", text, testCase.want)
			}
			if hover.Range == nil || *hover.Range != document.Range(offset, offset+nameLength) {
				t.Fatalf("hover range = %#v, want %v", hover.Range, document.Range(offset, offset+nameLength))
			}
		})
	}

	callOffset := strings.Index(source, `Multi("x", "y")`) + len(`Multi("`)
	rootHelp := server.signatureHelp(uri, document.PositionAt(callOffset))
	if rootHelp == nil || len(rootHelp.Signatures) != 1 || rootHelp.Signatures[0].Label != "Multi(ByVal first, ByRef second)" {
		t.Fatalf("root signature help = %#v, want recovered multiline signature", rootHelp)
	}
	if len(rootHelp.Signatures[0].Parameters) != 2 {
		t.Fatalf("root signature parameters = %#v, want two parameters", rootHelp.Signatures[0].Parameters)
	}

	propertyOffset := strings.Index(source, `widget.Value("x")`) + len(`widget.Value("`)
	propertyHelp := server.signatureHelp(uri, document.PositionAt(propertyOffset))
	if propertyHelp == nil || len(propertyHelp.Signatures) != 1 || propertyHelp.Signatures[0].Label != "Public Property Get Value(Optional ByVal index = Lookup(1, 2))" {
		t.Fatalf("property signature help = %#v, want recovered multiline accessor signature", propertyHelp)
	}
	if len(propertyHelp.Signatures[0].Parameters) != 1 {
		t.Fatalf("property signature parameters = %#v, want one parameter", propertyHelp.Signatures[0].Parameters)
	}

	for _, testCase := range []struct {
		call  string
		label string
		count int
	}{
		{call: `widget.Assigned("x", "y")`, label: "Public Property Let Assigned(ByVal key, Optional ByRef scalar = Lookup(\"fallback\"))", count: 2},
		{call: `widget.AssignedObject("x", objectValue)`, label: "Public Property Set AssignedObject(ByVal key, ByRef objectValue)", count: 2},
	} {
		callOffset := strings.Index(source, testCase.call) + len(testCase.call[:strings.Index(testCase.call, "(")+1])
		help := server.signatureHelp(uri, document.PositionAt(callOffset))
		if help == nil || len(help.Signatures) != 1 || help.Signatures[0].Label != testCase.label {
			t.Fatalf("%s signature help = %#v, want %s", testCase.call, help, testCase.label)
		}
		if len(help.Signatures[0].Parameters) != testCase.count {
			t.Fatalf("%s signature parameters = %#v, want %d", testCase.call, help.Signatures[0].Parameters, testCase.count)
		}
	}
}

func TestMultilineProcedureHeaderStdioHoverUsesLatestParameters(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		filename   string
		languageID string
		prefix     string
		suffix     string
	}{
		{name: "vbscript", filename: "multiline-parameter.vbs", languageID: "vbscript"},
		{name: "classic asp", filename: "multiline-parameter.asp", languageID: "classic-asp", prefix: "<%\r\n", suffix: "%>\r\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := startStdioTestClient(t)
			defer client.close()

			root := t.TempDir()
			uri := pathToFileURI(root + "/" + testCase.filename)
			body := "Function Multi( _\r\n  ByVal first, _\r\n  ByRef second _\r\n)\r\n  Response.Write second\r\nEnd Function\r\n"
			source := testCase.prefix + body + testCase.suffix
			client.request("initialize", map[string]any{
				"processId":    nil,
				"rootUri":      pathToFileURI(root),
				"capabilities": map[string]any{},
			})
			if err := client.notify("textDocument/didOpen", map[string]any{
				"textDocument": map[string]any{
					"uri":        uri,
					"languageId": testCase.languageID,
					"version":    1,
					"text":       source,
				},
			}); err != nil {
				t.Fatal(err)
			}
			offset := strings.Index(source, "Response.Write second") + len("Response.Write ")
			response := client.request("textDocument/hover", map[string]any{
				"textDocument": map[string]any{"uri": uri},
				"position":     positionAt(source, offset),
			})
			if response.Result == nil || mustJSONText(t, response.Result) == "null" {
				t.Fatalf("stdio multiline hover result = %s, want parameter hover", mustJSONText(t, response.Result))
			}
			if text := hoverMarkdownValue(t, response.Result); !strings.Contains(text, "ByRef second As Variant") {
				t.Fatalf("stdio multiline hover = %q, want recovered parameter metadata", text)
			}
		})
	}
}

func TestModifierLedProcedureAfterIncompleteContinuationPreservesParameterHover(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		declaration string
		terminator  string
	}{
		{name: "public sub", declaration: "Public Sub FollowingPublic(ByVal second)", terminator: "End Sub"},
		{name: "private function", declaration: "Private Function FollowingPrivate(ByRef second)", terminator: "End Function"},
		{name: "default property", declaration: "Default Property Get FollowingDefault(ByVal second)", terminator: "End Property"},
		{name: "public default property", declaration: "Public Default Property Get FollowingCombined(ByRef second)", terminator: "End Property"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := "<%\nClass Recovery\n  Function BrokenContinuation( _\n    ByVal first _\n  " + testCase.declaration + "\n    Response.Write second\n  " + testCase.terminator + "\nEnd Class\n%>"
			const uri = "file:///tmp/modifier-led-recovery.asp"
			server, document := testServerWithVBScriptDocument(uri, source)
			offset := strings.Index(source, "Response.Write second") + len("Response.Write ")
			hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("parameter hover = %#v, want recovered following procedure parameter", hover)
			}
			want := "ByVal second As Variant"
			if strings.Contains(strings.ToLower(testCase.declaration), "byref second") {
				want = "ByRef second As Variant"
			}
			if text := hoverMarkdownValueForTest(t, hover); !strings.Contains(text, want) {
				t.Fatalf("parameter hover = %q, want %q", text, want)
			}
		})
	}
}

func TestStdioModifierLedProcedureAfterIncompleteContinuationPreservesParameterHover(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		filename   string
		languageID string
		prefix     string
		suffix     string
	}{
		{name: "classic asp", filename: "modifier-recovery.asp", languageID: "classic-asp", prefix: "<%\r\n", suffix: "%>\r\n"},
		{name: "vbscript", filename: "modifier-recovery.vbs", languageID: "vbscript"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := startStdioTestClient(t)
			defer client.close()
			root := t.TempDir()
			uri := pathToFileURI(root + "/" + testCase.filename)
			body := "Class Recovery\r\n  Function BrokenContinuation( _\r\n    ByVal first _\r\n  Public Sub FollowingPublic(ByVal second)\r\n    Response.Write second\r\n  End Sub\r\nEnd Class\r\n"
			source := testCase.prefix + body + testCase.suffix
			client.request("initialize", map[string]any{
				"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
			})
			if err := client.notify("textDocument/didOpen", map[string]any{
				"textDocument": map[string]any{"uri": uri, "languageId": testCase.languageID, "version": 1, "text": source},
			}); err != nil {
				t.Fatal(err)
			}
			offset := strings.Index(source, "Response.Write second") + len("Response.Write ")
			response := client.request("textDocument/hover", map[string]any{
				"textDocument": map[string]any{"uri": uri}, "position": positionAt(source, offset),
			})
			if response.Result == nil || mustJSONText(t, response.Result) == "null" {
				t.Fatalf("stdio parameter hover = %s, want recovered Public Sub parameter", mustJSONText(t, response.Result))
			}
			if text := hoverMarkdownValue(t, response.Result); !strings.Contains(text, "ByVal second As Variant") {
				t.Fatalf("stdio parameter hover = %q, want ByVal second As Variant", text)
			}
		})
	}
}
