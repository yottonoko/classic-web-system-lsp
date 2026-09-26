package lspserver

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptLeadingDotWithCompletionUsesBuiltinReceiver(t *testing.T) {
	const uri = "file:///site/with-response.asp"
	source := `<%
With Response
  .Wr
End With
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	offset := strings.Index(source, ".Wr") + len(".Wr")
	items := server.completion(context.Background(), uri, document.PositionAt(offset), nil).Items
	if !completionItemsContainLabel(items, "Write") {
		t.Fatalf("leading-dot With completions missing Response.Write: %#v", items)
	}
}

func TestVBScriptPropertyAccessorsUseMemberSignatureHelp(t *testing.T) {
	const uri = "file:///site/property-signatures.asp"
	source := `<%
Class Customer
  Public Property Get Indexed(ByVal index)
    Indexed = index
  End Property
  Public Property Let Assigned(ByVal index, ByVal value)
  End Property
  Public Property Set AssignedObject(ByVal index, ByRef value)
  End Property
End Class
Dim customer
Set customer = New Customer
customer.Indexed("id")
customer.Assigned("id", value)
customer.AssignedObject("id", value)
%>`
	server, document := testServerWithVBScriptDocument(uri, source)
	for _, testCase := range []struct {
		call   string
		label  string
		params int
	}{
		{call: `customer.Indexed("id")`, label: "Property Get Indexed", params: 1},
		{call: `customer.Assigned("id", value)`, label: "Property Let Assigned", params: 2},
		{call: `customer.AssignedObject("id", value)`, label: "Property Set AssignedObject", params: 2},
	} {
		offset := strings.Index(source, testCase.call) + strings.Index(testCase.call, "(") + 1
		help := server.signatureHelp(uri, document.PositionAt(offset))
		if help == nil || len(help.Signatures) != 1 {
			t.Fatalf("%s signature help = %#v", testCase.label, help)
		}
		if !strings.Contains(help.Signatures[0].Label, testCase.label) {
			t.Fatalf("%s signature label = %q", testCase.label, help.Signatures[0].Label)
		}
		if len(help.Signatures[0].Parameters) != testCase.params {
			t.Fatalf("%s parameters = %#v", testCase.label, help.Signatures[0].Parameters)
		}
	}
}

func testServerWithVBScriptDocument(uri, source string) (*Server, *core.TextDocument) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, document)
	server.mu.Unlock()
	return server, document
}

func completionItemsContainLabel(items []lsp.CompletionItem, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}
