package lspserver

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

type recordedGraphProgress struct {
	label   string
	detail  string
	current int
	total   int
}

func TestDocumentSetGraphProgressIsDetailedAndMonotonicPerPhase(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	documents := []*core.ParsedDocument{
		server.parseText("file:///workspace/first.asp", `<% Dim FirstValue : FirstValue = 1 %>`, "VBScript"),
		server.parseText("file:///workspace/second.inc", `<% Dim SecondValue : SecondValue = FirstValue %>`, "VBScript"),
	}
	var mutex sync.Mutex
	events := []recordedGraphProgress{}
	report := func(label, detail string, current, total int) {
		mutex.Lock()
		events = append(events, recordedGraphProgress{label: label, detail: detail, current: current, total: total})
		mutex.Unlock()
	}

	server.buildDocumentSetGraphWithProgress(context.Background(), "workspace", "file:///workspace", documents, true, false, report)

	mutex.Lock()
	recorded := append([]recordedGraphProgress(nil), events...)
	mutex.Unlock()
	if len(recorded) == 0 {
		t.Fatal("graph build did not report progress")
	}
	lastByLabel := map[string]int{}
	completedByLabel := map[string]bool{}
	detailed := false
	for _, event := range recorded {
		if !strings.HasPrefix(event.label, "graph.workspace.") {
			t.Fatalf("progress label %q does not preserve graph scope", event.label)
		}
		if event.current < lastByLabel[event.label] {
			t.Fatalf("progress for %s regressed from %d to %d", event.label, lastByLabel[event.label], event.current)
		}
		if event.total > 0 && event.current > event.total {
			t.Fatalf("progress for %s exceeded total: %d/%d", event.label, event.current, event.total)
		}
		lastByLabel[event.label] = event.current
		if event.total > 0 && event.current == event.total {
			completedByLabel[event.label] = true
		}
		if event.detail == "first.asp" || event.detail == "second.inc" {
			detailed = true
		}
	}
	if !detailed {
		t.Fatalf("graph progress did not identify active files: %#v", recorded)
	}
	for _, label := range []string{
		"graph.workspace.indexDeclarations",
		"graph.workspace.addFiles",
		"graph.workspace.addDeclarations",
		"graph.workspace.resolveIncludes",
		"graph.workspace.indexReferences",
		"graph.workspace.linkUnresolved",
	} {
		if !completedByLabel[label] {
			t.Errorf("progress phase %s did not reach its real total", label)
		}
	}
}

func TestNavigationAndFlowchartCollectionProgressIdentifiesFiles(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	documents := []*core.ParsedDocument{
		server.parseText("file:///workspace/default.asp", `<% Sub Main() : End Sub %>`, "VBScript"),
		server.parseText("file:///workspace/common.inc", `<% Sub Shared() : End Sub %>`, "VBScript"),
	}

	navigationEvents := []recordedGraphProgress{}
	navigationIncludeOwnersWithProgress(context.Background(), server, documents, func(label, detail string, current, total int) {
		navigationEvents = append(navigationEvents, recordedGraphProgress{label: label, detail: detail, current: current, total: total})
	})
	if len(navigationEvents) != len(documents) || navigationEvents[len(navigationEvents)-1].current != len(documents) {
		t.Fatalf("navigation progress = %#v, want one completed update per document", navigationEvents)
	}
	if navigationEvents[0].detail != documents[0].URI || navigationEvents[1].detail != documents[1].URI {
		t.Fatalf("navigation progress omitted active URIs: %#v", navigationEvents)
	}

	flowchartEvents := []recordedGraphProgress{}
	server.flowchartSymbolTargetsWithProgress(context.Background(), documents[0], func(uri string, current, total int) {
		flowchartEvents = append(flowchartEvents, recordedGraphProgress{detail: uri, current: current, total: total})
	})
	if len(flowchartEvents) == 0 {
		t.Fatal("flowchart target collection did not report progress")
	}
	last := flowchartEvents[len(flowchartEvents)-1]
	if last.current != last.total || last.detail == "" {
		t.Fatalf("flowchart progress did not complete with an active file: %#v", flowchartEvents)
	}
}
