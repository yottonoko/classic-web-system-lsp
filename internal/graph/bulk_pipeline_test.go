package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestRunSpilledGraphIndexPipelineCanonicalizesImplicitGlobalsFromSpill(t *testing.T) {
	directory := t.TempDir()
	common := bulkGraphDocument("/site/common.inc", "<% Dim value %>")
	owner := bulkGraphDocument("/site/default.asp", `<!--#include file="common.inc"-->
<% value = 1 %>`)
	commonIndex := bulkGraphIndex(common, bulkGraphIndexOptions{
		Declarations: []BulkVBDeclaration{
			bulkDeclaration("common:value", "value", BulkVBDeclaration{
				Range:        bulkRange(0, 7, 12),
				BindingScope: "global",
			}),
		},
	})
	ownerIndex := bulkGraphIndex(owner, bulkGraphIndexOptions{
		IncludeRefs: []BulkIncludeRef{bulkIncludeRef("common.inc", bulkRange(0, 0, 32))},
		Declarations: []BulkVBDeclaration{
			bulkDeclaration("owner:value", "value", BulkVBDeclaration{
				Range:                   bulkRange(1, 3, 8),
				BindingScope:            "global",
				Implicit:                true,
				ImplicitGlobal:          true,
				ImplicitGlobalCandidate: true,
			}),
		},
		References: []BulkVBReference{{
			Name:           "value",
			NormalizedName: "value",
			Range:          bulkRange(1, 3, 8),
			Role:           "write",
			ResolvedID:     "owner:value",
		}},
	})
	indexes := map[string]BulkGraphFileIndex{
		owner.URI:  ownerIndex,
		common.URI: commonIndex,
	}
	progressEvents := []string{}
	progress := []BulkProgressEvent{}

	pipeline, err := RunSpilledGraphIndexPipeline(BulkPipelineOptions{
		Sources: []BulkSource{
			{
				URI:        owner.URI,
				FileName:   owner.FileName,
				TextLength: len(owner.Text),
				Load:       func() (BulkDocument, error) { return owner, nil },
			},
			{
				URI:        common.URI,
				FileName:   common.FileName,
				TextLength: len(common.Text),
				Load:       func() (BulkDocument, error) { return common, nil },
			},
		},
		SpillDirectory: directory,
		IndexDocument: func(document BulkDocument) (BulkIndexedDocument, error) {
			return BulkIndexedDocument{Document: document, GraphIndex: indexes[document.URI]}, nil
		},
		GraphFileKey:      func(fileName string) string { return strings.ToLower(filepath.Clean(fileName)) },
		NormalizeFileName: filepath.Clean,
		ResolveIncludePath: func(ownerURI string, includePath string) (string, error) {
			return filepath.Join(filepath.Dir(fileNameFromBulkURI(ownerURI)), includePath), nil
		},
		Fingerprint: bulkGraphFileIndexFingerprint,
		OnProgress: func(event BulkProgressEvent) {
			progress = append(progress, event)
			fileName := "(all)"
			if event.Source != nil {
				fileName = event.Source.FileName
			}
			progressEvents = append(progressEvents, event.Stage+":"+event.Phase+":"+fileName)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Dispose()

	firstScan, err := pipeline.ScanCanonicalized()
	if err != nil {
		t.Fatal(err)
	}
	secondScan, err := pipeline.ScanCanonicalized()
	if err != nil {
		t.Fatal(err)
	}

	if got := firstScan[0].GraphIndex.VBSymbolIndex.Declarations; len(got) != 0 {
		t.Fatalf("owner declarations after canonicalization = %#v", got)
	}
	if got := firstScan[0].GraphIndex.VBSymbolIndex.References[0].ResolvedID; got != "common:value" {
		t.Fatalf("owner reference resolvedId = %q", got)
	}
	if got := declarationIDs(firstScan[1].GraphIndex.VBSymbolIndex.Declarations); !reflect.DeepEqual(got, []string{"common:value"}) {
		t.Fatalf("common declarations = %#v", got)
	}
	if got := scanURIs(secondScan); !reflect.DeepEqual(got, scanURIs(firstScan)) {
		t.Fatalf("second scan URIs = %#v, want %#v", got, scanURIs(firstScan))
	}
	for _, expected := range []string{
		"load:start:" + owner.FileName,
		"load:done:" + owner.FileName,
		"index:start:" + owner.FileName,
		"index:done:" + owner.FileName,
		"spill:start:" + owner.FileName,
		"spill:done:" + owner.FileName,
		"canonicalize:start:(all)",
		"canonicalize:progress:" + owner.FileName,
		"canonicalize:progress:" + common.FileName,
		"canonicalize:done:(all)",
	} {
		if !bulkContainsString(progressEvents, expected) {
			t.Fatalf("progress missing %q in %#v", expected, progressEvents)
		}
	}
	assertBulkProgressStage(t, progress, "load", []int{0, 1, 1, 2}, 2)
	assertBulkProgressStage(t, progress, "index", []int{0, 1, 1, 2}, 2)
	assertBulkProgressStage(t, progress, "spill", []int{0, 1, 1, 2}, 2)
	// Each scan reads two file records and then performs one global canonicalization pass.
	assertBulkProgressStage(t, progress, "canonicalize", []int{0, 1, 2, 3, 0, 1, 2, 3}, 3)
	for _, event := range progress {
		if event.Source != nil && event.Detail == "" {
			t.Fatalf("source progress missing detail: %#v", event)
		}
	}
	if err := pipeline.Dispose(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("spill directory should be removed, stat err=%v", err)
	}
}

func assertBulkProgressStage(t *testing.T, events []BulkProgressEvent, stage string, wantCurrent []int, wantTotal int) {
	t.Helper()
	got := make([]int, 0, len(wantCurrent))
	for _, event := range events {
		if event.Stage != stage {
			continue
		}
		if event.Total != wantTotal {
			t.Fatalf("%s progress total = %d, want %d: %#v", stage, event.Total, wantTotal, event)
		}
		got = append(got, event.Current)
	}
	if !reflect.DeepEqual(got, wantCurrent) {
		t.Fatalf("%s progress current = %#v, want %#v", stage, got, wantCurrent)
	}
}

type bulkGraphIndexOptions struct {
	Declarations []BulkVBDeclaration
	References   []BulkVBReference
	IncludeRefs  []BulkIncludeRef
}

func bulkGraphDocument(fileName string, text string) BulkDocument {
	return BulkDocument{
		URI:      "file://" + fileName,
		FileName: fileName,
		Text:     text,
		Source:   BulkSourceMetadata{FileName: fileName, MtimeMS: 1, Size: int64(len(text))},
	}
}

func bulkGraphIndex(document BulkDocument, options bulkGraphIndexOptions) BulkGraphFileIndex {
	index := BulkVBSymbolIndex{
		URI:          document.URI,
		Declarations: options.Declarations,
		References:   options.References,
		IncludeRefs:  options.IncludeRefs,
	}
	return BulkGraphFileIndex{
		Key:           document.FileName,
		URI:           document.URI,
		FileName:      document.FileName,
		Source:        document.Source,
		IncludeRefs:   options.IncludeRefs,
		VBSymbolIndex: index,
		Fingerprint:   bulkGraphFileIndexFingerprint(index),
	}
}

func bulkDeclaration(id string, name string, options BulkVBDeclaration) BulkVBDeclaration {
	declarationRange := options.Range
	if declarationRange == (lsp.Range{}) {
		declarationRange = bulkRange(0, 0, len(name))
	}
	options.ID = id
	options.Name = name
	options.NormalizedName = strings.ToLower(name)
	options.Kind = "variable"
	options.Range = declarationRange
	if options.NameRange == (lsp.Range{}) {
		options.NameRange = declarationRange
	}
	return options
}

func bulkIncludeRef(path string, includeRange lsp.Range) BulkIncludeRef {
	return BulkIncludeRef{Path: path, Mode: "file", Range: includeRange}
}

func bulkRange(line int, start int, end int) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: line, Character: start},
		End:   lsp.Position{Line: line, Character: end},
	}
}

func bulkGraphFileIndexFingerprint(index BulkVBSymbolIndex) string {
	payload := struct {
		Declarations []string `json:"declarations"`
		References   []string `json:"references"`
	}{
		Declarations: declarationIDs(index.Declarations),
	}
	for _, reference := range index.References {
		payload.References = append(payload.References, reference.ResolvedID)
	}
	body, _ := json.Marshal(payload)
	return string(body)
}

func fileNameFromBulkURI(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}

func declarationIDs(declarations []BulkVBDeclaration) []string {
	ids := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		ids = append(ids, declaration.ID)
	}
	return ids
}

func scanURIs(documents []BulkIndexedDocument) []string {
	uris := make([]string, 0, len(documents))
	for _, document := range documents {
		uris = append(uris, document.Document.URI)
	}
	return uris
}

func bulkContainsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
