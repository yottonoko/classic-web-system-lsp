package lspserver

import (
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestVBScriptCallHierarchyUsesReferenceShardCallGroups(t *testing.T) {
	parsed := core.ParseDocument("file:///calls.asp", `<%
Sub Save()
End Sub

Sub Build()
  Save()
  Save()
End Sub

Sub Other()
  Build()
End Sub
%>`, core.Settings{DefaultLanguage: "VBScript"})
	signatures := vbscript.BuildSignatures(parsed)
	save := callHierarchyItem(parsed.URI, signatures["save"])
	build := callHierarchyItem(parsed.URI, signatures["build"])

	incoming := vbscriptIncomingCallsFromShard(parsed, save)
	if len(incoming) != 2 {
		t.Fatalf("incoming calls = %#v, want two Build calls", incoming)
	}
	for _, call := range incoming {
		if call.From.Name != "Build" || len(call.FromRanges) != 1 {
			t.Fatalf("incoming call = %#v, want Build with one range", call)
		}
	}

	outgoing := vbscriptOutgoingCallsFromShard(parsed, build)
	if len(outgoing) != 2 {
		t.Fatalf("outgoing calls = %#v, want two Save calls", outgoing)
	}
	for _, call := range outgoing {
		if call.To.Name != "Save" || len(call.FromRanges) != 1 {
			t.Fatalf("outgoing call = %#v, want Save with one range", call)
		}
	}

	self := lsp.CallHierarchyItem{Name: "Save", URI: parsed.URI}
	if calls := vbscriptIncomingCallsFromShard(parsed, self); len(calls) != 2 {
		t.Fatalf("incoming self-filter result = %#v, want external Build calls", calls)
	}
}
