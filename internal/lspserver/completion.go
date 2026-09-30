package lspserver

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func (s *Server) completion(ctx context.Context, uri string, position lsp.Position, completionContext *completionContext) lsp.CompletionList {
	if ctx == nil || ctx.Err() != nil {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	doc, parsed := s.parsed(uri)
	if doc == nil {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	if ctx.Err() != nil {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	offset := doc.OffsetAt(position)
	if items := aspDirectiveCompletionItems(doc, parsed, position, offset); len(items) > 0 {
		if ctx.Err() != nil {
			return lsp.CompletionList{Items: []lsp.CompletionItem{}}
		}
		return lsp.CompletionList{Items: items}
	}
	region := completionRegionAt(parsed, offset)
	if isCSSOnlyCompletionTrigger(completionContext) && (region == nil || region.Language != core.LanguageCSS) {
		// A space also continues SQL text inside a VBScript string literal.
		if completionContext.TriggerCharacter == " " && region != nil && region.Language == core.LanguageVBScript {
			if stringItems, handled := s.vbscriptStringCompletionsContext(ctx, parsed, offset); handled && ctx.Err() == nil {
				return lsp.CompletionList{Items: stringItems}
			}
		}
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	if region != nil {
		switch region.Language {
		case core.LanguageCSS:
			s.cssMu.Lock()
			completions := s.css.CompleteDocument(ctx, parsed, doc, position)
			s.cssMu.Unlock()
			if ctx.Err() != nil {
				return lsp.CompletionList{Items: []lsp.CompletionItem{}}
			}
			if items := cssSelectorCompletionItems(parsed, doc, position); len(items) > 0 {
				completions.Items = append(items, completions.Items...)
			}
			completions.Items = withEmbeddedCompletionData(completions.Items, "css", uri, s.settings.Locale)
			return completions
		case core.LanguageHTML:
			s.htmlMu.Lock()
			completions := s.html.Complete(parsed, position)
			s.htmlMu.Unlock()
			if ctx.Err() != nil {
				return lsp.CompletionList{Items: []lsp.CompletionItem{}}
			}
			if items := htmlClassCompletionItems(parsed, doc, position); len(items) > 0 {
				completions.Items = append(items, completions.Items...)
			}
			if items := htmlIDCompletionItems(parsed, doc, position); len(items) > 0 {
				completions.Items = append(items, completions.Items...)
			}
			if items := aspIncludeCompletions(doc, position); len(items) > 0 {
				completions.Items = append(items, completions.Items...)
			}
			completions.Items = withEmbeddedCompletionData(completions.Items, "html", uri, s.settings.Locale)
			return completions
		case core.LanguageJavaScript, core.LanguageJScript:
			if ctx.Err() != nil {
				return lsp.CompletionList{Items: []lsp.CompletionItem{}}
			}
			extra := map[string]any{}
			if completionContext != nil {
				extra["context"] = completionContext
			}
			var completions lsp.CompletionList
			if s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/completion", extra, &completions) {
				if ctx.Err() != nil {
					return lsp.CompletionList{Items: []lsp.CompletionItem{}}
				}
				memberCompletion := isJavaScriptMemberCompletion(doc, position)
				// TypeScript's JS service does not expose an unresolved `$` as an
				// auto-import/global completion when jQuery ambient types are
				// disabled. The Go service can return that unresolved identifier;
				// filter it while retaining an explicitly declared local `$`.
				jqueryCompletion := s.javascriptJQueryCompletionEnabled()
				if !jqueryCompletion && !javascriptSourceDeclaresDollar(doc.Text) {
					completions.Items = filterCompletionLabel(completions.Items, "$")
				}
				if !memberCompletion {
					completions.Items = appendMissingCompletionItem(completions.Items, lsp.CompletionItem{
						Label:  "document",
						Kind:   lsp.CompletionItemKindVariable,
						Detail: "DOM Document",
					})
				}
				if !memberCompletion && jqueryCompletion {
					completions.Items = append(completions.Items, lsp.CompletionItem{Label: "$", Kind: lsp.CompletionItemKindFunction, Detail: "jQuery"})
				}
				if !memberCompletion && s.settings.JavaScriptAutoImports {
					completions.Items = append(completions.Items, s.javascriptAutoImportCompletions(ctx, parsed, doc, position, offset)...)
				}
				completions.Items = dedupeCaseSensitiveCompletionItems(completions.Items)
				return completions
			}
			return lsp.CompletionList{Items: []lsp.CompletionItem{}}
		}
	}
	if items, handled := vbscriptCommentCompletions(parsed, doc.Text, offset); handled {
		if ctx.Err() != nil {
			return lsp.CompletionList{Items: []lsp.CompletionItem{}}
		}
		return lsp.CompletionList{Items: items}
	}
	if stringItems, handled := s.vbscriptStringCompletionsContext(ctx, parsed, offset); handled {
		if ctx.Err() != nil {
			return lsp.CompletionList{Items: []lsp.CompletionItem{}}
		}
		return lsp.CompletionList{Items: stringItems}
	}
	if memberItems, handled := s.vbscriptChainBuiltinMemberCompletionsContext(ctx, parsed, offset); handled {
		if ctx.Err() != nil {
			return lsp.CompletionList{Items: []lsp.CompletionItem{}}
		}
		return lsp.CompletionList{Items: dedupeCompletionItems(memberItems)}
	}
	if target, memberCompletion := vbCompletionMemberTargetAt(parsed, offset); memberCompletion {
		if target.explicit {
			if memberItems := s.vbscriptConfiguredMemberCompletionsContext(ctx, parsed, offset); len(memberItems) > 0 {
				if ctx.Err() != nil {
					return lsp.CompletionList{Items: []lsp.CompletionItem{}}
				}
				return lsp.CompletionList{Items: dedupeCompletionItems(memberItems)}
			}
			if memberItems := s.vbscriptTypedMemberCompletionsContext(ctx, parsed, target.owner, offset); len(memberItems) > 0 {
				if ctx.Err() != nil {
					return lsp.CompletionList{Items: []lsp.CompletionItem{}}
				}
				return lsp.CompletionList{Items: dedupeCompletionItems(memberItems)}
			}
			if ctx.Err() != nil {
				return lsp.CompletionList{Items: []lsp.CompletionItem{}}
			}
			if memberItems, handled := s.vbscriptBuiltinMemberCompletionsContext(ctx, parsed, offset); handled {
				if ctx.Err() != nil {
					return lsp.CompletionList{Items: []lsp.CompletionItem{}}
				}
				return lsp.CompletionList{Items: dedupeCompletionItems(memberItems)}
			}
		}
		standalone := isStandaloneVBScriptDocument(parsed)
		if target.explicit && isLegacyVBScriptMemberObject(target.owner, standalone) {
			memberItems := vbscript.CompletionsForOptions(doc.Text, offset, vbscript.CompletionOptions{Standalone: standalone})
			if ctx.Err() != nil {
				return lsp.CompletionList{Items: []lsp.CompletionItem{}}
			}
			return lsp.CompletionList{Items: dedupeCompletionItems(memberItems)}
		}
		memberItems := s.vbscriptTypedMemberCompletionsContext(ctx, parsed, target.owner, offset)
		if ctx.Err() != nil {
			return lsp.CompletionList{Items: []lsp.CompletionItem{}}
		}
		return lsp.CompletionList{Items: dedupeCompletionItems(memberItems)}
	}
	items := vbscript.CompletionsForOptions(doc.Text, offset, vbscript.CompletionOptions{
		SyntaxSnippets: s.settings.SyntaxSnippets,
		SyntaxKeywords: s.settings.VBScriptSyntaxKeywords,
		Standalone:     isStandaloneVBScriptDocument(parsed),
	})
	if len(items) == 0 {
		if owner, ok := vbCompletionMemberOwnerBefore(parsed.Text, offset); ok {
			memberItems := s.vbscriptTypedMemberCompletionsContext(ctx, parsed, owner, offset)
			if ctx.Err() != nil {
				return lsp.CompletionList{Items: []lsp.CompletionItem{}}
			}
			if len(memberItems) > 0 {
				return lsp.CompletionList{Items: dedupeCompletionItems(memberItems)}
			}
		}
	}
	if includesCompletionLabel(items, "Dim") {
		items = append(items, s.vbscriptBuiltInCompletionItems(isStandaloneVBScriptDocument(parsed))...)
	}
	if ctx.Err() != nil {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	items = append(items, s.vbscriptSymbolCompletionsContext(ctx, parsed, offset)...)
	if ctx.Err() != nil {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	items = append(items, s.legacyUndefinedGlobalCompletions(ctx)...)
	if ctx.Err() != nil {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	if s.settings.VBScriptAutoIncludes {
		autoIncludes, incomplete := s.vbscriptAutoIncludeCompletions(parsed, offset)
		if ctx.Err() != nil {
			return lsp.CompletionList{Items: []lsp.CompletionItem{}}
		}
		return lsp.CompletionList{
			IsIncomplete: incomplete,
			Items:        mergeVBScriptAutoIncludeCompletions(items, autoIncludes),
		}
	}
	return lsp.CompletionList{Items: dedupeCompletionItems(items)}
}

func completionRegionAt(parsed *core.ParsedDocument, offset int) *core.Region {
	region := core.RegionAt(parsed, offset)
	if region != nil && region.Kind != core.RegionHTML {
		return region
	}
	if parsed == nil {
		return region
	}
	for index := range parsed.Regions {
		candidate := &parsed.Regions[index]
		if candidate.Language != core.LanguageCSS && candidate.Language != core.LanguageJavaScript && candidate.Language != core.LanguageJScript {
			continue
		}
		atUnclosedStyleAttributeEnd := candidate.Kind == core.RegionStyleAttribute && candidate.End == candidate.ContentEnd && offset == candidate.End
		if offset >= candidate.ContentStart && offset <= candidate.ContentEnd && (offset < candidate.End || atUnclosedStyleAttributeEnd) {
			return candidate
		}
	}
	if region == nil {
		for index := range parsed.Regions {
			candidate := &parsed.Regions[index]
			if candidate.Kind == core.RegionHTML && offset == candidate.End {
				return candidate
			}
		}
	}
	return region
}

func isLegacyVBScriptMemberObject(owner string, standalone bool) bool {
	switch strings.ToLower(owner) {
	case "err":
		return true
	case "wscript":
		return standalone
	case "application", "session", "response", "request", "server":
		return !standalone
	default:
		return false
	}
}

func javascriptSourceDeclaresDollar(text string) bool {
	for _, prefix := range []string{
		"const $", "let $", "var $", "function $", "class $", "import $", "import { $",
	} {
		if strings.Contains(text, prefix) {
			return true
		}
	}
	return false
}

func filterCompletionLabel(items []lsp.CompletionItem, label string) []lsp.CompletionItem {
	filtered := items[:0]
	for _, item := range items {
		if item.Label == label {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func withEmbeddedCompletionData(items []lsp.CompletionItem, kind string, uri string, locale string) []lsp.CompletionItem {
	for index := range items {
		data := map[string]any{
			"kind":   kind,
			"uri":    uri,
			"locale": locale,
		}
		if items[index].Data != nil {
			var existing map[string]any
			if remarshal(items[index].Data, &existing) == nil {
				for key, value := range existing {
					data[key] = value
				}
			}
		}
		items[index].Data = data
	}
	return items
}

func appendMissingCompletionItem(items []lsp.CompletionItem, fallback lsp.CompletionItem) []lsp.CompletionItem {
	for _, item := range items {
		if item.Label == fallback.Label {
			return items
		}
	}
	return append(items, fallback)
}

func dedupeCaseSensitiveCompletionItems(items []lsp.CompletionItem) []lsp.CompletionItem {
	seen := make(map[string]struct{}, len(items))
	result := make([]lsp.CompletionItem, 0, len(items))
	for _, item := range items {
		if _, duplicate := seen[item.Label]; duplicate {
			continue
		}
		seen[item.Label] = struct{}{}
		result = append(result, item)
	}
	return result
}

func isJavaScriptMemberCompletion(doc *core.TextDocument, position lsp.Position) bool {
	offset := doc.OffsetAt(position)
	for offset > 0 {
		character, size := utf8.DecodeLastRuneInString(doc.Text[:offset])
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '$' {
			offset -= size
			continue
		}
		return character == '.'
	}
	return false
}

func includesCompletionLabel(items []lsp.CompletionItem, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}

func isStandaloneVBScriptDocument(parsed *core.ParsedDocument) bool {
	if parsed == nil || len(parsed.Regions) != 1 {
		return false
	}
	region := parsed.Regions[0]
	return parsed.DefaultLanguage == core.LanguageVBScript &&
		region.Kind == core.RegionServerScript &&
		region.Language == core.LanguageVBScript &&
		region.Start == 0 &&
		region.End == len(parsed.Text)
}

func isCSSOnlyCompletionTrigger(context *completionContext) bool {
	if context == nil || context.TriggerKind != 2 {
		return false
	}
	return context.TriggerCharacter == " " || context.TriggerCharacter == ":" || context.TriggerCharacter == ";"
}
