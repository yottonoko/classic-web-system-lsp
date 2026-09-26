package lspserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestNavigationVBPreparationMaterializesLinearIncludeRootsOnce(t *testing.T) {
	const documentCount = 96
	documents := navigationPreparationIncludeChain(t, documentCount)
	builder := newNavigationGraphBuilder("workspace", documents[0].URI, nil)
	if err := builder.prepareVBScriptFunctionsContext(context.Background(), documents, nil); err != nil {
		t.Fatalf("prepare include chain = %v", err)
	}

	if got := len(builder.vbExecutionPrograms); got != 1 {
		t.Fatalf("execution program roots = %d, want one root for a linear chain", got)
	}
	units := 0
	for _, program := range builder.vbExecutionPrograms {
		units += len(program)
	}
	if units > documentCount*2 {
		t.Fatalf("execution units = %d for %d documents, want linear materialization", units, documentCount)
	}
	definitionCount := 0
	for _, definitions := range builder.vbFunctions {
		definitionCount += len(definitions)
	}
	if got := len(builder.vbFunctions); got != documentCount {
		t.Fatalf("function definition documents = %d, want %d without owner copies", got, documentCount)
	}
	if definitionCount != documentCount {
		t.Fatalf("function definitions = %d, want %d unique definitions", definitionCount, documentCount)
	}
	ownerEdges := 0
	for _, parents := range builder.vbFunctionOwners {
		ownerEdges += len(parents)
	}
	if ownerEdges > documentCount*2 {
		t.Fatalf("function owner edges = %d for %d documents, want near-linear ownership", ownerEdges, documentCount)
	}
}

func TestNavigationVBPreparationIndexesExecutionProgramsByDocument(t *testing.T) {
	root := t.TempDir()
	first := core.ParseDocument(filePathURI(filepath.Join(root, "first.asp")), `<% firstTarget = "first-next.asp" %>`, core.Settings{})
	second := core.ParseDocument(filePathURI(filepath.Join(root, "second.asp")), `<% secondTarget = "second-next.asp" %>`, core.Settings{})
	shared := core.ParseDocument(filePathURI(filepath.Join(root, "shared.inc")), `<a href="<%= sharedTarget %>">shared</a>`, core.Settings{})
	sharedKey := workspacepkg.FileIdentityKeyFromURI(shared.URI)
	relations := map[string][]navigationVBIncludeRelation{
		workspacepkg.FileIdentityKeyFromURI(first.URI):  {{ParentURI: first.URI, ChildURI: shared.URI, ChildKey: sharedKey}},
		workspacepkg.FileIdentityKeyFromURI(second.URI): {{ParentURI: second.URI, ChildURI: shared.URI, ChildKey: sharedKey}},
	}
	builder := newNavigationGraphBuilder("workspace", "", nil)
	if err := builder.prepareVBScriptFunctionsWithIncludesContext(context.Background(), []*core.ParsedDocument{shared, second, first}, nil, relations); err != nil {
		t.Fatalf("prepare shared include programs = %v", err)
	}

	firstPrograms := builder.navigationVBHTMLProgramsForDocument(first, first.URI)
	if len(firstPrograms) != 1 || firstPrograms[0].key != workspacepkg.FileIdentityKeyFromURI(first.URI) {
		t.Fatalf("first programs = %#v, want only first root", firstPrograms)
	}
	secondPrograms := builder.navigationVBHTMLProgramsForDocument(second, second.URI)
	if len(secondPrograms) != 1 || secondPrograms[0].key != workspacepkg.FileIdentityKeyFromURI(second.URI) {
		t.Fatalf("second programs = %#v, want only second root", secondPrograms)
	}
	sharedPrograms := builder.navigationVBHTMLProgramsForDocument(shared, shared.URI)
	if len(sharedPrograms) != 2 {
		t.Fatalf("shared programs = %#v, want both roots", sharedPrograms)
	}
	if sharedPrograms[0].key >= sharedPrograms[1].key {
		t.Fatalf("shared program keys = %#v, want stable sorted root order", []string{sharedPrograms[0].key, sharedPrograms[1].key})
	}

	documentBuilder := newNavigationGraphBuilder("document", first.URI, nil)
	if err := documentBuilder.prepareVBScriptFunctionsWithIncludesContext(context.Background(), []*core.ParsedDocument{shared, second, first}, nil, relations); err != nil {
		t.Fatalf("prepare document programs = %v", err)
	}
	if documentBuilder.vbHTMLProgramsByDocument != nil {
		t.Fatalf("document program index = %#v, want no unused workspace index", documentBuilder.vbHTMLProgramsByDocument)
	}
}

func TestNavigationVBPreparationCancellationDoesNotPublishPartialState(t *testing.T) {
	const documentCount = 64
	documents := navigationPreparationIncludeChain(t, documentCount)
	base := context.Background()
	ctx := &navigationPreparationCancelContext{Context: base, cancelAfter: documentCount + 8}
	builder := newNavigationGraphBuilder("workspace", documents[0].URI, nil)
	err := builder.prepareVBScriptFunctionsContext(ctx, documents, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation error = %v, want context.Canceled", err)
	}
	if len(builder.vbExecutionPrograms) != 0 {
		t.Fatalf("cancelled preparation published execution programs: %d", len(builder.vbExecutionPrograms))
	}
	if len(builder.vbFunctions) != 0 {
		t.Fatalf("cancelled preparation published function scopes: %d", len(builder.vbFunctions))
	}
	if builder.vbResolvedIncludes != nil {
		t.Fatalf("cancelled preparation published include relations: %#v", builder.vbResolvedIncludes)
	}
}

func TestNavigationVBPreparationFunctionIndexCancellationDoesNotPublishPartialState(t *testing.T) {
	const documentCount = 64
	documents := navigationPreparationIncludeChain(t, documentCount)
	resolved, err := navigationVBIncludeRelationsForDocumentsContext(context.Background(), documents)
	if err != nil {
		t.Fatalf("resolve include chain = %v", err)
	}
	owners := make(map[string][]string, documentCount-1)
	for index := 1; index < documentCount; index++ {
		owners[workspacepkg.FileIdentityKeyFromURI(documents[index].URI)] = []string{documents[index-1].URI}
	}
	ctx := &navigationPreparationCancelContext{Context: context.Background(), cancelAfter: documentCount + 4}
	builder := newNavigationGraphBuilder("workspace", documents[0].URI, nil)
	err = builder.prepareVBScriptFunctionsWithIncludesContext(ctx, documents, owners, resolved)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled function-index preparation error = %v, want context.Canceled", err)
	}
	if len(builder.vbExecutionPrograms) != 0 || len(builder.vbExecutionRootKeys) != 0 || len(builder.vbHTMLProgramsByDocument) != 0 || len(builder.vbFunctions) != 0 || builder.vbResolvedIncludes != nil {
		t.Fatalf("cancelled function-index preparation published partial state: programs=%d roots=%d htmlIndexes=%d functions=%d includes=%#v", len(builder.vbExecutionPrograms), len(builder.vbExecutionRootKeys), len(builder.vbHTMLProgramsByDocument), len(builder.vbFunctions), builder.vbResolvedIncludes)
	}
}

type navigationPreparationCancelContext struct {
	context.Context
	cancelAfter int
	calls       int
}

func (ctx *navigationPreparationCancelContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAfter {
		return context.Canceled
	}
	return ctx.Context.Err()
}

func navigationPreparationIncludeChain(t *testing.T, count int) []*core.ParsedDocument {
	t.Helper()
	root := t.TempDir()
	documents := make([]*core.ParsedDocument, count)
	for index := range documents {
		name := "page.asp"
		if index > 0 {
			name = fmt.Sprintf("include-%03d.inc", index)
		}
		path := filepath.Join(root, name)
		next := ""
		if index+1 < count {
			next = fmt.Sprintf("include-%03d.inc", index+1)
		}
		functionName := fmt.Sprintf("UniqueFunction%03d", index)
		source := fmt.Sprintf("<%%\nFunction %s()\n%s = \"target-%03d.asp\"\nEnd Function\n%%>\n", functionName, functionName, index)
		if index%2 == 1 {
			subName := fmt.Sprintf("UniqueSub%03d", index)
			source = fmt.Sprintf("<%%\nSub %s()\nEnd Sub\n%%>\n", subName)
		}
		if next != "" {
			source += `<!-- #include file="` + next + `" -->`
		}
		documents[index] = core.ParseDocument(filePathURI(path), source, core.Settings{})
	}
	return documents
}
