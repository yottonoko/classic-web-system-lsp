package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestLegacyUndefinedGlobalCatalogCollectsScopedWorkspaceSymbols(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	firstURI := "file:///workspace/default.asp"
	firstText := `<div id="HtmlOnly">HtmlOnly</div>
<%
Function First()
  Dim ScopedOnly
  Dim created
  ScopedOnly = 1
  SharedLegacy = ScopedOnly
  Call LegacyCall(SharedLegacy)
  Set created = New LegacyClass
End Function
Function Second()
	Dim x
	Response.Write ScopedOnly
	Response.Write SharedLegacy
	Response.Write MaybeArray(1)
	x = ExpressionCall()
End Function
%>`
	secondURI := "file:///workspace/common.inc"
	secondText := `<%
Dim DeclaredGlobal
Response.Write DeclaredGlobal
Response.Write SharedLegacy
NamedTarget value := SharedLegacy
%>`
	first := server.parseText(firstURI, firstText, "VBScript")
	second := server.parseText(secondURI, secondText, "VBScript")
	catalog := collectLegacyUndefinedGlobalCatalog(context.Background(), []*core.ParsedDocument{first, second}, "source", "settings", nil, nil)

	for _, testCase := range []struct {
		name      string
		kind      LegacyUndefinedGlobalKind
		locations int
	}{
		{name: "LegacyCall", kind: LegacyUndefinedGlobalFunction, locations: 1},
		{name: "LegacyClass", kind: LegacyUndefinedGlobalClass, locations: 1},
		{name: "MaybeArray", kind: LegacyUndefinedGlobalFunction, locations: 1},
		{name: "ExpressionCall", kind: LegacyUndefinedGlobalFunction, locations: 1},
		{name: "ScopedOnly", kind: LegacyUndefinedGlobalVariableOrConstant, locations: 1},
		{name: "SharedLegacy", kind: LegacyUndefinedGlobalVariableOrConstant, locations: 5},
		{name: "NamedTarget", kind: LegacyUndefinedGlobalFunction, locations: 1},
	} {
		symbol := legacyUndefinedGlobalTestSymbol(t, catalog, testCase.name)
		if symbol.Kind != testCase.kind || len(symbol.Locations) != testCase.locations {
			t.Fatalf("%s = kind %q, locations %d; want %q/%d: %#v", testCase.name, symbol.Kind, len(symbol.Locations), testCase.kind, testCase.locations, symbol)
		}
		for index := 1; index < len(symbol.Locations); index++ {
			previous, current := symbol.Locations[index-1], symbol.Locations[index]
			if previous.URI > current.URI || previous.URI == current.URI && previous.Range.Start.Line > current.Range.Start.Line {
				t.Fatalf("%s locations are not deterministic: %#v", testCase.name, symbol.Locations)
			}
		}
	}
	for _, absent := range []string{"DeclaredGlobal", "First", "Second", "created", "HtmlOnly", "value"} {
		if _, ok := legacyUndefinedGlobalSymbolFromCatalog(catalog, absent); ok {
			t.Fatalf("catalog included declared, HTML, or named-argument identifier %q: %#v", absent, catalog.Symbols)
		}
	}
}

func TestLegacyUndefinedGlobalCatalogExcludesConfiguredGlobals(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	parsed := server.parseText(
		"file:///workspace/default.asp",
		`<% Response.Write ConfiguredValue : Response.Write OtherLegacy %>`,
		"VBScript",
	)
	catalog := collectLegacyUndefinedGlobalCatalog(
		context.Background(), []*core.ParsedDocument{parsed}, "source", "settings",
		map[string]struct{}{"configuredvalue": {}}, nil,
	)
	if _, ok := legacyUndefinedGlobalSymbolFromCatalog(catalog, "ConfiguredValue"); ok {
		t.Fatalf("configured global was included: %#v", catalog.Symbols)
	}
	legacyUndefinedGlobalTestSymbol(t, catalog, "OtherLegacy")
}

func TestWorkspaceLegacyUndefinedGlobalsRequiresSettingAndAtUsesVBScriptOccurrences(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/default.asp"
	text := `<div id="SharedLegacy">SharedLegacy</div>
<% Response.Write SharedLegacy %>`
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, text)
	if catalog, ok := server.WorkspaceLegacyUndefinedGlobals(context.Background()); ok || len(catalog.Symbols) != 0 {
		t.Fatalf("disabled legacy catalog = %#v, %v", catalog, ok)
	}

	server.settings.VBScriptAssumeUndefinedGlobals = true
	catalog, ok := server.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok {
		t.Fatal("enabled legacy catalog was not built")
	}
	symbol := legacyUndefinedGlobalTestSymbol(t, catalog, "SharedLegacy")
	document := core.NewTextDocument(uri, "classic-asp", 1, text)
	htmlPosition := document.PositionAt(strings.Index(text, `id="SharedLegacy"`) + len(`id="`))
	if _, ok := server.WorkspaceLegacyUndefinedGlobalAt(context.Background(), uri, htmlPosition); ok {
		t.Fatal("HTML attribute was resolved as a legacy VBScript global")
	}
	vbOffset := strings.LastIndex(text, "SharedLegacy")
	if at, ok := server.WorkspaceLegacyUndefinedGlobalAt(context.Background(), uri, document.PositionAt(vbOffset)); !ok || !strings.EqualFold(at.Name, symbol.Name) {
		t.Fatalf("VBScript occurrence lookup = %#v, %v; want %s", at, ok, symbol.Name)
	}
}

func TestWorkspaceLegacyUndefinedGlobalsCachesByGenerationAndConfiguredGlobals(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	uri := "file:///workspace/default.asp"
	server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, `<% Response.Write ConfiguredValue %>`)

	first, ok := server.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok {
		t.Fatal("first catalog was not built")
	}
	legacyUndefinedGlobalTestSymbol(t, first, "ConfiguredValue")
	first.Symbols[0].Locations = nil

	server.workspace = nil
	cached, ok := server.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok || len(legacyUndefinedGlobalTestSymbol(t, cached, "ConfiguredValue").Locations) == 0 {
		t.Fatalf("in-process catalog was not cloned and reused: %#v, %v", cached, ok)
	}

	server.settings.VBScriptGlobals = map[string]vbscriptGlobalSetting{
		"ConfiguredValue": {Type: "Variant", Kind: "variable"},
	}
	server.workspace = map[string]*core.TextDocument{
		uri: core.NewTextDocument(uri, "classic-asp", 1, `<% Response.Write ConfiguredValue %>`),
	}
	configured, ok := server.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok {
		t.Fatal("catalog with configured globals was not built")
	}
	if _, ok := legacyUndefinedGlobalSymbolFromCatalog(configured, "ConfiguredValue"); ok {
		t.Fatalf("settings fingerprint reused stale in-process catalog: %#v", configured.Symbols)
	}
}

func TestWorkspaceLegacyUndefinedGlobalsAppliesOneDocumentArtifactDelta(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	first := server.parseText("file:///workspace/a.asp", `<% Response.Write SharedLegacy : Response.Write FirstOnly %>`, "VBScript")
	second := server.parseText("file:///workspace/b.asp", `<% Response.Write SharedLegacy : Response.Write StableOnly %>`, "VBScript")
	settingsFingerprint := server.legacyUndefinedGlobalSettingsFingerprint(nil)
	initial := collectLegacyUndefinedGlobalCatalog(
		context.Background(), []*core.ParsedDocument{first, second}, "initial", settingsFingerprint, nil, nil,
	)
	server.rememberWorkspaceLegacyUndefinedGlobals(initial, server.graphGeneration, settingsFingerprint)

	changed := server.parseText("file:///workspace/a.asp", `<% Response.Write SharedLegacy : Response.Write ChangedOnly %>`, "VBScript")
	firstManifest := buildWorkspaceDocumentArtifactManifest(changed, nil, "")
	secondManifest := buildWorkspaceDocumentArtifactManifest(second, nil, "")
	server.mu.Lock()
	server.workspaceArtifacts = map[workspaceDocumentID]*workspaceDocumentArtifactManifest{
		firstManifest.DocumentID:  firstManifest,
		secondManifest.DocumentID: secondManifest,
	}
	server.workspaceIncludeGraphComplete = true
	server.graphGeneration++
	server.workspace = nil // An incremental hit must not fall back to workspace collection.
	var coldProgress bool
	server.legacyUndefinedGlobalProgressTestHook = func(label, _ string, _, _ int) {
		if label == "legacyUndefinedGlobals.collectDocuments" || label == "legacyUndefinedGlobals.fingerprintDocuments" || label == "legacyUndefinedGlobals.scanDocuments" {
			coldProgress = true
		}
	}
	server.mu.Unlock()

	catalog, ok := server.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok {
		t.Fatal("incremental catalog update failed")
	}
	if coldProgress {
		t.Fatal("one-document artifact delta fell back to workspace collection")
	}
	if _, ok := legacyUndefinedGlobalSymbolFromCatalog(catalog, "FirstOnly"); ok {
		t.Fatalf("removed one-document contribution survived: %#v", catalog.Symbols)
	}
	legacyUndefinedGlobalTestSymbol(t, catalog, "ChangedOnly")
	if got := len(legacyUndefinedGlobalTestSymbol(t, catalog, "SharedLegacy").Locations); got != 2 {
		t.Fatalf("SharedLegacy locations = %d; want 2", got)
	}
	legacyUndefinedGlobalTestSymbol(t, catalog, "StableOnly")

	expected := collectLegacyUndefinedGlobalCatalog(
		context.Background(), []*core.ParsedDocument{changed, second}, "expected", settingsFingerprint, nil, nil,
	)
	if !reflect.DeepEqual(catalog.Symbols, expected.Symbols) {
		t.Fatalf("incremental symbols differ from cold rebuild\nincremental: %#v\ncold: %#v", catalog.Symbols, expected.Symbols)
	}
}

func TestLegacyUndefinedGlobalIncrementalIndexRecomputesOnlyAffectedNames(t *testing.T) {
	firstID := workspaceDocumentID("/workspace/a.asp")
	secondID := workspaceDocumentID("/workspace/b.asp")
	initialDocuments := map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot{
		firstID: {
			DocumentID: firstID, URI: "file:///workspace/a.asp", SourceFingerprint: "a1",
			Facts: legacyUndefinedGlobalDocumentFacts{Occurrences: []legacyUndefinedGlobalOccurrence{
				{Name: "Shared", Range: lsp.Range{Start: lsp.Position{Line: 1}, End: lsp.Position{Line: 1, Character: 6}}},
				{Name: "Stable", Range: lsp.Range{Start: lsp.Position{Line: 2}, End: lsp.Position{Line: 2, Character: 6}}},
			}},
		},
		secondID: {
			DocumentID: secondID, URI: "file:///workspace/b.asp", SourceFingerprint: "b1",
			Facts: legacyUndefinedGlobalDocumentFacts{Occurrences: []legacyUndefinedGlobalOccurrence{
				{Name: "Shared", Range: lsp.Range{Start: lsp.Position{Line: 3}, End: lsp.Position{Line: 3, Character: 6}}},
			}},
		},
	}
	initial := buildLegacyUndefinedGlobalIndex(initialDocuments, nil)
	stableBefore := initial.symbolsByName["stable"]
	changedDocuments := make(map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot, len(initialDocuments))
	for documentID, snapshot := range initialDocuments {
		changedDocuments[documentID] = snapshot
	}
	changed := changedDocuments[firstID]
	changed.SourceFingerprint = "a2"
	changed.Facts.GlobalNames = []string{"shared"}
	changedDocuments[firstID] = changed

	catalog := updateLegacyUndefinedGlobalCatalog(context.Background(), initial, changedDocuments, "settings", nil)
	if _, ok := legacyUndefinedGlobalSymbolFromCatalog(catalog, "Shared"); ok {
		t.Fatalf("new global declaration did not suppress affected occurrences: %#v", catalog.Symbols)
	}
	stableAfter, ok := catalog.incrementalIndex.symbolsByName["stable"]
	if !ok || !reflect.DeepEqual(stableAfter, stableBefore) {
		t.Fatalf("unaffected symbol changed: before=%#v after=%#v", stableBefore, stableAfter)
	}
	if initial.symbolsByName["shared"].Name == "" {
		t.Fatal("published previous index was mutated")
	}
}

func TestWorkspaceLegacyUndefinedGlobalsCoalescesConcurrentColdBuild(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	for index, text := range []string{
		`<% Response.Write SharedLegacy %>`,
		`<% x = SharedLegacy : y = ExpressionCall() %>`,
	} {
		uri := "file:///workspace/" + string(rune('a'+index)) + ".asp"
		server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, text)
	}

	var builds atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once
	server.legacyUndefinedGlobalProgressTestHook = func(label, _ string, current, _ int) {
		if label != "legacyUndefinedGlobals.collectDocuments" || current != 1 {
			return
		}
		builds.Add(1)
		enterOnce.Do(func() { close(entered) })
		<-release
	}

	const callers = 8
	results := make([]LegacyUndefinedGlobalCatalog, callers)
	okResults := make([]bool, callers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)
	for index := 0; index < callers; index++ {
		go func() {
			defer waitGroup.Done()
			results[index], okResults[index] = server.WorkspaceLegacyUndefinedGlobals(context.Background())
		}()
		if index == 0 {
			<-entered
		}
	}
	close(release)
	waitGroup.Wait()

	if got := builds.Load(); got != 1 {
		t.Fatalf("cold catalog builds = %d; want 1", got)
	}
	for index := range results {
		if !okResults[index] || results[index].SourceFingerprint != results[0].SourceFingerprint {
			t.Fatalf("concurrent result %d = %#v, %v; want shared result", index, results[index], okResults[index])
		}
		legacyUndefinedGlobalTestSymbol(t, results[index], "SharedLegacy")
	}
}

func TestWorkspaceLegacyUndefinedGlobalsReportsColdBuildProgressAndKeepsMemoryHitsSilent(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	for index, text := range []string{
		`<% Response.Write FirstLegacy %>`,
		`<% Response.Write SecondLegacy %>`,
	} {
		uri := "file:///workspace/progress-" + string(rune('a'+index)) + ".asp"
		server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, text)
	}

	type progressState struct {
		current int
		total   int
	}
	progress := map[string]progressState{}
	var eventCount int
	server.legacyUndefinedGlobalProgressTestHook = func(label, _ string, current, total int) {
		eventCount++
		state := progress[label]
		if current > state.current {
			state.current = current
		}
		if total > state.total {
			state.total = total
		}
		progress[label] = state
	}

	if _, ok := server.WorkspaceLegacyUndefinedGlobals(context.Background()); !ok {
		t.Fatal("cold catalog was not built")
	}
	for _, label := range []string{
		"legacyUndefinedGlobals.collectDocuments",
		"legacyUndefinedGlobals.fingerprintDocuments",
		"legacyUndefinedGlobals.scanDocuments",
	} {
		if got := progress[label]; got.current != 2 || got.total != 2 {
			t.Fatalf("%s progress = %#v; want 2/2", label, got)
		}
	}
	for _, label := range []string{
		"legacyUndefinedGlobals.databaseRead",
		"legacyUndefinedGlobals.databaseWrite",
	} {
		if got := progress[label]; got.current != 1 || got.total != 1 {
			t.Fatalf("%s progress = %#v; want 1/1", label, got)
		}
	}

	eventsAfterColdBuild := eventCount
	if _, ok := server.WorkspaceLegacyUndefinedGlobals(context.Background()); !ok {
		t.Fatal("in-memory catalog hit failed")
	}
	if eventCount != eventsAfterColdBuild {
		t.Fatalf("in-memory hit emitted %d progress events", eventCount-eventsAfterColdBuild)
	}
}

func TestWorkspaceLegacyUndefinedGlobalsRestoresAndInvalidatesDatabaseCatalog(t *testing.T) {
	root := t.TempDir()
	cacheDirectory := filepath.Join(root, "cache")
	path := filepath.Join(root, "default.asp")
	uri := filePathURI(path)
	originalText := `<% Response.Write LegacyValue %>`
	newServer := func(text string, mutateSettings func(*Server)) *Server {
		server := New(nil, io.Discard, io.Discard)
		server.rootPath = root
		server.rootURI = filePathURI(root)
		server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
		server.settings.CacheEnabled = true
		server.settings.CacheDirectory = cacheDirectory
		server.settings.CacheTTLHours = 24
		server.settings.CacheMaxSizeMB = 16
		server.settings.VBScriptAssumeUndefinedGlobals = true
		if mutateSettings != nil {
			mutateSettings(server)
		}
		server.configureDiskAnalysisCache()
		server.workspace[uri] = core.NewTextDocument(uri, "classic-asp", 1, text)
		return server
	}

	first := newServer(originalText, nil)
	firstCatalog, ok := first.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok || firstCatalog.RestoredFromDatabase {
		t.Fatalf("first catalog = %#v, %v", firstCatalog, ok)
	}
	legacyUndefinedGlobalTestSymbol(t, firstCatalog, "LegacyValue")
	if err := first.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	first.closeDiskAnalysisCache()

	second := newServer(originalText, nil)
	secondCatalog, ok := second.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok || !secondCatalog.RestoredFromDatabase {
		t.Fatalf("restart catalog = %#v, %v; want database restore", secondCatalog, ok)
	}
	second.closeDiskAnalysisCache()

	changed := newServer(`<% Response.Write ChangedLegacyValue %>`, nil)
	changedCatalog, ok := changed.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok || changedCatalog.RestoredFromDatabase {
		t.Fatalf("changed-source catalog = %#v, %v; stale source was restored", changedCatalog, ok)
	}
	legacyUndefinedGlobalTestSymbol(t, changedCatalog, "ChangedLegacyValue")
	changed.closeDiskAnalysisCache()

	changedSettings := newServer(originalText, func(server *Server) {
		server.settings.VBScriptGlobals = map[string]vbscriptGlobalSetting{
			"LegacyValue": {Type: "Variant", Kind: "variable"},
		}
	})
	t.Cleanup(changedSettings.closeDiskAnalysisCache)
	settingsCatalog, ok := changedSettings.WorkspaceLegacyUndefinedGlobals(context.Background())
	if !ok || settingsCatalog.RestoredFromDatabase {
		t.Fatalf("changed-settings catalog = %#v, %v; stale settings were restored", settingsCatalog, ok)
	}
	if _, ok := legacyUndefinedGlobalSymbolFromCatalog(settingsCatalog, "LegacyValue"); ok {
		t.Fatalf("configured global was restored from stale database catalog: %#v", settingsCatalog.Symbols)
	}
}

func legacyUndefinedGlobalTestSymbol(t *testing.T, catalog LegacyUndefinedGlobalCatalog, name string) LegacyUndefinedGlobalSymbol {
	t.Helper()
	symbol, ok := legacyUndefinedGlobalSymbolFromCatalog(catalog, name)
	if !ok {
		t.Fatalf("legacy catalog missing %q: %#v", name, catalog.Symbols)
	}
	return symbol
}

func legacyUndefinedGlobalSymbolFromCatalog(catalog LegacyUndefinedGlobalCatalog, name string) (LegacyUndefinedGlobalSymbol, bool) {
	for _, symbol := range catalog.Symbols {
		if strings.EqualFold(symbol.Name, name) {
			return symbol, true
		}
	}
	return LegacyUndefinedGlobalSymbol{}, false
}
