package vbscript

import (
	"sort"
	"strings"
	"unsafe"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

// lexicalWithTarget is the object expression currently bound by a With block.
// The offsets cover the source expression when it is available, which keeps
// member navigation ranges stable for simple With targets.
type lexicalWithTarget struct {
	name  string
	start int
	end   int
}

const vbscriptDocumentTokensAnalysisKey = "vbscript.document-tokens.runtime.v1"

type vbscriptDocumentTokenRegion struct {
	start  int
	end    int
	tokens []Token
}

type vbscriptDocumentTokensRuntime struct {
	tokens  []Token
	regions []vbscriptDocumentTokenRegion
	bytes   int64
}

// SkipPreviousRuntimeInheritance marks tokens as revision-specific. Every
// token offset belongs to one exact parsed source revision.
func (*vbscriptDocumentTokensRuntime) SkipPreviousRuntimeInheritance() {}

// RuntimeAnalysisMemoryOwnerSet exposes one precomputed token backing for
// cache accounting without reflecting over every token and region slice.
func (v *vbscriptDocumentTokensRuntime) RuntimeAnalysisMemoryOwnerSet() []core.RuntimeAnalysisMemoryOwner {
	if v == nil {
		return nil
	}
	return []core.RuntimeAnalysisMemoryOwner{{Identity: v, Bytes: v.bytes}}
}

func vbscriptDocumentTokensRuntimeBytes(tokens []Token, regions []vbscriptDocumentTokenRegion) int64 {
	// Region slices share the document token array, including its capacity.
	return int64(unsafe.Sizeof(vbscriptDocumentTokensRuntime{})) +
		int64(cap(tokens))*int64(unsafe.Sizeof(Token{})) +
		int64(cap(regions))*int64(unsafe.Sizeof(vbscriptDocumentTokenRegion{}))
}

func vbscriptDocumentTokensRuntimeFor(parsed *core.ParsedDocument) *vbscriptDocumentTokensRuntime {
	if parsed == nil {
		return nil
	}
	if cached, ok := parsed.LoadRuntimeAnalysis(vbscriptDocumentTokensAnalysisKey); ok {
		if runtime, valid := cached.(*vbscriptDocumentTokensRuntime); valid && runtime != nil {
			return runtime
		}
	}
	candidate := buildVBScriptDocumentTokensRuntime(parsed)
	actual, _ := parsed.LoadOrStoreRuntimeAnalysis(vbscriptDocumentTokensAnalysisKey, candidate)
	if runtime, valid := actual.(*vbscriptDocumentTokensRuntime); valid && runtime != nil {
		return runtime
	}
	return candidate
}

func buildVBScriptDocumentTokensRuntime(parsed *core.ParsedDocument) *vbscriptDocumentTokensRuntime {
	type tokenSpan struct{ start, end int }
	contentBytes := 0
	for _, region := range parsed.Regions {
		if region.Language == core.LanguageVBScript && region.ContentStart < region.ContentEnd {
			contentBytes += region.ContentEnd - region.ContentStart
		}
	}
	// Stage every region in one buffer and compact each region in place right
	// after tokenizing it, so lossless per-region token arrays are not kept.
	staged := make([]Token, 0, contentBytes/4+1)
	regions := make([]vbscriptDocumentTokenRegion, 0)
	spans := make([]tokenSpan, 0)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript || region.ContentStart >= region.ContentEnd {
			continue
		}
		if len(regions) > 0 {
			staged = append(staged, Token{Kind: "newline", Start: region.ContentStart, End: region.ContentStart, Text: "\n"})
		}
		start := len(staged)
		staged = appendTokensWithBase(staged, parsed.Text[region.ContentStart:region.ContentEnd], region.ContentStart)
		significant := appendSignificantTokens(staged[start:start], staged[start:])
		staged = staged[:start+len(significant)]
		spans = append(spans, tokenSpan{start: start, end: len(staged)})
		regions = append(regions, vbscriptDocumentTokenRegion{start: region.Start, end: region.End})
	}
	tokens := make([]Token, len(staged))
	copy(tokens, staged)
	for index, span := range spans {
		// Limit capacity so appending to a region cannot overwrite its neighbor.
		regions[index].tokens = tokens[span.start:span.end:span.end]
	}
	return &vbscriptDocumentTokensRuntime{
		tokens:  tokens,
		regions: regions,
		bytes:   vbscriptDocumentTokensRuntimeBytes(tokens, regions),
	}
}

// vbscriptDocumentTokens returns one significant token stream for all
// VBScript regions. Synthetic newlines keep statement boundaries at HTML/ASP
// islands while allowing declaration and With scopes to continue across them.
func vbscriptDocumentTokens(parsed *core.ParsedDocument) []Token {
	runtime := vbscriptDocumentTokensRuntimeFor(parsed)
	if runtime == nil {
		return nil
	}
	return runtime.tokens
}

func memberOwnerFromTokens(text string, start int, withOwners *withOwnerIndex) (string, int, int) {
	if start < 0 {
		start = 0
	}
	if start > len(text) {
		start = len(text)
	}
	cursor := start
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	if cursor == 0 || text[cursor-1] != '.' {
		return "", -1, -1
	}
	dot := cursor - 1
	if ownerStart, ownerEnd, ok := dottedOwnerBounds(text, dot); ok {
		if !hasLeadingDotBefore(text, ownerStart) {
			return strings.ToLower(text[ownerStart:ownerEnd]), ownerStart, ownerEnd
		}
		if withOwner, withStart, withEnd := withOwners.at(start); withOwner != "" {
			suffix := strings.ToLower(text[ownerStart:ownerEnd])
			return withOwner + "." + suffix, withStart, withEnd
		}
		return "", -1, -1
	}
	if withOwner, withStart, withEnd := withOwners.at(start); withOwner != "" {
		return withOwner, withStart, withEnd
	}
	return "", -1, -1
}

func dottedOwnerBounds(text string, dot int) (int, int, bool) {
	cursor := dot
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	end := cursor
	for cursor > 0 && isIdent(text[cursor-1]) {
		cursor--
	}
	if cursor == end {
		return 0, 0, false
	}
	start := cursor
	for {
		cursor = start
		for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
			cursor--
		}
		if cursor == 0 || text[cursor-1] != '.' {
			break
		}
		cursor--
		for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
			cursor--
		}
		componentEnd := cursor
		for cursor > 0 && isIdent(text[cursor-1]) {
			cursor--
		}
		if cursor == componentEnd {
			break
		}
		start = cursor
	}
	return start, end, true
}

func hasLeadingDotBefore(text string, start int) bool {
	cursor := start
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	return cursor > 0 && text[cursor-1] == '.'
}

// withOwnerIndex records the innermost With target after each statement so
// member owner lookups do not rescan the token stream from the start.
type withOwnerIndex struct {
	text    string
	tokens  []Token
	built   bool
	starts  []int
	targets []lexicalWithTarget
}

func newWithOwnerIndex(text string, tokens []Token) *withOwnerIndex {
	return &withOwnerIndex{text: text, tokens: tokens}
}

func (index *withOwnerIndex) build() {
	index.built = true
	tokens := index.tokens
	stack := make([]lexicalWithTarget, 0, 2)
	for cursor := 0; cursor < len(tokens); {
		if tokens[cursor].Kind == "newline" || tokens[cursor].Text == ":" {
			cursor++
			continue
		}
		statementStart := tokens[cursor].Start
		end := cstStatementEndIndex(tokens, cursor)
		if end <= cursor {
			cursor++
			continue
		}
		statement := tokens[cursor:end]
		first := strings.ToLower(statement[0].Text)
		changed := false
		switch {
		case first == "with":
			if target := withTargetFromTokens(index.text, statement[1:], stack); target.name != "" {
				stack = append(stack, target)
				changed = true
			}
		case first == "end" && len(statement) > 1 && strings.EqualFold(statement[1].Text, "with"):
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
				changed = true
			}
		}
		if changed {
			var top lexicalWithTarget
			if len(stack) > 0 {
				top = stack[len(stack)-1]
			}
			index.starts = append(index.starts, statementStart)
			index.targets = append(index.targets, top)
		}
		cursor = end
	}
}

// at returns the With target in effect for statements starting before offset.
func (index *withOwnerIndex) at(offset int) (string, int, int) {
	if index == nil {
		return "", -1, -1
	}
	if !index.built {
		index.build()
	}
	position := sort.SearchInts(index.starts, offset) - 1
	if position < 0 || index.targets[position].name == "" {
		return "", -1, -1
	}
	target := index.targets[position]
	return target.name, target.start, target.end
}

func withTargetFromTokens(text string, tokens []Token, stack []lexicalWithTarget) lexicalWithTarget {
	if len(tokens) == 0 {
		return lexicalWithTarget{}
	}
	start := 0
	base := ""
	baseStart := tokens[0].Start
	if tokens[0].Text == "." {
		if len(stack) == 0 {
			return lexicalWithTarget{}
		}
		base = stack[len(stack)-1].name
		baseStart = stack[len(stack)-1].start
		start = 1
	}
	if start >= len(tokens) || tokens[start].Kind != "identifier" {
		return lexicalWithTarget{}
	}
	end := start + 1
	for end+1 < len(tokens) && tokens[end].Text == "." && tokens[end+1].Kind == "identifier" {
		end += 2
	}
	if base == "" {
		base = strings.ToLower(text[tokens[start].Start:tokens[end-1].End])
	} else {
		base += "." + strings.ToLower(text[tokens[start].Start:tokens[end-1].End])
	}
	return lexicalWithTarget{name: base, start: baseStart, end: tokens[end-1].End}
}
