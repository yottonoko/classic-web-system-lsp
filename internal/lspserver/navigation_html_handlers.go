package lspserver

import (
	"context"
	"html"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
)

// Inline event handlers (onclick="...") and javascript: URLs run page script
// when the user acts on an element. Each handler is appended to the page's
// classic scripts as its own invoked function, so calls into page functions
// such as goPage('list.asp') resolve at the handler's call site.
const (
	navigationHTMLHandlerLimit           = 256
	navigationHTMLHandlerProgramMaxBytes = 1 << 20
)

type navigationHTMLHandler struct {
	span         navigationHTMLSourceSpan
	programStart int
	programEnd   int
}

func (b *navigationGraphBuilder) addHTMLEventHandlerNavigation(parsed *core.ParsedDocument, sourceID, ownerURI string) {
	if b == nil || parsed == nil || sourceID == "" {
		return
	}
	if _, ok := b.javascriptCandidates.(typeScriptGoNavigationCandidates); !ok {
		return
	}
	if !navigationTextMayContainHandler(parsed.Text) {
		return
	}
	ctx := b.cancelContext
	if ctx == nil {
		ctx = context.Background()
	}
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	text := maskHTMLNavigationExpressions(parsed, maskEmbeddedHTMLComments(virtual.Text))
	type handlerSource struct {
		span navigationHTMLSourceSpan
		code string
	}
	var sources []handlerSource
	for _, tag := range scanNavigationHTMLTags(text) {
		if tag.Closing {
			continue
		}
		for _, attribute := range tag.Attributes {
			if attribute.ValueStart < 0 || attribute.ValueEnd <= attribute.ValueStart || attribute.ValueEnd > len(parsed.Text) {
				continue
			}
			start := attribute.ValueStart
			switch {
			case len(attribute.Name) > 2 && strings.HasPrefix(attribute.Name, "on"):
			case attribute.Name == "href" || attribute.Name == "action" || attribute.Name == "formaction":
				value := strings.TrimLeft(attribute.RawValue, " \t\r\n")
				if len(value) < len("javascript:") || !strings.EqualFold(value[:len("javascript:")], "javascript:") {
					continue
				}
				start += len(attribute.RawValue) - len(value) + len("javascript:")
			default:
				continue
			}
			code := b.navigationHandlerSourceText(parsed, ownerURI, start, attribute.ValueEnd)
			if strings.TrimSpace(code) == "" {
				continue
			}
			sources = append(sources, handlerSource{span: navigationHTMLSourceSpan{Start: attribute.ValueStart, End: attribute.ValueEnd}, code: code})
			if len(sources) >= navigationHTMLHandlerLimit {
				break
			}
		}
		if len(sources) >= navigationHTMLHandlerLimit {
			break
		}
	}
	if len(sources) == 0 {
		return
	}

	var program strings.Builder
	for _, region := range parsed.Regions {
		if region.Kind != core.RegionClientScript || (region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript) {
			continue
		}
		header := parsed.Text[region.Start:region.ContentStart]
		if strings.EqualFold(strings.TrimSpace(htmlAttributeValue(header, "type")), "module") {
			continue
		}
		script := b.navigationHandlerSourceText(parsed, ownerURI, region.ContentStart, region.ContentEnd)
		if program.Len()+len(script) > navigationHTMLHandlerProgramMaxBytes {
			break
		}
		program.WriteString(script)
		program.WriteString("\n;\n")
	}
	handlers := make([]navigationHTMLHandler, 0, len(sources))
	for _, source := range sources {
		if program.Len()+len(source.code)+64 > navigationHTMLHandlerProgramMaxBytes {
			break
		}
		program.WriteString("(function (event) {\n")
		handler := navigationHTMLHandler{span: source.span, programStart: program.Len()}
		program.WriteString(source.code)
		handler.programEnd = program.Len()
		program.WriteString("\n})();\n")
		handlers = append(handlers, handler)
	}
	if len(handlers) == 0 {
		return
	}
	analysis, err := tsgoadapter.AnalyzeJavaScriptNavigation(ctx, program.String())
	if err != nil {
		b.navigationError = err
		return
	}
	if analysis.Cancelled {
		b.navigationError = ctx.Err()
		if b.navigationError == nil {
			b.navigationError = context.Canceled
		}
		return
	}

	previousCurrent, previousDocument := b.current, b.document
	b.current, b.document = parsed, core.SourceDocument(parsed)
	defer func() {
		b.current, b.document = previousCurrent, previousDocument
	}()
	for _, sink := range analysis.Sinks {
		if err := b.navigationContextError(); err != nil {
			b.navigationError = err
			return
		}
		if sink.Kind == "javascriptFormAction" || len(sink.Expression.Values) == 0 {
			continue
		}
		handler, ok := navigationHandlerForOffset(handlers, sink.Range.ByteStart)
		if !ok {
			continue
		}
		rangeValue, snippet := b.currentRangeAt(handler.span.Start, handler.span.End)
		for _, value := range sink.Expression.Values {
			converted := navigationHandlerValue(value)
			confidence := "probable"
			if len(sink.Expression.Values) > 1 {
				confidence = "possible"
			}
			confidence = lowerNavigationConfidenceString(confidence, converted.confidence())
			extra := map[string]any{
				"confidence": confidence,
				"dynamic":    converted.Kind != navigationValueLiteral,
				"pathKnown":  navigationValuePathKnown(converted),
			}
			if sink.TargetFrame != "" {
				extra["targetFrame"] = sink.TargetFrame
			}
			if sink.Kind == "javascriptFormSubmit" {
				method := strings.ToUpper(strings.TrimSpace(sink.Method))
				if method == "" {
					method = "GET"
				}
				extra["method"] = method
			}
			b.context = &navigationCandidateContext{
				rangeValue: rangeValue, snippet: snippet, extractor: "javascript",
				confidence: confidence, value: converted,
			}
			b.addTargetEdge(ctx, parsed.URI, sourceID, sink.Kind, converted.Text, extra)
			b.context = nil
			if b.navigationError != nil {
				return
			}
		}
	}
}

// navigationTextMayContainHandler is a cheap filter for an on* attribute
// name or a javascript: URL before the page is scanned as HTML.
func navigationTextMayContainHandler(text string) bool {
	for index := 1; index+2 < len(text); index++ {
		switch text[index] {
		case 'o', 'O':
			if (text[index+1] == 'n' || text[index+1] == 'N') && isNavigationHTMLSpace(text[index-1]) && isASCIILetter(text[index+2]) {
				return true
			}
		case ':':
			if index >= len("javascript") && strings.EqualFold(text[index-len("javascript"):index], "javascript") {
				return true
			}
		}
	}
	return false
}

func isASCIILetter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func navigationHandlerForOffset(handlers []navigationHTMLHandler, offset int) (navigationHTMLHandler, bool) {
	for _, handler := range handlers {
		if offset >= handler.programStart && offset <= handler.programEnd {
			return handler, true
		}
	}
	return navigationHTMLHandler{}, false
}

func navigationHandlerValue(value tsgoadapter.NavigationValue) navigationValue {
	kind := navigationValueKindFromTypeScriptGo(value.Kind)
	text := value.Text
	if kind == navigationValueLiteral && strings.Contains(text, navigationVBUnknownOperandPlaceholder) {
		// The placeholder stands for a server expression rendered into a string.
		kind = navigationValueTemplate
	}
	if kind == navigationValueUnknown {
		text = "{unknown}"
	}
	return navigationValue{Kind: kind, Primitive: navigationJavaScriptInferPrimitive(text), Text: text}
}

// navigationHandlerSourceText returns client script source for [start, end)
// with HTML entities decoded and embedded ASP output replaced by its single
// known literal, or by a placeholder when the rendered value is not known.
func (b *navigationGraphBuilder) navigationHandlerSourceText(parsed *core.ParsedDocument, ownerURI string, start, end int) string {
	var builder strings.Builder
	cursor := start
	for _, region := range parsed.Regions {
		if region.Start < cursor || region.End > end {
			continue
		}
		if region.Kind != core.RegionASPExpression && region.Kind != core.RegionASPBlock {
			continue
		}
		builder.WriteString(parsed.Text[cursor:region.Start])
		replacement := navigationVBUnknownOperandPlaceholder
		if region.Kind == core.RegionASPBlock {
			replacement = ""
		} else if value, ok := b.navigationHandlerExpressionLiteral(parsed, ownerURI, region); ok {
			replacement = navigationEscapeJavaScriptStringText(value)
		}
		builder.WriteString(replacement)
		cursor = region.End
	}
	if cursor < end {
		builder.WriteString(parsed.Text[cursor:end])
	}
	return html.UnescapeString(builder.String())
}

// navigationHandlerExpressionLiteral returns the single literal an ASP
// expression renders, whether the page ran as a plain document or as the root
// occurrence of an include program.
func (b *navigationGraphBuilder) navigationHandlerExpressionLiteral(parsed *core.ParsedDocument, ownerURI string, region core.Region) (string, bool) {
	for _, occurrenceID := range []string{"", navigationVBRootOccurrenceID(ownerURI)} {
		values := b.vbExpressionValuesForRegion(parsed, ownerURI, region, occurrenceID)
		if len(values) == 0 {
			continue
		}
		if len(values) == 1 && values[0].Kind == navigationValueLiteral && len(values[0].Alternatives) == 0 {
			return values[0].Text, true
		}
		return "", false
	}
	return "", false
}

func navigationEscapeJavaScriptStringText(text string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "`", "\\`").Replace(text)
}

// addWrittenScriptNavigation analyzes client script that server code writes
// with Response.Write, such as "<script>location.href='list.asp'</script>" or
// an onclick attribute inside written markup. The caller's candidate context
// supplies the Response.Write statement as evidence.
func (b *navigationGraphBuilder) addWrittenScriptNavigation(parsed *core.ParsedDocument, sourceID string, written navigationValue) {
	if b == nil || parsed == nil || sourceID == "" {
		return
	}
	if _, ok := b.javascriptCandidates.(typeScriptGoNavigationCandidates); !ok {
		return
	}
	text := written.Text
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "<script") && !strings.Contains(lower, " on") && !strings.Contains(lower, "javascript:") {
		return
	}
	if len(text) > navigationHTMLHandlerProgramMaxBytes {
		return
	}
	var program strings.Builder
	for _, tag := range scanNavigationHTMLTags(text) {
		if tag.Closing {
			continue
		}
		if tag.Name == "script" {
			if strings.TrimSpace(htmlAttributeValue(tag.attrsText(text), "src")) != "" {
				continue
			}
			if closeStart := findNavigationHTMLClosingTag(text, tag.End, "script"); closeStart >= tag.End {
				program.WriteString(text[tag.End:closeStart])
				program.WriteString("\n;\n")
			}
			continue
		}
		for _, attribute := range tag.Attributes {
			if attribute.ValueStart < 0 {
				continue
			}
			code := attribute.RawValue
			switch {
			case len(attribute.Name) > 2 && strings.HasPrefix(attribute.Name, "on"):
			case attribute.Name == "href" || attribute.Name == "action" || attribute.Name == "formaction":
				trimmed := strings.TrimSpace(code)
				if len(trimmed) < len("javascript:") || !strings.EqualFold(trimmed[:len("javascript:")], "javascript:") {
					continue
				}
				code = trimmed[len("javascript:"):]
			default:
				continue
			}
			program.WriteString("(function (event) {\n")
			program.WriteString(html.UnescapeString(code))
			program.WriteString("\n})();\n")
		}
	}
	if program.Len() == 0 {
		return
	}
	ctx := b.cancelContext
	if ctx == nil {
		ctx = context.Background()
	}
	analysis, err := tsgoadapter.AnalyzeJavaScriptNavigation(ctx, program.String())
	if err != nil {
		b.navigationError = err
		return
	}
	if analysis.Cancelled {
		b.navigationError = ctx.Err()
		if b.navigationError == nil {
			b.navigationError = context.Canceled
		}
		return
	}
	for _, sink := range analysis.Sinks {
		if sink.Kind == "javascriptFormAction" || len(sink.Expression.Values) == 0 {
			continue
		}
		for _, value := range sink.Expression.Values {
			converted := navigationHandlerValue(value)
			if written.Kind != navigationValueLiteral && converted.Kind == navigationValueLiteral && strings.ContainsAny(converted.Text, "{}") {
				converted.Kind = navigationValueTemplate
			}
			if converted.Kind == navigationValueUnknown {
				continue
			}
			confidence := "probable"
			if len(sink.Expression.Values) > 1 {
				confidence = "possible"
			}
			extra := map[string]any{
				"confidence": lowerNavigationConfidenceString(confidence, converted.confidence()),
				"dynamic":    converted.Kind != navigationValueLiteral,
				"pathKnown":  navigationValuePathKnown(converted),
			}
			if sink.TargetFrame != "" {
				extra["targetFrame"] = sink.TargetFrame
			}
			if sink.Kind == "javascriptFormSubmit" {
				method := strings.ToUpper(strings.TrimSpace(sink.Method))
				if method == "" {
					method = "GET"
				}
				extra["method"] = method
			}
			b.addTargetEdge(ctx, parsed.URI, sourceID, sink.Kind, converted.Text, extra)
			if b.navigationError != nil {
				return
			}
		}
	}
}
