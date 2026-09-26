package lspserver

import (
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/excel"
)

func TestAnalysisExcelGraphProgressReporterPreservesDetailedPhase(t *testing.T) {
	var events []excel.AnalysisProgressEvent
	report := analysisExcelGraphProgressReporter(func(event excel.AnalysisProgressEvent) {
		events = append(events, event)
	})

	report("graph.document.linkReferences", "default.asp", 3, 7)

	if len(events) != 1 {
		t.Fatalf("progress events = %d, want 1", len(events))
	}
	event := events[0]
	if event.Label != "excel.graph.document.linkReferences" || event.Detail != "default.asp" || event.Current != 3 || event.Total != 7 {
		t.Fatalf("graph progress event = %#v", event)
	}
	if len(event.ActiveItems) != 1 || event.ActiveItems[0] != "default.asp" {
		t.Fatalf("active items = %#v, want current document", event.ActiveItems)
	}
}
