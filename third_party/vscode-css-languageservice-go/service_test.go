package cssls

import (
	"context"
	"strings"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
	"github.com/yottonoko/vscode-css-languageservice-go/parser"
)

func TestLanguageServiceFormatUsesGoFormatter(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, ".foo { display:  node;  }")
	edits := service.Format(document, nil, CSSFormatConfiguration{InsertSpaces: true, TabSize: 2})
	got := lsp.ApplyEdits(document, edits)
	want := ".foo {\n  display: node;\n}"
	if got != want {
		t.Fatalf("formatted = %q, want %q", got, want)
	}
}

func TestLanguageServiceValidationUsesGoLint(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, "selector {} #selector { float: right }")

	diagnostics := service.DoValidation(document, service.ParseStylesheet(document), nil)
	if len(diagnostics) != 1 || diagnostics[0].Code != "emptyRules" {
		t.Fatalf("default diagnostics = %#v", diagnostics)
	}

	settings := LanguageSettings{Lint: map[string]any{"idSelector": "warning", "float": "error"}}
	diagnostics = service.DoValidation(document, service.ParseStylesheet(document), &settings)
	if len(diagnostics) != 3 || diagnostics[1].Code != "idSelector" || diagnostics[2].Code != "float" {
		t.Fatalf("configured diagnostics = %#v", diagnostics)
	}
}

func TestLanguageServiceParseStylesheetUsesGoParser(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, ".foo { color: red; }")
	stylesheet := service.ParseStylesheet(document)
	if stylesheet == nil || stylesheet.Root == nil {
		t.Fatal("stylesheet root is nil")
	}
	if stylesheet.Root.Type() != parser.NodeTypeStylesheet {
		t.Fatalf("stylesheet root type = %#v", stylesheet.Root.Type())
	}
	children := stylesheet.Root.GetChildren()
	if len(children) != 1 || children[0].Type() != parser.NodeTypeRuleset {
		t.Fatalf("stylesheet children = %#v", children)
	}
}

func TestLanguageServiceSelectionRanges(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, ".foo { color: blue; }")

	ranges := service.GetSelectionRanges(document, []lsp.Position{document.PositionAt(9)}, service.ParseStylesheet(document))
	if len(ranges) != 1 || document.GetText(&ranges[0].Range) != "color" {
		t.Fatalf("selection ranges = %#v", ranges)
	}
	if ranges[0].Parent == nil || document.GetText(&ranges[0].Parent.Range) != "color: blue" {
		t.Fatalf("selection parent = %#v", ranges[0].Parent)
	}
}

func TestLanguageServiceDocumentSymbols(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, ".foo {}")
	stylesheet := service.ParseStylesheet(document)

	infos := service.FindDocumentSymbols(document, stylesheet)
	if len(infos) != 1 || infos[0].Name != ".foo" || infos[0].Kind != lsp.SymbolKindClass {
		t.Fatalf("symbol infos = %#v", infos)
	}
	symbols := service.FindDocumentSymbols2(document, stylesheet)
	if len(symbols) != 1 || symbols[0].Name != ".foo" || symbols[0].SelectionRange.End.Character != 4 {
		t.Fatalf("document symbols = %#v", symbols)
	}
}

func TestLanguageServiceColors(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, ".foo { color: #ff0000 }")
	stylesheet := service.ParseStylesheet(document)

	colors := service.FindDocumentColors(document, stylesheet)
	if len(colors) != 1 || colors[0].Range.Start.Character != 14 {
		t.Fatalf("document colors = %#v", colors)
	}
	presentations := service.GetColorPresentations(document, stylesheet, colors[0].Color, colors[0].Range)
	if len(presentations) == 0 || presentations[0].Label != "rgb(255, 0, 0)" {
		t.Fatalf("color presentations = %#v", presentations)
	}
}

func TestLanguageServiceColorsAfterPropertyAtRule(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/property.css", "css", 0, "@property --accent { syntax: '<color>'; inherits: false; initial-value: #c0ffee; } .after { color: red; }")
	stylesheet := service.ParseStylesheet(document)

	colors := service.FindDocumentColors(document, stylesheet)
	if len(colors) != 2 || document.GetText(&colors[0].Range) != "#c0ffee" || document.GetText(&colors[1].Range) != "red" {
		t.Fatalf("document colors after @property = %#v", colors)
	}
}

func TestLanguageServiceHighlightsAndRename(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, "body { display: inline } #foo { display: inline }")
	stylesheet := service.ParseStylesheet(document)
	position := document.PositionAt(10)

	highlights := service.FindDocumentHighlights(document, position, stylesheet)
	if len(highlights) != 2 {
		t.Fatalf("highlights = %#v", highlights)
	}
	r := service.PrepareRename(document, position, stylesheet)
	if r == nil || document.GetText(r) != "display" {
		t.Fatalf("prepare rename = %#v", r)
	}
	edit := service.DoRename(document, position, "visibility", stylesheet)
	if got := lsp.ApplyEdits(document, edit.Changes[document.URI]); got != "body { visibility: inline } #foo { visibility: inline }" {
		t.Fatalf("renamed = %q", got)
	}
}

func TestLanguageServiceDocumentLinks(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, `@import "foo.css";`)
	links, err := service.FindDocumentLinks(context.Background(), document, service.ParseStylesheet(document), testDocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Target != "test://test/foo.css" {
		t.Fatalf("links = %#v", links)
	}
}

func TestLanguageServiceDocumentLinksUseImportAliases(t *testing.T) {
	service := GetCSSLanguageService()
	service.Configure(LanguageSettings{ImportAliases: AliasSettings{
		"@SingleStylesheet": "/src/assets/styles.css",
		"@AssetsDir/":       "/src/assets/",
	}})
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, `@import "@AssetsDir/styles.css";`)
	links, err := service.FindDocumentLinks(context.Background(), document, service.ParseStylesheet(document), testDocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Target != "test://test/src/assets/styles.css" {
		t.Fatalf("links = %#v", links)
	}
}

func TestLanguageServiceCodeActions(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, "body { displai: inline }")
	stylesheet := service.ParseStylesheet(document)
	diagnostics := service.DoValidation(document, stylesheet, nil)
	actions := service.DoCodeActions(document, lsp.Range{}, lsp.CodeActionContext{Diagnostics: diagnostics}, stylesheet)
	if len(actions) == 0 || actions[0].Title != "Rename to 'display'" {
		t.Fatalf("code actions = %#v", actions)
	}
	if actions[0].Command != "_css.applyCodeAction" || len(actions[0].Arguments) != 3 {
		t.Fatalf("legacy code action command = %#v", actions[0])
	}
	if actions[0].Arguments[0] != document.URI || actions[0].Arguments[1] != document.Version {
		t.Fatalf("legacy code action arguments = %#v", actions[0].Arguments)
	}
	edits, ok := actions[0].Arguments[2].([]lsp.TextEdit)
	if !ok || lsp.ApplyEdits(document, edits) != "body { display: inline }" {
		t.Fatalf("legacy code action edits = %#v", actions[0].Arguments[2])
	}
}

func TestLanguageServiceCompletion(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, "body { dis }")
	position := document.PositionAt(10)
	list, err := service.DoComplete(context.Background(), document, position, service.ParseStylesheet(document), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range list.Items {
		if item.Label == "display" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("completion labels = %v", rootCompletionLabels(list.Items))
	}
}

func TestLanguageServiceCompatibilityFacadeMethods(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, `@import "foo.css"; body { displai: inline }`)
	stylesheet := service.ParseStylesheet(document)
	var propertyContexts []PropertyCompletionContext
	service.SetCompletionParticipants([]ICompletionParticipant{{
		OnCSSProperty: func(context PropertyCompletionContext) {
			propertyContexts = append(propertyContexts, context)
		},
	}})

	list, err := service.DoComplete2(context.Background(), document, document.PositionAt(strings.LastIndex(document.Text(), "displai")+3), stylesheet, testDocumentContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) == 0 {
		t.Fatal("DoComplete2 returned no items")
	}
	if len(propertyContexts) != 1 || propertyContexts[0].PropertyName != "dis" {
		t.Fatalf("property contexts = %#v", propertyContexts)
	}

	links, err := service.FindDocumentLinks2(context.Background(), document, stylesheet, testDocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Target != "test://test/foo.css" {
		t.Fatalf("FindDocumentLinks2 = %#v", links)
	}

	diagnostics := service.DoValidation(document, stylesheet, nil)
	actions := service.DoCodeActions2(document, lsp.Range{}, lsp.CodeActionContext{Diagnostics: diagnostics}, stylesheet)
	if len(actions) == 0 || actions[0].Title != "Rename to 'display'" {
		t.Fatalf("DoCodeActions2 = %#v", actions)
	}
}

func TestPublicCompatibilityTypes(t *testing.T) {
	capabilities := LatestClientCapabilities
	if capabilities.TextDocument == nil ||
		capabilities.TextDocument.Completion == nil ||
		capabilities.TextDocument.Completion.CompletionItem == nil ||
		len(capabilities.TextDocument.Completion.CompletionItem.DocumentationFormat) != 2 {
		t.Fatalf("LatestClientCapabilities = %#v", capabilities)
	}

	var aliases AliasSettings = map[string]string{"~": "/src"}
	if aliases["~"] != "/src" {
		t.Fatalf("aliases = %#v", aliases)
	}
	var lint LintSettings = map[string]any{"emptyRules": "warning"}
	if lint["emptyRules"] != "warning" {
		t.Fatalf("lint = %#v", lint)
	}

	provider := FileSystemProvider{
		Stat: func(ctx context.Context, uri lsp.DocumentURI) (FileStat, error) {
			return FileStat{Type: FileTypeFile, CTime: 1, MTime: 2, Size: 10}, nil
		},
	}
	stat, err := provider.Stat(context.Background(), "test://test/test.css")
	if err != nil || stat.Type != FileTypeFile || stat.Size != 10 {
		t.Fatalf("stat = %#v, err = %v", stat, err)
	}

	item := lsp.CompletionItem{Label: "display", Tags: []lsp.CompletionItemTag{lsp.CompletionItemTagDeprecated}}
	if len(item.Tags) != 1 || item.Tags[0] != lsp.CompletionItemTagDeprecated {
		t.Fatalf("completion item tags = %#v", item.Tags)
	}
	definition := lsp.DefinitionLink{TargetURI: "test://test/test.css"}
	if definition.TargetURI != "test://test/test.css" {
		t.Fatalf("definition link = %#v", definition)
	}
	marked := lsp.MarkedString{Language: "css", Value: ".foo"}
	if marked.Language != "css" || marked.Value != ".foo" {
		t.Fatalf("marked string = %#v", marked)
	}
}

func TestSCSSLanguageServiceCompletionVariables(t *testing.T) {
	service := GetSCSSLanguageService()
	input := "$i: 0; body { width: "
	document := lsp.NewTextDocument("test://test/test.scss", "scss", 0, input)
	list, err := service.DoComplete(context.Background(), document, document.PositionAt(len(input)), service.ParseStylesheet(document), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.Label == "$i" {
			if item.Kind != lsp.CompletionItemKindVariable {
				t.Fatalf("$i kind = %#v", item.Kind)
			}
			if item.Documentation != "0" {
				t.Fatalf("$i documentation = %#v", item.Documentation)
			}
			return
		}
	}
	t.Fatalf("completion labels = %v", rootCompletionLabels(list.Items))
}

func TestLESSLanguageServiceCompletionVariables(t *testing.T) {
	service := GetLESSLanguageService()
	input := "@var1: 3; body { vertical-align: "
	document := lsp.NewTextDocument("test://test/test.less", "less", 0, input)
	list, err := service.DoComplete(context.Background(), document, document.PositionAt(len(input)), service.ParseStylesheet(document), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.Label == "@var1" {
			if item.Kind != lsp.CompletionItemKindVariable {
				t.Fatalf("@var1 kind = %#v", item.Kind)
			}
			if item.Documentation != "3" {
				t.Fatalf("@var1 documentation = %#v", item.Documentation)
			}
			return
		}
	}
	t.Fatalf("completion labels = %v", rootCompletionLabels(list.Items))
}

func TestLESSLanguageServiceParseStylesheetUsesLESSParser(t *testing.T) {
	service := GetLESSLanguageService()
	document := lsp.NewTextDocument("test://test/test.less", "less", 0, "selector { prop; }")
	stylesheet := service.ParseStylesheet(document)
	if stylesheet == nil || stylesheet.Root == nil {
		t.Fatal("stylesheet root is nil")
	}
	foundProperty := false
	stylesheet.Root.Accept(func(node *parser.Node) bool {
		if node.Type() == parser.NodeTypeProperty && node.GetText() == "prop" {
			foundProperty = true
		}
		return true
	})
	if !foundProperty {
		t.Fatalf("LESS property statement was not parsed: %#v", stylesheet.Root.GetChildren())
	}
}

func TestLanguageServicePathCompletion(t *testing.T) {
	service := GetCSSLanguageService(LanguageServiceOptions{
		ReadDirectory: fakeRootReadDirectory(map[string][]FileEntry{
			"test://test/pathCompletionFixtures/about/": {
				{Name: "about.css", Type: FileTypeFile},
				{Name: "about.html", Type: FileTypeFile},
			},
		}),
		ResolveReference: fakeRootResolveReference,
	})
	markedInput := `html { background-image: url("./|")`
	offset := strings.Index(markedInput, "|")
	input := markedInput[:offset] + markedInput[offset+1:]
	document := lsp.NewTextDocument("test://test/pathCompletionFixtures/about/about.css", "css", 0, input)
	list, err := service.DoComplete(context.Background(), document, document.PositionAt(offset), service.ParseStylesheet(document), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.Label == "about.html" {
			if item.Kind != lsp.CompletionItemKindFile {
				t.Fatalf("about.html kind = %#v", item.Kind)
			}
			if item.TextEdit == nil {
				t.Fatal("about.html has no text edit")
			}
			if got := lsp.ApplyEdits(document, []lsp.TextEdit{*item.TextEdit}); got != `html { background-image: url("./about.html")` {
				t.Fatalf("path completion result = %q", got)
			}
			return
		}
	}
	t.Fatalf("completion labels = %v", rootCompletionLabels(list.Items))
}

func TestLanguageServicePathCompletionUsesFileSystemProvider(t *testing.T) {
	service := GetCSSLanguageService(LanguageServiceOptions{
		FileSystemProvider: &FileSystemProvider{
			ReadDirectory: func(ctx context.Context, uri lsp.DocumentURI) ([]FileEntry, error) {
				if uri != "test://test/pathCompletionFixtures/about/" {
					t.Fatalf("uri = %q", uri)
				}
				return []FileEntry{{Name: "provider.css", Type: FileTypeFile}}, nil
			},
		},
		ResolveReference: fakeRootResolveReference,
	})
	markedInput := `html { background-image: url("./|")`
	offset := strings.Index(markedInput, "|")
	input := markedInput[:offset] + markedInput[offset+1:]
	document := lsp.NewTextDocument("test://test/pathCompletionFixtures/about/about.css", "css", 0, input)
	list, err := service.DoComplete(context.Background(), document, document.PositionAt(offset), service.ParseStylesheet(document), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.Label == "provider.css" {
			return
		}
	}
	t.Fatalf("completion labels = %v", rootCompletionLabels(list.Items))
}

func TestLanguageServiceHover(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, ".test { color: blue; }")
	hover := service.DoHover(document, document.PositionAt(8), service.ParseStylesheet(document), nil)
	if hover == nil {
		t.Fatal("hover is nil")
	}
	content, ok := hover.Contents.(lsp.MarkupContent)
	if !ok {
		t.Fatalf("hover contents = %#v", hover.Contents)
	}
	if !strings.Contains(content.Value, "Sets the color of an element's text") || !strings.Contains(content.Value, "MDN Reference") {
		t.Fatalf("hover content = %q", content.Value)
	}

	disabled := false
	hover = service.DoHover(document, document.PositionAt(8), service.ParseStylesheet(document), &HoverSettings{References: &disabled})
	content, ok = hover.Contents.(lsp.MarkupContent)
	if !ok {
		t.Fatalf("hover contents = %#v", hover.Contents)
	}
	if strings.Contains(content.Value, "MDN Reference") {
		t.Fatalf("references disabled hover = %q", content.Value)
	}
}

func TestLanguageServiceDefinition(t *testing.T) {
	service := GetCSSLanguageService()
	input := ":root{ --var1: abc;} .a{ color: var(--var1); }"
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, input)
	location := service.FindDefinition(document, document.PositionAt(strings.LastIndex(input, "--var1")), service.ParseStylesheet(document))
	if location == nil {
		t.Fatal("definition is nil")
	}
	if location.URI != document.URI || document.GetText(&location.Range) != "--var1" || location.Range.Start.Character != 7 {
		t.Fatalf("definition = %#v", location)
	}
}

func TestLanguageServiceOptionsCustomDataProviders(t *testing.T) {
	service := GetCSSLanguageService(LanguageServiceOptions{
		CustomDataProviders: []CSSDataProvider{NewCSSDataProvider(CSSDataV1{
			Version:    1,
			Properties: []PropertyData{{Name: "foo"}},
		})},
	})
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, ".foo { foo: 1; }")
	if diagnostics := service.DoValidation(document, service.ParseStylesheet(document), nil); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

type testDocumentContext struct{}

func (testDocumentContext) ResolveReference(ref, baseURL string) (string, bool) {
	ref = strings.TrimPrefix(ref, "./")
	if strings.HasPrefix(ref, "/") {
		return "test://test" + ref, true
	}
	return "test://test/" + ref, true
}

func rootCompletionLabels(items []lsp.CompletionItem) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func fakeRootReadDirectory(entries map[string][]FileEntry) ReadDirectoryFunc {
	return func(ctx context.Context, uri string) ([]FileEntry, error) {
		return entries[uri], nil
	}
}

func fakeRootResolveReference(ref, baseURL string) (string, bool) {
	if ref == "." || ref == "" {
		return "test://test/pathCompletionFixtures/about/", true
	}
	if ref == "./" {
		return "test://test/pathCompletionFixtures/about/", true
	}
	return "", false
}
