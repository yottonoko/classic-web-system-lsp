package lspserver

import (
	"context"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestGraphReferenceLinksDocumentTraversalMatchesLegacyDeclarationTraversal(t *testing.T) {
	server := New(nil, io.Discard, nil)
	documents := []*core.ParsedDocument{
		server.parseText("file:///workspace/default.asp", `<%
Dim total
total = 1
Sub Render(value)
  total = total + value
End Sub
Call Render(total)
implicitValue = total
Response.Write implicitValue
%>`, "VBScript"),
		server.parseText("file:///workspace/child.inc", `<%
implicitValue = implicitValue + 1
%>`, "VBScript"),
		server.parseText("file:///workspace/last.inc", `<%
Response.Write implicitValue
%>`, "VBScript"),
	}

	legacy := graph.Payload{Stats: map[string]int{}}
	addLegacyGraphReferenceLinksForTest(server, &legacy, documents)
	current := graph.Payload{Stats: map[string]int{}}
	server.addGraphReferenceLinksWithProgress(context.Background(), &current, documents, nil, "graph.document")

	if !reflect.DeepEqual(current.Links, legacy.Links) {
		t.Fatalf("document-centric links differ from legacy declaration traversal:\ncurrent=%#v\nlegacy=%#v", current.Links, legacy.Links)
	}
	if !reflect.DeepEqual(current.Stats, legacy.Stats) {
		t.Fatalf("document-centric stats = %#v, legacy = %#v", current.Stats, legacy.Stats)
	}
}

func TestGraphReferenceLinksVisitEachRelevantPostingOnce(t *testing.T) {
	server := New(nil, io.Discard, nil)
	var source strings.Builder
	source.WriteString("<%\n")
	const names = 200
	for index := range names {
		name := "implicitValue" + strconv.Itoa(index)
		source.WriteString(name + " = " + strconv.Itoa(index) + "\n")
		source.WriteString("Response.Write " + name + "\n")
	}
	source.WriteString("%>")
	document := server.parseText("file:///workspace/default.asp", source.String(), "VBScript")
	documents := []*core.ParsedDocument{document}

	stats := graphReferenceTraversalStats{}
	payload := graph.Payload{Stats: map[string]int{}}
	server.addGraphReferenceLinksWithProgressAndStats(context.Background(), &payload, documents, nil, "graph.document", &stats)

	shard := vbscript.BuildReferenceShard(document)
	wantPostings := 0
	for _, declaration := range graphVBDeclarations(document) {
		wantPostings += len(shard.PostingsFor(declaration.Name))
	}
	if stats.Documents != 1 || stats.Names != names {
		t.Fatalf("traversal stats = %#v, want one document and %d names", stats, names)
	}
	if stats.Postings != wantPostings {
		t.Fatalf("examined postings = %d, want %d", stats.Postings, wantPostings)
	}
	if stats.TargetResolutions != wantPostings {
		t.Fatalf("target resolutions = %d, want %d", stats.TargetResolutions, wantPostings)
	}
}

func BenchmarkGraphReferenceLinksDocumentCentric(b *testing.B) {
	for _, documentCount := range []int{2_000, 10_000} {
		b.Run(strconv.Itoa(documentCount)+"-documents", func(b *testing.B) {
			server := New(nil, io.Discard, nil)
			documents := make([]*core.ParsedDocument, documentCount)
			for index := range documents {
				name := "sharedGraphValue" + strconv.Itoa(index)
				documents[index] = server.parseText(
					"file:///workspace/graph-"+strconv.Itoa(index)+".asp",
					"<% "+name+" = "+name+" + "+strconv.Itoa(index)+" %>",
					"VBScript",
				)
				_ = vbscript.BuildReferenceShard(documents[index])
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				payload := graph.Payload{Stats: map[string]int{}}
				server.addGraphReferenceLinksWithProgress(context.Background(), &payload, documents, nil, "graph.workspace")
				lspPerformanceBenchmarkSink = payload
			}
		})
	}
}

func addLegacyGraphReferenceLinksForTest(server *Server, payload *graph.Payload, documents []*core.ParsedDocument) {
	documentByURI := make(map[string]*core.ParsedDocument, len(documents))
	for _, document := range documents {
		if document != nil {
			documentByURI[document.URI] = document
		}
	}
	canonicalImplicitIDs := server.graphCanonicalImplicitDeclarationIDs(documents, documentByURI)
	type target struct {
		declaration vbUsageDeclaration
		id          string
		documents   []*core.ParsedDocument
	}
	targets := make([]target, 0)
	referenceDocumentsByURI := map[string][]*core.ParsedDocument{}
	for _, document := range documents {
		if document == nil {
			continue
		}
		for _, declaration := range graphVBDeclarations(document) {
			id := graphDeclarationNodeID(document.URI, declaration.Name, declaration.Range)
			if declaration.Implicit {
				canonicalID := canonicalImplicitIDs[strings.ToLower(declaration.Name)]
				if canonicalID != "" && canonicalID != id {
					continue
				}
				targets = append(targets, target{declaration: declaration, id: id, documents: documents})
				continue
			}
			referenceDocuments, ok := referenceDocumentsByURI[document.URI]
			if !ok {
				referenceDocuments = server.workspaceReferenceDocumentsContext(context.Background(), document)
				referenceDocumentsByURI[document.URI] = referenceDocuments
			}
			targets = append(targets, target{declaration: declaration, id: id, documents: referenceDocuments})
		}
	}
	links := map[string]*graphReferenceLinkCount{}
	for _, target := range targets {
		for _, document := range target.documents {
			if document == nil {
				continue
			}
			if _, inGraph := documentByURI[document.URI]; !inGraph {
				continue
			}
			declarations := vbReferenceRangeIndex(vbReferenceDeclarationFacts(document, false))[strings.ToLower(target.declaration.Name)]
			for _, posting := range vbscript.BuildReferenceShard(document).PostingsFor(target.declaration.Name) {
				if _, declaration := declarations[posting.Range]; declaration {
					continue
				}
				kind := vbGraphReferenceLinkKind(document, posting.Range, target.declaration.Kind)
				role := vbGraphReferenceRole(kind)
				sourceID := graphReferenceSourceID(document, posting.Range)
				if sourceID == target.id && kind == "assignments" {
					continue
				}
				key := sourceID + "\x00" + target.id + "\x00" + kind + "\x00" + role
				link := links[key]
				if link == nil {
					link = &graphReferenceLinkCount{source: sourceID, target: target.id, kind: kind, role: role}
					links[key] = link
				}
				link.count++
				link.ranges = append(link.ranges, lsp.Location{URI: document.URI, Range: posting.Range})
			}
		}
	}
	keys := make([]string, 0, len(links))
	for key := range links {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		link := links[key]
		payload.AddEdge(graph.Edge{
			ID:     "reference:" + strconv.Itoa(len(payload.Links)),
			Source: link.source,
			Target: link.target,
			Kind:   link.kind,
			Label:  link.role,
			Role:   link.role,
			Count:  link.count,
			Ranges: link.ranges,
		})
		payload.Stats[link.kind] += link.count
	}
}
