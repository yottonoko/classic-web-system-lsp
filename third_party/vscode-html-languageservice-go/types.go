package htmlservice

import "encoding/json"

// Position is a zero-based LSP position.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func NewPosition(line, character int) Position {
	return Position{Line: line, Character: character}
}

// Range is a half-open LSP range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

func NewRange(start, end Position) Range {
	return Range{Start: start, End: end}
}

// MarkupKind identifies the markup format used by LSP content.
type MarkupKind string

const (
	MarkupKindPlainText MarkupKind = "plaintext"
	MarkupKindMarkdown  MarkupKind = "markdown"
)

// MarkupContent represents LSP markup content.
type MarkupContent struct {
	Kind  MarkupKind `json:"kind"`
	Value string     `json:"value"`
}

// TextEdit describes a text replacement over a document range.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

func NewTextEdit(r Range, newText string) TextEdit {
	return TextEdit{Range: r, NewText: newText}
}

// DocumentUri identifies a document resource URI.
type DocumentUri = string

// CompletionItemKind identifies the kind of a completion item.
type CompletionItemKind int

const (
	CompletionItemKindText CompletionItemKind = iota + 1
	CompletionItemKindMethod
	CompletionItemKindFunction
	CompletionItemKindConstructor
	CompletionItemKindField
	CompletionItemKindVariable
	CompletionItemKindClass
	CompletionItemKindInterface
	CompletionItemKindModule
	CompletionItemKindProperty
	CompletionItemKindUnit
	CompletionItemKindValue
	CompletionItemKindEnum
	CompletionItemKindKeyword
	CompletionItemKindSnippet
	CompletionItemKindColor
	CompletionItemKindFile
	CompletionItemKindReference
	CompletionItemKindFolder
	CompletionItemKindEnumMember
	CompletionItemKindConstant
	CompletionItemKindStruct
	CompletionItemKindEvent
	CompletionItemKindOperator
	CompletionItemKindTypeParameter
)

// InsertTextFormat identifies how completion insert text should be interpreted.
type InsertTextFormat int

const (
	InsertTextFormatPlainText InsertTextFormat = 1
	InsertTextFormatSnippet   InsertTextFormat = 2
)

// InsertTextMode identifies how indentation is handled for completion insertion.
type InsertTextMode int

const (
	InsertTextModeAsIs              InsertTextMode = 1
	InsertTextModeAdjustIndentation InsertTextMode = 2
)

// CompletionItemTag identifies extra metadata for a completion item.
type CompletionItemTag int

const (
	CompletionItemTagDeprecated CompletionItemTag = 1
)

// CompletionItemLabelDetails describes additional rendered label details for a completion item.
type CompletionItemLabelDetails struct {
	Detail      string `json:"detail,omitempty"`
	Description string `json:"description,omitempty"`
}

// Command represents an LSP command invocation.
type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

// CompletionItem represents an LSP completion proposal.
type CompletionItem struct {
	Label               string                      `json:"label"`
	LabelDetails        *CompletionItemLabelDetails `json:"labelDetails,omitempty"`
	Kind                CompletionItemKind          `json:"kind,omitempty"`
	Documentation       any                         `json:"documentation,omitempty"`
	Detail              string                      `json:"detail,omitempty"`
	Deprecated          *bool                       `json:"deprecated,omitempty"`
	Preselect           bool                        `json:"preselect,omitempty"`
	TextEdit            *TextEdit                   `json:"textEdit,omitempty"`
	InsertText          string                      `json:"insertText,omitempty"`
	InsertTextFormat    InsertTextFormat            `json:"insertTextFormat,omitempty"`
	InsertTextMode      InsertTextMode              `json:"insertTextMode,omitempty"`
	TextEditText        string                      `json:"textEditText,omitempty"`
	AdditionalTextEdits []TextEdit                  `json:"additionalTextEdits,omitempty"`
	CommitCharacters    []string                    `json:"commitCharacters,omitempty"`
	FilterText          string                      `json:"filterText,omitempty"`
	SortText            string                      `json:"sortText,omitempty"`
	Tags                []CompletionItemTag         `json:"tags,omitempty"`
	Command             *Command                    `json:"command,omitempty"`
	Data                any                         `json:"data,omitempty"`
}

// CompletionList contains completion items returned by the service.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

func (l CompletionList) MarshalJSON() ([]byte, error) {
	items := l.Items
	if items == nil {
		items = []CompletionItem{}
	}
	return json.Marshal(struct {
		IsIncomplete bool             `json:"isIncomplete"`
		Items        []CompletionItem `json:"items"`
	}{
		IsIncomplete: l.IsIncomplete,
		Items:        items,
	})
}

// Hover represents LSP hover content and its optional range.
type Hover struct {
	Contents any    `json:"contents"`
	Range    *Range `json:"range,omitempty"`
}

// MarkedString represents legacy LSP marked string content.
type MarkedString struct {
	Language string `json:"language,omitempty"`
	Value    string `json:"value"`
}

// SymbolKind identifies the kind of a document symbol.
type SymbolKind int

const (
	SymbolKindFile SymbolKind = iota + 1
	SymbolKindModule
	SymbolKindNamespace
	SymbolKindPackage
	SymbolKindClass
	SymbolKindMethod
	SymbolKindProperty
	SymbolKindField
	SymbolKindConstructor
	SymbolKindEnum
	SymbolKindInterface
	SymbolKindFunction
	SymbolKindVariable
	SymbolKindConstant
	SymbolKindString
	SymbolKindNumber
	SymbolKindBoolean
	SymbolKindArray
	SymbolKindObject
	SymbolKindKey
	SymbolKindNull
	SymbolKindEnumMember
	SymbolKindStruct
	SymbolKindEvent
	SymbolKindOperator
	SymbolKindTypeParameter
)

// SymbolTag identifies extra metadata for a symbol.
type SymbolTag int

const (
	SymbolTagDeprecated SymbolTag = 1
)

// Location identifies a document URI and range.
type Location struct {
	URI   DocumentUri `json:"uri"`
	Range Range       `json:"range"`
}

// Definition represents one or more definition locations.
type Definition []Location

// DiagnosticSeverity identifies the severity of a diagnostic.
type DiagnosticSeverity int

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

// DiagnosticTag identifies extra metadata for a diagnostic.
type DiagnosticTag int

const (
	DiagnosticTagUnnecessary DiagnosticTag = 1
	DiagnosticTagDeprecated  DiagnosticTag = 2
)

// CodeDescription provides a URI for diagnostic code documentation.
type CodeDescription struct {
	Href DocumentUri `json:"href"`
}

// DiagnosticRelatedInformation links a diagnostic to a related location.
type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// Diagnostic represents an LSP diagnostic message.
type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           DiagnosticSeverity             `json:"severity,omitempty"`
	Code               any                            `json:"code,omitempty"`
	CodeDescription    *CodeDescription               `json:"codeDescription,omitempty"`
	Source             string                         `json:"source,omitempty"`
	Message            string                         `json:"message"`
	Tags               []DiagnosticTag                `json:"tags,omitempty"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
	Data               any                            `json:"data,omitempty"`
}

// FormattingOptions represents generic LSP formatting options.
type FormattingOptions struct {
	TabSize                int            `json:"tabSize"`
	InsertSpaces           bool           `json:"insertSpaces"`
	TrimTrailingWhitespace *bool          `json:"trimTrailingWhitespace,omitempty"`
	InsertFinalNewline     *bool          `json:"insertFinalNewline,omitempty"`
	TrimFinalNewlines      *bool          `json:"trimFinalNewlines,omitempty"`
	Properties             map[string]any `json:"-"`
}

// SymbolInformation represents flat LSP document symbol information.
type SymbolInformation struct {
	Name          string      `json:"name"`
	Kind          SymbolKind  `json:"kind"`
	Location      Location    `json:"location"`
	Tags          []SymbolTag `json:"tags,omitempty"`
	Deprecated    *bool       `json:"deprecated,omitempty"`
	ContainerName string      `json:"containerName"`
}

// DocumentSymbol represents hierarchical LSP document symbol information.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Tags           []SymbolTag      `json:"tags,omitempty"`
	Deprecated     *bool            `json:"deprecated,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// DocumentHighlightKind identifies the kind of a document highlight.
type DocumentHighlightKind int

const (
	DocumentHighlightKindText  DocumentHighlightKind = 1
	DocumentHighlightKindRead  DocumentHighlightKind = 2
	DocumentHighlightKindWrite DocumentHighlightKind = 3
)

// DocumentHighlight represents a highlighted document range.
type DocumentHighlight struct {
	Range Range                 `json:"range"`
	Kind  DocumentHighlightKind `json:"kind,omitempty"`
}

// DocumentLink represents a link range inside a document.
type DocumentLink struct {
	Range   Range  `json:"range"`
	Target  string `json:"target,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// FoldingRangeKind identifies the category of a folding range.
type FoldingRangeKind string

const (
	FoldingRangeKindComment FoldingRangeKind = "comment"
	FoldingRangeKindImports FoldingRangeKind = "imports"
	FoldingRangeKindRegion  FoldingRangeKind = "region"
)

// FoldingRange represents a foldable range in a document.
type FoldingRange struct {
	StartLine      int              `json:"startLine"`
	StartCharacter *int             `json:"startCharacter,omitempty"`
	EndLine        int              `json:"endLine"`
	EndCharacter   *int             `json:"endCharacter,omitempty"`
	Kind           FoldingRangeKind `json:"kind,omitempty"`
	CollapsedText  string           `json:"collapsedText,omitempty"`
}

// SelectionRange represents a node in an LSP selection range hierarchy.
type SelectionRange struct {
	Range  Range           `json:"range"`
	Parent *SelectionRange `json:"parent,omitempty"`
}

// WorkspaceEdit represents edits across one or more workspace resources.
type WorkspaceEdit struct {
	Changes           map[DocumentUri][]TextEdit                      `json:"changes,omitempty"`
	DocumentChanges   []any                                           `json:"documentChanges,omitempty"`
	ChangeAnnotations map[ChangeAnnotationIdentifier]ChangeAnnotation `json:"changeAnnotations,omitempty"`
}

// InsertReplaceEdit describes separate insert and replace ranges for completion.
type InsertReplaceEdit struct {
	NewText string `json:"newText"`
	Insert  Range  `json:"insert"`
	Replace Range  `json:"replace"`
}

// ChangeAnnotationIdentifier identifies a workspace edit change annotation.
type ChangeAnnotationIdentifier = string

// ChangeAnnotation describes a workspace edit annotation.
type ChangeAnnotation struct {
	Label             string `json:"label"`
	NeedsConfirmation *bool  `json:"needsConfirmation,omitempty"`
	Description       string `json:"description,omitempty"`
}

// AnnotatedTextEdit is a text edit with a change annotation identifier.
type AnnotatedTextEdit struct {
	Range        Range                      `json:"range"`
	NewText      string                     `json:"newText"`
	AnnotationID ChangeAnnotationIdentifier `json:"annotationId"`
}

// OptionalVersionedTextDocumentIdentifier identifies a document and optional version.
type OptionalVersionedTextDocumentIdentifier struct {
	URI     DocumentUri `json:"uri"`
	Version *int        `json:"version"`
}

// TextDocumentEdit groups edits for a specific text document.
type TextDocumentEdit struct {
	TextDocument OptionalVersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []any                                   `json:"edits"`
}

// CreateFileOptions describes options for a create file operation.
type CreateFileOptions struct {
	Overwrite      *bool `json:"overwrite,omitempty"`
	IgnoreIfExists *bool `json:"ignoreIfExists,omitempty"`
}

// CreateFile represents a workspace create file operation.
type CreateFile struct {
	Kind         string                     `json:"kind"`
	URI          DocumentUri                `json:"uri"`
	Options      *CreateFileOptions         `json:"options,omitempty"`
	AnnotationID ChangeAnnotationIdentifier `json:"annotationId,omitempty"`
}

// RenameFileOptions describes options for a rename file operation.
type RenameFileOptions struct {
	Overwrite      *bool `json:"overwrite,omitempty"`
	IgnoreIfExists *bool `json:"ignoreIfExists,omitempty"`
}

// RenameFile represents a workspace rename file operation.
type RenameFile struct {
	Kind         string                     `json:"kind"`
	OldURI       DocumentUri                `json:"oldUri"`
	NewURI       DocumentUri                `json:"newUri"`
	Options      *RenameFileOptions         `json:"options,omitempty"`
	AnnotationID ChangeAnnotationIdentifier `json:"annotationId,omitempty"`
}

// DeleteFileOptions describes options for a delete file operation.
type DeleteFileOptions struct {
	Recursive         *bool `json:"recursive,omitempty"`
	IgnoreIfNotExists *bool `json:"ignoreIfNotExists,omitempty"`
}

// DeleteFile represents a workspace delete file operation.
type DeleteFile struct {
	Kind         string                     `json:"kind"`
	URI          DocumentUri                `json:"uri"`
	Options      *DeleteFileOptions         `json:"options,omitempty"`
	AnnotationID ChangeAnnotationIdentifier `json:"annotationId,omitempty"`
}

// ParameterInformation describes one signature help parameter.
type ParameterInformation struct {
	Label         any `json:"label"`
	Documentation any `json:"documentation,omitempty"`
}

// SignatureInformation describes one callable signature.
type SignatureInformation struct {
	Label           string                 `json:"label"`
	Documentation   any                    `json:"documentation,omitempty"`
	Parameters      []ParameterInformation `json:"parameters,omitempty"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}

// SignatureHelp represents LSP signature help content.
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature *int                   `json:"activeSignature,omitempty"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}

// Color represents an RGBA color using floating point components.
type Color struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
	Alpha float64 `json:"alpha"`
}

// ColorInformation associates a color with a document range.
type ColorInformation struct {
	Range Range `json:"range"`
	Color Color `json:"color"`
}

// ColorPresentation describes how a color can be inserted or displayed.
type ColorPresentation struct {
	Label               string     `json:"label"`
	TextEdit            *TextEdit  `json:"textEdit,omitempty"`
	AdditionalTextEdits []TextEdit `json:"additionalTextEdits,omitempty"`
}

// HTMLFormatConfiguration controls HTML formatter behavior.
type HTMLFormatConfiguration struct {
	TabSize                     int
	InsertSpaces                bool
	IndentEmptyLines            *bool
	WrapLineLength              *int
	Unformatted                 *string
	ContentUnformatted          *string
	IndentInnerHTML             *bool
	WrapAttributes              *string
	WrapAttributesIndentSize    *int
	PreserveNewLines            *bool
	MaxPreserveNewLines         *int
	IndentHandlebars            *bool
	EndWithNewline              *bool
	ExtraLiners                 *string
	IndentScripts               *string
	Templating                  any
	UnformattedContentDelimiter string
	CSS                         *EmbeddedCSSFormatConfiguration
}

// EmbeddedCSSFormatConfiguration controls formatting for CSS inside HTML.
type EmbeddedCSSFormatConfiguration struct {
	NewlineBetweenSelectors      *bool
	NewlineBetweenRules          *bool
	SpaceAroundSelectorSeparator *bool
	BraceStyle                   *string
	PreserveNewLines             *bool
	MaxPreserveNewLines          *int
}

// HTMLBeautifier is the adapter point for a Go port of js-beautify's HTML formatter.
type HTMLBeautifier interface {
	BeautifyHTML(source string, options BeautifyHTMLOptions) (string, error)
}

// BeautifyHTMLOptions mirrors the js-beautify HTML options used by the TypeScript service.
type BeautifyHTMLOptions struct {
	IndentSize                  int                 `json:"indent_size"`
	IndentChar                  string              `json:"indent_char"`
	IndentEmptyLines            bool                `json:"indent_empty_lines"`
	WrapLineLength              int                 `json:"wrap_line_length"`
	Unformatted                 []string            `json:"unformatted,omitempty"`
	ContentUnformatted          []string            `json:"content_unformatted,omitempty"`
	IndentInnerHTML             bool                `json:"indent_inner_html"`
	PreserveNewLines            bool                `json:"preserve_newlines"`
	MaxPreserveNewLines         int                 `json:"max_preserve_newlines"`
	IndentHandlebars            bool                `json:"indent_handlebars"`
	EndWithNewline              bool                `json:"end_with_newline"`
	ExtraLiners                 []string            `json:"extra_liners,omitempty"`
	WrapAttributes              string              `json:"wrap_attributes"`
	WrapAttributesIndentSize    *int                `json:"wrap_attributes_indent_size,omitempty"`
	EOL                         string              `json:"eol"`
	IndentScripts               string              `json:"indent_scripts"`
	Templating                  []string            `json:"templating"`
	UnformattedContentDelimiter string              `json:"unformatted_content_delimiter"`
	CSS                         *BeautifyCSSOptions `json:"css,omitempty"`
}

// BeautifyCSSOptions mirrors the embedded CSS options passed through js-beautify.
type BeautifyCSSOptions struct {
	SelectorSeparatorNewline     *bool   `json:"selector_separator_newline,omitempty"`
	NewlineBetweenRules          *bool   `json:"newline_between_rules,omitempty"`
	SpaceAroundSelectorSeparator *bool   `json:"space_around_selector_separator,omitempty"`
	BraceStyle                   *string `json:"brace_style,omitempty"`
	PreserveNewLines             *bool   `json:"preserve_newlines,omitempty"`
	MaxPreserveNewLines          *int    `json:"max_preserve_newlines,omitempty"`
}

// HoverSettings controls which hover sections are returned.
type HoverSettings struct {
	Documentation *bool
	References    *bool
}

// CompletionConfiguration controls completion behavior.
type CompletionConfiguration struct {
	HideAutoCompleteProposals *bool
	HideEndTagSuggestions     *bool
	AttributeDefaultValue     string
	HTML5                     *bool
	Provider                  map[string]bool
}

// DocumentContext resolves document-relative references.
type DocumentContext interface {
	ResolveReference(ref, base string) (string, bool)
}

// HtmlAttributeValueContext describes an HTML attribute value completion context.
type HtmlAttributeValueContext struct {
	Document   *TextDocument
	Position   Position
	Tag        string
	Attribute  string
	Value      string
	Range      Range
	Attributes map[string]*string
}

// HtmlContentContext describes an HTML content completion context.
type HtmlContentContext struct {
	Document *TextDocument
	Position Position
}

// CompletionParticipant is the marker interface for completion participants.
type CompletionParticipant interface {
}

// HTMLAttributeValueCompletionParticipant receives attribute value completion contexts.
type HTMLAttributeValueCompletionParticipant interface {
	OnHTMLAttributeValue(context HtmlAttributeValueContext)
}

// HTMLContentCompletionParticipant receives HTML content completion contexts.
type HTMLContentCompletionParticipant interface {
	OnHTMLContent(context HtmlContentContext)
}

// Reference identifies external documentation for a data item.
type Reference struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Baseline represents a web platform baseline status value.
type Baseline = any

// BaselineStatus describes web platform baseline metadata.
type BaselineStatus struct {
	Baseline         Baseline `json:"baseline"`
	BaselineLowDate  string   `json:"baseline_low_date,omitempty"`
	BaselineHighDate string   `json:"baseline_high_date,omitempty"`
}

// TagData describes an HTML tag for the data provider.
type TagData struct {
	Name        string          `json:"name"`
	Description any             `json:"description,omitempty"`
	Attributes  []AttributeData `json:"attributes"`
	References  []Reference     `json:"references,omitempty"`
	Void        bool            `json:"void,omitempty"`
	Browsers    []string        `json:"browsers,omitempty"`
	Status      *BaselineStatus `json:"status,omitempty"`
}

// AttributeData describes an HTML attribute for the data provider.
type AttributeData struct {
	Name        string          `json:"name"`
	Description any             `json:"description,omitempty"`
	ValueSet    string          `json:"valueSet,omitempty"`
	Values      []ValueData     `json:"values,omitempty"`
	References  []Reference     `json:"references,omitempty"`
	Browsers    []string        `json:"browsers,omitempty"`
	Status      *BaselineStatus `json:"status,omitempty"`
}

// ValueData describes an HTML attribute value for the data provider.
type ValueData struct {
	Name        string          `json:"name"`
	Description any             `json:"description,omitempty"`
	References  []Reference     `json:"references,omitempty"`
	Browsers    []string        `json:"browsers,omitempty"`
	Status      *BaselineStatus `json:"status,omitempty"`
}

// ValueSet groups reusable HTML attribute values.
type ValueSet struct {
	Name   string      `json:"name"`
	Values []ValueData `json:"values"`
}

// HTMLDataV1 represents custom HTML data provider input.
type HTMLDataV1 struct {
	Version          any             `json:"version"`
	Tags             []TagData       `json:"tags,omitempty"`
	GlobalAttributes []AttributeData `json:"globalAttributes,omitempty"`
	ValueSets        []ValueSet      `json:"valueSets,omitempty"`
}

// HTMLDataProvider supplies tag, attribute, and value metadata.
type HTMLDataProvider interface {
	GetID() string
	IsApplicable(languageID string) bool
	ProvideTags() []TagData
	ProvideAttributes(tag string) []AttributeData
	ProvideValues(tag, attribute string) []ValueData
}

// ICompletionParticipant aliases CompletionParticipant for TypeScript parity.
type ICompletionParticipant = CompletionParticipant

// IReference aliases Reference for TypeScript parity.
type IReference = Reference

// ITagData aliases TagData for TypeScript parity.
type ITagData = TagData

// IAttributeData aliases AttributeData for TypeScript parity.
type IAttributeData = AttributeData

// IValueData aliases ValueData for TypeScript parity.
type IValueData = ValueData

// IValueSet aliases ValueSet for TypeScript parity.
type IValueSet = ValueSet

// IHTMLDataProvider aliases HTMLDataProvider for TypeScript parity.
type IHTMLDataProvider = HTMLDataProvider

// ClientCapabilities describes supported LSP client capabilities.
type ClientCapabilities struct {
	TextDocument *TextDocumentClientCapabilities
}

var LatestClientCapabilities = ClientCapabilities{
	TextDocument: &TextDocumentClientCapabilities{
		Completion: &CompletionClientCapabilities{
			CompletionItem: &CompletionItemClientCapabilities{
				DocumentationFormat: []MarkupKind{MarkupKindMarkdown, MarkupKindPlainText},
			},
		},
		Hover: &HoverClientCapabilities{
			ContentFormat: []MarkupKind{MarkupKindMarkdown, MarkupKindPlainText},
		},
	},
}

// TextDocumentClientCapabilities describes text document client capabilities.
type TextDocumentClientCapabilities struct {
	Completion *CompletionClientCapabilities
	Hover      *HoverClientCapabilities
}

// CompletionClientCapabilities describes completion client capabilities.
type CompletionClientCapabilities struct {
	CompletionItem *CompletionItemClientCapabilities
}

// CompletionItemClientCapabilities describes completion item client capabilities.
type CompletionItemClientCapabilities struct {
	DocumentationFormat []MarkupKind
}

// HoverClientCapabilities describes hover client capabilities.
type HoverClientCapabilities struct {
	ContentFormat []MarkupKind
}

// FileType identifies the type of a file system entry.
type FileType int

const (
	FileTypeUnknown      FileType = 0
	FileTypeFile         FileType = 1
	FileTypeDirectory    FileType = 2
	FileTypeSymbolicLink FileType = 64
)

// FileStat describes a file system entry.
type FileStat struct {
	Type  FileType `json:"type"`
	CTime int64    `json:"ctime"`
	MTime int64    `json:"mtime"`
	Size  int64    `json:"size"`
}

// FileSystemProvider provides file metadata.
type FileSystemProvider interface {
	Stat(uri DocumentUri) (FileStat, error)
}

// FileSystemStatProvider aliases FileSystemProvider for TypeScript API parity.
type FileSystemStatProvider = FileSystemProvider

// FileSystemReadDirectoryProvider optionally reads directory entries for path completion.
type FileSystemReadDirectoryProvider interface {
	ReadDirectory(uri DocumentUri) ([][2]any, error)
}

// LanguageServiceOptions configures a language service instance.
type LanguageServiceOptions struct {
	UseDefaultDataProvider *bool
	CustomDataProviders    []HTMLDataProvider
	FileSystemProvider     FileSystemProvider
	ClientCapabilities     *ClientCapabilities
	HTMLBeautifier         HTMLBeautifier
}
