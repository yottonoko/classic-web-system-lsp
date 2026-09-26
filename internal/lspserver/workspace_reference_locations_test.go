package lspserver

import (
	"context"
	"io"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestMaterializeWorkspaceReferenceSegmentsEmitsCandidateOrder(t *testing.T) {
	server := New(nil, io.Discard, nil)
	server.analysisWorkers.setWorkers(4)
	var emitted []lsp.Location
	ctx := context.WithValue(context.Background(), workspaceReferenceLocationSinkContextKey{}, workspaceReferenceLocationSink(func(locations []lsp.Location) {
		emitted = append(emitted, locations...)
	}))
	uris := []string{"file:///z.asp", "file:///a.asp", "file:///m.asp"}
	segments := make([]*workspaceReferenceDocumentSegment, len(uris))
	for index, uri := range uris {
		parsed := core.ParseDocument(uri, "<% SharedValue() %>", core.Settings{DefaultLanguage: "VBScript"})
		posting := vbscript.BuildReferenceShard(parsed).PostingsFor("sharedvalue")[0]
		segments[index] = &workspaceReferenceDocumentSegment{
			documentKey: workspacepkg.FileIdentityKeyFromURI(uri),
			parsed:      parsed,
			postings:    []vbscript.ReferencePosting{posting},
		}
	}
	locations, _ := server.materializeWorkspaceReferenceSegments(ctx, segments, workspaceReferenceLocationPlan{
		originURI:          uris[0],
		originDocumentKey:  workspacepkg.FileIdentityKeyFromURI(uris[0]),
		symbolKind:         "reference:function",
		includeDeclaration: true,
	}, nil)
	if len(locations) != len(uris) || len(emitted) != len(uris) {
		t.Fatalf("locations=%#v emitted=%#v, want %d each", locations, emitted, len(uris))
	}
	for index, uri := range uris {
		if locations[index].URI != uri || emitted[index].URI != uri {
			t.Fatalf("position %d locations=%s emitted=%s, want %s", index, locations[index].URI, emitted[index].URI, uri)
		}
	}
}

func TestMaterializeWorkspaceReferenceSegmentUsesClassScopeResolution(t *testing.T) {
	const source = `<%
Function Utility()
End Function
Class LocalContainer
  Public Sub Run()
    Dim Utility
    Utility()
  End Sub
  Public Sub Configure()
    Utility()
  End Sub
End Class
Class MemberContainer
  Public Sub Utility()
  End Sub
  Public Sub Run()
    Utility()
  End Sub
End Class
Class UnshadowedContainer
  Public Sub Run()
    Utility()
  End Sub
End Class
%>`
	parsed := core.ParseDocument("file:///class-scope-resolution.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	key := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	prepared := prepareWorkspaceReferenceDocument(key, parsed, workspacepkg.DiskContentHash(source), vbscript.BuildReferenceShard(parsed), 1)
	segment := prepared.segments["utility"]
	if segment == nil || len(segment.globalResolutions) != len(segment.postings) {
		t.Fatalf("segment global resolutions = %#v", segment)
	}

	locations := materializeWorkspaceReferenceSegment(segment, workspaceReferenceLocationPlan{
		originURI:          parsed.URI,
		originDocumentKey:  key,
		symbolKind:         "reference:function",
		includeDeclaration: true,
		unqualifiedTarget:  true,
	})
	if len(locations) != 3 || locations[0].Range.Start.Line != 1 || locations[1].Range.Start.Line != 9 || locations[2].Range.Start.Line != 21 {
		t.Fatalf("unqualified locations = %#v, want global declaration and calls on lines 9 and 21", locations)
	}
}
