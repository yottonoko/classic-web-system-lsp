package core

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type FormattingOptions struct {
	TabSize                            int                                                               `json:"tabSize"`
	InsertSpaces                       bool                                                              `json:"insertSpaces"`
	EndOfLine                          string                                                            `json:"endOfLine,omitempty"`
	EmbeddedLanguageFormatting         string                                                            `json:"embeddedLanguageFormatting,omitempty"`
	FragmentMode                       string                                                            `json:"fragmentMode,omitempty"`
	IndentEmptyLines                   *bool                                                             `json:"-"`
	MaxPreserveNewLines                *int                                                              `json:"-"`
	PreserveNewLines                   *bool                                                             `json:"-"`
	PrintWidth                         int                                                               `json:"printWidth,omitempty"`
	HTMLContentUnformatted             string                                                            `json:"htmlContentUnformatted,omitempty"`
	HTMLExtraLiners                    string                                                            `json:"htmlExtraLiners,omitempty"`
	HTMLIndentInnerHTML                *bool                                                             `json:"-"`
	HTMLTabSize                        int                                                               `json:"htmlIndentSize,omitempty"`
	HTMLInsertSpaces                   *bool                                                             `json:"-"`
	HTMLUnformatted                    string                                                            `json:"htmlUnformatted,omitempty"`
	HTMLWrapLineLength                 int                                                               `json:"htmlWrapLineLength,omitempty"`
	HTMLWrapAttributes                 string                                                            `json:"htmlWrapAttributes,omitempty"`
	HTMLWrapAttributesIndentSize       int                                                               `json:"htmlWrapAttributesIndentSize,omitempty"`
	CSSTabSize                         int                                                               `json:"cssIndentSize,omitempty"`
	CSSInsertSpaces                    *bool                                                             `json:"-"`
	CSSWrapLineLength                  int                                                               `json:"cssWrapLineLength,omitempty"`
	CSSNewlineBetweenRules             *bool                                                             `json:"-"`
	CSSNewlineBetweenSelectors         *bool                                                             `json:"-"`
	CSSSpaceAroundSelectorSeparator    *bool                                                             `json:"-"`
	CSSBraceStyle                      string                                                            `json:"cssBraceStyle,omitempty"`
	CSSTagIndentMode                   string                                                            `json:"-"`
	IgnoreCSSTagIndent                 bool                                                              `json:"-"`
	JavaScriptTabSize                  int                                                               `json:"javascriptIndentSize,omitempty"`
	JavaScriptInsertSpaces             *bool                                                             `json:"-"`
	JScriptTabSize                     int                                                               `json:"jscriptIndentSize,omitempty"`
	JScriptInsertSpaces                *bool                                                             `json:"-"`
	JavaScriptBraceStyle               string                                                            `json:"-"`
	JavaScriptBraceFunctionsNewLine    *bool                                                             `json:"-"`
	JavaScriptBraceControlNewLine      *bool                                                             `json:"-"`
	JavaScriptTagIndentMode            string                                                            `json:"-"`
	IgnoreJavaScriptTagIndent          bool                                                              `json:"-"`
	JavaScriptSemicolons               string                                                            `json:"-"`
	JavaScriptIndentSwitchCase         *bool                                                             `json:"-"`
	JavaScriptSpaceAfterComma          *bool                                                             `json:"-"`
	JavaScriptSpaceAfterForSemicolon   *bool                                                             `json:"-"`
	JavaScriptSpaceAroundBinaryOps     *bool                                                             `json:"-"`
	JavaScriptSpaceAfterAnonFunction   *bool                                                             `json:"-"`
	JavaScriptSpaceAfterNamedFunction  *bool                                                             `json:"-"`
	JavaScriptSpaceBeforeConditional   *bool                                                             `json:"-"`
	JavaScriptSpaceInsideParentheses   *bool                                                             `json:"-"`
	JavaScriptSpaceInsideBrackets      *bool                                                             `json:"-"`
	JavaScriptSpaceInsideBraces        *bool                                                             `json:"-"`
	JavaScriptSpaceInsideEmptyBraces   *bool                                                             `json:"-"`
	NestedASPInCSSJS                   string                                                            `json:"-"`
	InsertFinalNewline                 bool                                                              `json:"insertFinalNewline,omitempty"`
	VBScriptTabSize                    int                                                               `json:"vbscriptIndentSize,omitempty"`
	VBScriptInsertSpaces               *bool                                                             `json:"-"`
	VBScriptKeywordCase                string                                                            `json:"-"`
	VBScriptLineContinuationIndentSize int                                                               `json:"-"`
	VBScriptSelectCaseIndent           string                                                            `json:"-"`
	VBScriptBlockIndent                string                                                            `json:"-"`
	VBScriptTagIndentMode              string                                                            `json:"-"`
	IgnoreVBScriptTagIndent            bool                                                              `json:"-"`
	UppercaseKeywords                  bool                                                              `json:"-"`
	AlignAssignments                   bool                                                              `json:"-"`
	ASPDelimiterSpacing                string                                                            `json:"-"`
	ASPBlockNewline                    string                                                            `json:"-"`
	EnabledLanguages                   []string                                                          `json:"enabledLanguages,omitempty"`
	RespectDisableRegions              bool                                                              `json:"respectDisableRegions,omitempty"`
	FormatHTML                         func(string, FormattingOptions) (string, error)                   `json:"-"`
	FormatCSS                          func(string, FormattingOptions) (string, error)                   `json:"-"`
	FormatJavaScript                   func(string, FormattingOptions, EmbeddedLanguage) (string, error) `json:"-"`
}

func FormatDocument(parsed *ParsedDocument, options FormattingOptions) []lsp.TextEdit {
	options = SanitizeFormattingOptions(options)
	formatted := ""
	if shouldFormatWholeHTMLDocument(parsed, options) {
		var err error
		formatted, err = options.FormatHTML(parsed.Text, options)
		if err != nil {
			formatted = parsed.Text
		}
	} else if shouldFormatHTMLAroundASP(parsed, options) {
		var ok bool
		formatted, ok = formatHTMLAroundASP(parsed, options)
		if !ok {
			formatted = formatText(parsed, options, 0, len(parsed.Text))
		}
	} else {
		formatted = formatText(parsed, options, 0, len(parsed.Text))
	}
	formatted = finalizeFormattedText(formatted, parsed.Text, options)
	if formatted == parsed.Text || !formattingPreservesServerRegions(parsed, formatted) {
		return nil
	}
	source := NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	return []lsp.TextEdit{{Range: source.Range(0, len(parsed.Text)), NewText: formatted}}
}

func FormatRange(parsed *ParsedDocument, r lsp.Range, options FormattingOptions) []lsp.TextEdit {
	if parsed == nil {
		return nil
	}
	options = SanitizeFormattingOptions(options)
	source := NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	start := source.OffsetAt(r.Start)
	end := source.OffsetAt(r.End)
	if start > end {
		start, end = end, start
	}
	if start < 0 || end > len(parsed.Text) || start > end {
		return nil
	}
	formatted := formatText(parsed, options, start, end)
	original := parsed.Text[start:end]
	formatted = finalizeFormattedRangeText(formatted, original, options)
	if formatted == original || !formattingPreservesServerRegions(parsed, parsed.Text[:start]+formatted+parsed.Text[end:]) {
		return nil
	}
	return []lsp.TextEdit{{Range: source.Range(start, end), NewText: formatted}}
}

// formattingPreservesServerRegions rejects output that creates, drops, or
// rewrites server regions. Embedded formatters do not know Classic ASP; the
// HTML formatter, for example, collapses the text "< %" into a "<%" delimiter.
// Server code may only change in whitespace and letter case, which is what the
// VBScript formatter adjusts.
func formattingPreservesServerRegions(parsed *ParsedDocument, formatted string) bool {
	reparsed := ParseDocument(parsed.URI, formatted, Settings{DefaultLanguage: string(parsed.DefaultLanguage)})
	return slices.Equal(serverRegionSignatures(parsed.Text, parsed.Regions), serverRegionSignatures(formatted, reparsed.Regions))
}

type serverRegionSignature struct {
	kind RegionKind
	code string
}

func serverRegionSignatures(text string, regions []Region) []serverRegionSignature {
	signatures := []serverRegionSignature{}
	for _, region := range regions {
		if !isASPHole(region) && region.Kind != RegionServerScript {
			continue
		}
		code := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return unicode.ToLower(r)
		}, text[region.ContentStart:region.ContentEnd])
		signatures = append(signatures, serverRegionSignature{kind: region.Kind, code: code})
	}
	return signatures
}

func shouldFormatWholeHTMLDocument(parsed *ParsedDocument, options FormattingOptions) bool {
	if parsed == nil || options.FormatHTML == nil || !formatLanguageEnabled(LanguageHTML, options) {
		return false
	}
	for _, region := range parsed.Regions {
		if isASPHole(region) || region.Kind == RegionServerScript {
			return false
		}
	}
	return true
}

func shouldFormatHTMLAroundASP(parsed *ParsedDocument, options FormattingOptions) bool {
	if parsed == nil || options.FormatHTML == nil || !formatLanguageEnabled(LanguageHTML, options) {
		return false
	}
	hasHTML := false
	hasProtectedRegion := false
	for _, region := range parsed.Regions {
		if region.Language == LanguageHTML {
			hasHTML = true
		}
		if isASPHole(region) || region.Kind == RegionServerScript {
			hasProtectedRegion = true
		}
	}
	return hasHTML && hasProtectedRegion
}

type protectedHTMLRegion struct {
	Region    Region
	Token     string
	IsElement bool
}

func formatHTMLAroundASP(parsed *ParsedDocument, options FormattingOptions) (string, bool) {
	protected, holes := protectRegionsForHTMLFormatting(parsed)
	formatted, err := options.FormatHTML(protected, options)
	if err != nil || formatted == "" {
		return "", false
	}
	restored, ok := restoreRegionsAfterHTMLFormatting(formatted, parsed.Text, holes, options)
	if !ok {
		return "", false
	}
	return restored, true
}

func protectRegionsForHTMLFormatting(parsed *ParsedDocument) (string, []protectedHTMLRegion) {
	regions := append([]Region(nil), parsed.Regions...)
	sort.SliceStable(regions, func(i, j int) bool {
		return regions[i].Start < regions[j].Start
	})
	var out strings.Builder
	holes := []protectedHTMLRegion{}
	cursor := 0
	// Collision checks rescan the whole source per hole, so only run them when
	// the source mentions a placeholder prefix at all.
	aspTokenSource, elementTokenSource := "", ""
	if strings.Contains(parsed.Text, aspHTMLPlaceholderPrefix) {
		aspTokenSource = parsed.Text
	}
	if containsASCIIFold(parsed.Text, htmlPlaceholderElementPrefix) {
		elementTokenSource = parsed.Text
	}
	for _, region := range regions {
		if !protectRegionDuringHTMLFormatting(region) || region.Start < cursor {
			continue
		}
		out.WriteString(parsed.Text[cursor:region.Start])
		if region.Kind == RegionStyle || region.Kind == RegionClientScript || region.Kind == RegionServerScript {
			token := htmlPlaceholderElementName(elementTokenSource, len(holes))
			out.WriteString("<" + token + "></" + token + ">")
			holes = append(holes, protectedHTMLRegion{Region: region, Token: token, IsElement: true})
		} else {
			token := aspHTMLPlaceholderToken(aspTokenSource, len(holes))
			out.WriteString(token)
			holes = append(holes, protectedHTMLRegion{Region: region, Token: token})
		}
		cursor = region.End
	}
	out.WriteString(parsed.Text[cursor:])
	return out.String(), holes
}

func protectRegionDuringHTMLFormatting(region Region) bool {
	return isASPHole(region) || region.Kind == RegionStyle || region.Kind == RegionClientScript || region.Kind == RegionServerScript
}

const (
	aspHTMLPlaceholderPrefix     = "__ASP_LSP_FORMAT_HOLE_"
	htmlPlaceholderElementPrefix = "asp-lsp-format-hole-"
)

func aspHTMLPlaceholderToken(source string, index int) string {
	for {
		token := aspHTMLPlaceholderPrefix + strconv.Itoa(index) + "__"
		if !strings.Contains(source, token) {
			return token
		}
		index++
	}
}

func htmlPlaceholderElementName(source string, index int) string {
	for {
		token := htmlPlaceholderElementPrefix + strconv.Itoa(index)
		if !strings.Contains(strings.ToLower(source), token) {
			return token
		}
		index++
	}
}

func restoreRegionsAfterHTMLFormatting(formatted string, source string, holes []protectedHTMLRegion, options FormattingOptions) (string, bool) {
	// Placeholders keep their source order through HTML formatting, so restore
	// them in one forward pass instead of rescanning and copying the whole
	// document for every hole.
	var out strings.Builder
	out.Grow(len(formatted))
	cursor := 0
	for _, hole := range holes {
		start, end, ok := protectedRegionPlaceholderRange(formatted[cursor:], hole)
		if !ok {
			return "", false
		}
		out.WriteString(formatted[cursor : cursor+start])
		restored := out.String()
		offset := len(restored)
		replacement := formatRegionForHTMLRestore(source, hole.Region, options)
		if strings.Contains(replacement, "\n") {
			if tagIndentIgnored(hole.Region, options) {
				if _, ok := leadingWhitespaceBeforeOffset(restored, offset); !ok && protectedRegionPrefersOwnLine(hole) {
					replacement = "\n" + leadingWhitespaceAtLineStart(restored, offset) + replacement
				}
			} else if prefix, ok := leadingWhitespaceBeforeOffset(restored, offset); ok {
				replacement = strings.ReplaceAll(replacement, "\n", "\n"+prefix)
			} else if protectedRegionPrefersOwnLine(hole) {
				prefix := leadingWhitespaceAtLineStart(restored, offset)
				replacement = "\n" + prefix + strings.ReplaceAll(replacement, "\n", "\n"+prefix)
			}
		}
		out.WriteString(replacement)
		cursor += end
	}
	out.WriteString(formatted[cursor:])
	return out.String(), true
}

func protectedRegionPrefersOwnLine(hole protectedHTMLRegion) bool {
	return hole.IsElement || hole.Region.Kind == RegionASPBlock || hole.Region.Kind == RegionASPDirective
}

func protectedRegionPlaceholderRange(formatted string, hole protectedHTMLRegion) (int, int, bool) {
	if !hole.IsElement {
		index := strings.Index(formatted, hole.Token)
		if index < 0 {
			return 0, 0, false
		}
		return index, index + len(hole.Token), true
	}
	pattern := regexp.MustCompile(`(?is)<\s*` + regexp.QuoteMeta(hole.Token) + `\b[^>]*>\s*</\s*` + regexp.QuoteMeta(hole.Token) + `\s*>`)
	match := pattern.FindStringIndex(formatted)
	if match == nil {
		return 0, 0, false
	}
	return match[0], match[1], true
}

func formatRegionForHTMLRestore(source string, region Region, options FormattingOptions) string {
	switch {
	case region.Language == LanguageCSS && region.Kind == RegionStyle:
		return formatEmbeddedRegionWithOptionsMode(source, region, options, options.FormatCSS, true)
	case (region.Language == LanguageJavaScript || region.Language == LanguageJScript) && (region.Kind == RegionClientScript || region.Kind == RegionServerScript):
		return formatEmbeddedScriptRegionMode(source, region, options, options.FormatJavaScript, true)
	default:
		replacement := formatRegion(source, region, options)
		if isASPHole(region) && strings.Contains(replacement, "\n") {
			if prefix, ok := leadingWhitespaceBeforeOffset(source, region.Start); ok && prefix != "" {
				replacement = strings.ReplaceAll(replacement, "\n"+prefix, "\n")
			}
		}
		return replacement
	}
}

func leadingWhitespaceBeforeOffset(text string, offset int) (string, bool) {
	lineStart := strings.LastIndexAny(text[:offset], "\r\n") + 1
	prefix := text[lineStart:offset]
	for i := 0; i < len(prefix); i++ {
		if prefix[i] != ' ' && prefix[i] != '\t' {
			return "", false
		}
	}
	return prefix, true
}

func leadingWhitespaceAtLineStart(text string, offset int) string {
	lineStart := strings.LastIndexAny(text[:offset], "\r\n") + 1
	cursor := lineStart
	for cursor < offset && (text[cursor] == ' ' || text[cursor] == '\t') {
		cursor++
	}
	return text[lineStart:cursor]
}

func formatText(parsed *ParsedDocument, options FormattingOptions, start, end int) string {
	var out strings.Builder
	cursor := start
	regions := append([]Region(nil), parsed.Regions...)
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].Start != regions[j].Start {
			return regions[i].Start < regions[j].Start
		}
		return regions[i].End < regions[j].End
	})
	for _, region := range regions {
		if region.Language == LanguageHTML {
			continue
		}
		if region.End <= start || region.Start >= end || !formatLanguageEnabled(region.Language, options) {
			continue
		}
		if region.Start < start || region.End > end {
			if region.Start <= start || region.ContentStart >= end || region.Kind != RegionASPBlock {
				continue
			}
			out.WriteString(parsed.Text[cursor:region.Start])
			partial := region
			partial.End = end
			partial.ContentEnd = end
			out.WriteString(formatPartialRegion(parsed.Text, partial, options))
			cursor = end
			continue
		}
		if region.Start < cursor {
			continue
		}
		out.WriteString(parsed.Text[cursor:min(region.Start, end)])
		out.WriteString(formatRegion(parsed.Text, region, options))
		cursor = min(region.End, end)
	}
	out.WriteString(parsed.Text[cursor:end])
	return out.String()
}

func formatPartialRegion(text string, region Region, options FormattingOptions) string {
	formatted := formatRegion(text, region, options)
	if region.Kind == RegionASPBlock || region.Kind == RegionServerScript {
		formatted = strings.TrimSuffix(formatted, "\n%>")
		formatted = strings.TrimSuffix(formatted, "%>")
	}
	return formatted
}
