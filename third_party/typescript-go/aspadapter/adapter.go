package aspadapter

import (
	"context"
	"sort"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/format"
	"github.com/microsoft/typescript-go/internal/ls/lsutil"
	"github.com/microsoft/typescript-go/internal/parser"
	"github.com/microsoft/typescript-go/internal/scanner"
)

type Identifier struct {
	Text  string
	Start int
	End   int
}

type Analysis struct {
	Identifiers []Identifier
	TokenCount  int
}

// JavaScriptLiteralContext identifies whether a source offset belongs to
// literal text that changes how generated characters must be escaped.
type JavaScriptLiteralContext uint8

const (
	JavaScriptLiteralRaw JavaScriptLiteralContext = iota
	JavaScriptLiteralSingleQuoted
	JavaScriptLiteralDoubleQuoted
	JavaScriptLiteralTemplate
	JavaScriptLiteralRegex
)

// ClassifyJavaScriptLiteralContexts parses source with TypeScript's own parser
// and classifies each byte offset against string, template, and regex tokens.
// Offsets outside literal text remain JavaScriptLiteralRaw.
func ClassifyJavaScriptLiteralContexts(source string, offsets []int) []JavaScriptLiteralContext {
	contexts, _ := ClassifyJavaScriptLiteralContextsContext(context.Background(), source, offsets)
	return contexts
}

// ClassifyJavaScriptLiteralContextsContext is the cancellable form of
// ClassifyJavaScriptLiteralContexts.
func ClassifyJavaScriptLiteralContextsContext(ctx context.Context, source string, offsets []int) ([]JavaScriptLiteralContext, error) {
	contexts := make([]JavaScriptLiteralContext, len(offsets))
	if len(offsets) == 0 {
		return contexts, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type indexedOffset struct {
		offset int
		index  int
	}
	sortedOffsets := make([]indexedOffset, len(offsets))
	for index, offset := range offsets {
		sortedOffsets[index] = indexedOffset{offset: offset, index: index}
	}
	sort.SliceStable(sortedOffsets, func(left, right int) bool {
		return sortedOffsets[left].offset < sortedOffsets[right].offset
	})
	file, err := parser.ParseSourceFileWithCancellation(
		ast.SourceFileParseOptions{FileName: "/__asp_lsp.js", Path: "/__asp_lsp.js"},
		source,
		core.ScriptKindJS,
		func() bool { return ctx.Err() != nil },
	)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, err
	}
	var visitErr error
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if visitErr != nil {
			return false
		}
		if err := ctx.Err(); err != nil {
			visitErr = err
			return false
		}
		if node == nil || node.Kind != ast.KindStringLiteral && node.Kind != ast.KindRegularExpressionLiteral && !ast.IsTemplateLiteralKind(node.Kind) {
			return node != nil && node.ForEachChild(visit)
		}
		start, end := scanner.SkipTrivia(source, node.Pos()), node.End()
		if start < 0 || end <= start || end > len(source) {
			return node.ForEachChild(visit)
		}
		context := JavaScriptLiteralTemplate
		if node.Kind == ast.KindRegularExpressionLiteral {
			context = JavaScriptLiteralRegex
		} else if node.Kind == ast.KindStringLiteral {
			switch source[start] {
			case '\'':
				context = JavaScriptLiteralSingleQuoted
			case '"':
				context = JavaScriptLiteralDoubleQuoted
			default:
				return node.ForEachChild(visit)
			}
		}
		first := sort.Search(len(sortedOffsets), func(index int) bool {
			return sortedOffsets[index].offset > start
		})
		for index := first; index < len(sortedOffsets) && sortedOffsets[index].offset < end; index++ {
			if err := ctx.Err(); err != nil {
				visitErr = err
				return false
			}
			contexts[sortedOffsets[index].index] = context
		}
		return node.ForEachChild(visit)
	}
	file.ForEachChild(visit)
	if visitErr != nil {
		return nil, visitErr
	}
	return contexts, nil
}

type FormattingOptions struct {
	BaseIndentSize                           int
	IndentSize                               int
	TabSize                                  int
	ConvertTabsToSpaces                      bool
	Semicolons                               string
	IndentSwitchCase                         *bool
	PlaceOpenBraceOnNewLineForFunctions      *bool
	PlaceOpenBraceOnNewLineForControlBlocks  *bool
	InsertSpaceAfterCommaDelimiter           *bool
	InsertSpaceAfterSemicolonInForStatements *bool
	InsertSpaceBeforeAndAfterBinaryOperators *bool
	InsertSpaceAfterKeywordsInControlFlow    *bool
	InsertSpaceAfterAnonymousFunctionKeyword *bool
	InsertSpaceInsideNonemptyParentheses     *bool
	InsertSpaceInsideNonemptyBrackets        *bool
	InsertSpaceInsideNonemptyBraces          *bool
	InsertSpaceInsideEmptyBraces             *bool
	InsertSpaceBeforeFunctionParenthesis     *bool
}

type TextChange struct {
	Start   int
	End     int
	NewText string
}

func AnalyzeJavaScript(source string) Analysis {
	s := scanner.NewScanner()
	s.SetText(source)
	s.SetScriptTarget(core.ScriptTargetESNext)
	var result Analysis
	for {
		kind := s.Scan()
		if kind == ast.KindEndOfFile {
			break
		}
		result.TokenCount++
		if kind == ast.KindIdentifier {
			result.Identifiers = append(result.Identifiers, Identifier{
				Text:  s.TokenText(),
				Start: s.TokenStart(),
				End:   s.TokenEnd(),
			})
		}
	}
	return result
}

// AnalyzeJavaScriptAST returns identifier nodes from the TypeScript parser,
// excluding comments and literal text while retaining JSX and syntax-aware ranges.
func AnalyzeJavaScriptAST(source string) Analysis {
	file := parseJavaScript(source)
	var result Analysis
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if node == nil {
			return false
		}
		if node.Kind == ast.KindIdentifier {
			result.Identifiers = append(result.Identifiers, Identifier{Text: node.Text(), Start: node.Pos(), End: node.End()})
		}
		return node.ForEachChild(visit)
	}
	file.ForEachChild(visit)
	return result
}

func FormatJavaScript(source string, options FormattingOptions) []TextChange {
	file := parseJavaScript(source)
	ctx := format.WithFormatCodeSettings(context.Background(), formatCodeSettings(options), "\n")
	return textChanges(format.FormatDocument(ctx, file))
}

func FormatJavaScriptAfterKeystroke(source string, position int, key string, options FormattingOptions) []TextChange {
	file := parseJavaScript(source)
	ctx := format.WithFormatCodeSettings(context.Background(), formatCodeSettings(options), "\n")
	var changes []core.TextChange
	switch key {
	case "\n", "\r":
		changes = format.FormatOnEnter(ctx, file, position)
	case "{":
		changes = format.FormatOnOpeningCurly(ctx, file, position)
	case "}":
		changes = format.FormatOnClosingCurly(ctx, file, position)
	case ";":
		changes = format.FormatOnSemicolon(ctx, file, position)
	}
	return textChanges(changes)
}

func parseJavaScript(source string) *ast.SourceFile {
	return parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/__asp_lsp.js", Path: "/__asp_lsp.js"}, source, core.ScriptKindJS)
}

func formatCodeSettings(options FormattingOptions) lsutil.FormatCodeSettings {
	settings := lsutil.GetDefaultFormatCodeSettings()
	settings.BaseIndentSize = options.BaseIndentSize
	if options.IndentSize > 0 {
		settings.IndentSize = options.IndentSize
	}
	if options.TabSize > 0 {
		settings.TabSize = options.TabSize
	}
	settings.ConvertTabsToSpaces = core.BoolToTristate(options.ConvertTabsToSpaces)
	settings.NewLineCharacter = "\n"
	switch options.Semicolons {
	case string(lsutil.SemicolonPreferenceInsert), string(lsutil.SemicolonPreferenceRemove), string(lsutil.SemicolonPreferenceIgnore):
		settings.Semicolons = lsutil.SemicolonPreference(options.Semicolons)
	}
	setTristate := func(target *core.Tristate, value *bool) {
		if value != nil {
			*target = core.BoolToTristate(*value)
		}
	}
	setTristate(&settings.IndentSwitchCase, options.IndentSwitchCase)
	setTristate(&settings.PlaceOpenBraceOnNewLineForFunctions, options.PlaceOpenBraceOnNewLineForFunctions)
	setTristate(&settings.PlaceOpenBraceOnNewLineForControlBlocks, options.PlaceOpenBraceOnNewLineForControlBlocks)
	setTristate(&settings.InsertSpaceAfterCommaDelimiter, options.InsertSpaceAfterCommaDelimiter)
	setTristate(&settings.InsertSpaceAfterSemicolonInForStatements, options.InsertSpaceAfterSemicolonInForStatements)
	setTristate(&settings.InsertSpaceBeforeAndAfterBinaryOperators, options.InsertSpaceBeforeAndAfterBinaryOperators)
	setTristate(&settings.InsertSpaceAfterKeywordsInControlFlowStatements, options.InsertSpaceAfterKeywordsInControlFlow)
	setTristate(&settings.InsertSpaceAfterFunctionKeywordForAnonymousFunctions, options.InsertSpaceAfterAnonymousFunctionKeyword)
	setTristate(&settings.InsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis, options.InsertSpaceInsideNonemptyParentheses)
	setTristate(&settings.InsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets, options.InsertSpaceInsideNonemptyBrackets)
	setTristate(&settings.InsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces, options.InsertSpaceInsideNonemptyBraces)
	setTristate(&settings.InsertSpaceAfterOpeningAndBeforeClosingEmptyBraces, options.InsertSpaceInsideEmptyBraces)
	setTristate(&settings.InsertSpaceBeforeFunctionParenthesis, options.InsertSpaceBeforeFunctionParenthesis)
	return settings
}

func textChanges(changes []core.TextChange) []TextChange {
	result := make([]TextChange, 0, len(changes))
	for _, change := range changes {
		result = append(result, TextChange{Start: change.TextRange.Pos(), End: change.TextRange.End(), NewText: change.NewText})
	}
	return result
}
