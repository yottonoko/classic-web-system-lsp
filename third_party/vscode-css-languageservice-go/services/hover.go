package services

import (
	"encoding/json"
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

// HoverOptions configures standalone hover content.
type HoverOptions struct {
	Documentation *bool
	References    *bool
}

func Hover(document *lsp.TextDocument, position lsp.Position, manager *languagefacts.DataManager, options HoverOptions) *lsp.Hover {
	if manager == nil {
		manager = languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
	}
	offset := byteOffsetAtPosition(document, position)
	text := document.Text()
	for _, block := range parseCSSBlocks(text) {
		if offset >= block.headStart && offset <= block.start {
			if document.LanguageID == "scss" && strings.EqualFold(strings.TrimSpace(block.head), "@at-root") {
				return &lsp.Hover{Contents: []any{}}
			}
			return &lsp.Hover{Contents: selectorHoverMarkedStrings(text, block, offset)}
		}
		if offset < block.bodyStart || offset > block.bodyEnd {
			continue
		}
		for _, declaration := range parseDeclarations(text, block.bodyStart, block.bodyEnd) {
			if offset < declaration.nameOffset || offset > declaration.nameOffset+len(declaration.name) {
				continue
			}
			property, ok := manager.GetProperty(declaration.name)
			if !ok {
				return nil
			}
			content := propertyMarkdownDescription(property, options)
			if content.Value == "" {
				return nil
			}
			return &lsp.Hover{Contents: content}
		}
	}
	return nil
}

func selectorHoverMarkedStrings(text string, block cssBlock, offset int) []any {
	current := strings.TrimSpace(selectorPartAtOffset(text, block.headStart, block.start, offset))
	selectors := parentStyleSelectors(text, block)
	selectors = append(selectors, current)
	html := selectorHTML(strings.Join(selectors, " "))
	if contextLines := parentAtRuleContextLines(text, block); len(contextLines) > 0 {
		html = strings.Join(append(contextLines, html), "\n")
	}
	return []any{
		lsp.MarkedString{Language: "html", Value: html},
		selectorSpecificityMarkedString(CalculateSpecificity(current)),
	}
}

func parentStyleSelectors(text string, block cssBlock) []string {
	var selectors []string
	for _, candidate := range parseCSSBlocks(text) {
		if candidate.start >= block.start || block.start >= candidate.end {
			continue
		}
		head := strings.TrimSpace(candidate.head)
		if head == "" || strings.HasPrefix(head, "@") {
			continue
		}
		selectors = append(selectors, head)
	}
	return selectors
}

func parentAtRuleContextLines(text string, block cssBlock) []string {
	var lines []string
	for _, candidate := range parseCSSBlocks(text) {
		if candidate.start >= block.start || block.start >= candidate.end {
			continue
		}
		head := strings.TrimSpace(candidate.head)
		lower := strings.ToLower(head)
		switch {
		case strings.HasPrefix(lower, "@media"):
			lines = append(lines, head)
		case strings.HasPrefix(lower, "@scope"):
			scopeText := strings.TrimSpace(head[len("@scope"):])
			lines = append(lines, strings.TrimSpace("@scope "+scopeDisplayName(scopeText)))
		}
	}
	return lines
}

func propertyMarkdownDescription(property languagefacts.PropertyData, options HoverOptions) lsp.MarkupContent {
	includeDocumentation := options.Documentation == nil || *options.Documentation
	includeReferences := options.References == nil || *options.References

	parts := []string{}
	if includeDocumentation {
		if status := propertyStatusMarkdown(property.Status); status != "" {
			parts = append(parts, status)
		}
		if description := entryDescriptionMarkdown(property.Description); description != "" {
			parts = append(parts, description)
		}
		if property.Baseline != nil && propertyStatusMarkdown(property.Status) == "" {
			status := baselineMarkdown(property.Baseline, property.Browsers)
			if status != "" {
				parts = append(parts, baselineImageMarkdown(property.Baseline)+" _"+status+"_")
			}
		}
		if property.Syntax != "" {
			parts = append(parts, "Syntax: "+textToMarkedString(property.Syntax))
		}
	}
	if includeReferences && len(property.References) > 0 {
		references := make([]string, 0, len(property.References))
		for _, reference := range property.References {
			if reference.Name != "" && reference.URL != "" {
				references = append(references, "["+reference.Name+"]("+reference.URL+")")
			}
		}
		if len(references) > 0 {
			parts = append(parts, strings.Join(references, " | "))
		}
	}
	return lsp.MarkupContent{Kind: lsp.MarkupKindMarkdown, Value: strings.Join(parts, "\n\n")}
}

func propertyStatusMarkdown(status languagefacts.EntryStatus) string {
	switch status {
	case languagefacts.EntryStatusObsolete:
		return "🚨️️️ Property is obsolete. Avoid using it."
	case languagefacts.EntryStatusNonstandard:
		return "🚨️ Property is nonstandard. Avoid using it."
	default:
		return ""
	}
}

func entryDescriptionMarkdown(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == `""` {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return textToMarkedString(text)
	}
	var markup lsp.MarkupContent
	if err := json.Unmarshal(raw, &markup); err == nil {
		if markup.Kind == lsp.MarkupKindMarkdown {
			return markup.Value
		}
		return textToMarkedString(markup.Value)
	}
	return ""
}

func baselineMarkdown(baseline *languagefacts.BaselineStatus, browsers []string) string {
	if baseline == nil {
		return ""
	}
	switch baseline.Status {
	case languagefacts.BaselineLow, languagefacts.BaselineHigh:
		year := ""
		if baseline.BaselineLowDate != "" {
			year = strings.SplitN(baseline.BaselineLowDate, "-", 2)[0]
		}
		prefix := "Widely"
		if baseline.Status == languagefacts.BaselineLow {
			prefix = "Newly"
		}
		if year == "" {
			return prefix + " available across major browsers"
		}
		return prefix + " available across major browsers (Baseline since " + year + ")"
	case languagefacts.BaselineFalse:
		if missing := languagefacts.GetMissingBaselineBrowsers(browsers); missing != "" {
			return "Limited availability across major browsers (Not fully implemented in " + missing + ")"
		}
		return "Limited availability across major browsers"
	default:
		return ""
	}
}

func baselineImageMarkdown(baseline *languagefacts.BaselineStatus) string {
	if baseline == nil {
		return ""
	}
	switch baseline.Status {
	case languagefacts.BaselineLow:
		return "![Baseline icon](" + baselineLowImage + ")"
	case languagefacts.BaselineHigh:
		return "![Baseline icon](" + baselineHighImage + ")"
	default:
		return "![Baseline icon](" + baselineLimitedImage + ")"
	}
}

func textToMarkedString(text string) string {
	var out strings.Builder
	for _, r := range text {
		switch r {
		case '\\', '`', '*', '_', '{', '}', '[', ']', '(', ')', '#', '+', '-', '.', '!':
			out.WriteRune('\\')
		case '<':
			out.WriteString("&lt;")
			continue
		case '>':
			out.WriteString("&gt;")
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

const baselineLimitedImage = "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTgiIGhlaWdodD0iMTAiIHZpZXdCb3g9IjAgMCA1NDAgMzAwIiBmaWxsPSJub25lIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPgogIDxzdHlsZT4KICAgIC5ncmF5LXNoYXBlIHsKICAgICAgZmlsbDogI0M2QzZDNjsgLyogTGlnaHQgbW9kZSAqLwogICAgfQoKICAgIEBtZWRpYSAocHJlZmVycy1jb2xvci1zY2hlbWU6IGRhcmspIHsKICAgICAgLmdyYXktc2hhcGUgewogICAgICAgIGZpbGw6ICM1NjU2NTY7IC8qIERhcmsgbW9kZSAqLwogICAgICB9CiAgICB9CiAgPC9zdHlsZT4KICA8cGF0aCBkPSJNMTUwIDBMMjQwIDkwTDIxMCAxMjBMMTIwIDMwTDE1MCAwWiIgZmlsbD0iI0YwOTQwOSIvPgogIDxwYXRoIGQ9Ik00MjAgMzBMNTQwIDE1MEw0MjAgMjcwTDM5MCAyNDBMNDgwIDE1MEwzOTAgNjBMNDIwIDMwWiIgY2xhc3M9ImdyYXktc2hhcGUiLz4KICA8cGF0aCBkPSJNMzMwIDE4MEwzMDAgMjEwTDM5MCAzMDBMNDIwIDI3MEwzMzAgMTgwWiIgZmlsbD0iI0YwOTQwOSIvPgogIDxwYXRoIGQ9Ik0xMjAgMzBMMTUwIDYwTDYwIDE1MEwxNTAgMjQwTDEyMCAyNzBMMCAxNTBMMTIwIDMwWiIgY2xhc3M9ImdyYXktc2hhcGUiLz4KICA8cGF0aCBkPSJNMzkwIDBMNDIwIDMwTDE1MCAzMDBMMTIwIDI3MEwzOTAgMFoiIGZpbGw9IiNGMDk0MDkiLz4KPC9zdmc+"
const baselineLowImage = "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTgiIGhlaWdodD0iMTAiIHZpZXdCb3g9IjAgMCA1NDAgMzAwIiBmaWxsPSJub25lIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPgogIDxzdHlsZT4KICAgIC5ibHVlLXNoYXBlIHsKICAgICAgZmlsbDogI0E4QzdGQTsgLyogTGlnaHQgbW9kZSAqLwogICAgfQoKICAgIEBtZWRpYSAocHJlZmVycy1jb2xvci1zY2hlbWU6IGRhcmspIHsKICAgICAgLmJsdWUtc2hhcGUgewogICAgICAgIGZpbGw6ICMyRDUwOUU7IC8qIERhcmsgbW9kZSAqLwogICAgICB9CiAgICB9CgogICAgLmRhcmtlci1ibHVlLXNoYXBlIHsKICAgICAgICBmaWxsOiAjMUI2RUYzOwogICAgfQoKICAgIEBtZWRpYSAocHJlZmVycy1jb2xvci1zY2hlbWU6IGRhcmspIHsKICAgICAgICAuZGFya2VyLWJsdWUtc2hhcGUgewogICAgICAgICAgICBmaWxsOiAjNDE4NUZGOwogICAgICAgIH0KICAgIH0KCiAgPC9zdHlsZT4KICA8cGF0aCBkPSJNMTUwIDBMMTgwIDMwTDE1MCA2MEwxMjAgMzBMMTUwIDBaIiBjbGFzcz0iYmx1ZS1zaGFwZSIvPgogIDxwYXRoIGQ9Ik0yMTAgNjBMMjQwIDkwTDIxMCAxMjBMMTgwIDkwTDIxMCA2MFoiIGNsYXNzPSJibHVlLXNoYXBlIi8+CiAgPHBhdGggZD0iTTQ1MCA2MEw0ODAgOTBMNDUwIDEyMEw0MjAgOTBMNDUwIDYwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNNTEwIDEyMEw1NDAgMTUwTDUxMCAxODBMNDgwIDE1MEw1MTAgMTIwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNNDUwIDE4MEw0ODAgMjEwTDQ1MCAyNDBMNDIwIDIxMEw0NTAgMTgwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNMzkwIDI0MEw0MjAgMjcwTDM5MCAzMDBMMzYwIDI3MEwzOTAgMjQwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNMzMwIDE4MEwzNjAgMjEwTDMzMCAyNDBMMzAwIDIxMEwzMzAgMTgwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNOTAgNjBMMTIwIDkwTDkwIDEyMEw2MCA5MEw5MCA2MFoiIGNsYXNzPSJibHVlLXNoYXBlIi8+CiAgPHBhdGggZD0iTTM5MCAwTDQyMCAzMEwxNTAgMzAwTDAgMTUwTDMwIDEyMEwxNTAgMjQwTDM5MCAwWiIgY2xhc3M9ImRhcmtlci1ibHVlLXNoYXBlIi8+Cjwvc3ZnPg=="
const baselineHighImage = "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTgiIGhlaWdodD0iMTAiIHZpZXdCb3g9IjAgMCA1NDAgMzAwIiBmaWxsPSJub25lIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPgogIDxzdHlsZT4KICAgIC5ncmVlbi1zaGFwZSB7CiAgICAgIGZpbGw6ICNDNEVFRDA7IC8qIExpZ2h0IG1vZGUgKi8KICAgIH0KCiAgICBAbWVkaWEgKHByZWZlcnMtY29sb3Itc2NoZW1lOiBkYXJrKSB7CiAgICAgIC5ncmVlbi1zaGFwZSB7CiAgICAgICAgZmlsbDogIzEyNTIyNTsgLyogRGFyayBtb2RlICovCiAgICAgIH0KICAgIH0KICA8L3N0eWxlPgogIDxwYXRoIGQ9Ik00MjAgMzBMMzkwIDYwTDQ4MCAxNTBMMzkwIDI0MEwzMzAgMTgwTDMwMCAyMTBMMzkwIDMwMEw1NDAgMTUwTDQyMCAzMFoiIGNsYXNzPSJncmVlbi1zaGFwZSIvPgogIDxwYXRoIGQ9Ik0xNTAgMEwzMCAxMjBMNjAgMTUwTDE1MCA2MEwyMTAgMTIwTDI0MCA5MEwxNTAgMFoiIGNsYXNzPSJncmVlbi1zaGFwZSIvPgogIDxwYXRoIGQ9Ik0zOTAgMEw0MjAgMzBMMTUwIDMwMEwwIDE1MEwzMCAxMjBMMTUwIDI0MEwzOTAgMFoiIGZpbGw9IiMxRUE0NDYiLz4KPC9zdmc+"
