package lspserver

import "testing"

func TestClassifyRequestWorkKeepsEditorFeaturesSeparateFromBulkCommands(t *testing.T) {
	for _, method := range []string{
		"aspLsp/textDocument/lineCommentEdits",
		"textDocument/completion",
		"textDocument/hover",
		"textDocument/definition",
		"textDocument/signatureHelp",
		"textDocument/rename",
		"textDocument/formatting",
		"textDocument/documentSymbol",
		"textDocument/foldingRange",
		"textDocument/documentLink",
		"textDocument/prepareCallHierarchy",
		"callHierarchy/incomingCalls",
		"textDocument/prepareTypeHierarchy",
		"typeHierarchy/subtypes",
	} {
		if class := classifyRequestWork(method, nil); class != requestWorkInteractive {
			t.Fatalf("%s class = %d, want interactive", method, class)
		}
	}

	for _, command := range []string{
		"aspLsp.server.buildGraph",
		"aspLsp.server.buildFlowchart",
		"aspLsp.server.buildNavigationGraph",
		"aspLsp.server.exportAnalysisExcel",
		"aspLsp.server.reindexWorkspace",
		"aspLsp.server.clearProcessCache",
		"aspLsp.server.clearDiskCache",
		"aspLsp.server.clearCache",
	} {
		params := mustRaw(map[string]any{"command": command})
		if class := classifyRequestWork("workspace/executeCommand", params); class != requestWorkBackground {
			t.Fatalf("%s class = %d, want background", command, class)
		}
	}

	if class := classifyRequestWork("workspace/willRenameFiles", nil); class != requestWorkBackground {
		t.Fatalf("workspace/willRenameFiles class = %d, want background", class)
	}
}
