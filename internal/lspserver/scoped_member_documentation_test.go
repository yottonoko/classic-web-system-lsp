package lspserver

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestScopedMemberDocumentationUsesClassDeclarationForHoverAndSignatureHelp(t *testing.T) {
	const uri = "file:///tmp/scoped-member-documentation.asp"
	source := `<%
''' <summary>Root method documentation.</summary>
''' <param name="value">Root value documentation.</param>
Function Method(ByVal value)
End Function
Class Widget
  ''' <summary>Widget method documentation.</summary>
  ''' <param name="value">Widget value documentation.</param>
  Public Function Method(ByVal value)
  End Function
  Public Sub Caller()
    Method("widget")
  End Sub
End Class
%>`
	server, document := testServerWithVBScriptDocument(uri, source)

	callOffset := strings.Index(source, `Method("widget")`)
	if callOffset < 0 {
		t.Fatal("class method call not found")
	}
	hoverValue, ok := server.hover(uri, document.PositionAt(callOffset)).(*lsp.Hover)
	if !ok || hoverValue == nil {
		t.Fatalf("hover = %#v, want class method hover", hoverValue)
	}
	hoverText := hoverMarkdownValueForTest(t, hoverValue)
	for _, expected := range []string{"Widget method documentation.", "Widget value documentation."} {
		if !strings.Contains(hoverText, expected) {
			t.Fatalf("hover = %q, want %q", hoverText, expected)
		}
	}
	if strings.Contains(hoverText, "Root method documentation.") {
		t.Fatalf("hover = %q, unexpectedly used root method documentation", hoverText)
	}

	argumentOffset := strings.Index(source, `Method("widget")`) + len(`Method("`)
	help := server.signatureHelp(uri, document.PositionAt(argumentOffset))
	if help == nil || len(help.Signatures) != 1 {
		t.Fatalf("signature help = %#v, want one class method signature", help)
	}
	if !strings.Contains(signatureDocumentationText(help.Signatures[0].Documentation), "Widget method documentation.") {
		t.Fatalf("signature help documentation = %#v, want class method docs", help.Signatures[0].Documentation)
	}
	if strings.Contains(signatureDocumentationText(help.Signatures[0].Documentation), "Root method documentation.") {
		t.Fatalf("signature help documentation = %#v, unexpectedly used root docs", help.Signatures[0].Documentation)
	}
}

func TestPropertyAccessorDocumentationUsesAccessorDeclaration(t *testing.T) {
	const uri = "file:///tmp/property-accessor-documentation.asp"
	source := `<%
Class Widget
  ''' <summary>Getter documentation.</summary>
  Property Get Value()
    Value = 1
  End Property
  ''' <summary>Setter documentation.</summary>
  Property Let Value(ByVal value)
  End Property
  ''' <summary>Object setter documentation.</summary>
  Property Set Value(ByRef value)
  End Property
End Class
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, testCase := range []struct {
		name string
		docs string
	}{
		{name: "Get", docs: "Getter documentation."},
		{name: "Let", docs: "Setter documentation."},
		{name: "Set", docs: "Object setter documentation."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			needle := "Property " + testCase.name + " Value"
			offset := strings.Index(source, needle) + len(needle) - len("Value")
			hoverValue, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hoverValue == nil {
				t.Fatalf("hover = %#v, want property hover", hoverValue)
			}
			text := hoverMarkdownValueForTest(t, hoverValue)
			if !strings.Contains(text, testCase.docs) {
				t.Fatalf("hover = %q, want %q", text, testCase.docs)
			}
		})
	}
}

func TestCompletionDocumentationUsesTheDeclarationRange(t *testing.T) {
	const uri = "file:///tmp/completion-declaration-documentation.asp"
	source := `<%
Class Widget
  ''' <summary>Widget completion documentation.</summary>
  Public Function Method(ByVal value)
  End Function
End Class
''' <summary>Root completion documentation.</summary>
Function Method(ByVal value)
End Function
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	offset := strings.Index(source, "Class Widget")
	items := server.completion(context.Background(), uri, document.PositionAt(offset), nil).Items
	var method *lsp.CompletionItem
	for index := range items {
		if items[index].Label == "Method" {
			method = &items[index]
			break
		}
	}
	if method == nil {
		t.Fatalf("Method completion missing: %#v", items)
	}
	if !strings.Contains(signatureDocumentationText(method.Documentation), "Widget completion documentation.") {
		t.Fatalf("Method completion documentation = %#v, want class declaration docs", method.Documentation)
	}
}

func TestQualifiedMemberDocumentationUsesOwningClass(t *testing.T) {
	const uri = "file:///tmp/qualified-member-documentation.asp"
	source := `<%
Class Widget
  ''' <summary>Widget qualified documentation.</summary>
  Public Function Method(ByVal value)
  End Function
End Class
Class Gadget
  ' Gadget plain documentation.
  Public Function Method(ByVal value)
  End Function
End Class
Dim widget, gadget
Set widget = New Widget
Set gadget = New Gadget
widget.Method("widget")
gadget.Method("gadget")
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, test := range []struct {
		call   string
		want   string
		reject string
	}{
		{call: `widget.Method("widget")`, want: "Widget qualified documentation.", reject: "Gadget plain documentation"},
		{call: `gadget.Method("gadget")`, want: "Gadget plain documentation", reject: "Widget qualified documentation."},
	} {
		t.Run(test.call, func(t *testing.T) {
			callStart := strings.Index(source, test.call)
			nameStart := callStart + strings.Index(test.call, "Method")
			hover, ok := server.hover(uri, document.PositionAt(nameStart)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want qualified member hover", hover)
			}
			hoverText := hoverMarkdownValueForTest(t, hover)
			if !strings.Contains(hoverText, test.want) || strings.Contains(hoverText, test.reject) {
				t.Fatalf("hover = %q, want %q without %q", hoverText, test.want, test.reject)
			}
			argumentOffset := callStart + strings.Index(test.call, `("`) + 2
			help := server.signatureHelp(uri, document.PositionAt(argumentOffset))
			if help == nil || len(help.Signatures) != 1 {
				t.Fatalf("signature help = %#v, want one qualified member signature", help)
			}
			docs := signatureDocumentationText(help.Signatures[0].Documentation)
			if !strings.Contains(docs, test.want) || strings.Contains(docs, test.reject) {
				t.Fatalf("signature help docs = %q, want %q without %q", docs, test.want, test.reject)
			}
		})
	}
}

func TestPropertyAccessorSignatureHelpUsesAccessorDocumentation(t *testing.T) {
	const uri = "file:///tmp/property-accessor-signature-documentation.asp"
	source := `<%
Class Widget
  ''' <summary>Indexed getter documentation.</summary>
  Public Property Get Indexed(ByVal index)
    Indexed = index
  End Property
  ''' <summary>Assigned setter documentation.</summary>
  Public Property Let Assigned(ByVal index, ByVal value)
  End Property
  ''' <summary>Object setter documentation.</summary>
  Public Property Set AssignedObject(ByVal index, ByRef value)
  End Property
End Class
Dim widget
Set widget = New Widget
widget.Indexed("id")
widget.Assigned("id", value)
widget.AssignedObject("id", value)
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, test := range []struct {
		call string
		want string
	}{
		{call: `widget.Indexed("id")`, want: "Indexed getter documentation."},
		{call: `widget.Assigned("id", value)`, want: "Assigned setter documentation."},
		{call: `widget.AssignedObject("id", value)`, want: "Object setter documentation."},
	} {
		t.Run(test.call, func(t *testing.T) {
			offset := strings.Index(source, test.call) + strings.Index(test.call, `("`) + 2
			help := server.signatureHelp(uri, document.PositionAt(offset))
			if help == nil || len(help.Signatures) != 1 {
				t.Fatalf("signature help = %#v, want one property accessor signature", help)
			}
			docs := signatureDocumentationText(help.Signatures[0].Documentation)
			if !strings.Contains(docs, test.want) {
				t.Fatalf("signature help docs = %q, want %q", docs, test.want)
			}
		})
	}
}

func TestRootSignatureDocumentationIgnoresEarlierClassMethod(t *testing.T) {
	const uri = "file:///tmp/root-signature-after-class.asp"
	source := `<%
Class Widget
  ''' <summary>Class-first documentation.</summary>
  Public Function Method(ByVal classOnly)
  End Function
End Class
''' <summary>Root-later documentation.</summary>
''' <param name="rootOnly">Root argument documentation.</param>
Function Method(ByVal rootOnly)
End Function
Method("root")
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	callStart := strings.LastIndex(source, `Method("root")`)
	hover, ok := server.hover(uri, document.PositionAt(callStart)).(*lsp.Hover)
	if !ok || hover == nil {
		t.Fatalf("hover = %#v, want root method hover", hover)
	}
	hoverText := hoverMarkdownValueForTest(t, hover)
	if !strings.Contains(hoverText, "Root-later documentation.") || strings.Contains(hoverText, "Class-first documentation.") {
		t.Fatalf("hover = %q, want root docs without class docs", hoverText)
	}
	help := server.signatureHelp(uri, document.PositionAt(callStart+len(`Method("`)))
	if help == nil || len(help.Signatures) != 1 {
		t.Fatalf("signature help = %#v, want root signature", help)
	}
	if !strings.Contains(help.Signatures[0].Label, "rootOnly") || strings.Contains(help.Signatures[0].Label, "classOnly") {
		t.Fatalf("signature label = %q, want root parameter", help.Signatures[0].Label)
	}
	docs := signatureDocumentationText(help.Signatures[0].Documentation)
	if !strings.Contains(docs, "Root-later documentation.") || strings.Contains(docs, "Class-first documentation.") {
		t.Fatalf("signature docs = %q, want root docs without class docs", docs)
	}
}

func TestSignatureHelpScannerRespectsStringsAndComments(t *testing.T) {
	const uri = "file:///tmp/signature-help-lexical-context.asp"
	source := `<%
Class Widget
  ''' <summary>Class method documentation.</summary>
  Public Function Method(ByVal first, ByVal second)
  End Function
  Public Sub Caller()
    Method("a:b", 2)
    ' Method("comment", 2)
    Rem Method("comment", 2)
  End Sub
End Class
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	colon := strings.Index(source, "a:b") + 2
	help := server.signatureHelp(uri, document.PositionAt(colon))
	if help == nil || len(help.Signatures) != 1 || !strings.Contains(help.Signatures[0].Label, "first") {
		t.Fatalf("colon string signature help = %#v, want class method", help)
	}
	if docs := signatureDocumentationText(help.Signatures[0].Documentation); !strings.Contains(docs, "Class method documentation.") {
		t.Fatalf("colon string signature docs = %q", docs)
	}
	for _, comment := range []string{`' Method("comment", 2)`, `Rem Method("comment", 2)`} {
		offset := strings.Index(source, comment) + strings.Index(comment, "comment") + len("comment")
		if help := server.signatureHelp(uri, document.PositionAt(offset)); help != nil {
			t.Fatalf("comment %q signature help = %#v, want nil", comment, help)
		}
	}
}

func TestRootSignatureHelpRejectsClassOnlyMethod(t *testing.T) {
	const uri = "file:///tmp/root-class-only-signature.asp"
	source := `<%
Class Alpha
  ''' <summary>Alpha class-only documentation.</summary>
  Public Function Same(ByVal alphaOnly)
  End Function
End Class
Class Beta
  ''' <summary>Beta class-only documentation.</summary>
  Public Function Same(ByVal betaOnly)
  End Function
End Class
Same("root")
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	offset := strings.LastIndex(source, `Same("root")`) + len(`Same("`)
	if help := server.signatureHelp(uri, document.PositionAt(offset)); help != nil {
		t.Fatalf("root class-only signature help = %#v, want nil", help)
	}
}

func TestStandaloneRootSignatureHelpRejectsClassOnlyMethod(t *testing.T) {
	const uri = "file:///tmp/root-class-only-signature.vbs"
	source := `Class Widget
  Public Function Same(ByRef classOnly)
  End Function
End Class
Same("root")
`
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	document := core.NewTextDocument(uri, "vbscript", 1, source)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, document)
	server.mu.Unlock()
	offset := strings.LastIndex(source, `Same("root")`) + len(`Same("`)
	if help := server.signatureHelp(uri, document.PositionAt(offset)); help != nil {
		t.Fatalf("standalone root class-only signature help = %#v, want nil", help)
	}
}

func TestBuiltinSignatureHelpSurvivesClassMethodNameCollision(t *testing.T) {
	const uri = "file:///tmp/builtin-class-name-collision.asp"
	source := `<%
Class Widget
  Public Function CStr(ByVal classOnly)
  End Function
End Class
value = CStr("root")
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	offset := strings.LastIndex(source, `CStr("root")`) + len(`CStr("`)
	help := server.signatureHelp(uri, document.PositionAt(offset))
	if help == nil || len(help.Signatures) != 1 {
		t.Fatalf("builtin signature help = %#v, want CStr builtin", help)
	}
	if label := help.Signatures[0].Label; !strings.Contains(label, "CStr(value)") || strings.Contains(label, "classOnly") {
		t.Fatalf("builtin signature label = %q, want CStr(value)", label)
	}
}

func TestNoParenSignatureHelpAfterColonUsesCurrentStatement(t *testing.T) {
	const uri = "file:///tmp/no-paren-signature-after-colon.asp"
	source := `<%
''' <summary>Root procedure documentation.</summary>
Sub RootProc(ByVal first, ByVal second)
End Sub
x = 1 : RootProc "after-colon", 2
Class Widget
  ''' <summary>Class method documentation.</summary>
  Public Sub Method(ByVal first, ByVal second)
  End Sub
  Public Sub Caller()
    x = 1 : Method "class-after-colon", 2
  End Sub
End Class
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, test := range []struct {
		call string
		want string
	}{
		{call: `RootProc "after-colon", 2`, want: "Root procedure documentation."},
		{call: `Method "class-after-colon", 2`, want: "Class method documentation."},
	} {
		t.Run(test.call, func(t *testing.T) {
			offset := strings.Index(source, test.call) + strings.Index(test.call, "after-colon") + len("after-colon")
			help := server.signatureHelp(uri, document.PositionAt(offset))
			if help == nil || len(help.Signatures) != 1 {
				t.Fatalf("signature help = %#v, want one no-paren signature", help)
			}
			if docs := signatureDocumentationText(help.Signatures[0].Documentation); !strings.Contains(docs, test.want) {
				t.Fatalf("signature docs = %q, want %q", docs, test.want)
			}
		})
	}
}

func TestParenthesizedSignatureHelpHonorsLocalShadow(t *testing.T) {
	const uri = "file:///tmp/parenthesized-local-shadow-signature.asp"
	source := `<%
Function Same(ByVal rootOnly)
End Function
Sub LocalCaller()
  Dim Same
  Same("local")
End Sub
Sub ParameterCaller(ByVal CStr)
  CStr("parameter")
End Sub
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, call := range []string{`Same("local")`, `CStr("parameter")`} {
		offset := strings.Index(source, call) + strings.Index(call, `("`) + 2
		if help := server.signatureHelp(uri, document.PositionAt(offset)); help != nil {
			t.Fatalf("shadowed call %q signature help = %#v, want nil", call, help)
		}
	}
}

func TestHoverResolvesForwardRootProcedure(t *testing.T) {
	const uri = "file:///tmp/forward-root-procedure-hover.asp"
	source := `<%
Response.Write Later("x")
''' <summary>Forward root documentation.</summary>
Function Later(ByVal value)
  Later = value
End Function
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	offset := strings.Index(source, `Later("x")`)
	hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
	if !ok || hover == nil {
		t.Fatalf("forward root hover = %#v, want procedure hover", hover)
	}
	text := hoverMarkdownValueForTest(t, hover)
	if !strings.Contains(text, "Later(ByVal value)") || !strings.Contains(text, "Forward root documentation.") {
		t.Fatalf("forward root hover = %q", text)
	}
}

func TestNoParenSignatureHelpTracksNestedArguments(t *testing.T) {
	const uri = "file:///tmp/no-paren-nested-arguments.asp"
	source := `<%
Sub P(ByVal first, ByVal second, ByVal third)
End Sub
P "a,""b:c""", Nested(1, 2), 3
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	call := `P "a,""b:c""", Nested(1, 2), 3`
	callStart := strings.Index(source, call)
	for _, test := range []struct {
		name   string
		offset int
		want   int
	}{
		{name: "nested second argument", offset: callStart + strings.Index(call, "Nested(1, 2)") + len("Nested(1, 2)"), want: 1},
		{name: "third argument", offset: callStart + len(call), want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			help := server.signatureHelp(uri, document.PositionAt(test.offset))
			if help == nil || help.ActiveParameter != test.want {
				t.Fatalf("signature help = %#v, want active parameter %d", help, test.want)
			}
		})
	}
}

func TestQualifiedNoParenSignatureHelpKeepsColonInsideString(t *testing.T) {
	const uri = "file:///tmp/qualified-no-paren-colon-string.asp"
	source := `<%
Class A
  ''' <summary>Qualified no-paren documentation.</summary>
  Public Sub M(ByVal first, ByVal second)
  End Sub
End Class
Dim x
Set x = New A
x.M "a:b", 2
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	call := `x.M "a:b", 2`
	callStart := strings.Index(source, call)
	for _, test := range []struct {
		name   string
		offset int
		active int
	}{
		{name: "inside colon string", offset: callStart + strings.Index(call, "a:b") + 2, active: 0},
		{name: "second argument", offset: callStart + len(call), active: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			help := server.signatureHelp(uri, document.PositionAt(test.offset))
			if help == nil || len(help.Signatures) != 1 || help.ActiveParameter != test.active {
				t.Fatalf("signature help = %#v, want active parameter %d", help, test.active)
			}
			if docs := signatureDocumentationText(help.Signatures[0].Documentation); !strings.Contains(docs, "Qualified no-paren documentation.") {
				t.Fatalf("signature docs = %q", docs)
			}
		})
	}
}

func TestLocalVariableHoverPrecedesBuiltin(t *testing.T) {
	const uri = "file:///tmp/local-variable-builtin-shadow-hover.asp"
	source := `<%
Sub T()
  Dim CStr
  CStr = "local"
  Response.Write CStr
End Sub
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	offset := strings.Index(source, "Response.Write CStr") + len("Response.Write ")
	hover, ok := server.hover(uri, document.PositionAt(offset)).(*lsp.Hover)
	if !ok || hover == nil {
		t.Fatalf("local builtin-shadow hover = %#v, want local variable", hover)
	}
	text := hoverMarkdownValueForTest(t, hover)
	if !strings.Contains(text, "(local) Dim CStr As ") || strings.Contains(text, "Function CStr") {
		t.Fatalf("local builtin-shadow hover = %q", text)
	}
}

func TestBuiltinNoParenMemberSignatureHelpKeepsColonInsideString(t *testing.T) {
	const uri = "file:///tmp/builtin-no-paren-colon-string.asp"
	source := `<%
Response.AddHeader "X:Trace", "value"
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	call := `Response.AddHeader "X:Trace", "value"`
	callStart := strings.Index(source, call)
	for _, test := range []struct {
		name   string
		offset int
		active int
	}{
		{name: "inside header name", offset: callStart + strings.Index(call, "X:Trace") + len("X:Trace"), active: 0},
		{name: "header value", offset: callStart + len(call), active: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			help := server.signatureHelp(uri, document.PositionAt(test.offset))
			if help == nil || len(help.Signatures) != 1 || help.ActiveParameter != test.active {
				t.Fatalf("signature help = %#v, want active parameter %d", help, test.active)
			}
			if !strings.Contains(help.Signatures[0].Label, "Response.AddHeader(name, value)") {
				t.Fatalf("signature label = %q", help.Signatures[0].Label)
			}
		})
	}
}

func TestQualifiedCustomMemberHoverPrecedesGlobalBuiltin(t *testing.T) {
	for _, test := range []struct {
		name       string
		uri        string
		languageID string
		prefix     string
		suffix     string
	}{
		{name: "classic asp", uri: "file:///tmp/qualified-custom-builtin-shadow.asp", languageID: "classic-asp", prefix: "<%\n", suffix: "%>"},
		{name: "standalone vbscript", uri: "file:///tmp/qualified-custom-builtin-shadow.vbs", languageID: "vbscript"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := test.prefix + `Class Widget
  ''' <summary>Custom CStr documentation.</summary>
  Public Function CStr(ByVal customArg)
  End Function
End Class
Dim item
Set item = New Widget
item.CStr("value")
` + test.suffix
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			document := core.NewTextDocument(test.uri, test.languageID, 1, source)
			server.mu.Lock()
			server.rememberOpenDocumentLocked(test.uri, document)
			server.mu.Unlock()
			offset := strings.Index(source, `item.CStr("value")`) + len("item.")
			hover, ok := server.hover(test.uri, document.PositionAt(offset)).(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("qualified custom hover = %#v", hover)
			}
			text := hoverMarkdownValueForTest(t, hover)
			if !strings.Contains(text, "CStr(ByVal customArg)") || !strings.Contains(text, "Custom CStr documentation.") || strings.Contains(text, "CStr(value) As String") {
				t.Fatalf("qualified custom hover = %q", text)
			}
		})
	}
}

func hoverMarkdownValueForTest(t *testing.T, hover *lsp.Hover) string {
	t.Helper()
	contents, ok := hover.Contents.(lsp.MarkupContent)
	if !ok {
		t.Fatalf("hover contents = %#v, want markup content", hover.Contents)
	}
	return contents.Value
}
