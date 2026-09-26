package lsp

// DocumentURI identifies a text document by URI.
type DocumentURI string

// Position identifies a zero-based UTF-16 line and character location.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range identifies a half-open span between two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location identifies a range inside a document URI.
type Location struct {
	URI   DocumentURI `json:"uri"`
	Range Range       `json:"range"`
}

// DefinitionLink describes a linked definition target.
type DefinitionLink struct {
	OriginSelectionRange *Range      `json:"originSelectionRange,omitempty"`
	TargetURI            DocumentURI `json:"targetUri"`
	TargetRange          Range       `json:"targetRange"`
	TargetSelectionRange Range       `json:"targetSelectionRange"`
}

// MarkupKind identifies the format of markup content.
type MarkupKind string

const (
	MarkupKindPlainText MarkupKind = "plaintext"
	MarkupKindMarkdown  MarkupKind = "markdown"
)

// MarkedString represents a language-tagged markdown code block.
type MarkedString struct {
	Language string `json:"language,omitempty"`
	Value    string `json:"value"`
}

// MarkupContent stores markdown or plaintext LSP content.
type MarkupContent struct {
	Kind  MarkupKind `json:"kind"`
	Value string     `json:"value"`
}

// Hover stores hover contents and an optional source range.
type Hover struct {
	Contents any    `json:"contents"`
	Range    *Range `json:"range,omitempty"`
}

// DiagnosticSeverity identifies the severity of a diagnostic.
type DiagnosticSeverity int

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

// Diagnostic describes a validation issue in a text document.
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     any                `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// CompletionItemKind identifies the semantic kind of a completion item.
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

// InsertTextFormat identifies whether completion text is plain text or a snippet.
type InsertTextFormat int

const (
	InsertTextFormatPlainText InsertTextFormat = 1
	InsertTextFormatSnippet   InsertTextFormat = 2
)

// CompletionItemTag describes additional completion item metadata.
type CompletionItemTag int

const CompletionItemTagDeprecated CompletionItemTag = 1

// CompletionItem describes a single completion proposal.
type CompletionItem struct {
	Label            string              `json:"label"`
	Kind             CompletionItemKind  `json:"kind,omitempty"`
	Tags             []CompletionItemTag `json:"tags,omitempty"`
	Detail           string              `json:"detail,omitempty"`
	Documentation    any                 `json:"documentation,omitempty"`
	SortText         string              `json:"sortText,omitempty"`
	InsertText       string              `json:"insertText,omitempty"`
	InsertTextFormat InsertTextFormat    `json:"insertTextFormat,omitempty"`
	TextEdit         *TextEdit           `json:"textEdit,omitempty"`
	Command          *Command            `json:"command,omitempty"`
}

// CompletionList stores completion proposals and completeness metadata.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// TextEdit replaces a document range with new text.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

func Replace(r Range, newText string) TextEdit {
	return TextEdit{Range: r, NewText: newText}
}

// Command describes an LSP command invocation.
type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

// CodeActionKind identifies the class of a code action.
type CodeActionKind string

const CodeActionKindQuickFix CodeActionKind = "quickfix"

// CodeAction describes an edit or command offered for a diagnostic.
type CodeAction struct {
	Title       string         `json:"title"`
	Kind        CodeActionKind `json:"kind,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
	Command     *Command       `json:"command,omitempty"`
}

// CodeActionContext carries diagnostics relevant to a code action request.
type CodeActionContext struct {
	Diagnostics []Diagnostic     `json:"diagnostics"`
	Only        []CodeActionKind `json:"only,omitempty"`
}

// WorkspaceEdit stores text edits grouped by document.
type WorkspaceEdit struct {
	Changes         map[DocumentURI][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []TextDocumentEdit         `json:"documentChanges,omitempty"`
}

// VersionedTextDocumentIdentifier identifies a specific document version.
type VersionedTextDocumentIdentifier struct {
	URI     DocumentURI `json:"uri"`
	Version int         `json:"version"`
}

// TextDocumentEdit stores edits for one versioned document.
type TextDocumentEdit struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                      `json:"edits"`
}

// SymbolKind identifies the semantic kind of a document symbol.
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

// SymbolInformation describes a flat document symbol.
type SymbolInformation struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	Location Location   `json:"location"`
}

// DocumentSymbol describes a hierarchical document symbol.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           SymbolKind       `json:"kind"`
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

// DocumentHighlight identifies a highlighted document range.
type DocumentHighlight struct {
	Range Range                 `json:"range"`
	Kind  DocumentHighlightKind `json:"kind,omitempty"`
}

// DocumentLink links a document range to a target URI.
type DocumentLink struct {
	Range  Range       `json:"range"`
	Target DocumentURI `json:"target,omitempty"`
}

// Color stores an RGBA color with normalized channels.
type Color struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
	Alpha float64 `json:"alpha"`
}

// ColorInformation stores a color and the range where it appears.
type ColorInformation struct {
	Range Range `json:"range"`
	Color Color `json:"color"`
}

// ColorPresentation stores a textual representation for a color.
type ColorPresentation struct {
	Label    string    `json:"label"`
	TextEdit *TextEdit `json:"textEdit,omitempty"`
}

// FoldingRangeKind identifies the semantic kind of a folding range.
type FoldingRangeKind string

const (
	FoldingRangeKindComment FoldingRangeKind = "comment"
	FoldingRangeKindRegion  FoldingRangeKind = "region"
)

// FoldingRange describes a foldable line range.
type FoldingRange struct {
	StartLine      int               `json:"startLine"`
	StartCharacter *int              `json:"startCharacter,omitempty"`
	EndLine        int               `json:"endLine"`
	EndCharacter   *int              `json:"endCharacter,omitempty"`
	Kind           *FoldingRangeKind `json:"kind,omitempty"`
}

// SelectionRange describes nested ranges for smart selection.
type SelectionRange struct {
	Range  Range           `json:"range"`
	Parent *SelectionRange `json:"parent,omitempty"`
}
