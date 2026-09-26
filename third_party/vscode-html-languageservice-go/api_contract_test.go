package htmlservice

import (
	"encoding/json"
	"testing"
)

func TestPublicAPIContractSupportsOptionalParticipantsAndMinimalFS(t *testing.T) {
	var _ FileSystemProvider = minimalFSProvider{}
	var _ FileSystemStatProvider = statFSProvider{}
	var _ IHTMLDataProvider = GetDefaultHTMLDataProvider()
	var _ ICompletionParticipant = attributeOnlyParticipant{}

	ls := GetLanguageService(LanguageServiceOptions{FileSystemProvider: minimalFSProvider{}})
	ls.SetCompletionParticipants([]CompletionParticipant{
		attributeOnlyParticipant{},
		contentOnlyParticipant{},
	})

	doc := NewTextDocument("file:///workspace/index.html", "html", 0, `<script src="./"></script>`)
	htmlDoc := ls.ParseHTMLDocument(doc)
	_ = ls.DoComplete2(doc, NewPosition(0, len(`<script src="./`)), htmlDoc, fixtureDocumentContext{root: "file:///workspace"}, nil)
}

func TestPublicAPIContractSupportsProviderFiltering(t *testing.T) {
	ls := GetLanguageService()
	doc := NewTextDocument("file:///workspace/index.html", "html", 0, `<|`)
	htmlDoc := ls.ParseHTMLDocument(doc)

	html5 := false
	list := ls.DoComplete(doc, NewPosition(0, 1), htmlDoc, &CompletionConfiguration{Provider: map[string]bool{"html5": html5}})
	assertNoCompletion(t, list, "div")
}

func TestReadmeLibraryExampleCompiles(t *testing.T) {
	ls := GetLanguageService(LanguageServiceOptions{ClientCapabilities: &LatestClientCapabilities})
	doc := NewTextDocument("file:///index.html", "html", 0, "<div></div>")
	htmlDoc := ls.ParseHTMLDocument(doc)
	if symbols := ls.FindDocumentSymbols2(doc, htmlDoc); len(symbols) != 1 || symbols[0].Name != "div" {
		t.Fatalf("unexpected symbols from README-style example: %#v", symbols)
	}
}

func TestPublicLSPTypesJSONShape(t *testing.T) {
	edit := NewTextEdit(NewRange(NewPosition(1, 2), NewPosition(3, 4)), "x")
	if edit.Range.Start.Line != 1 || edit.Range.Start.Character != 2 || edit.NewText != "x" {
		t.Fatalf("unexpected TextEdit shape: %#v", edit)
	}
	deprecated := true
	item := CompletionItem{
		Label:            "x",
		LabelDetails:     &CompletionItemLabelDetails{Detail: "()", Description: "detail"},
		Kind:             CompletionItemKindProperty,
		Documentation:    MarkupContent{Kind: MarkupKindMarkdown, Value: "doc"},
		Detail:           "detail",
		Deprecated:       &deprecated,
		Preselect:        true,
		TextEdit:         &edit,
		InsertTextFormat: InsertTextFormatSnippet,
		InsertTextMode:   InsertTextModeAdjustIndentation,
		TextEditText:     "x",
		AdditionalTextEdits: []TextEdit{
			NewTextEdit(edit.Range, "extra"),
		},
		CommitCharacters: []string{"."},
		Tags:             []CompletionItemTag{CompletionItemTagDeprecated},
		Command:          &Command{Title: "Suggest", Command: "editor.action.triggerSuggest"},
		Data:             map[string]string{"id": "x"},
	}
	if item.TextEdit == nil || item.Command == nil || item.Documentation == nil {
		t.Fatalf("unexpected completion item shape: %#v", item)
	}
	active := 0
	payload := map[string]any{
		"insertReplace": InsertReplaceEdit{NewText: "x", Insert: edit.Range, Replace: edit.Range},
		"workspaceEdit": WorkspaceEdit{
			Changes: map[DocumentUri][]TextEdit{"file:///index.html": {edit}},
			DocumentChanges: []any{
				TextDocumentEdit{
					TextDocument: OptionalVersionedTextDocumentIdentifier{URI: "file:///index.html", Version: &active},
					Edits:        []any{edit, AnnotatedTextEdit{Range: edit.Range, NewText: "y", AnnotationID: "change-1"}},
				},
				CreateFile{Kind: "create", URI: "file:///new.html", AnnotationID: "change-1"},
				RenameFile{Kind: "rename", OldURI: "file:///old.html", NewURI: "file:///new.html"},
				DeleteFile{Kind: "delete", URI: "file:///old.html"},
			},
			ChangeAnnotations: map[ChangeAnnotationIdentifier]ChangeAnnotation{
				"change-1": {Label: "change", NeedsConfirmation: &deprecated, Description: "desc"},
			},
		},
		"marked":     MarkedString{Language: "html", Value: "<div>"},
		"definition": Definition{{URI: "file:///index.html", Range: edit.Range}},
		"baseline":   BaselineStatus{Baseline: "low"},
		"customData": ITagData{
			Name:       "x-card",
			References: []IReference{{Name: "ref", URL: "https://example.test"}},
			Attributes: []IAttributeData{{
				Name:   "tone",
				Values: []IValueData{{Name: "soft"}},
			}},
		},
		"valueSet": IValueSet{Name: "tone", Values: []ValueData{{Name: "soft"}}},
		"diagnostic": Diagnostic{
			Range:    edit.Range,
			Severity: DiagnosticSeverityWarning,
			Message:  "message",
			Tags:     []DiagnosticTag{DiagnosticTagDeprecated},
		},
		"formatting": FormattingOptions{TabSize: 2, InsertSpaces: true},
		"signature": SignatureHelp{
			Signatures:      []SignatureInformation{{Label: "fn", Parameters: []ParameterInformation{{Label: "arg"}}}},
			ActiveSignature: &active,
			ActiveParameter: &active,
		},
		"color": ColorPresentation{
			Label:    "#fff",
			TextEdit: &edit,
			AdditionalTextEdits: []TextEdit{
				NewTextEdit(edit.Range, "color"),
			},
		},
		"colorInfo":    ColorInformation{Range: edit.Range, Color: Color{Red: 1, Green: 1, Blue: 1, Alpha: 1}},
		"documentLink": DocumentLink{Range: edit.Range, Target: "https://example.test", Tooltip: "open", Data: "link"},
		"folding":      FoldingRange{StartLine: 0, EndLine: 1, Kind: FoldingRangeKindRegion, CollapsedText: "..."},
		"symbols": []any{
			SymbolInformation{Name: "x", Kind: SymbolKindField, Location: Location{URI: "file:///index.html", Range: edit.Range}, Tags: []SymbolTag{SymbolTagDeprecated}, Deprecated: &deprecated},
			DocumentSymbol{Name: "x", Kind: SymbolKindField, Range: edit.Range, SelectionRange: edit.Range, Tags: []SymbolTag{SymbolTagDeprecated}, Deprecated: &deprecated},
		},
	}
	if _, err := json.Marshal(payload); err != nil {
		t.Fatalf("public LSP shape marshal failed: %v", err)
	}
}

type attributeOnlyParticipant struct{}

func (attributeOnlyParticipant) OnHTMLAttributeValue(context HtmlAttributeValueContext) {}

type contentOnlyParticipant struct{}

func (contentOnlyParticipant) OnHTMLContent(context HtmlContentContext) {}

type minimalFSProvider struct{}

func (minimalFSProvider) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeDirectory}, nil
}

func (minimalFSProvider) ReadDirectory(uri DocumentUri) ([][2]any, error) {
	return [][2]any{{"index.html", FileTypeFile}}, nil
}

type statFSProvider struct{}

func (statFSProvider) Stat(uri DocumentUri) (FileStat, error) {
	return FileStat{Type: FileTypeFile}, nil
}
