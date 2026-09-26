package cssls

import (
	"context"
	"strings"
	"sync"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
	"github.com/yottonoko/vscode-css-languageservice-go/parser"
	"github.com/yottonoko/vscode-css-languageservice-go/services"
)

// Stylesheet wraps the parsed root node used by language-service methods.
type Stylesheet struct {
	Root *parser.Node
}

// LanguageService exposes CSS, LESS, and SCSS language features.
type LanguageService interface {
	Configure(settings LanguageSettings)
	SetDataProviders(useDefaultDataProvider bool, customDataProviders []CSSDataProvider)
	DoValidation(document *lsp.TextDocument, stylesheet *Stylesheet, settings *LanguageSettings) []lsp.Diagnostic
	ParseStylesheet(document *lsp.TextDocument) *Stylesheet
	DoComplete(ctx context.Context, document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet, settings *CompletionSettings) (lsp.CompletionList, error)
	DoComplete2(ctx context.Context, document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet, documentContext DocumentContext, settings *CompletionSettings) (lsp.CompletionList, error)
	SetCompletionParticipants(registeredCompletionParticipants []ICompletionParticipant)
	DoHover(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet, settings *HoverSettings) *lsp.Hover
	FindDefinition(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet) *lsp.Location
	FindReferences(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet) []lsp.Location
	FindDocumentHighlights(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet) []lsp.DocumentHighlight
	FindDocumentLinks(ctx context.Context, document *lsp.TextDocument, stylesheet *Stylesheet, documentContext DocumentContext) ([]lsp.DocumentLink, error)
	FindDocumentLinks2(ctx context.Context, document *lsp.TextDocument, stylesheet *Stylesheet, documentContext DocumentContext) ([]lsp.DocumentLink, error)
	FindDocumentSymbols(document *lsp.TextDocument, stylesheet *Stylesheet) []lsp.SymbolInformation
	FindDocumentSymbols2(document *lsp.TextDocument, stylesheet *Stylesheet) []lsp.DocumentSymbol
	DoCodeActions(document *lsp.TextDocument, r lsp.Range, context lsp.CodeActionContext, stylesheet *Stylesheet) []lsp.Command
	DoCodeActions2(document *lsp.TextDocument, r lsp.Range, context lsp.CodeActionContext, stylesheet *Stylesheet) []lsp.CodeAction
	FindDocumentColors(document *lsp.TextDocument, stylesheet *Stylesheet) []lsp.ColorInformation
	GetColorPresentations(document *lsp.TextDocument, stylesheet *Stylesheet, color lsp.Color, r lsp.Range) []lsp.ColorPresentation
	PrepareRename(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet) *lsp.Range
	DoRename(document *lsp.TextDocument, position lsp.Position, newName string, stylesheet *Stylesheet) lsp.WorkspaceEdit
	GetFoldingRanges(document *lsp.TextDocument, context *FoldingContext) []lsp.FoldingRange
	GetSelectionRanges(document *lsp.TextDocument, positions []lsp.Position, stylesheet *Stylesheet) []lsp.SelectionRange
	Format(document *lsp.TextDocument, r *lsp.Range, options CSSFormatConfiguration) []lsp.TextEdit
}

// LanguageSettings configures validation, lint, completion, hover, and imports.
type LanguageSettings struct {
	Validate      *bool
	Lint          LintSettings
	Completion    *CompletionSettings
	Hover         *HoverSettings
	ImportAliases AliasSettings
}

// LintSettings maps lint rule IDs to severity settings.
type LintSettings map[string]any

// AliasSettings maps import aliases to target paths.
type AliasSettings map[string]string

// CompletionSettings configures language-service completion behavior.
type CompletionSettings struct {
	TriggerPropertyValueCompletion bool
	CompletePropertyWithSemicolon  bool
}

// HoverSettings configures hover content sections.
type HoverSettings struct {
	Documentation *bool
	References    *bool
}

// FoldingContext configures folding range requests.
type FoldingContext struct {
	RangeLimit int
}

// CSSFormatConfiguration configures CSS formatter output.
type CSSFormatConfiguration struct {
	TabSize                      int
	InsertSpaces                 bool
	InsertFinalNewline           bool
	NewlineBetweenSelectors      bool
	NewlineBetweenRules          bool
	SpaceAroundSelectorSeparator bool
	BraceStyle                   string
	PreserveNewLines             bool
	MaxPreserveNewLines          int
	WrapLineLength               int
	IndentEmptyLines             bool
	SelectorSeparatorNewline     bool
}

// DocumentContext resolves stylesheet references relative to a base URI.
type DocumentContext interface {
	ResolveReference(ref, baseURL string) (string, bool)
}

// PropertyCompletionContext describes a property-name completion participant event.
type PropertyCompletionContext = services.PropertyCompletionContext

// PropertyValueCompletionContext describes a property-value completion participant event.
type PropertyValueCompletionContext = services.PropertyValueCompletionContext

// URILiteralCompletionContext describes a URL literal completion participant event.
type URILiteralCompletionContext = services.URILiteralCompletionContext

// ImportPathCompletionContext describes an import path completion participant event.
type ImportPathCompletionContext = services.ImportPathCompletionContext

// MixinReferenceCompletionContext describes a SCSS or LESS mixin completion participant event.
type MixinReferenceCompletionContext = services.MixinReferenceCompletionContext

// ICompletionParticipant receives completion participant callbacks.
type ICompletionParticipant struct {
	OnCSSProperty        func(context PropertyCompletionContext)
	OnCSSPropertyValue   func(context PropertyValueCompletionContext)
	OnCSSURILiteralValue func(context URILiteralCompletionContext)
	OnCSSImportPath      func(context ImportPathCompletionContext)
	OnCSSMixinReference  func(context MixinReferenceCompletionContext)
}

// ClientCapabilities describes supported client language-service features.
type ClientCapabilities struct {
	TextDocument *TextDocumentClientCapabilities
}

// TextDocumentClientCapabilities describes text-document client support.
type TextDocumentClientCapabilities struct {
	Completion *CompletionClientCapabilities
	Hover      *HoverClientCapabilities
}

// CompletionClientCapabilities describes completion client support.
type CompletionClientCapabilities struct {
	CompletionItem *CompletionItemClientCapabilities
}

// CompletionItemClientCapabilities describes completion item client support.
type CompletionItemClientCapabilities struct {
	DocumentationFormat []lsp.MarkupKind
}

// HoverClientCapabilities describes hover client support.
type HoverClientCapabilities struct {
	ContentFormat []lsp.MarkupKind
}

var LatestClientCapabilities = ClientCapabilities{
	TextDocument: &TextDocumentClientCapabilities{
		Completion: &CompletionClientCapabilities{
			CompletionItem: &CompletionItemClientCapabilities{
				DocumentationFormat: []lsp.MarkupKind{lsp.MarkupKindMarkdown, lsp.MarkupKindPlainText},
			},
		},
		Hover: &HoverClientCapabilities{
			ContentFormat: []lsp.MarkupKind{lsp.MarkupKindMarkdown, lsp.MarkupKindPlainText},
		},
	},
}

// FileStat describes a filesystem entry for path completion.
type FileStat struct {
	Type  FileType
	CTime int64
	MTime int64
	Size  int64
}

// FileSystemProvider supplies filesystem data for path completion and links.
type FileSystemProvider struct {
	Stat          func(ctx context.Context, uri lsp.DocumentURI) (FileStat, error)
	ReadDirectory func(ctx context.Context, uri lsp.DocumentURI) ([]FileEntry, error)
	GetContent    func(ctx context.Context, uri lsp.DocumentURI, encoding string) (string, error)
}

// FileType describes the kind of a filesystem entry.
type FileType = services.FileType

// FileEntry describes a directory entry for path completion.
type FileEntry = services.FileEntry

// ReadDirectoryFunc reads directory entries for path completion.
type ReadDirectoryFunc = services.ReadDirectoryFunc

// ResolveReferenceFunc resolves a reference against a base URL.
type ResolveReferenceFunc = services.ResolveReferenceFunc

const (
	FileTypeUnknown      = services.FileTypeUnknown
	FileTypeFile         = services.FileTypeFile
	FileTypeDirectory    = services.FileTypeDirectory
	FileTypeSymbolicLink = services.FileTypeSymbolicLink
)

// CSSDataProvider supplies CSS metadata to completion, hover, and validation.
type CSSDataProvider = languagefacts.CSSDataProvider

// PropertyData describes a CSS property from custom or built-in data.
type PropertyData = languagefacts.PropertyData

// AtDirectiveData describes a CSS at-rule entry.
type AtDirectiveData = languagefacts.AtDirectiveData

// PseudoClassData describes a CSS pseudo-class entry.
type PseudoClassData = languagefacts.PseudoClassData

// PseudoElementData describes a CSS pseudo-element entry.
type PseudoElementData = languagefacts.PseudoElementData

// CSSDataV1 is the custom data schema accepted by the language service.
type CSSDataV1 = languagefacts.CSSDataV1

func GetCSSLanguageService(options ...LanguageServiceOptions) LanguageService {
	return newStubLanguageService(languageSyntaxCSS, options...)
}

func GetSCSSLanguageService(options ...LanguageServiceOptions) LanguageService {
	return newStubLanguageService(languageSyntaxSCSS, options...)
}

func GetLESSLanguageService(options ...LanguageServiceOptions) LanguageService {
	return newStubLanguageService(languageSyntaxLESS, options...)
}

func NewCSSDataProvider(data CSSDataV1) CSSDataProvider {
	return languagefacts.NewCSSDataProvider(data)
}

func GetDefaultCSSDataProvider() CSSDataProvider {
	return languagefacts.GetDefaultCSSDataProvider()
}

// LanguageServiceOptions configures a new language service instance.
type LanguageServiceOptions struct {
	UseDefaultDataProvider *bool
	CustomDataProviders    []CSSDataProvider
	ReadDirectory          ReadDirectoryFunc
	ResolveReference       ResolveReferenceFunc
	FileSystemProvider     *FileSystemProvider
	ClientCapabilities     *ClientCapabilities
}

type languageSyntax int

const (
	languageSyntaxCSS languageSyntax = iota
	languageSyntaxSCSS
	languageSyntaxLESS
)

type stubLanguageService struct {
	mu           sync.RWMutex
	dataManager  *languagefacts.DataManager
	settings     LanguageSettings
	options      LanguageServiceOptions
	syntax       languageSyntax
	participants []ICompletionParticipant
}

type languageServiceSnapshot struct {
	dataManager  *languagefacts.DataManager
	completion   *CompletionSettings
	options      LanguageServiceOptions
	participants []ICompletionParticipant
}

type validationServiceSnapshot struct {
	dataManager *languagefacts.DataManager
	validate    *bool
	lint        map[string]any
}

type hoverServiceSnapshot struct {
	dataManager *languagefacts.DataManager
	hover       *HoverSettings
}

func newStubLanguageService(syntax languageSyntax, options ...LanguageServiceOptions) LanguageService {
	var activeOptions LanguageServiceOptions
	if len(options) > 0 {
		activeOptions = copyLanguageServiceOptions(options[0])
	}
	return &stubLanguageService{
		dataManager: languagefacts.NewDataManager(languagefacts.DataManagerOptions{
			UseDefaultDataProvider: activeOptions.UseDefaultDataProvider,
			CustomDataProviders:    activeOptions.CustomDataProviders,
		}),
		options: activeOptions,
		syntax:  syntax,
	}
}

func copyLanguageSettings(settings LanguageSettings) LanguageSettings {
	copied := LanguageSettings{
		Validate:   copyBoolPointer(settings.Validate),
		Lint:       copyAnyMap(settings.Lint),
		Completion: copyCompletionSettingsPointer(settings.Completion),
		Hover:      copyHoverSettingsPointer(settings.Hover),
	}
	copied.ImportAliases = copyAliasSettings(settings.ImportAliases)
	return copied
}

func copyAliasSettings(settings AliasSettings) AliasSettings {
	if len(settings) == 0 {
		return nil
	}
	copied := make(AliasSettings, len(settings))
	for alias, target := range settings {
		copied[alias] = target
	}
	return copied
}

func copyCompletionSettingsPointer(settings *CompletionSettings) *CompletionSettings {
	if settings == nil {
		return nil
	}
	copied := *settings
	return &copied
}

func copyHoverSettingsPointer(settings *HoverSettings) *HoverSettings {
	if settings == nil {
		return nil
	}
	copied := HoverSettings{
		Documentation: copyBoolPointer(settings.Documentation),
		References:    copyBoolPointer(settings.References),
	}
	return &copied
}

func copyBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyLanguageServiceOptions(options LanguageServiceOptions) LanguageServiceOptions {
	copied := options
	copied.CustomDataProviders = append([]CSSDataProvider(nil), options.CustomDataProviders...)
	copied.ClientCapabilities = copyClientCapabilities(options.ClientCapabilities)
	if options.FileSystemProvider != nil {
		provider := *options.FileSystemProvider
		copied.FileSystemProvider = &provider
	}
	return copied
}

func copyClientCapabilities(capabilities *ClientCapabilities) *ClientCapabilities {
	if capabilities == nil {
		return nil
	}
	copied := &ClientCapabilities{}
	if capabilities.TextDocument != nil {
		copied.TextDocument = &TextDocumentClientCapabilities{}
		if capabilities.TextDocument.Completion != nil {
			copied.TextDocument.Completion = &CompletionClientCapabilities{}
			if capabilities.TextDocument.Completion.CompletionItem != nil {
				copied.TextDocument.Completion.CompletionItem = &CompletionItemClientCapabilities{
					DocumentationFormat: append([]lsp.MarkupKind(nil), capabilities.TextDocument.Completion.CompletionItem.DocumentationFormat...),
				}
			}
		}
		if capabilities.TextDocument.Hover != nil {
			copied.TextDocument.Hover = &HoverClientCapabilities{
				ContentFormat: append([]lsp.MarkupKind(nil), capabilities.TextDocument.Hover.ContentFormat...),
			}
		}
	}
	return copied
}

func (s *stubLanguageService) Configure(settings LanguageSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = copyLanguageSettings(settings)
}

func (s *stubLanguageService) SetDataProviders(useDefaultDataProvider bool, customDataProviders []CSSDataProvider) {
	manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{
		UseDefaultDataProvider: &useDefaultDataProvider,
		CustomDataProviders:    append([]CSSDataProvider(nil), customDataProviders...),
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dataManager = manager
}

func (s *stubLanguageService) DoValidation(document *lsp.TextDocument, stylesheet *Stylesheet, settings *LanguageSettings) []lsp.Diagnostic {
	snapshot := s.validationSnapshot()
	validate := snapshot.validate
	lintSettings := snapshot.lint
	if settings != nil {
		validate = settings.Validate
		lintSettings = settings.Lint
	}
	if validate != nil && !*validate {
		return []lsp.Diagnostic{}
	}
	return services.Validate(document, snapshot.dataManager, lintSettings)
}

func (s *stubLanguageService) ParseStylesheet(document *lsp.TextDocument) *Stylesheet {
	var p *parser.Parser
	switch s.syntax {
	case languageSyntaxSCSS:
		p = parser.NewSCSSParser()
	case languageSyntaxLESS:
		p = parser.NewLESSParser()
	default:
		p = parser.NewParser()
	}
	return &Stylesheet{Root: p.ParseStylesheet(document.Text())}
}

func (s *stubLanguageService) DoComplete(ctx context.Context, document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet, settings *CompletionSettings) (lsp.CompletionList, error) {
	snapshot := s.completionSnapshot()
	options := snapshot.completionOptions(settings, nil)
	return services.Complete(ctx, document, position, snapshot.dataManager, options)
}

func (s *stubLanguageService) DoComplete2(ctx context.Context, document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet, documentContext DocumentContext, settings *CompletionSettings) (lsp.CompletionList, error) {
	snapshot := s.completionSnapshot()
	options := snapshot.completionOptions(settings, documentContext)
	return services.Complete(ctx, document, position, snapshot.dataManager, options)
}

func (s *stubLanguageService) SetCompletionParticipants(registeredCompletionParticipants []ICompletionParticipant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.participants = append([]ICompletionParticipant(nil), registeredCompletionParticipants...)
}

func (s *stubLanguageService) dataManagerSnapshot() *languagefacts.DataManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dataManager
}

func (s *stubLanguageService) validationSnapshot() validationServiceSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return validationServiceSnapshot{
		dataManager: s.dataManager,
		validate:    copyBoolPointer(s.settings.Validate),
		lint:        copyAnyMap(s.settings.Lint),
	}
}

func (s *stubLanguageService) hoverSnapshot() hoverServiceSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return hoverServiceSnapshot{
		dataManager: s.dataManager,
		hover:       copyHoverSettingsPointer(s.settings.Hover),
	}
}

func (s *stubLanguageService) aliasSettingsSnapshot() LanguageSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return LanguageSettings{ImportAliases: copyAliasSettings(s.settings.ImportAliases)}
}

func (s *stubLanguageService) completionSnapshot() languageServiceSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return languageServiceSnapshot{
		dataManager:  s.dataManager,
		completion:   copyCompletionSettingsPointer(s.settings.Completion),
		options:      s.options,
		participants: append([]ICompletionParticipant(nil), s.participants...),
	}
}

func (snapshot languageServiceSnapshot) completionOptions(settings *CompletionSettings, documentContext DocumentContext) services.CompletionOptions {
	activeSettings := settings
	if activeSettings == nil && snapshot.completion != nil {
		activeSettings = snapshot.completion
	}
	var options services.CompletionOptions
	if activeSettings != nil {
		options.TriggerPropertyValueCompletion = activeSettings.TriggerPropertyValueCompletion
		options.CompletePropertyWithSemicolon = activeSettings.CompletePropertyWithSemicolon
	}
	options.ReadDirectory = snapshot.options.ReadDirectory
	options.ResolveReference = snapshot.options.ResolveReference
	if options.ReadDirectory == nil && snapshot.options.FileSystemProvider != nil && snapshot.options.FileSystemProvider.ReadDirectory != nil {
		provider := snapshot.options.FileSystemProvider
		options.ReadDirectory = func(ctx context.Context, uri string) ([]FileEntry, error) {
			return provider.ReadDirectory(ctx, lsp.DocumentURI(uri))
		}
	}
	if options.ResolveReference == nil && documentContext != nil {
		options.ResolveReference = documentContext.ResolveReference
	}
	if len(snapshot.participants) > 0 {
		options.Participants = make([]services.CompletionParticipant, 0, len(snapshot.participants))
		for _, participant := range snapshot.participants {
			p := participant
			options.Participants = append(options.Participants, services.CompletionParticipant{
				OnProperty:        p.OnCSSProperty,
				OnPropertyValue:   p.OnCSSPropertyValue,
				OnURILiteralValue: p.OnCSSURILiteralValue,
				OnImportPath:      p.OnCSSImportPath,
				OnMixinReference:  p.OnCSSMixinReference,
			})
		}
	}
	return options
}

func (s *stubLanguageService) DoHover(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet, settings *HoverSettings) *lsp.Hover {
	snapshot := s.hoverSnapshot()
	activeSettings := settings
	if activeSettings == nil && snapshot.hover != nil {
		activeSettings = snapshot.hover
	}
	var options services.HoverOptions
	if activeSettings != nil {
		options.Documentation = activeSettings.Documentation
		options.References = activeSettings.References
	}
	return services.Hover(document, position, snapshot.dataManager, options)
}

func (s *stubLanguageService) FindDefinition(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet) *lsp.Location {
	return services.Definition(document, position)
}

func (s *stubLanguageService) FindReferences(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet) []lsp.Location {
	highlights := services.FindDocumentHighlights(document, position)
	locations := make([]lsp.Location, len(highlights))
	for i, highlight := range highlights {
		locations[i] = lsp.Location{URI: document.URI, Range: highlight.Range}
	}
	return locations
}

func (s *stubLanguageService) FindDocumentHighlights(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet) []lsp.DocumentHighlight {
	return services.FindDocumentHighlights(document, position)
}

func (s *stubLanguageService) FindDocumentLinks(ctx context.Context, document *lsp.TextDocument, stylesheet *Stylesheet, documentContext DocumentContext) ([]lsp.DocumentLink, error) {
	settings := s.aliasSettingsSnapshot()
	return services.FindDocumentLinks(ctx, document, settings.linkResolver(documentContext))
}

func (s *stubLanguageService) FindDocumentLinks2(ctx context.Context, document *lsp.TextDocument, stylesheet *Stylesheet, documentContext DocumentContext) ([]lsp.DocumentLink, error) {
	return s.FindDocumentLinks(ctx, document, stylesheet, documentContext)
}

func (settings LanguageSettings) linkResolver(documentContext DocumentContext) services.DocumentResolver {
	if documentContext == nil || len(settings.ImportAliases) == 0 {
		return documentContext
	}
	return importAliasDocumentContext{aliases: settings.ImportAliases, fallback: documentContext}
}

type importAliasDocumentContext struct {
	aliases  AliasSettings
	fallback DocumentContext
}

func (r importAliasDocumentContext) ResolveReference(ref, baseURL string) (string, bool) {
	for alias, target := range r.aliases {
		if ref == alias || strings.HasPrefix(ref, alias) {
			return r.fallback.ResolveReference(target+strings.TrimPrefix(ref, alias), baseURL)
		}
	}
	return r.fallback.ResolveReference(ref, baseURL)
}

func (s *stubLanguageService) FindDocumentSymbols(document *lsp.TextDocument, stylesheet *Stylesheet) []lsp.SymbolInformation {
	return services.FindDocumentSymbols(document)
}

func (s *stubLanguageService) FindDocumentSymbols2(document *lsp.TextDocument, stylesheet *Stylesheet) []lsp.DocumentSymbol {
	return services.FindDocumentSymbols2(document)
}

func (s *stubLanguageService) DoCodeActions(document *lsp.TextDocument, r lsp.Range, context lsp.CodeActionContext, stylesheet *Stylesheet) []lsp.Command {
	codeActions := s.DoCodeActions2(document, r, context, stylesheet)
	commands := make([]lsp.Command, 0, len(codeActions))
	for _, action := range codeActions {
		var edits []lsp.TextEdit
		if action.Edit != nil && len(action.Edit.DocumentChanges) > 0 {
			edits = action.Edit.DocumentChanges[0].Edits
		}
		commands = append(commands, lsp.Command{
			Title:     action.Title,
			Command:   "_css.applyCodeAction",
			Arguments: []any{document.URI, document.Version, edits},
		})
	}
	return commands
}

func (s *stubLanguageService) DoCodeActions2(document *lsp.TextDocument, r lsp.Range, context lsp.CodeActionContext, stylesheet *Stylesheet) []lsp.CodeAction {
	return services.CodeActions(document, r, context, s.dataManagerSnapshot())
}

func (s *stubLanguageService) FindDocumentColors(document *lsp.TextDocument, stylesheet *Stylesheet) []lsp.ColorInformation {
	return services.FindDocumentColors(document)
}

func (s *stubLanguageService) GetColorPresentations(document *lsp.TextDocument, stylesheet *Stylesheet, color lsp.Color, r lsp.Range) []lsp.ColorPresentation {
	return services.GetColorPresentations(color, r)
}

func (s *stubLanguageService) PrepareRename(document *lsp.TextDocument, position lsp.Position, stylesheet *Stylesheet) *lsp.Range {
	return services.PrepareRename(document, position)
}

func (s *stubLanguageService) DoRename(document *lsp.TextDocument, position lsp.Position, newName string, stylesheet *Stylesheet) lsp.WorkspaceEdit {
	return services.Rename(document, position, newName)
}

func (s *stubLanguageService) GetFoldingRanges(document *lsp.TextDocument, context *FoldingContext) []lsp.FoldingRange {
	var rangeLimit int
	if context != nil {
		rangeLimit = context.RangeLimit
	}
	return services.GetFoldingRanges(document, rangeLimit)
}

func (s *stubLanguageService) GetSelectionRanges(document *lsp.TextDocument, positions []lsp.Position, stylesheet *Stylesheet) []lsp.SelectionRange {
	return services.GetSelectionRanges(document, positions)
}

func (s *stubLanguageService) Format(document *lsp.TextDocument, r *lsp.Range, options CSSFormatConfiguration) []lsp.TextEdit {
	return services.Format(document, r, services.FormatOptions{
		TabSize:                      options.TabSize,
		InsertSpaces:                 options.InsertSpaces,
		InsertFinalNewline:           options.InsertFinalNewline,
		NewlineBetweenSelectors:      options.NewlineBetweenSelectors,
		NewlineBetweenRules:          options.NewlineBetweenRules,
		SpaceAroundSelectorSeparator: options.SpaceAroundSelectorSeparator,
		BraceStyle:                   options.BraceStyle,
		PreserveNewLines:             options.PreserveNewLines,
		MaxPreserveNewLines:          options.MaxPreserveNewLines,
		WrapLineLength:               options.WrapLineLength,
		IndentEmptyLines:             options.IndentEmptyLines,
		SelectorSeparatorNewline:     options.SelectorSeparatorNewline,
	})
}
