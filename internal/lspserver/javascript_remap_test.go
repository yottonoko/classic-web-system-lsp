package lspserver

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestJavaScriptInlayHintPositionsMapPastHTMLPrefix(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "inlay-prefix.asp"))
	source := `<div>prefix</div>
<script>
function add(first, second) { return first + second; }
const total = add(1, 2);
</script>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc

	hints := server.inlayHints(uri, doc.Range(0, len(source)))
	positions := map[string]lsp.Position{}
	for _, hint := range hints {
		positions[javaScriptInlayHintLabelText(hint.Label)] = hint.Position
	}
	for _, label := range []string{"first:", "second:"} {
		position, ok := positions[label]
		if !ok {
			t.Fatalf("JavaScript inlay hint %q missing: %#v", label, hints)
		}
		if position.Line != 3 {
			t.Fatalf("JavaScript inlay hint %q position = %#v, want source line 3", label, position)
		}
	}
}

func TestJavaScriptInlayHintPreservesAndRemapsLabelPartLocations(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "inlay-label-location.asp"))
	source := `<div>prefix</div>
<script>
function add(first, second) { return first + second; }
const total = add(1, 2);
</script>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc

	hints := server.inlayHints(uri, doc.Range(0, len(source)))
	for _, hint := range hints {
		parts, ok := hint.Label.([]any)
		if !ok {
			continue
		}
		for _, partValue := range parts {
			part, ok := partValue.(map[string]any)
			if !ok || part["value"] != "first" {
				continue
			}
			var location lsp.Location
			if remarshal(part["location"], &location) != nil {
				t.Fatalf("first inlay label part location missing: %#v", part)
			}
			if location.URI != uri {
				t.Fatalf("first inlay label location URI = %q, want %q", location.URI, uri)
			}
			got := doc.Text[doc.OffsetAt(location.Range.Start):doc.OffsetAt(location.Range.End)]
			if got != "first" || location.Range.Start.Line != 2 {
				t.Fatalf("first inlay label location = %#v (%q), want source declaration", location.Range, got)
			}
			return
		}
	}
	t.Fatalf("JavaScript inlay label part location missing: %#v", hints)
}

func TestJavaScriptInlayHintPreservesOptionalTextEditsAndTooltip(t *testing.T) {
	uri := "file:///tmp/inlay-optional.asp"
	source := `<div>prefix</div>
<script>
const targetName = 1;
</script>`
	mapping := newJavaScriptRemapTestMapping(t, uri, source, core.LanguageJavaScript)
	sourceRange := sourceRangeForSubstring(t, mapping.source, "targetName")
	virtualRange := javaScriptRemapTestVirtualRange(t, mapping, sourceRange)
	virtualPosition, ok := mapping.virtual.ToVirtualPosition(sourceRange.End, mapping.source)
	if !ok {
		t.Fatal("inlay hint position did not map to the virtual document")
	}
	value := []any{map[string]any{
		"position": javaScriptRemapTestPositionValue(virtualPosition),
		"label":    ": number",
		"textEdits": []any{map[string]any{
			"range":   javaScriptRemapTestRangeValue(virtualRange),
			"newText": "renamedTarget",
		}},
		"tooltip": map[string]any{"kind": "markdown", "value": "**inferred**"},
	}}

	remapped := remapJavaScriptServiceValue(value, mapping, map[string]*javaScriptVirtualFile{mapping.uri: mapping})
	var hints []lsp.InlayHint
	if remarshal(remapped, &hints) != nil || len(hints) != 1 || len(hints[0].TextEdits) != 1 {
		t.Fatalf("remapped optional inlay fields = %#v", remapped)
	}
	edit := hints[0].TextEdits[0]
	if got := mapping.source.Text[mapping.source.OffsetAt(edit.Range.Start):mapping.source.OffsetAt(edit.Range.End)]; got != "targetName" {
		t.Fatalf("inlay text edit range = %#v (%q), want targetName", edit.Range, got)
	}
	var tooltip lsp.MarkupContent
	if remarshal(hints[0].Tooltip, &tooltip) != nil || tooltip.Kind != "markdown" || tooltip.Value != "**inferred**" {
		t.Fatalf("inlay tooltip = %#v", hints[0].Tooltip)
	}
}

func TestJavaScriptCallHierarchyFromRangesMapPastHTMLPrefix(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "call-prefix.asp"))
	source := `<div>prefix</div>
<script>
function callee() { return 1; }
function caller() { return callee(); }
</script>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc

	items := server.prepareCallHierarchy(uri, positionAtSuffix(source, "callee"))
	if len(items) != 1 {
		t.Fatalf("prepare call hierarchy = %#v", items)
	}
	incoming := server.incomingCalls(items[0])
	if len(incoming) != 1 || len(incoming[0].FromRanges) != 1 {
		t.Fatalf("incoming calls = %#v", incoming)
	}
	callRange := incoming[0].FromRanges[0]
	if callRange.Start.Line != 3 {
		t.Fatalf("incoming call range = %#v, want source line 3", callRange)
	}
	if got := doc.Text[doc.OffsetAt(callRange.Start):doc.OffsetAt(callRange.End)]; got != "callee" {
		t.Fatalf("incoming call range text = %q, want callee", got)
	}
}

func TestJavaScriptVirtualFileLookupSupportsWindowsAndUnixURIForms(t *testing.T) {
	tests := []struct {
		name       string
		mappingURI string
		serviceURI string
	}{
		{name: "Windows path", mappingURI: "file:///c:/Site/page.asp.__asp_client.js", serviceURI: "file:///c%3A/Site/page.asp.__asp_client.js"},
		{name: "Unix path", mappingURI: "file:///srv/site/page.asp.__asp_client.js", serviceURI: "file:///srv/site/%70age.asp.__asp_client.js"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapping := &javaScriptVirtualFile{uri: test.mappingURI}
			files := map[string]*javaScriptVirtualFile{test.mappingURI: mapping}
			if got := javaScriptVirtualFileForURI(files, test.serviceURI); got != mapping {
				t.Fatalf("lookup for %q did not match %q", test.serviceURI, test.mappingURI)
			}
		})
	}
}

func TestJavaScriptVirtualPathSupportsWindowsAndUnixPaths(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		language core.EmbeddedLanguage
		want     func(t *testing.T) string
	}{
		{
			name:     "Windows path",
			uri:      "file:///C:/Site/page.asp",
			language: core.LanguageJavaScript,
			want:     func(*testing.T) string { return "c:/Site/page.asp.__asp_client.js" },
		},
		{
			name:     "Unix path",
			uri:      "file:///srv/site/page.asp",
			language: core.LanguageJScript,
			want: func(t *testing.T) string {
				path, err := filepath.Abs(fileURIPath("file:///srv/site/page.asp"))
				if err != nil {
					t.Fatal(err)
				}
				path = filepath.ToSlash(path) + ".__asp_server.js"
				if len(path) >= 2 && path[1] == ':' {
					path = strings.ToLower(path[:1]) + path[1:]
				}
				return path
			},
		},
		{
			name:     "UNC path",
			uri:      "file://Server/Share/page.asp",
			language: core.LanguageJavaScript,
			want:     func(*testing.T) string { return "//Server/Share/page.asp.__asp_client.js" },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := test.want(t)
			if got := javaScriptVirtualPath(test.uri, test.language); got != want {
				t.Fatalf("javaScriptVirtualPath(%q, %q) = %q, want %q", test.uri, test.language, got, want)
			}
		})
	}
}

func TestJavaScriptWorkspaceEditRemapMergesVirtualFilesWithSameOwner(t *testing.T) {
	uri := "file:///tmp/remap-mixed.asp"
	source := `<script>
const clientName = 1;
</script>
<script runat="server" language="JScript">
var serverName = 2;
</script>`
	client := newJavaScriptRemapTestMapping(t, uri, source, core.LanguageJavaScript)
	server := newJavaScriptRemapTestMapping(t, uri, source, core.LanguageJScript)
	files := map[string]*javaScriptVirtualFile{client.uri: client, server.uri: server}
	clientRange := javaScriptRemapTestVirtualRange(t, client, sourceRangeForSubstring(t, client.source, "clientName"))
	serverRange := javaScriptRemapTestVirtualRange(t, server, sourceRangeForSubstring(t, server.source, "serverName"))
	value := map[string]any{"changes": map[string]any{
		client.uri: []any{map[string]any{"range": javaScriptRemapTestRangeValue(clientRange), "newText": "renamedClient"}},
		server.uri: []any{map[string]any{"range": javaScriptRemapTestRangeValue(serverRange), "newText": "renamedServer"}},
	}}

	remapped := remapJavaScriptServiceValue(value, client, files)
	var edit lsp.WorkspaceEdit
	if remarshal(remapped, &edit) != nil {
		t.Fatalf("remapped workspace edit = %#v", remapped)
	}
	edits := edit.Changes[uri]
	if len(edits) != 2 {
		t.Fatalf("merged workspace edits = %#v, want both virtual-file edits", edit.Changes)
	}
	got := map[string]string{}
	for _, textEdit := range edits {
		got[textEdit.NewText] = client.source.Text[client.source.OffsetAt(textEdit.Range.Start):client.source.OffsetAt(textEdit.Range.End)]
	}
	if got["renamedClient"] != "clientName" || got["renamedServer"] != "serverName" {
		t.Fatalf("merged workspace edit ranges = %#v", got)
	}
}

func TestJavaScriptDocumentChangesUseSiblingTextDocumentMapping(t *testing.T) {
	active := newJavaScriptRemapTestMapping(t, "file:///tmp/remap-active.asp", `<script>
const activeName = 1;
</script>`, core.LanguageJavaScript)
	target := newJavaScriptRemapTestMapping(t, "file:///tmp/remap-target.asp", `<div>prefix</div>
<script>
const targetName = 1;
</script>`, core.LanguageJavaScript)
	files := map[string]*javaScriptVirtualFile{active.uri: active, target.uri: target}
	targetRange := javaScriptRemapTestVirtualRange(t, target, sourceRangeForSubstring(t, target.source, "targetName"))
	value := map[string]any{"documentChanges": []any{map[string]any{
		"textDocument": map[string]any{"uri": target.uri, "version": float64(1)},
		"edits":        []any{map[string]any{"range": javaScriptRemapTestRangeValue(targetRange), "newText": "renamedTarget"}},
	}}}

	remapped := remapJavaScriptServiceValue(value, active, files)
	var edit struct {
		DocumentChanges []struct {
			TextDocument lsp.VersionedTextDocumentIdentifier `json:"textDocument"`
			Edits        []lsp.TextEdit                      `json:"edits"`
		} `json:"documentChanges"`
	}
	if remarshal(remapped, &edit) != nil || len(edit.DocumentChanges) != 1 || len(edit.DocumentChanges[0].Edits) != 1 {
		t.Fatalf("remapped document changes = %#v", remapped)
	}
	change := edit.DocumentChanges[0]
	if change.TextDocument.URI != target.owner {
		t.Fatalf("document change URI = %q, want %q", change.TextDocument.URI, target.owner)
	}
	r := change.Edits[0].Range
	if got := target.source.Text[target.source.OffsetAt(r.Start):target.source.OffsetAt(r.End)]; got != "targetName" {
		t.Fatalf("document change edit range text = %q, want targetName; range=%#v", got, r)
	}
}

func TestJavaScriptFoldingRangesMapPastHTMLPrefix(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "fold-prefix.asp"))
	source := `<div>prefix</div>
<script>
function folded() {
  if (true) {
    return 1;
  }
}
</script>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)

	ranges := server.foldingRanges(uri)
	for _, foldingRange := range ranges {
		if foldingRange.StartLine == 2 && foldingRange.EndLine >= 5 {
			return
		}
	}
	t.Fatalf("JavaScript function folding range was not mapped to source lines 2-6: %#v", ranges)
}

func TestJavaScriptFoldingRangesQueryClientAndServerMappingsOnce(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "fold-mixed.asp"))
	source := `<div>prefix</div>
<script>
function clientFold() {
  return 1;
}
</script>
<script runat="server" language="JScript">
function serverFold() {
  return 2;
}
</script>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)

	ranges := server.foldingRanges(uri)
	wanted := map[[2]int]bool{{2, 4}: false, {7, 9}: false}
	seen := map[lsp.FoldingRange]struct{}{}
	for _, foldingRange := range ranges {
		if _, duplicate := seen[foldingRange]; duplicate {
			t.Fatalf("duplicate folding range returned: %#v in %#v", foldingRange, ranges)
		}
		seen[foldingRange] = struct{}{}
		key := [2]int{foldingRange.StartLine, foldingRange.EndLine}
		if _, ok := wanted[key]; ok {
			wanted[key] = true
		}
	}
	for lines, found := range wanted {
		if !found {
			t.Fatalf("mixed JavaScript folding range %v missing: %#v", lines, ranges)
		}
	}
}

func TestJavaScriptCompletionResolveUsesVirtualLanguageFromItemData(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "mixed-resolve.asp"))
	source := `<script>
const clientValue = 1;
</script>
<script runat="server" language="JScript">
const serverObject = { serverMember: 1 };
serverObject.serverM
</script>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc
	position := positionAtSuffix(source, "serverObject.serverM")
	var completions lsp.CompletionList
	if !server.javaScriptLanguageServiceRequest(context.Background(), uri, position, "textDocument/completion", nil, &completions) {
		t.Fatal("JScript completion request failed")
	}
	for _, item := range completions.Items {
		if item.Label != "serverMember" {
			continue
		}
		replacementStart := strings.LastIndex(source, "serverM")
		item.TextEdit = &lsp.TextEdit{
			Range:   doc.Range(replacementStart, replacementStart+len("serverM")),
			NewText: "serverMember",
		}
		resolved, ok := server.resolveJavaScriptCompletionItem(context.Background(), item)
		if !ok {
			t.Fatal("JScript completion resolve failed")
		}
		if resolved.TextEdit == nil {
			t.Fatal("JScript completion resolve dropped its text edit")
		}
		r := resolved.TextEdit.Range
		if r.Start.Line != 5 || r.End.Line != 5 {
			t.Fatalf("JScript completion resolve text edit = %#v, want source line 5", r)
		}
		if got := doc.Text[doc.OffsetAt(r.Start):doc.OffsetAt(r.End)]; got != "serverM" {
			t.Fatalf("JScript completion resolve text edit range = %q, want serverM", got)
		}
		return
	}
	t.Fatal("serverMember JScript completion missing")
}

func TestJavaScriptAutoImportCompletionTargetsLaterScriptRegion(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	server.settings.CheckJS = true
	server.settings.JavaScriptAutoImports = true
	if err := os.WriteFile(filepath.Join(server.rootPath, "helpers.js"), []byte("export function helperThing() { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(server.rootPath, "later-import.asp"))
	source := `<script type="module">
const firstModuleValue = 1;
</script>
<div>between</div>
<script type="module">
helper
</script>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc
	position := positionAtSuffix(source, "helper")
	completions := server.completion(context.Background(), uri, position, nil)
	for _, item := range completions.Items {
		if item.Label != "helperThing" {
			continue
		}
		resolved := server.resolveCompletionItem(item)
		if len(resolved.AdditionalTextEdits) == 0 {
			t.Fatalf("auto-import completion resolve = %#v", resolved)
		}
		parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
		region := core.RegionAt(parsed, doc.OffsetAt(position))
		if region == nil {
			t.Fatal("later JavaScript region missing")
		}
		want := doc.PositionAt(region.ContentStart)
		for _, edit := range resolved.AdditionalTextEdits {
			if strings.Contains(edit.NewText, "import") {
				if edit.Range.Start != want || edit.Range.End != want {
					t.Fatalf("auto-import insertion = %#v, want later script start %#v", edit.Range, want)
				}
				return
			}
		}
		t.Fatalf("resolved completion has no import edit: %#v", resolved.AdditionalTextEdits)
	}
	t.Fatal("helperThing auto-import completion missing")
}

func TestJavaScriptCompilerAutoImportEditTargetsCompletionSegment(t *testing.T) {
	uri := "file:///tmp/compiler-later-import.asp"
	source := `<script type="module">
const firstModuleValue = 1;
</script>
<div>between</div>
<script type="module">
helper
</script>`
	mapping := newJavaScriptRemapTestMapping(t, uri, source, core.LanguageJavaScript)
	sourceCompletion := mapping.source.PositionAt(strings.Index(source, "helper") + len("helper"))
	virtualCompletion, ok := mapping.virtual.ToVirtualPosition(sourceCompletion, mapping.source)
	if !ok {
		t.Fatal("later completion position did not map to the virtual document")
	}
	completionOffset := mapping.virtualDocument.OffsetAt(virtualCompletion)
	value := map[string]any{"additionalTextEdits": []any{map[string]any{
		"range":   javaScriptRemapTestRangeValue(lsp.Range{}),
		"newText": "import { helperThing } from \"./helpers\";\n",
	}}}

	relocateJavaScriptCompletionImportEdits(value, mapping, completionOffset)
	remapped := remapJavaScriptServiceValue(value, mapping, map[string]*javaScriptVirtualFile{mapping.uri: mapping})
	var item lsp.CompletionItem
	if remarshal(remapped, &item) != nil || len(item.AdditionalTextEdits) != 1 {
		t.Fatalf("remapped compiler auto-import item = %#v", remapped)
	}
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	region := core.RegionAt(parsed, mapping.source.OffsetAt(sourceCompletion))
	if region == nil {
		t.Fatal("later JavaScript region missing")
	}
	want := mapping.source.PositionAt(region.ContentStart)
	if got := item.AdditionalTextEdits[0].Range; got.Start != want || got.End != want {
		t.Fatalf("compiler auto-import insertion = %#v, want later script start %#v", got, want)
	}
}

func TestRemapJavaScriptDiagnosticResponseDropsRangesAcrossMaskedASPHoles(t *testing.T) {
	uri := "file:///tmp/diagnostic-source-map.asp"
	source := `<script>
const before = true;
<% If enabled Then %>
const after = missingName;
</script>`
	mapping := newJavaScriptRemapTestMapping(t, uri, source, core.LanguageJavaScript)
	virtualStart := strings.Index(mapping.virtual.Text, "before")
	virtualAfter := strings.Index(mapping.virtual.Text, "missingName")
	virtualDoc := mapping.virtualDocument
	crossHole := lsp.Range{
		Start: virtualDoc.PositionAt(virtualStart),
		End:   virtualDoc.PositionAt(virtualAfter + len("missingName")),
	}
	afterRange := lsp.Range{
		Start: virtualDoc.PositionAt(virtualAfter),
		End:   virtualDoc.PositionAt(virtualAfter + len("missingName")),
	}
	raw, err := json.Marshal(javaScriptDiagnosticReport{Items: []lsp.Diagnostic{
		{Range: crossHole, Message: "crosses ASP hole"},
		{Range: afterRange, Message: "after ASP hole"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var report javaScriptDiagnosticReport
	if !remapJavaScriptDiagnosticResponse(raw, &report, mapping) {
		t.Fatal("diagnostic response remapping failed")
	}
	if len(report.Items) != 1 || report.Items[0].Message != "after ASP hole" {
		t.Fatalf("remapped diagnostics = %#v", report.Items)
	}
	want := sourceRangeForSubstring(t, mapping.source, "missingName")
	if report.Items[0].Range != want {
		t.Fatalf("diagnostic range = %#v, want %#v", report.Items[0].Range, want)
	}
}

func newJavaScriptRemapTestMapping(t *testing.T, uri, source string, language core.EmbeddedLanguage) *javaScriptVirtualFile {
	t.Helper()
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	virtual := core.BuildVirtualDocument(parsed, language)
	if virtual.Text == "" {
		t.Fatalf("virtual document for %s is empty", language)
	}
	path := javaScriptVirtualPath(uri, language)
	serviceURI := (&javaScriptFileURI{path: path}).String()
	return &javaScriptVirtualFile{
		path: path, uri: serviceURI, owner: uri, virtual: virtual,
		virtualDocument: core.NewTextDocument(serviceURI, virtual.LanguageID, 0, virtual.Text), source: doc,
	}
}

func sourceRangeForSubstring(t *testing.T, doc *core.TextDocument, substring string) lsp.Range {
	t.Helper()
	start := strings.Index(doc.Text, substring)
	if start < 0 {
		t.Fatalf("substring %q missing", substring)
	}
	return doc.Range(start, start+len(substring))
}

func javaScriptRemapTestVirtualRange(t *testing.T, mapping *javaScriptVirtualFile, sourceRange lsp.Range) lsp.Range {
	t.Helper()
	start, startOK := mapping.virtual.ToVirtualPosition(sourceRange.Start, mapping.source)
	end, endOK := mapping.virtual.ToVirtualPosition(sourceRange.End, mapping.source)
	if !startOK || !endOK {
		t.Fatalf("source range %#v did not map to %s", sourceRange, mapping.path)
	}
	return lsp.Range{Start: start, End: end}
}

func javaScriptRemapTestRangeValue(r lsp.Range) map[string]any {
	value, _ := json.Marshal(r)
	var result map[string]any
	_ = json.Unmarshal(value, &result)
	return result
}

func javaScriptRemapTestPositionValue(position lsp.Position) map[string]any {
	return map[string]any{"line": float64(position.Line), "character": float64(position.Character)}
}

func javaScriptInlayHintLabelText(label any) string {
	if text, ok := label.(string); ok {
		return text
	}
	parts, ok := label.([]any)
	if !ok {
		return ""
	}
	var result strings.Builder
	for _, partValue := range parts {
		if part, ok := partValue.(map[string]any); ok {
			if value, ok := part["value"].(string); ok {
				result.WriteString(value)
			}
		}
	}
	return result.String()
}
