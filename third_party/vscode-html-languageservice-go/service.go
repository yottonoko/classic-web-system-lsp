package htmlservice

import "sync"

// LanguageService exposes the HTML language service operations.
type LanguageService interface {
	SetDataProviders(useDefaultDataProvider bool, customDataProviders []HTMLDataProvider)
	CreateScanner(input string, initialOffset ...int) Scanner
	ParseHTMLDocument(document *TextDocument) *HTMLDocument
	FindDocumentHighlights(document *TextDocument, position Position, htmlDocument *HTMLDocument) []DocumentHighlight
	DoComplete(document *TextDocument, position Position, htmlDocument *HTMLDocument, options *CompletionConfiguration) CompletionList
	DoComplete2(document *TextDocument, position Position, htmlDocument *HTMLDocument, documentContext DocumentContext, options *CompletionConfiguration) CompletionList
	SetCompletionParticipants(registeredCompletionParticipants []CompletionParticipant)
	DoHover(document *TextDocument, position Position, htmlDocument *HTMLDocument, options *HoverSettings) *Hover
	Format(document *TextDocument, r *Range, options HTMLFormatConfiguration) []TextEdit
	FindDocumentLinks(document *TextDocument, documentContext DocumentContext) []DocumentLink
	FindDocumentSymbols(document *TextDocument, htmlDocument *HTMLDocument) []SymbolInformation
	FindDocumentSymbols2(document *TextDocument, htmlDocument *HTMLDocument) []DocumentSymbol
	DoQuoteComplete(document *TextDocument, position Position, htmlDocument *HTMLDocument, options *CompletionConfiguration) *string
	DoTagComplete(document *TextDocument, position Position, htmlDocument *HTMLDocument) *string
	GetFoldingRanges(document *TextDocument, rangeLimit ...int) []FoldingRange
	GetSelectionRanges(document *TextDocument, positions []Position) []SelectionRange
	DoRename(document *TextDocument, position Position, newName string, htmlDocument *HTMLDocument) *WorkspaceEdit
	FindMatchingTagPosition(document *TextDocument, position Position, htmlDocument *HTMLDocument) *Position
	FindOnTypeRenameRanges(document *TextDocument, position Position, htmlDocument *HTMLDocument) []Range
	FindLinkedEditingRanges(document *TextDocument, position Position, htmlDocument *HTMLDocument) []Range
}

type languageService struct {
	mu           sync.RWMutex
	options      LanguageServiceOptions
	dataManager  *HTMLDataManager
	parser       *HTMLParser
	participants []CompletionParticipant
}

func GetLanguageService(options ...LanguageServiceOptions) LanguageService {
	var opts LanguageServiceOptions
	if len(options) > 0 {
		opts = options[0]
	}
	opts.ClientCapabilities = cloneClientCapabilities(opts.ClientCapabilities)
	dataManager := NewHTMLDataManager(opts)
	return &languageService{
		options:     opts,
		dataManager: dataManager,
		parser:      NewHTMLParser(dataManager),
	}
}

func cloneClientCapabilities(caps *ClientCapabilities) *ClientCapabilities {
	if caps == nil {
		return nil
	}
	clone := &ClientCapabilities{}
	if caps.TextDocument != nil {
		clone.TextDocument = &TextDocumentClientCapabilities{}
		if caps.TextDocument.Completion != nil {
			clone.TextDocument.Completion = &CompletionClientCapabilities{}
			if caps.TextDocument.Completion.CompletionItem != nil {
				formats := append([]MarkupKind(nil), caps.TextDocument.Completion.CompletionItem.DocumentationFormat...)
				clone.TextDocument.Completion.CompletionItem = &CompletionItemClientCapabilities{DocumentationFormat: formats}
			}
		}
		if caps.TextDocument.Hover != nil {
			formats := append([]MarkupKind(nil), caps.TextDocument.Hover.ContentFormat...)
			clone.TextDocument.Hover = &HoverClientCapabilities{ContentFormat: formats}
		}
	}
	return clone
}

func (s *languageService) SetDataProviders(useDefaultDataProvider bool, customDataProviders []HTMLDataProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dataManager.SetDataProviders(useDefaultDataProvider, customDataProviders)
}

func (s *languageService) CreateScanner(input string, initialOffset ...int) Scanner {
	return CreateScanner(input, initialOffset...)
}

func (s *languageService) ParseHTMLDocument(document *TextDocument) *HTMLDocument {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.parser.ParseDocument(document)
}

func (s *languageService) SetCompletionParticipants(registeredCompletionParticipants []CompletionParticipant) {
	participants := append([]CompletionParticipant(nil), registeredCompletionParticipants...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.participants = participants
}

func (s *languageService) FindDocumentHighlights(document *TextDocument, position Position, htmlDocument *HTMLDocument) []DocumentHighlight {
	return FindDocumentHighlights(document, position, htmlDocument)
}

func (s *languageService) DoComplete(document *TextDocument, position Position, htmlDocument *HTMLDocument, options *CompletionConfiguration) CompletionList {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return doComplete(s.dataManager, nil, s.options.ClientCapabilities, s.participants, document, position, htmlDocument, nil, options)
}

func (s *languageService) DoComplete2(document *TextDocument, position Position, htmlDocument *HTMLDocument, documentContext DocumentContext, options *CompletionConfiguration) CompletionList {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return doComplete(s.dataManager, s.options.FileSystemProvider, s.options.ClientCapabilities, s.participants, document, position, htmlDocument, documentContext, options)
}

func (s *languageService) DoHover(document *TextDocument, position Position, htmlDocument *HTMLDocument, options *HoverSettings) *Hover {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return DoHover(s.dataManager, s.options.ClientCapabilities, document, position, htmlDocument, options)
}

func (s *languageService) Format(document *TextDocument, r *Range, options HTMLFormatConfiguration) []TextEdit {
	beautifier := s.options.HTMLBeautifier
	if beautifier == nil {
		beautifier = defaultHTMLBeautifier
	}
	return FormatWithBeautifier(document, r, options, beautifier)
}

func (s *languageService) FindDocumentLinks(document *TextDocument, documentContext DocumentContext) []DocumentLink {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return FindDocumentLinks(s.dataManager, document, documentContext)
}

func (s *languageService) FindDocumentSymbols(document *TextDocument, htmlDocument *HTMLDocument) []SymbolInformation {
	return FindDocumentSymbols(document, htmlDocument)
}

func (s *languageService) FindDocumentSymbols2(document *TextDocument, htmlDocument *HTMLDocument) []DocumentSymbol {
	return FindDocumentSymbols2(document, htmlDocument)
}

func (s *languageService) DoQuoteComplete(document *TextDocument, position Position, htmlDocument *HTMLDocument, options *CompletionConfiguration) *string {
	return DoQuoteComplete(document, position, htmlDocument, options)
}

func (s *languageService) DoTagComplete(document *TextDocument, position Position, htmlDocument *HTMLDocument) *string {
	return DoTagComplete(document, position, htmlDocument)
}

func (s *languageService) GetFoldingRanges(document *TextDocument, rangeLimit ...int) []FoldingRange {
	limit := 0
	if len(rangeLimit) > 0 {
		limit = rangeLimit[0]
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return GetFoldingRanges(s.dataManager, document, limit)
}

func (s *languageService) GetSelectionRanges(document *TextDocument, positions []Position) []SelectionRange {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return GetSelectionRanges(s.parser, document, positions)
}

func (s *languageService) DoRename(document *TextDocument, position Position, newName string, htmlDocument *HTMLDocument) *WorkspaceEdit {
	return DoRename(document, position, newName, htmlDocument)
}

func (s *languageService) FindMatchingTagPosition(document *TextDocument, position Position, htmlDocument *HTMLDocument) *Position {
	return FindMatchingTagPosition(document, position, htmlDocument)
}

func (s *languageService) FindOnTypeRenameRanges(document *TextDocument, position Position, htmlDocument *HTMLDocument) []Range {
	return FindLinkedEditingRanges(document, position, htmlDocument)
}

func (s *languageService) FindLinkedEditingRanges(document *TextDocument, position Position, htmlDocument *HTMLDocument) []Range {
	return FindLinkedEditingRanges(document, position, htmlDocument)
}
