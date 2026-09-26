package lspserver

import (
	"path/filepath"
	"testing"
)

func TestFlowchartCSTCFGPreservesNestedBranchTopologyAndRanges(t *testing.T) {
	source := `<%
Sub Main()
  If ready Then
    If nested Then
      Call A()
    ElseIf retry Then
      Call B()
    Else
      Call C()
    End If
  Else
    Call D()
  End If
  Select Case mode
    Case 1
      Call One()
    Case 2
      Call Two()
    Case Else
      Call Other()
  End Select
End Sub
%>`
	flowchart := auditBuildFlowchart(t, source)

	for _, want := range []struct {
		label                string
		startLine, startChar int
		endLine, endChar     int
	}{
		{"If ready Then", 2, 2, 12, len("  End If")},
		{"If nested Then", 3, 4, 9, len("    End If")},
		{"If retry Then", 5, 4, 5, len("    ElseIf retry Then")},
		{"mode", 13, 2, 20, len("  End Select")},
		{"When 1", 14, 4, 14, len("    Case 1")},
		{"When 2", 16, 4, 16, len("    Case 2")},
		{"Else", 18, 4, 18, len("    Case Else")},
	} {
		node := auditFlowchartNodeByLabel(flowchart, want.label)
		if want.label == "Else" {
			node = auditFlowchartNodeByKindAndLabel(flowchart, "case", want.label)
		}
		if node == nil || !sameRange(node["range"], want.startLine, want.startChar, want.endLine, want.endChar) {
			t.Errorf("node %q range mismatch: %s", want.label, mustJSONText(t, node))
		}
	}

	for _, edge := range []struct{ source, target, label string }{
		{"If ready Then", "If nested Then", "Yes"},
		{"If nested Then", "Call A()", "Yes"},
		{"If nested Then", "If retry Then", "No"},
		{"If retry Then", "Call B()", "Yes"},
		{"mode", "When 1", "When 1"},
		{"mode", "When 2", "When 2"},
		{"mode", "Else", "Else"},
	} {
		if !flowchartHasEdgeBetweenLabels(flowchart, edge.source, edge.target, edge.label) {
			t.Fatalf("missing CFG edge %q -%q-> %q: %s", edge.source, edge.label, edge.target, mustJSONText(t, flowchart))
		}
	}
}

func TestFlowchartCSTCFGBuildsLoopConditionsAndExitTargetsWithExactRanges(t *testing.T) {
	source := `<%
Sub Main()
  For index = 1 To 3
    Exit For
  Next
  For Each item In items
    Call Visit(item)
  Next
  Do While active
    Exit Do
  Loop
  Do
    Call Poll()
  Loop Until finished
  While waiting
    Call WaitOnce()
  Wend
End Sub
%>`
	flowchart := auditBuildFlowchart(t, source)

	for _, want := range []struct {
		label                string
		startLine, startChar int
		endLine, endChar     int
	}{
		{"For index = 1 To 3", 2, 2, 4, len("  Next")},
		{"For Each item In items", 5, 2, 7, len("  Next")},
		{"Do While active", 8, 2, 10, len("  Loop")},
		{"Do", 11, 2, 13, len("  Loop Until finished")},
		{"Loop Until finished", 13, 2, 13, len("  Loop Until finished")},
		{"While waiting", 14, 2, 16, len("  Wend")},
		{"Exit For", 3, 4, 3, len("    Exit For")},
		{"Exit Do", 9, 4, 9, len("    Exit Do")},
	} {
		node := auditFlowchartNodeByLabel(flowchart, want.label)
		if node == nil || !sameRange(node["range"], want.startLine, want.startChar, want.endLine, want.endChar) {
			t.Errorf("node %q range mismatch: %s", want.label, mustJSONText(t, node))
		}
	}

	for _, edge := range []struct{ source, target, label string }{
		{"Exit For", "After For", "Exit"},
		{"Exit Do", "After Do", "Exit"},
		{"Loop Until finished", "Do", "No"},
		{"Loop Until finished", "After Do", "Yes"},
		{"While waiting", "Call WaitOnce()", "Yes"},
		{"Call WaitOnce()", "While waiting", "Repeat"},
	} {
		if !flowchartHasEdgeBetweenLabels(flowchart, edge.source, edge.target, edge.label) {
			t.Fatalf("missing CFG edge %q -%q-> %q: %s", edge.source, edge.label, edge.target, mustJSONText(t, flowchart))
		}
	}
}

func TestFlowchartCSTCFGPreservesStatementBoundariesContinuationsAndASPIslands(t *testing.T) {
	source := `<%
Sub Main()
  stamp = #12:30:00# : first = 1 : second = "a:b"
  total = first + _
    second + _
    third
  If enabled Then
%>
<div>HTML between VBScript islands</div>
<%
    Call Render(total)
  End If
End Sub
%>`
	flowchart := auditBuildFlowchart(t, source)

	for _, want := range []struct {
		label                string
		startLine, startChar int
		endLine, endChar     int
	}{
		{"stamp = #12:30:00#", 2, 2, 2, len("  stamp = #12:30:00#")},
		{"first = 1", 2, len("  stamp = #12:30:00# : "), 2, len("  stamp = #12:30:00# : first = 1")},
		{"second = \"a:b\"", 2, len("  stamp = #12:30:00# : first = 1 : "), 2, len("  stamp = #12:30:00# : first = 1 : second = \"a:b\"")},
		{"total = first + second + third", 3, 2, 5, len("    third")},
		{"If enabled Then", 6, 2, 11, len("  End If")},
		{"Call Render(total)", 10, 4, 10, len("    Call Render(total)")},
	} {
		node := auditFlowchartNodeByLabel(flowchart, want.label)
		if node == nil || !sameRange(node["range"], want.startLine, want.startChar, want.endLine, want.endChar) {
			t.Errorf("node %q range mismatch: %s", want.label, mustJSONText(t, node))
		}
	}
	if !flowchartHasEdgeBetweenLabels(flowchart, "If enabled Then", "Render HTML", "Yes") ||
		!flowchartHasEdgeBetweenLabels(flowchart, "Render HTML", "Call Render(total)", "") {
		t.Fatalf("ASP island output was not preserved inside the If body: %s", mustJSONText(t, flowchart))
	}
}

func TestFlowchartCSTCFGSeparatesProcedureKindsAndInlineIfBranches(t *testing.T) {
	source := `<%
topValue = 1
Function Calculate(value)
  If value > 0 Then Calculate = value Else Calculate = 0
  Exit Function
End Function
Sub Run()
  Call Calculate(1)
End Sub
Class Widget
  Property Get Name()
    Name = "widget"
  End Property
End Class
%>`
	flowchart := auditBuildFlowchart(t, source)

	for _, want := range []struct {
		label                string
		startLine, startChar int
		endLine, endChar     int
	}{
		{"Top level", 1, 0, 1, len("topValue = 1")},
		{"Function Calculate", 2, 0, 5, len("End Function")},
		{"Sub Run", 6, 0, 8, len("End Sub")},
		{"Property Name", 10, 2, 12, len("  End Property")},
	} {
		section := auditFlowchartSectionByLabel(flowchart, want.label)
		if section == nil || !sameRange(section["range"], want.startLine, want.startChar, want.endLine, want.endChar) {
			t.Errorf("section %q range mismatch: %s", want.label, mustJSONText(t, section))
		}
	}

	inlineIf := auditFlowchartNodeByLabel(flowchart, "If value > 0 Then")
	if inlineIf == nil || !sameRange(inlineIf["range"], 3, 2, 3, len("  If value > 0 Then Calculate = value Else Calculate = 0")) {
		t.Fatalf("inline If range mismatch: %s", mustJSONText(t, inlineIf))
	}
	for _, edge := range []struct{ source, target, label string }{
		{"If value > 0 Then", "Calculate = value", "Yes"},
		{"If value > 0 Then", "Else", "No"},
		{"Exit Function", "End", "Exit"},
	} {
		if !flowchartHasEdgeBetweenLabels(flowchart, edge.source, edge.target, edge.label) {
			t.Fatalf("missing CFG edge %q -%q-> %q: %s", edge.source, edge.label, edge.target, mustJSONText(t, flowchart))
		}
	}
}

func auditBuildFlowchart(t *testing.T, source string) map[string]any {
	t.Helper()
	client := startStdioTestClient(t)
	t.Cleanup(client.close)
	root := t.TempDir()
	page := filepath.Join(root, "audit.asp")
	uri := pathToFileURI(page)
	client.request("initialize", map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})
	notifyOpenClassicASPDocument(t, client, uri, source)
	return buildFlowchart(t, client, map[string]any{"uri": uri, "labelMode": "raw"})
}

func auditFlowchartNodeByLabel(payload map[string]any, label string) map[string]any {
	for _, value := range payload["nodes"].([]any) {
		node := value.(map[string]any)
		if node["label"] == label {
			return node
		}
	}
	return nil
}

func auditFlowchartNodeByKindAndLabel(payload map[string]any, kind, label string) map[string]any {
	for _, value := range payload["nodes"].([]any) {
		node := value.(map[string]any)
		if node["kind"] == kind && node["label"] == label {
			return node
		}
	}
	return nil
}

func auditFlowchartSectionByLabel(payload map[string]any, label string) map[string]any {
	for _, value := range payload["sections"].([]any) {
		section := value.(map[string]any)
		if section["label"] == label {
			return section
		}
	}
	return nil
}
