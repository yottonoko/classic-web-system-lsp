package lspserver

import (
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func BenchmarkLegacyUndefinedGlobalAtMemoryHit(b *testing.B) {
	server, uri, position := benchmarkLegacyUndefinedGlobalServer(b, 1_000, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		symbol, ok := server.WorkspaceLegacyUndefinedGlobalAt(context.Background(), uri, position)
		if !ok || symbol.Name != "Legacy999" {
			b.Fatalf("legacy symbol = %#v, %t", symbol, ok)
		}
	}
}

func BenchmarkLegacyUndefinedGlobalCompletionsMemoryHit(b *testing.B) {
	server, _, _ := benchmarkLegacyUndefinedGlobalServer(b, 1_000, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		items := server.legacyUndefinedGlobalCompletions(context.Background())
		if len(items) != 1_000 {
			b.Fatalf("completion items = %d", len(items))
		}
	}
}

func BenchmarkLegacyUndefinedGlobalOneDocumentDeltaTwoThousandFiles(b *testing.B) {
	documents := make(map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot, 2_000)
	for index := range 2_000 {
		documentID := workspaceDocumentID("/workspace/legacy-" + strconv.Itoa(index) + ".asp")
		name := "Legacy" + strconv.Itoa(index)
		documents[documentID] = legacyUndefinedGlobalDocumentSnapshot{
			DocumentID:        documentID,
			URI:               "file:///workspace/legacy-" + strconv.Itoa(index) + ".asp",
			SourceFingerprint: workspaceArtifactFingerprint("source-" + strconv.Itoa(index)),
			Facts: legacyUndefinedGlobalDocumentFacts{Occurrences: []legacyUndefinedGlobalOccurrence{{
				Name:  name,
				Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 4}, End: lsp.Position{Line: 1, Character: 4 + len(name)}},
			}}},
		}
	}
	index := buildLegacyUndefinedGlobalIndex(documents, nil)
	targetID := workspaceDocumentID("/workspace/legacy-1999.asp")
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; b.Loop(); iteration++ {
		changedDocuments := make(map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot, len(documents))
		for documentID, snapshot := range documents {
			changedDocuments[documentID] = snapshot
		}
		changed := changedDocuments[targetID]
		changed.SourceFingerprint = workspaceArtifactFingerprint("changed-" + strconv.Itoa(iteration))
		changed.Facts.Occurrences = append([]legacyUndefinedGlobalOccurrence(nil), changed.Facts.Occurrences...)
		changed.Facts.Occurrences[0].Name = "ChangedLegacy"
		changedDocuments[targetID] = changed
		catalog := updateLegacyUndefinedGlobalCatalog(context.Background(), index, changedDocuments, "settings", nil)
		if _, ok := catalog.incrementalIndex.symbolsByName["changedlegacy"]; !ok {
			b.Fatal("changed symbol was not indexed")
		}
	}
}

func benchmarkLegacyUndefinedGlobalServer(b *testing.B, symbolCount, locationsPerSymbol int) (*Server, string, lsp.Position) {
	b.Helper()
	server := New(nil, io.Discard, io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	catalog := LegacyUndefinedGlobalCatalog{
		SchemaVersion: legacyUndefinedGlobalCatalogSchemaVersion,
		Symbols:       make([]LegacyUndefinedGlobalSymbol, symbolCount),
	}
	for symbolIndex := range symbolCount {
		name := "Legacy" + strconv.Itoa(symbolIndex)
		locations := make([]lsp.Location, locationsPerSymbol)
		for locationIndex := range locationsPerSymbol {
			locations[locationIndex] = lsp.Location{
				URI: "file:///workspace/legacy-" + strconv.Itoa(symbolIndex) + ".asp",
				Range: lsp.Range{
					Start: lsp.Position{Line: locationIndex, Character: 4},
					End:   lsp.Position{Line: locationIndex, Character: 4 + len(name)},
				},
			}
		}
		catalog.Symbols[symbolIndex] = LegacyUndefinedGlobalSymbol{
			Name:      name,
			Kind:      LegacyUndefinedGlobalVariableOrConstant,
			OriginURI: locations[0].URI,
			Range:     locations[0].Range,
			Locations: locations,
		}
	}
	prepareLegacyUndefinedGlobalCatalog(&catalog)
	server.legacyUndefinedGlobalCatalog = &catalog
	server.legacyUndefinedGlobalCatalogGeneration = server.graphGeneration
	server.legacyUndefinedGlobalCatalogSettingsFingerprint = server.legacyUndefinedGlobalSettingsFingerprint(nil)
	target := catalog.Symbols[len(catalog.Symbols)-1]
	return server, target.Locations[len(target.Locations)-1].URI, target.Locations[len(target.Locations)-1].Range.Start
}
