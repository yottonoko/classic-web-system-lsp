package lspserver

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestDiagnosticPublishingDeduplicatesWarnings(t *testing.T) {
	var output bytes.Buffer
	server := New(strings.NewReader(""), &output, io.Discard)
	uri := "file:///tmp/diagnostic-dedupe.asp"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<% missingName %>")

	duplicate := lsp.Diagnostic{
		Range:    lsp.Range{Start: lsp.Position{Line: 0, Character: 3}, End: lsp.Position{Line: 0, Character: 14}},
		Severity: lsp.DiagnosticSeverityWarning,
		Code:     "first-code",
		Source:   "asp-lsp-vbscript",
		Message:  "'missingName' is not declared under Option Explicit.",
	}
	duplicateWithDifferentMetadata := duplicate
	duplicateWithDifferentMetadata.Code = "second-code"
	distinctRange := duplicate
	distinctRange.Range = lsp.Range{Start: lsp.Position{Line: 1, Character: 3}, End: lsp.Position{Line: 1, Character: 14}}

	written, err := server.writeDiagnosticNotificationForVersion(uri, 1, []lsp.Diagnostic{
		duplicate,
		duplicateWithDifferentMetadata,
		distinctRange,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("diagnostic notification was not written")
	}

	message, err := readMessage(bufio.NewReader(bytes.NewReader(output.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	var params struct {
		Diagnostics []lsp.Diagnostic `json:"diagnostics"`
	}
	if err := remarshal(message.Params, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Diagnostics) != 2 {
		t.Fatalf("published diagnostics = %#v, want one item per distinct range", params.Diagnostics)
	}
	if params.Diagnostics[0].Code != duplicate.Code {
		t.Fatalf("first duplicate metadata changed: %#v", params.Diagnostics[0])
	}
	if params.Diagnostics[1].Range != distinctRange.Range {
		t.Fatalf("distinct warning was removed: %#v", params.Diagnostics)
	}
}

func TestDiagnosticPublishingDoesNotHoldDocumentLockWhileWriting(t *testing.T) {
	writer := newBlockingStatusWriter()
	server := New(strings.NewReader(""), writer, io.Discard)
	uri := "file:///tmp/diagnostic-lock.asp"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<% missingName %>")

	publishDone := make(chan error, 1)
	go func() {
		_, err := server.writeDiagnosticNotificationForVersion(uri, 1, nil)
		publishDone <- err
	}()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("diagnostic writer did not start")
	}

	mutationDone := make(chan struct{})
	go func() {
		server.mu.Lock()
		server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 2, "<% freshName %>")
		server.mu.Unlock()
		close(mutationDone)
	}()
	select {
	case <-mutationDone:
	case <-time.After(time.Second):
		t.Fatal("document mutation remained blocked while diagnostic bytes were being written")
	}

	close(writer.release)
	select {
	case err := <-publishDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("diagnostic publication did not complete")
	}
	select {
	case <-mutationDone:
	case <-time.After(time.Second):
		t.Fatal("document mutation remained blocked after diagnostic publication")
	}
}
