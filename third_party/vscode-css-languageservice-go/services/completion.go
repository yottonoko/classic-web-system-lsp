package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

// CompletionOptions configures standalone completion requests.
type CompletionOptions struct {
	TriggerPropertyValueCompletion bool
	CompletePropertyWithSemicolon  bool
	ReadDirectory                  ReadDirectoryFunc
	ResolveReference               ResolveReferenceFunc
	Participants                   []CompletionParticipant
}

// PropertyCompletionContext describes a property-name completion participant event.
type PropertyCompletionContext struct {
	PropertyName string
	Range        lsp.Range
}

// PropertyValueCompletionContext describes a property-value completion participant event.
type PropertyValueCompletionContext struct {
	PropertyName  string
	PropertyValue string
	Range         lsp.Range
}

// URILiteralCompletionContext describes a URL literal completion participant event.
type URILiteralCompletionContext struct {
	URIValue string
	Position lsp.Position
	Range    lsp.Range
}

// ImportPathCompletionContext describes an import path completion participant event.
type ImportPathCompletionContext struct {
	PathValue string
	Position  lsp.Position
	Range     lsp.Range
}

// MixinReferenceCompletionContext describes a SCSS or LESS mixin completion participant event.
type MixinReferenceCompletionContext struct {
	MixinName string
	Range     lsp.Range
}

// CompletionParticipant receives standalone completion callbacks.
type CompletionParticipant struct {
	OnProperty        func(PropertyCompletionContext)
	OnPropertyValue   func(PropertyValueCompletionContext)
	OnURILiteralValue func(URILiteralCompletionContext)
	OnImportPath      func(ImportPathCompletionContext)
	OnMixinReference  func(MixinReferenceCompletionContext)
}

// FileType describes the kind of a filesystem entry.
type FileType int

const (
	FileTypeUnknown FileType = iota
	FileTypeFile
	FileTypeDirectory
	FileTypeSymbolicLink
)

// FileEntry describes a directory entry for path completion.
type FileEntry struct {
	Name string
	Type FileType
}

// ReadDirectoryFunc reads directory entries for path completion.
type ReadDirectoryFunc func(context.Context, string) ([]FileEntry, error)

// ResolveReferenceFunc resolves a reference against a base URL.
type ResolveReferenceFunc func(ref, baseURL string) (string, bool)

type completionContextKind int

const (
	completionTopLevel completionContextKind = iota
	completionNone
	completionSelector
	completionProperty
	completionValue
)

type completionContext struct {
	kind        completionContextKind
	block       *cssBlock
	declaration *cssDeclaration
	replace     selectionInterval
	currentWord string
	offset      int
}

func Complete(ctx context.Context, document *lsp.TextDocument, position lsp.Position, manager *languagefacts.DataManager, options CompletionOptions) (lsp.CompletionList, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return lsp.CompletionList{}, err
	}
	if manager == nil {
		manager = languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
	}
	offset := byteOffsetAtPosition(document, position)
	if list, ok := completeSCSSModuleBuiltIns(document, offset); ok {
		return list, nil
	}
	pathParticipantNotified := notifyPathCompletionParticipants(document, offset, options)
	if list, ok, err := completePath(ctx, document, offset, options); ok || err != nil {
		return list, err
	}
	if pathParticipantNotified && (options.ReadDirectory == nil || options.ResolveReference == nil) {
		return lsp.CompletionList{IsIncomplete: false}, nil
	}
	if list, ok := completeSupportsCondition(document, offset, manager, options); ok {
		return list, nil
	}
	if list, ok := completeMediaCondition(document, offset, manager); ok {
		return list, nil
	}
	c := analyzeCompletionContext(document.Text(), offset)
	notifyCompletionParticipants(document, offset, c, options)
	if err := ctx.Err(); err != nil {
		return lsp.CompletionList{}, err
	}
	builder := completionBuilder{ctx: ctx, document: document, manager: manager, context: c, options: options}
	return lsp.CompletionList{IsIncomplete: false, Items: builder.complete()}, nil
}

type completionBuilder struct {
	ctx      context.Context
	document *lsp.TextDocument
	manager  *languagefacts.DataManager
	context  completionContext
	options  CompletionOptions
}

func notifyCompletionParticipants(document *lsp.TextDocument, offset int, context completionContext, options CompletionOptions) {
	if len(options.Participants) == 0 {
		return
	}
	if notifyMixinReferenceCompletionParticipants(document, offset, options) {
		return
	}
	switch context.kind {
	case completionProperty:
		r := rangeFromOffsets(document, context.replace.start, offset)
		propertyName := document.Text()[context.replace.start:offset]
		for _, participant := range options.Participants {
			if participant.OnProperty != nil {
				participant.OnProperty(PropertyCompletionContext{PropertyName: propertyName, Range: r})
			}
		}
	case completionValue:
		if context.declaration == nil {
			return
		}
		r := rangeFromOffsets(document, context.replace.start, offset)
		propertyValue := document.Text()[context.replace.start:offset]
		for _, participant := range options.Participants {
			if participant.OnPropertyValue != nil {
				participant.OnPropertyValue(PropertyValueCompletionContext{
					PropertyName:  context.declaration.name,
					PropertyValue: propertyValue,
					Range:         r,
				})
			}
		}
	}
}

func notifyPathCompletionParticipants(document *lsp.TextDocument, offset int, options CompletionOptions) bool {
	if len(options.Participants) == 0 {
		return false
	}
	text := document.Text()
	if pathContext, ok := findURLPathCompletionContext(text, offset); ok {
		r := rangeFromOffsets(document, pathContext.valueStart, pathContext.valueEnd)
		context := URILiteralCompletionContext{
			URIValue: text[pathContext.valueStart:pathContext.valueEnd],
			Position: positionAtByteOffset(document, offset),
			Range:    r,
		}
		notified := false
		for _, participant := range options.Participants {
			if participant.OnURILiteralValue != nil {
				participant.OnURILiteralValue(context)
				notified = true
			}
		}
		return notified
	}
	if pathContext, ok := findImportPathCompletionContext(text, offset); ok {
		r := rangeFromOffsets(document, pathContext.valueStart, pathContext.valueEnd)
		context := ImportPathCompletionContext{
			PathValue: text[pathContext.valueStart:pathContext.valueEnd],
			Position:  positionAtByteOffset(document, offset),
			Range:     r,
		}
		notified := false
		for _, participant := range options.Participants {
			if participant.OnImportPath != nil {
				participant.OnImportPath(context)
				notified = true
			}
		}
		return notified
	}
	return false
}

func notifyMixinReferenceCompletionParticipants(document *lsp.TextDocument, offset int, options CompletionOptions) bool {
	context, ok := mixinReferenceCompletionContext(document.Text(), document.LanguageID, offset)
	if !ok {
		return false
	}
	notified := false
	mixinContext := MixinReferenceCompletionContext{
		MixinName: context.name,
		Range:     rangeFromOffsets(document, context.interval.start, context.interval.end),
	}
	for _, participant := range options.Participants {
		if participant.OnMixinReference != nil {
			participant.OnMixinReference(mixinContext)
			notified = true
		}
	}
	return notified
}

type mixinReferenceContext struct {
	name     string
	interval selectionInterval
}

func mixinReferenceCompletionContext(text, languageID string, offset int) (mixinReferenceContext, bool) {
	switch strings.ToLower(languageID) {
	case "scss":
		return scssMixinReferenceCompletionContext(text, offset)
	case "less":
		return lessMixinReferenceCompletionContext(text, offset)
	default:
		return mixinReferenceContext{}, false
	}
}

func scssMixinReferenceCompletionContext(text string, offset int) (mixinReferenceContext, bool) {
	segmentStart := lastStatementBoundary(text, offset)
	segment := strings.ToLower(text[segmentStart:offset])
	includeIndex := strings.LastIndex(segment, "@include")
	if includeIndex == -1 {
		return mixinReferenceContext{}, false
	}
	nameStart := segmentStart + includeIndex + len("@include")
	for nameStart < offset && isCSSSpace(text[nameStart]) {
		nameStart++
	}
	if open := strings.LastIndex(text[nameStart:offset], "("); open != -1 {
		return mixinReferenceContext{interval: selectionInterval{start: offset, end: offset}}, true
	}
	if nameStart > offset {
		nameStart = offset
	}
	for i := nameStart; i < offset; i++ {
		if !isMixinReferenceNameByte(text[i]) {
			return mixinReferenceContext{}, false
		}
	}
	return mixinReferenceContext{
		name:     text[nameStart:offset],
		interval: selectionInterval{start: nameStart, end: offset},
	}, true
}

func lessMixinReferenceCompletionContext(text string, offset int) (mixinReferenceContext, bool) {
	segmentStart := lastStatementBoundary(text, offset)
	nameStart := segmentStart
	for nameStart < offset && isCSSSpace(text[nameStart]) {
		nameStart++
	}
	if nameStart >= offset || text[nameStart] != '.' && text[nameStart] != '#' {
		return mixinReferenceContext{}, false
	}
	if open := strings.LastIndex(text[nameStart:offset], "("); open != -1 {
		return mixinReferenceContext{interval: selectionInterval{start: offset, end: offset}}, true
	}
	for i := nameStart + 1; i < offset; i++ {
		if !isMixinReferenceNameByte(text[i]) {
			return mixinReferenceContext{}, false
		}
	}
	return mixinReferenceContext{
		name:     text[nameStart:offset],
		interval: selectionInterval{start: nameStart, end: offset},
	}, true
}

func isMixinReferenceNameByte(ch byte) bool {
	return isSelectorIdentifierByte(ch) || ch == '.' || ch == '#'
}

func (b completionBuilder) complete() []lsp.CompletionItem {
	if items, ok := b.preprocessorVariableCompletions(); ok {
		return items
	}
	if items, ok := b.scssExtendSelectorCompletions(); ok {
		return items
	}
	if items, ok := b.scssAtRuleCompletions(); ok {
		if b.context.kind == completionProperty {
			items = append(items, b.propertyCompletions()...)
			items = dedupeCompletionItems(items)
		}
		return items
	}
	if items, ok := b.scssMixinReferenceCompletions(); ok {
		return items
	}
	switch b.context.kind {
	case completionNone:
		return nil
	case completionProperty:
		return b.propertyCompletions()
	case completionValue:
		if replace, ok := b.declarationSelectorPrefixContext(); ok {
			b.context.replace = replace
			b.context.currentWord = b.document.Text()[replace.start:b.context.offset]
			return b.selectorCompletions()
		}
		return b.valueCompletions()
	case completionSelector:
		return b.selectorCompletions()
	default:
		return b.topLevelCompletions()
	}
}

func (b completionBuilder) scssAtRuleCompletions() ([]lsp.CompletionItem, bool) {
	if !strings.EqualFold(b.document.LanguageID, "scss") {
		return nil, false
	}
	replace, ok := atRuleCompletionRange(b.document.Text(), b.context.offset)
	if !ok {
		if b.context.kind != completionProperty || strings.TrimSpace(b.context.currentWord) != "" {
			return nil, false
		}
		replace = selectionInterval{start: b.context.offset, end: b.context.offset}
	}
	proposals := scssAtRuleProposals()
	if b.context.kind == completionTopLevel {
		proposals = append(proposals, scssModuleLoaderProposals()...)
	}
	items := make([]lsp.CompletionItem, 0, len(proposals))
	for _, proposal := range proposals {
		insertText := proposal.insertText
		if insertText == "" {
			insertText = proposal.label
		}
		item := b.itemWithRange(proposal.label, lsp.CompletionItemKindKeyword, insertText, proposal.insertTextFormat, replace)
		if proposal.documentation != "" {
			item.Documentation = proposal.documentation
		}
		items = append(items, item)
	}
	return items, true
}

type scssAtRuleProposal struct {
	label            string
	documentation    string
	insertText       string
	insertTextFormat lsp.InsertTextFormat
}

func scssAtRuleProposals() []scssAtRuleProposal {
	return []scssAtRuleProposal{
		{label: "@extend", documentation: "Inherits the styles of another selector."},
		{label: "@at-root", documentation: "Causes one or more rules to be emitted at the root of the document."},
		{label: "@debug", documentation: "Prints the value of an expression to the standard error output stream. Useful for debugging complicated Sass files."},
		{label: "@warn", documentation: "Prints the value of an expression to the standard error output stream."},
		{label: "@error", documentation: "Throws the value of an expression as a fatal error with stack trace."},
		{label: "@if", documentation: "Includes the body if the expression does not evaluate to false or null.", insertText: "@if ${1:expr} {\n\t$0\n}", insertTextFormat: lsp.InsertTextFormatSnippet},
		{label: "@for", documentation: "For loop that repeatedly outputs a set of styles.", insertText: "@for \\$${1:var} from ${2:start} ${3|to,through|} ${4:end} {\n\t$0\n}", insertTextFormat: lsp.InsertTextFormatSnippet},
		{label: "@each", documentation: "Each loop that sets a variable to each item in a list or map.", insertText: "@each \\$${1:var} in ${2:list} {\n\t$0\n}", insertTextFormat: lsp.InsertTextFormatSnippet},
		{label: "@while", documentation: "While loop that repeatedly outputs nested styles until the condition is false.", insertText: "@while ${1:condition} {\n\t$0\n}", insertTextFormat: lsp.InsertTextFormatSnippet},
		{label: "@mixin", documentation: "Defines styles that can be re-used throughout the stylesheet with @include.", insertText: "@mixin ${1:name} {\n\t$0\n}", insertTextFormat: lsp.InsertTextFormatSnippet},
		{label: "@include", documentation: "Includes the styles defined by another mixin into the current rule."},
		{label: "@function", documentation: "Defines complex operations that can be re-used throughout stylesheets."},
	}
}

func scssModuleLoaderProposals() []scssAtRuleProposal {
	return []scssAtRuleProposal{
		{label: "@use", documentation: "Loads mixins, functions, and variables from other Sass stylesheets as modules.\n\n[Sass documentation](https://sass-lang.com/documentation/at-rules/use)", insertText: "@use $0;", insertTextFormat: lsp.InsertTextFormatSnippet},
		{label: "@forward", documentation: "Loads a Sass stylesheet and makes its mixins, functions, and variables available when this stylesheet is loaded with @use.\n\n[Sass documentation](https://sass-lang.com/documentation/at-rules/forward)", insertText: "@forward $0;", insertTextFormat: lsp.InsertTextFormatSnippet},
	}
}

func (b completionBuilder) topLevelCompletions() []lsp.CompletionItem {
	var items []lsp.CompletionItem
	for _, directive := range b.manager.GetAtDirectives() {
		items = append(items, b.item(directive.Name, lsp.CompletionItemKindKeyword, directive.Name, 0))
	}
	items = append(items, b.selectorCompletions()...)
	return dedupeCompletionItems(items)
}

func (b completionBuilder) selectorCompletions() []lsp.CompletionItem {
	var items []lsp.CompletionItem
	for _, className := range collectCSSClassSelectors(b.document.Text()) {
		items = append(items, b.item(className, lsp.CompletionItemKindKeyword, className, 0))
	}
	for _, pseudoClass := range b.manager.GetPseudoClasses() {
		items = append(items, b.item(pseudoClass.Name, lsp.CompletionItemKindFunction, pseudoClass.Name, 0))
	}
	for _, pseudoElement := range b.manager.GetPseudoElements() {
		items = append(items, b.item(pseudoElement.Name, lsp.CompletionItemKindFunction, pseudoElement.Name, 0))
	}
	for _, tag := range []string{"html", "body", "div", "span", "a", "p", "ul", "ol", "li"} {
		items = append(items, b.item(tag, lsp.CompletionItemKindKeyword, tag, 0))
	}
	return dedupeCompletionItems(items)
}

func (b completionBuilder) propertyCompletions() []lsp.CompletionItem {
	var items []lsp.CompletionItem
	for _, property := range b.manager.GetProperties() {
		insertText := property.Name
		addsColon := !b.propertyHasColonAfterRange()
		if addsColon {
			insertText += ": "
			if b.options.CompletePropertyWithSemicolon && b.context.offset >= b.context.replace.end && !b.propertyHasSemicolonAfterRange() {
				insertText += "$0;"
			}
		}
		item := b.item(property.Name, lsp.CompletionItemKindProperty, insertText, lsp.InsertTextFormatSnippet)
		if documentation := propertyMarkdownDescription(property, HoverOptions{}); documentation.Value != "" {
			item.Documentation = documentation
		}
		item.SortText = propertySortText(property)
		if addsColon && b.options.TriggerPropertyValueCompletion {
			item.Command = &lsp.Command{Title: "Suggest", Command: "editor.action.triggerSuggest"}
		}
		items = append(items, item)
	}
	return dedupeCompletionItems(items)
}

func (b completionBuilder) valueCompletions() []lsp.CompletionItem {
	if b.context.declaration == nil {
		return nil
	}
	if b.ctx != nil && b.ctx.Err() != nil {
		return nil
	}
	text := b.document.Text()
	propertyName := strings.ToLower(b.context.declaration.name)
	property, known := b.manager.GetProperty(propertyName)
	if !known && !strings.HasPrefix(propertyName, "--") {
		return b.reusedValueCompletions(propertyName)
	}
	parallelCollections := shouldParallelizeCompletionData(text)
	var collected valueCompletionData
	if parallelCollections {
		collected = b.collectValueCompletionData(propertyName)
	}

	var items []lsp.CompletionItem
	seen := map[string]bool{}
	addValueWithSortAndRange := func(label string, kind lsp.CompletionItemKind, insertText string, insertFormat lsp.InsertTextFormat, documentation any, sortText string, replace selectionInterval, detail string) {
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		item := b.itemWithRange(label, kind, insertText, insertFormat, replace)
		item.Documentation = documentation
		item.Detail = detail
		item.SortText = sortText
		items = append(items, item)
	}
	addValueWithSort := func(label string, kind lsp.CompletionItemKind, insertText string, insertFormat lsp.InsertTextFormat, documentation any, sortText string) {
		addValueWithSortAndRange(label, kind, insertText, insertFormat, documentation, sortText, b.context.replace, "")
	}
	addValue := func(label string, kind lsp.CompletionItemKind, insertText string, insertFormat lsp.InsertTextFormat, documentation any) {
		addValueWithSort(label, kind, insertText, insertFormat, documentation, "")
	}
	if known {
		if propertyName == "transform" {
			addValue("scaleX()", lsp.CompletionItemKindFunction, "scaleX($1)", lsp.InsertTextFormatSnippet, nil)
		}
		if propertyName == "background-position" {
			for _, label := range []string{"top", "right", "bottom", "left", "center"} {
				addValue(label, lsp.CompletionItemKindValue, label, 0, nil)
			}
		}
		if propertyName == "mask" {
			addValue("round", lsp.CompletionItemKindValue, "round", 0, nil)
		}
		for _, value := range property.Values {
			sortText := " "
			if strings.HasPrefix(value.Name, "-") {
				sortText = " x"
			}
			addValueWithSort(value.Name, lsp.CompletionItemKindValue, value.Name, 0, nil, sortText)
		}
		for _, restriction := range property.Restrictions {
			switch restriction {
			case "length", "line-width":
				b.addUnitCompletions(addValueWithSortAndRange)
			case "color":
				addColorValueCompletions(addValue)
			case "image":
				addValue("url()", lsp.CompletionItemKindFunction, "url($1)", lsp.InsertTextFormatSnippet, nil)
			case "line-style":
				addValue("dotted", lsp.CompletionItemKindValue, "dotted", 0, nil)
			}
		}
		addValue("inherit", lsp.CompletionItemKindValue, "inherit", 0, nil)
		addValue("initial", lsp.CompletionItemKindValue, "initial", 0, nil)
		addValue("unset", lsp.CompletionItemKindValue, "unset", 0, nil)
	}
	reusedValues := collected.reused
	if !parallelCollections {
		reusedValues = b.collectReusedValues(propertyName)
	}
	for _, reused := range reusedValues {
		addValue(reused.value, reused.kind, reused.value, 0, nil)
	}
	cssVariables := collected.cssVariables
	if !parallelCollections {
		cssVariables = collectCSSVariables(text)
	}
	for _, variable := range cssVariables {
		kind := lsp.CompletionItemKindVariable
		if isColorLike(variable.value) {
			kind = lsp.CompletionItemKindColor
		}
		insertText := "var(" + variable.name + ")"
		if b.isInsideVarFunction() {
			insertText = variable.name
		}
		addValue(variable.name, kind, insertText, 0, variable.value)
	}
	if strings.EqualFold(b.document.LanguageID, "scss") {
		for _, fn := range scssBuiltInFunctionCompletions() {
			addValue(fn.label, lsp.CompletionItemKindFunction, fn.insertText, lsp.InsertTextFormatSnippet, nil)
		}
		scssFunctions := collected.scssFunctions
		if !parallelCollections {
			scssFunctions = collectSCSSCallables(text, "function")
		}
		for _, callable := range scssFunctions {
			addValue(callable.name, lsp.CompletionItemKindFunction, scssCallSnippet(callable.name, callable.params), lsp.InsertTextFormatSnippet, nil)
		}
		replace := preprocessorVariableReplaceRange(text, b.context.offset, '$')
		scssVariables := collected.scssVariables
		if !parallelCollections {
			scssVariables = collectSCSSVariables(text)
		}
		for _, variable := range scssVariables {
			addValueWithSortAndRange(variable.name, lsp.CompletionItemKindVariable, variable.name, 0, variable.value, "", replace, variable.detail)
		}
	}
	if strings.EqualFold(b.document.LanguageID, "less") {
		for _, fn := range scssBuiltInFunctionCompletions() {
			addValue(fn.label, lsp.CompletionItemKindFunction, fn.insertText, lsp.InsertTextFormatSnippet, nil)
		}
		replace := preprocessorVariableReplaceRange(text, b.context.offset, '@')
		lessVariables := collected.lessVariables
		if !parallelCollections {
			lessVariables = collectLESSVariables(text)
		}
		for _, variable := range lessVariables {
			addValueWithSortAndRange(variable.name, lsp.CompletionItemKindVariable, variable.name, 0, variable.value, "", replace, variable.detail)
		}
	}
	return items
}

type valueCompletionData struct {
	reused        []reusedCSSValue
	cssVariables  []cssVariable
	scssFunctions []scssCallable
	scssVariables []cssVariable
	lessVariables []cssVariable
}

func (b completionBuilder) collectValueCompletionData(propertyName string) valueCompletionData {
	text := b.document.Text()
	languageID := strings.ToLower(b.document.LanguageID)
	if b.ctx != nil && b.ctx.Err() != nil {
		return valueCompletionData{}
	}

	var data valueCompletionData
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		data.reused = b.collectReusedValues(propertyName)
	}()
	go func() {
		defer wg.Done()
		data.cssVariables = collectCSSVariables(text)
	}()
	switch languageID {
	case "scss":
		wg.Add(2)
		go func() {
			defer wg.Done()
			data.scssFunctions = collectSCSSCallables(text, "function")
		}()
		go func() {
			defer wg.Done()
			data.scssVariables = collectSCSSVariables(text)
		}()
	case "less":
		wg.Add(1)
		go func() {
			defer wg.Done()
			data.lessVariables = collectLESSVariables(text)
		}()
	}
	wg.Wait()
	return data
}

func shouldParallelizeCompletionData(text string) bool {
	return shouldParallelize(text, nil)
}

func (b completionBuilder) addUnitCompletions(add func(string, lsp.CompletionItemKind, string, lsp.InsertTextFormat, any, string, selectionInterval, string)) {
	stem := "0"
	replace := b.context.replace
	if unitContext, ok := numericUnitCompletionContext(b.document.Text(), *b.context.declaration, b.context.offset); ok {
		stem = unitContext.stem
		replace = unitContext.replace
	}
	for _, unit := range []string{"cm", "em", "px"} {
		value := stem + unit
		add(value, lsp.CompletionItemKindUnit, value, 0, nil, "", replace, "")
	}
}

type scssFunctionCompletion struct {
	label      string
	insertText string
}

type scssModuleCompletion struct {
	label         string
	documentation string
}

func scssBuiltInFunctionCompletions() []scssFunctionCompletion {
	return []scssFunctionCompletion{
		{label: "darken", insertText: `darken(\$color: ${1:#000000}, \$amount: ${2:0})`},
		{label: "desaturate", insertText: `desaturate(\$color: ${1:#000000}, \$amount: ${2:0})`},
	}
}

func scssBuiltInModuleCompletions() []scssModuleCompletion {
	return []scssModuleCompletion{
		{label: "sass:math", documentation: "Provides functions that operate on numbers."},
		{label: "sass:string", documentation: "Makes it easy to combine, search, or split apart strings."},
		{label: "sass:color", documentation: "Generates new colors based on existing ones, making it easy to build color themes."},
		{label: "sass:list", documentation: "Lets you access and modify values in lists."},
		{label: "sass:map", documentation: "Makes it possible to look up the value associated with a key in a map, and much more."},
		{label: "sass:selector", documentation: "Provides access to Sass's powerful selector engine."},
		{label: "sass:meta", documentation: "Exposes the details of Sass's inner workings."},
	}
}

func (b completionBuilder) scssMixinReferenceCompletions() ([]lsp.CompletionItem, bool) {
	if !strings.EqualFold(b.document.LanguageID, "scss") {
		return nil, false
	}
	text := b.document.Text()
	replace := completionReplaceRange(text, b.context.offset, false)
	segmentStart := lastStatementBoundary(text, b.context.offset)
	segment := strings.ToLower(text[segmentStart:b.context.offset])
	includeIndex := strings.LastIndex(segment, "@include")
	if includeIndex == -1 {
		return nil, false
	}
	afterInclude := segmentStart + includeIndex + len("@include")
	if b.context.offset < afterInclude {
		return nil, false
	}
	for i := afterInclude; i < b.context.offset; i++ {
		if !isCSSSpace(text[i]) && i < replace.start {
			return nil, false
		}
	}
	callables := collectSCSSCallables(text, "mixin")
	items := make([]lsp.CompletionItem, 0, len(callables))
	for _, callable := range callables {
		items = append(items, b.itemWithRange(callable.name, lsp.CompletionItemKindFunction, scssCallSnippet(callable.name, callable.params), lsp.InsertTextFormatSnippet, replace))
	}
	return items, true
}

func (b completionBuilder) scssExtendSelectorCompletions() ([]lsp.CompletionItem, bool) {
	if !strings.EqualFold(b.document.LanguageID, "scss") {
		return nil, false
	}
	text := b.document.Text()
	replace := completionReplaceRange(text, b.context.offset, false)
	segmentStart := lastStatementBoundary(text, b.context.offset)
	segment := strings.ToLower(text[segmentStart:b.context.offset])
	extendIndex := strings.LastIndex(segment, "@extend")
	if extendIndex == -1 {
		return nil, false
	}
	afterExtend := segmentStart + extendIndex + len("@extend")
	if b.context.offset < afterExtend {
		return nil, false
	}
	for i := afterExtend; i < b.context.offset; i++ {
		if !isCSSSpace(text[i]) && i < replace.start {
			return nil, false
		}
	}
	classes := collectCSSClassSelectors(text[:segmentStart])
	items := make([]lsp.CompletionItem, 0, len(classes))
	for _, className := range classes {
		items = append(items, b.itemWithRange(className, lsp.CompletionItemKindKeyword, className, 0, replace))
	}
	return items, true
}

func (b completionBuilder) preprocessorVariableCompletions() ([]lsp.CompletionItem, bool) {
	prefix, ok := preprocessorVariablePrefix(b.document.LanguageID)
	if !ok {
		return nil, false
	}
	text := b.document.Text()
	replace := preprocessorVariableReplaceRange(text, b.context.offset, prefix)
	hasPrefix := replace.start < len(text) && text[replace.start] == prefix && replace.start <= b.context.offset
	if !hasPrefix && !(prefix == '$' && insideSCSSInterpolation(text, b.context.offset)) {
		return nil, false
	}
	if prefix == '@' && b.context.kind != completionValue {
		return nil, false
	}
	var variables []cssVariable
	switch strings.ToLower(b.document.LanguageID) {
	case "scss":
		variables = collectSCSSVariables(text)
	case "less":
		variables = collectLESSVariables(text)
	}
	items := make([]lsp.CompletionItem, 0, len(variables))
	seen := map[string]bool{}
	for _, variable := range variables {
		if variable.name == "" || seen[variable.name] {
			continue
		}
		seen[variable.name] = true
		item := b.itemWithRange(variable.name, lsp.CompletionItemKindVariable, variable.name, 0, replace)
		item.Documentation = variable.value
		item.Detail = variable.detail
		items = append(items, item)
	}
	return items, true
}

func (b completionBuilder) isInsideVarFunction() bool {
	if b.context.declaration == nil {
		return false
	}
	text := b.document.Text()
	if b.context.replace.start < b.context.declaration.valueStart || b.context.replace.start > len(text) {
		return false
	}
	prefix := strings.ToLower(text[b.context.declaration.valueStart:b.context.replace.start])
	index := strings.LastIndex(prefix, "var(")
	if index == -1 {
		return false
	}
	return !strings.Contains(prefix[index+len("var("):], ")")
}

type reusedCSSValue struct {
	value string
	kind  lsp.CompletionItemKind
}

func (b completionBuilder) reusedValueCompletions(propertyName string) []lsp.CompletionItem {
	var items []lsp.CompletionItem
	for _, reused := range b.collectReusedValues(propertyName) {
		items = append(items, b.item(reused.value, reused.kind, reused.value, 0))
	}
	return items
}

func (b completionBuilder) collectReusedValues(propertyName string) []reusedCSSValue {
	text := b.document.Text()
	blocks := parseCSSBlocks(text)
	if shouldParallelizeBlockWork(text, blocks) {
		values := parallelBlockMap(blocks, parallelWorkerCount(len(blocks)), func(chunk []cssBlock) []reusedCSSValue {
			return b.collectReusedValuesForBlocks(text, propertyName, chunk)
		})
		return dedupeReusedValues(values)
	}
	return b.collectReusedValuesForBlocks(text, propertyName, blocks)
}

func (b completionBuilder) collectReusedValuesForBlocks(text string, propertyName string, blocks []cssBlock) []reusedCSSValue {
	seen := map[string]bool{}
	var result []reusedCSSValue
	for _, block := range blocks {
		for _, declaration := range parseDeclarations(text, block.bodyStart, block.bodyEnd) {
			if b.context.offset >= declaration.valueStart && b.context.offset <= declaration.offset+declaration.length {
				continue
			}
			if strings.ToLower(declaration.name) == propertyName && strings.TrimSpace(declaration.value) != "" {
				value := strings.TrimSpace(declaration.value)
				if !seen[value] {
					seen[value] = true
					kind := lsp.CompletionItemKindValue
					if isColorLike(value) {
						kind = lsp.CompletionItemKindColor
					}
					result = append(result, reusedCSSValue{value: value, kind: kind})
				}
			}
		}
	}
	return result
}

func dedupeReusedValues(values []reusedCSSValue) []reusedCSSValue {
	seen := map[string]bool{}
	result := make([]reusedCSSValue, 0, len(values))
	for _, value := range values {
		if seen[value.value] {
			continue
		}
		seen[value.value] = true
		result = append(result, value)
	}
	return result
}

func (b completionBuilder) item(label string, kind lsp.CompletionItemKind, insertText string, insertTextFormat lsp.InsertTextFormat) lsp.CompletionItem {
	return b.itemWithRange(label, kind, insertText, insertTextFormat, b.context.replace)
}

func (b completionBuilder) itemWithRange(label string, kind lsp.CompletionItemKind, insertText string, insertTextFormat lsp.InsertTextFormat, replace selectionInterval) lsp.CompletionItem {
	edit := lsp.Replace(rangeFromOffsets(b.document, replace.start, replace.end), insertText)
	return lsp.CompletionItem{
		Label:            label,
		Kind:             kind,
		TextEdit:         &edit,
		InsertTextFormat: insertTextFormat,
	}
}

func (b completionBuilder) propertyHasColonAfterRange() bool {
	text := b.document.Text()
	i := b.context.replace.end
	for i < len(text) && isCSSSpace(text[i]) {
		i++
	}
	return i < len(text) && text[i] == ':'
}

func (b completionBuilder) propertyHasSemicolonAfterRange() bool {
	text := b.document.Text()
	i := b.context.replace.end
	for i < len(text) && isCSSSpace(text[i]) {
		i++
	}
	return i < len(text) && text[i] == ';'
}

func (b completionBuilder) declarationSelectorPrefixContext() (selectionInterval, bool) {
	if b.context.declaration == nil || !selectorPrefixContextAt(b.document.Text(), b.context.offset) {
		return selectionInterval{}, false
	}
	name := strings.ToLower(strings.TrimSpace(b.context.declaration.name))
	if name == "" {
		return selectionInterval{}, false
	}
	if _, known := b.manager.GetProperty(name); known {
		return selectionInterval{}, false
	}
	if !selectorHeadName(name) {
		return selectionInterval{}, false
	}
	return completionReplaceRange(b.document.Text(), b.context.offset, true), true
}

func selectorHeadName(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "&") {
		return true
	}
	switch name {
	case "a", "body", "div", "html", "li", "ol", "p", "span", "ul":
		return true
	default:
		return false
	}
}

func analyzeCompletionContext(text string, offset int) completionContext {
	replace := completionReplaceRange(text, offset, false)
	ctx := completionContext{kind: completionTopLevel, replace: replace, currentWord: text[replace.start:offset], offset: offset}
	blocks := completionCSSBlocks(text, offset)
	var selected *cssBlock
	for i := range blocks {
		block := blocks[i]
		if offset >= block.bodyStart && offset <= block.bodyEnd {
			if selected == nil || block.start >= selected.start {
				selected = &blocks[i]
			}
		}
	}
	if selected == nil {
		for i := range blocks {
			block := blocks[i]
			if offset >= block.headStart && offset <= block.start {
				if selected == nil || block.start >= selected.start {
					selected = &blocks[i]
				}
			}
		}
	}
	if selected == nil {
		if selectorPrefixContextAt(text, offset) {
			ctx.kind = completionSelector
			ctx.replace = completionReplaceRange(text, offset, true)
			ctx.currentWord = text[ctx.replace.start:offset]
		}
		return ctx
	}
	ctx.block = selected
	if offset < selected.start {
		ctx.kind = completionSelector
		ctx.replace = completionReplaceRange(text, offset, true)
		ctx.currentWord = text[ctx.replace.start:offset]
		return ctx
	}
	if offset < selected.bodyStart || offset > selected.bodyEnd {
		return ctx
	}
	if offset > selected.bodyStart && offset <= len(text) && text[offset-1] == ';' {
		ctx.kind = completionNone
		return ctx
	}
	declarations := parseDeclarations(text, selected.bodyStart, selected.bodyEnd)
	for _, declaration := range declarations {
		if offset >= declaration.offset && offset <= declaration.offset+declaration.length {
			applyDeclarationCompletionContext(&ctx, text, declaration, offset)
			return ctx
		}
	}
	if declaration, ok := completionDeclarationAt(text, selected, offset); ok {
		applyDeclarationCompletionContext(&ctx, text, declaration, offset)
		return ctx
	}
	if selectorPrefixContextAt(text, offset) {
		ctx.kind = completionSelector
		ctx.replace = completionReplaceRange(text, offset, true)
		ctx.currentWord = text[ctx.replace.start:offset]
		return ctx
	}
	if atRuleContainsStyleRules(selected.head) {
		ctx.kind = completionSelector
		ctx.replace = completionReplaceRange(text, offset, true)
		ctx.currentWord = text[ctx.replace.start:offset]
		return ctx
	}
	ctx.kind = completionProperty
	ctx.replace = completionReplaceRange(text, offset, false)
	ctx.currentWord = text[ctx.replace.start:offset]
	return ctx
}

func applyDeclarationCompletionContext(ctx *completionContext, text string, declaration cssDeclaration, offset int) {
	ctx.declaration = &declaration
	declarationEnd := min(len(text), declaration.offset+declaration.length)
	colon := strings.Index(text[declaration.offset:declarationEnd], ":")
	if colon != -1 && offset > declaration.offset+colon {
		ctx.kind = completionValue
		ctx.replace = valueCompletionRange(text, declaration, offset)
		ctx.currentWord = text[ctx.replace.start:offset]
	} else {
		ctx.kind = completionProperty
		ctx.replace = propertyCompletionRange(text, declaration, offset)
		ctx.currentWord = text[ctx.replace.start:offset]
	}
}

func completionDeclarationAt(text string, block *cssBlock, offset int) (cssDeclaration, bool) {
	segmentStart := block.bodyStart
	segmentEnd := min(block.bodyEnd, len(text))
	parenDepth := 0
	for i := block.bodyStart; i < segmentEnd; i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		switch text[i] {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case ';':
			if parenDepth != 0 {
				continue
			}
			if i < offset {
				segmentStart = i + 1
				continue
			}
			segmentEnd = i
			i = segmentEnd
		}
	}
	declaration, ok := parseDeclarationSegment(text, segmentStart, segmentEnd)
	if !ok {
		return cssDeclaration{}, false
	}
	if offset < declaration.offset || offset > segmentEnd {
		return cssDeclaration{}, false
	}
	declaration.length = segmentEnd - declaration.offset
	return declaration, true
}

func propertyCompletionRange(text string, declaration cssDeclaration, offset int) selectionInterval {
	start := declaration.nameOffset
	end := declaration.nameOffset + len(declaration.name)
	if offset < end {
		end = offset
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
	}
	return selectionInterval{start: start, end: end}
}

func valueCompletionRange(text string, declaration cssDeclaration, offset int) selectionInterval {
	start := offset
	for start > declaration.valueStart && isSelectorIdentifierByte(text[start-1]) {
		start--
	}
	end := offset
	valueEnd := declaration.valueStart + len(declaration.value)
	for end < valueEnd && isSelectorIdentifierByte(text[end]) {
		end++
	}
	return selectionInterval{start: start, end: end}
}

type numericUnitContext struct {
	stem    string
	replace selectionInterval
}

func numericUnitCompletionContext(text string, declaration cssDeclaration, offset int) (numericUnitContext, bool) {
	valueEnd := min(len(text), declaration.valueStart+len(declaration.value))
	tokenStart := offset
	for tokenStart > declaration.valueStart && !isValueDelimiter(text[tokenStart-1]) {
		tokenStart--
	}
	tokenEnd := offset
	for tokenEnd < valueEnd && !isValueDelimiter(text[tokenEnd]) {
		tokenEnd++
	}
	stem := numericStemBeforeCursor(text[tokenStart:offset])
	if stem == "" {
		return numericUnitContext{}, false
	}
	return numericUnitContext{
		stem:    stem,
		replace: selectionInterval{start: tokenStart, end: tokenEnd},
	}, true
}

func numericStemBeforeCursor(value string) string {
	if value == "" {
		return ""
	}
	i := 0
	if value[i] == '-' || value[i] == '+' {
		i++
	}
	digitSeen := false
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
		digitSeen = true
	}
	if i < len(value) && value[i] == '.' {
		i++
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
			digitSeen = true
		}
	}
	if !digitSeen {
		return ""
	}
	return value[:i]
}

func completionReplaceRange(text string, offset int, selector bool) selectionInterval {
	start := offset
	for start > 0 && isSelectorIdentifierByte(text[start-1]) {
		start--
	}
	if selector {
		for start > 0 && (text[start-1] == ':' || text[start-1] == '.' || text[start-1] == '#') {
			start--
		}
	}
	end := offset
	for end < len(text) && isSelectorIdentifierByte(text[end]) {
		end++
	}
	return selectionInterval{start: start, end: end}
}

func atRuleCompletionRange(text string, offset int) (selectionInterval, bool) {
	start := offset
	for start > 0 && isSelectorIdentifierByte(text[start-1]) {
		start--
	}
	if start == 0 || text[start-1] != '@' {
		return selectionInterval{}, false
	}
	start--
	end := offset
	for end < len(text) && isSelectorIdentifierByte(text[end]) {
		end++
	}
	return selectionInterval{start: start, end: end}, true
}

func selectorPrefixContextAt(text string, offset int) bool {
	start := offset
	for start > 0 && isSelectorIdentifierByte(text[start-1]) {
		start--
	}
	return start > 0 && (text[start-1] == ':' || text[start-1] == '.' || text[start-1] == '#')
}

func atRuleContainsStyleRules(head string) bool {
	lower := strings.ToLower(strings.TrimSpace(head))
	for _, prefix := range []string{"@media", "@supports", "@layer", "@scope", "@container"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func completionCSSBlocks(text string, offset int) []cssBlock {
	blocks := parseCSSBlocks(text)
	for _, block := range blocks {
		if offset >= block.headStart && offset <= block.end+1 {
			return blocks
		}
	}

	var stack []int
	for i := 0; i < offset && i < len(text); i++ {
		switch text[i] {
		case '/', '"', '\'':
			next := skipCSSIgnored(text, i)
			if next != i {
				i = next
				continue
			}
		case '{':
			stack = append(stack, i)
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) == 0 {
		return blocks
	}

	start := stack[len(stack)-1]
	headStart, headEnd := blockHeadRange(text, start)
	blocks = append(blocks, cssBlock{
		head:      strings.TrimSpace(text[headStart:headEnd]),
		headStart: headStart,
		start:     start,
		bodyStart: start + 1,
		bodyEnd:   len(text),
		end:       len(text),
	})
	return blocks
}

func addColorValueCompletions(add func(string, lsp.CompletionItemKind, string, lsp.InsertTextFormat, any)) {
	add("rgb", lsp.CompletionItemKindFunction, "rgb(${1:red}, ${2:green}, ${3:blue})", lsp.InsertTextFormatSnippet, nil)
	add("rgba", lsp.CompletionItemKindFunction, "rgba(${1:red}, ${2:green}, ${3:blue}, ${4:alpha})", lsp.InsertTextFormatSnippet, nil)
	add("rgb relative", lsp.CompletionItemKindFunction, "rgb(from ${1:color} ${2:r} ${3:g} ${4:b})", lsp.InsertTextFormatSnippet, nil)
	for _, name := range []string{"red", "cyan", "darkcyan", "aqua", "black", "white", "transparent", "currentColor"} {
		add(name, lsp.CompletionItemKindColor, name, 0, nil)
	}
}

func collectCSSClassSelectors(text string) []string {
	seen := map[string]bool{}
	var result []string
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != '.' || i+1 >= len(text) || !isSelectorIdentifierByte(text[i+1]) {
			continue
		}
		if i > 0 && (isSelectorIdentifierByte(text[i-1]) || text[i-1] == '-') {
			continue
		}
		end := i + 2
		for end < len(text) && isSelectorIdentifierByte(text[end]) {
			end++
		}
		name := text[i:end]
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
		i = end - 1
	}
	return result
}

type cssVariable struct {
	name   string
	value  string
	detail string
}

type scssCallable struct {
	kind   string
	name   string
	params []cssVariable
}

var (
	scssCallablePattern    = regexp.MustCompile(`(?i)@(mixin|function)\s+([A-Za-z_][A-Za-z0-9_-]*)\s*\(`)
	scssForVariablePattern = regexp.MustCompile(`(?i)@for\s+(\$[A-Za-z_][A-Za-z0-9_-]*)`)
	lessMixinPattern       = regexp.MustCompile(`([.#][A-Za-z_][A-Za-z0-9_-]*)\s*\(`)
)

func collectCSSVariables(text string) []cssVariable {
	blocks := completionCSSBlocks(text, len(text))
	if shouldParallelizeBlockWork(text, blocks) {
		variables := parallelBlockMap(blocks, parallelWorkerCount(len(blocks)), func(chunk []cssBlock) []cssVariable {
			return collectCSSVariablesForBlocks(text, chunk)
		})
		return dedupeCSSVariables(variables)
	}
	return collectCSSVariablesForBlocks(text, blocks)
}

func collectCSSVariablesForBlocks(text string, blocks []cssBlock) []cssVariable {
	seen := map[string]bool{}
	var result []cssVariable
	for _, block := range blocks {
		for _, declaration := range parseDeclarations(text, block.bodyStart, block.bodyEnd) {
			if strings.HasPrefix(declaration.name, "--") && !seen[declaration.name] {
				seen[declaration.name] = true
				result = append(result, cssVariable{name: declaration.name, value: strings.TrimSpace(declaration.value)})
			}
			for _, token := range valueHighlightTokens(declaration) {
				if strings.HasPrefix(token.text, "--") && !seen[token.text] {
					seen[token.text] = true
					result = append(result, cssVariable{name: token.text})
				}
			}
		}
	}
	return result
}

func collectSCSSVariables(text string) []cssVariable {
	return dedupeCSSVariables(append(append(
		collectSCSSCallableArguments(text),
		collectSCSSForVariables(text)...,
	), collectPreprocessorVariableDeclarations(text, '$')...))
}

func collectSCSSCallables(text string, kind string) []scssCallable {
	var result []scssCallable
	for _, match := range scssCallablePattern.FindAllStringSubmatchIndex(text, -1) {
		callableKind := strings.ToLower(text[match[2]:match[3]])
		if callableKind != kind {
			continue
		}
		name := text[match[4]:match[5]]
		open := match[1] - 1
		close := matchingParenIndex(text, open)
		if close == -1 {
			continue
		}
		result = append(result, scssCallable{
			kind:   callableKind,
			name:   name,
			params: collectPreprocessorParameters(text[open+1:close], '$', ""),
		})
	}
	return result
}

func collectLESSVariables(text string) []cssVariable {
	return dedupeCSSVariables(append(
		collectLESSMixinArguments(text),
		collectPreprocessorVariableDeclarations(text, '@')...,
	))
}

func dedupeCSSVariables(variables []cssVariable) []cssVariable {
	seen := map[string]bool{}
	var result []cssVariable
	for _, variable := range variables {
		if variable.name == "" || seen[variable.name] {
			continue
		}
		seen[variable.name] = true
		result = append(result, variable)
	}
	return result
}

func collectSCSSCallableArguments(text string) []cssVariable {
	var result []cssVariable
	for _, match := range scssCallablePattern.FindAllStringSubmatchIndex(text, -1) {
		name := text[match[4]:match[5]]
		open := match[1] - 1
		close := matchingParenIndex(text, open)
		if close == -1 {
			continue
		}
		result = append(result, collectPreprocessorParameters(text[open+1:close], '$', "argument from '"+name+"'")...)
	}
	return result
}

func collectLESSMixinArguments(text string) []cssVariable {
	var result []cssVariable
	for _, match := range lessMixinPattern.FindAllStringSubmatchIndex(text, -1) {
		name := text[match[2]:match[3]]
		open := match[1] - 1
		close := matchingParenIndex(text, open)
		if close == -1 {
			continue
		}
		result = append(result, collectPreprocessorParameters(text[open+1:close], '@', "argument from '"+name+"'")...)
	}
	return result
}

func scssCallSnippet(name string, params []cssVariable) string {
	if len(params) == 0 {
		return name + "()"
	}
	parts := make([]string, 0, len(params))
	for i, param := range params {
		parts = append(parts, fmt.Sprintf("${%d:%s}", i+1, param.name))
	}
	return name + "(" + strings.Join(parts, ", ") + ")"
}

func lastStatementBoundary(text string, offset int) int {
	for i := offset - 1; i >= 0; i-- {
		switch text[i] {
		case ';', '{', '}', '\n', '\r':
			return i + 1
		}
	}
	return 0
}

func collectSCSSForVariables(text string) []cssVariable {
	var result []cssVariable
	for _, match := range scssForVariablePattern.FindAllStringSubmatchIndex(text, -1) {
		result = append(result, cssVariable{name: text[match[2]:match[3]]})
	}
	return result
}

func collectPreprocessorParameters(text string, prefix byte, detail string) []cssVariable {
	var result []cssVariable
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != prefix {
			continue
		}
		nameStart := i
		nameEnd := i + 1
		for nameEnd < len(text) && isSelectorIdentifierByte(text[nameEnd]) {
			nameEnd++
		}
		if nameEnd == nameStart+1 {
			continue
		}
		valueStart := nameEnd
		for valueStart < len(text) && isCSSSpace(text[valueStart]) {
			valueStart++
		}
		value := ""
		if valueStart < len(text) && text[valueStart] == ':' {
			valueStart++
			valueEnd := scanPreprocessorParameterValueEnd(text, valueStart)
			value = strings.TrimSpace(text[valueStart:valueEnd])
			i = valueEnd
		} else {
			i = nameEnd
		}
		result = append(result, cssVariable{name: text[nameStart:nameEnd], value: value, detail: detail})
	}
	return result
}

func collectPreprocessorVariableDeclarations(text string, prefix byte) []cssVariable {
	var result []cssVariable
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] != prefix {
			continue
		}
		nameStart := i
		nameEnd := i + 1
		for nameEnd < len(text) && isSelectorIdentifierByte(text[nameEnd]) {
			nameEnd++
		}
		if nameEnd == nameStart+1 {
			continue
		}
		colon := nameEnd
		for colon < len(text) && isCSSSpace(text[colon]) {
			colon++
		}
		if colon >= len(text) || text[colon] != ':' {
			continue
		}
		valueStart := colon + 1
		valueEnd := scanPreprocessorDeclarationValueEnd(text, valueStart)
		result = append(result, cssVariable{name: text[nameStart:nameEnd], value: strings.TrimSpace(text[valueStart:valueEnd])})
		i = valueEnd
	}
	return result
}

func scanPreprocessorParameterValueEnd(text string, start int) int {
	for i := start; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		switch text[i] {
		case ',', ')':
			return i
		}
	}
	return len(text)
}

func scanPreprocessorDeclarationValueEnd(text string, start int) int {
	braceDepth := 0
	parenDepth := 0
	bracketDepth := 0
	for i := start; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		switch text[i] {
		case '{':
			braceDepth++
		case '}':
			if braceDepth == 0 {
				return i
			}
			braceDepth--
		case '(':
			parenDepth++
		case ')':
			if parenDepth == 0 {
				return i
			}
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case ';':
			if braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 {
				return i
			}
		case '\n', '\r':
			if braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 {
				return i
			}
		}
	}
	return len(text)
}

func matchingParenIndex(text string, open int) int {
	if open < 0 || open >= len(text) || text[open] != '(' {
		return -1
	}
	depth := 0
	for i := open; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func preprocessorVariablePrefix(languageID string) (byte, bool) {
	switch strings.ToLower(languageID) {
	case "scss":
		return '$', true
	case "less":
		return '@', true
	default:
		return 0, false
	}
}

func preprocessorVariableReplaceRange(text string, offset int, prefix byte) selectionInterval {
	start := offset
	for start > 0 && isSelectorIdentifierByte(text[start-1]) {
		start--
	}
	if start > 0 && text[start-1] == prefix {
		start--
	}
	end := offset
	if end < len(text) && text[end] == prefix {
		end++
	}
	for end < len(text) && isSelectorIdentifierByte(text[end]) {
		end++
	}
	return selectionInterval{start: start, end: end}
}

func insideSCSSInterpolation(text string, offset int) bool {
	open := strings.LastIndex(text[:offset], "#{")
	if open == -1 {
		return false
	}
	for i := open + 2; i < offset; i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] == '}' {
			return false
		}
	}
	for i := offset; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if text[i] == '}' {
			return true
		}
		if text[i] == '{' || text[i] == ';' || text[i] == '\n' || text[i] == '\r' {
			return false
		}
	}
	return false
}

func isColorLike(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if languagefacts.ColorFromHex(value) != nil {
		return true
	}
	for _, name := range []string{"red", "black", "white", "cyan", "darkcyan", "currentcolor", "transparent"} {
		if lower == name {
			return true
		}
	}
	return false
}

func propertySortText(property languagefacts.PropertyData) string {
	prefix := "d"
	if strings.HasPrefix(property.Name, "-") {
		prefix = "x"
	}
	relevance := 50.0
	if property.Relevance != nil {
		relevance = *property.Relevance
	}
	if relevance < 0 {
		relevance = 0
	}
	if relevance > 99 {
		relevance = 99
	}
	return fmt.Sprintf("%s_%x", prefix, 255-int(relevance))
}

func dedupeCompletionItems(items []lsp.CompletionItem) []lsp.CompletionItem {
	seen := map[string]bool{}
	result := make([]lsp.CompletionItem, 0, len(items))
	for _, item := range items {
		if item.Label == "" || seen[item.Label] {
			continue
		}
		seen[item.Label] = true
		result = append(result, item)
	}
	return result
}

type pathCompletionContext struct {
	valueStart    int
	valueEnd      int
	importPath    bool
	importKeyword string
}

func completeSCSSModuleBuiltIns(document *lsp.TextDocument, offset int) (lsp.CompletionList, bool) {
	if !strings.EqualFold(document.LanguageID, "scss") {
		return lsp.CompletionList{}, false
	}
	text := document.Text()
	pathContext, ok := findImportPathCompletionContext(text, offset)
	if !ok || (pathContext.importKeyword != "@use" && pathContext.importKeyword != "@forward") {
		return lsp.CompletionList{}, false
	}
	fullStart, fullEnd, ok := pathCompletionValueBounds(text, pathContext)
	if !ok {
		return lsp.CompletionList{}, false
	}
	if offset < fullStart {
		offset = fullStart
	}
	if offset > fullEnd {
		offset = fullEnd
	}
	valueBeforeCursor := text[fullStart:offset]
	if isSassFilesystemModuleReference(valueBeforeCursor) {
		return lsp.CompletionList{}, false
	}
	replaceRange := rangeFromOffsets(document, pathContext.valueStart, pathContext.valueEnd)
	items := make([]lsp.CompletionItem, 0, len(scssBuiltInModuleCompletions()))
	for _, module := range scssBuiltInModuleCompletions() {
		insertText := "'" + module.label + "'"
		edit := lsp.Replace(replaceRange, insertText)
		items = append(items, lsp.CompletionItem{
			Label:         module.label,
			Kind:          lsp.CompletionItemKindModule,
			Documentation: scssModuleDocumentation(module),
			TextEdit:      &edit,
		})
	}
	return lsp.CompletionList{IsIncomplete: false, Items: items}, true
}

func scssModuleDocumentation(module scssModuleCompletion) lsp.MarkupContent {
	return lsp.MarkupContent{
		Kind:  lsp.MarkupKindMarkdown,
		Value: module.documentation + "\n\n[Sass documentation](https://sass-lang.com/documentation/modules/" + strings.TrimPrefix(module.label, "sass:") + ")",
	}
}

func isSassFilesystemModuleReference(value string) bool {
	return strings.HasPrefix(value, ".") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~")
}

func completePath(ctx context.Context, document *lsp.TextDocument, offset int, options CompletionOptions) (lsp.CompletionList, bool, error) {
	if options.ReadDirectory == nil || options.ResolveReference == nil {
		return lsp.CompletionList{}, false, nil
	}
	text := document.Text()
	pathContext, ok := findURLPathCompletionContext(text, offset)
	if !ok {
		pathContext, ok = findImportPathCompletionContext(text, offset)
	}
	if !ok {
		return lsp.CompletionList{}, false, nil
	}
	items, err := pathCompletionItems(ctx, document, pathContext, offset, options)
	if err != nil {
		return lsp.CompletionList{}, true, err
	}
	return lsp.CompletionList{IsIncomplete: false, Items: items}, true, nil
}

func findURLPathCompletionContext(text string, offset int) (pathCompletionContext, bool) {
	lowerPrefix := strings.ToLower(text[:offset])
	open := strings.LastIndex(lowerPrefix, "url(")
	if open == -1 {
		return pathCompletionContext{}, false
	}
	contentStart := open + len("url(")
	if strings.Contains(text[contentStart:offset], ")") {
		return pathCompletionContext{}, false
	}
	for contentStart < len(text) && isCSSSpace(text[contentStart]) {
		contentStart++
	}
	if contentStart >= len(text) || offset < contentStart {
		return pathCompletionContext{}, false
	}
	if text[contentStart] == '"' || text[contentStart] == '\'' {
		quote := text[contentStart]
		valueEnd := len(text)
		for i := offset; i < len(text); i++ {
			if text[i] == quote {
				valueEnd = i + 1
				break
			}
			if text[i] == ')' || text[i] == ';' || text[i] == '\n' || text[i] == '\r' {
				valueEnd = i
				break
			}
		}
		return pathCompletionContext{valueStart: contentStart, valueEnd: valueEnd}, true
	}
	valueEnd := len(text)
	for i := offset; i < len(text); i++ {
		if text[i] == ')' || text[i] == ';' || isCSSSpace(text[i]) {
			valueEnd = i
			break
		}
	}
	return pathCompletionContext{valueStart: contentStart, valueEnd: valueEnd}, true
}

func findImportPathCompletionContext(text string, offset int) (pathCompletionContext, bool) {
	segmentStart := 0
	for i := offset - 1; i >= 0; i-- {
		switch text[i] {
		case ';', '{', '}', '\n', '\r':
			segmentStart = i + 1
			i = -1
		}
	}
	segment := text[segmentStart:offset]
	lowerSegment := strings.ToLower(segment)
	importOffset := -1
	importKeyword := ""
	for _, candidate := range []string{"@import", "@use", "@forward"} {
		if candidateOffset := strings.LastIndex(lowerSegment, candidate); candidateOffset > importOffset {
			importOffset = candidateOffset
			importKeyword = candidate
		}
	}
	if importOffset == -1 {
		return pathCompletionContext{}, false
	}
	importStart := segmentStart + importOffset
	quoteStart := -1
	for i := importStart; i < offset; i++ {
		if text[i] == '"' || text[i] == '\'' {
			quoteStart = i
		}
	}
	if quoteStart == -1 {
		return pathCompletionContext{}, false
	}
	quote := text[quoteStart]
	valueEnd := len(text)
	for i := offset; i < len(text); i++ {
		if text[i] == quote {
			valueEnd = i + 1
			break
		}
		if text[i] == ';' || text[i] == '\n' || text[i] == '\r' {
			valueEnd = i
			break
		}
	}
	return pathCompletionContext{valueStart: quoteStart, valueEnd: valueEnd, importPath: true, importKeyword: importKeyword}, true
}

func pathCompletionItems(ctx context.Context, document *lsp.TextDocument, pathContext pathCompletionContext, offset int, options CompletionOptions) ([]lsp.CompletionItem, error) {
	text := document.Text()
	fullStart, fullEnd, ok := pathCompletionValueBounds(text, pathContext)
	if !ok {
		return nil, nil
	}
	if offset < fullStart {
		offset = fullStart
	}
	if offset > fullEnd {
		offset = fullEnd
	}
	fullValue := text[fullStart:fullEnd]
	valueBeforeCursor := text[fullStart:offset]
	if valueBeforeCursor == "." || valueBeforeCursor == ".." {
		return nil, nil
	}

	replaceStart, replaceEnd := fullStart, fullEnd
	parentRef := "."
	if slash := strings.LastIndex(valueBeforeCursor, "/"); slash != -1 {
		parentRef = valueBeforeCursor[:slash+1]
		replaceStart = fullStart + slash + 1
		valueAfterLastSlash := fullValue[slash+1:]
		if whitespace := strings.IndexFunc(valueAfterLastSlash, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }); whitespace != -1 {
			replaceEnd = replaceStart + whitespace
		}
	}
	parentURI, ok := options.ResolveReference(parentRef, string(document.URI))
	if !ok || parentURI == "" {
		return nil, nil
	}
	entries, err := options.ReadDirectory(ctx, parentURI)
	if err != nil {
		return nil, err
	}
	replaceRange := rangeFromOffsets(document, replaceStart, replaceEnd)
	currentURI := string(document.URI)
	items := make([]lsp.CompletionItem, 0, len(entries))
	for _, entry := range entries {
		if entry.Name == "" || strings.HasPrefix(entry.Name, ".") {
			continue
		}
		isDirectory := entry.Type == FileTypeDirectory
		if !isDirectory && joinPathURI(parentURI, entry.Name) == currentURI {
			continue
		}
		label := escapePathCompletion(entry.Name)
		insertText := label
		kind := lsp.CompletionItemKindFile
		if isDirectory {
			label = escapePathCompletion(entry.Name + "/")
			insertText = label
			kind = lsp.CompletionItemKindFolder
		} else if pathContext.importPath && strings.EqualFold(document.LanguageID, "scss") {
			insertText = scssImportCompletionText(entry.Name)
		}
		edit := lsp.Replace(replaceRange, insertText)
		item := lsp.CompletionItem{Label: label, Kind: kind, TextEdit: &edit}
		if isDirectory {
			item.Command = &lsp.Command{Title: "Suggest", Command: "editor.action.triggerSuggest"}
		}
		items = append(items, item)
	}
	return items, nil
}

func pathCompletionValueBounds(text string, pathContext pathCompletionContext) (int, int, bool) {
	valueStart, valueEnd := pathContext.valueStart, pathContext.valueEnd
	if valueEnd < valueStart || valueStart < 0 || valueEnd > len(text) {
		return 0, 0, false
	}
	fullStart, fullEnd := valueStart, valueEnd
	if fullStart < fullEnd && (text[fullStart] == '"' || text[fullStart] == '\'') {
		fullStart++
		if fullEnd > fullStart && text[fullEnd-1] == text[valueStart] {
			fullEnd--
		}
	}
	return fullStart, fullEnd, true
}

func scssImportCompletionText(name string) string {
	if strings.HasPrefix(name, "_") && strings.HasSuffix(strings.ToLower(name), ".scss") {
		return escapePathCompletion(name[1 : len(name)-len(".scss")])
	}
	return escapePathCompletion(name)
}

func joinPathURI(parentURI, name string) string {
	if strings.HasSuffix(parentURI, "/") {
		return parentURI + name
	}
	return parentURI + "/" + name
}

func escapePathCompletion(path string) string {
	var out strings.Builder
	for _, r := range path {
		switch r {
		case ' ', '\t', '\n', '\r', '(', ')', ',', '"', '\'':
			out.WriteRune('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}

func completeSupportsCondition(document *lsp.TextDocument, offset int, manager *languagefacts.DataManager, options CompletionOptions) (lsp.CompletionList, bool) {
	text := document.Text()
	context, ok := supportsCompletionContextAt(text, offset)
	if !ok {
		return lsp.CompletionList{}, false
	}
	if context.propertyName == "" {
		builder := completionBuilder{
			document: document,
			manager:  manager,
			context:  completionContext{kind: completionProperty, replace: context.replace, currentWord: text[context.replace.start:offset], offset: offset},
			options:  options,
		}
		return lsp.CompletionList{Items: builder.propertyCompletions()}, true
	}
	property, ok := manager.GetProperty(context.propertyName)
	if !ok {
		return lsp.CompletionList{}, true
	}
	declaration := cssDeclaration{
		name:       property.Name,
		lowerName:  strings.ToLower(property.Name),
		value:      text[context.valueStart:context.valueEnd],
		nameOffset: context.nameStart,
		valueStart: context.valueStart,
		offset:     context.nameStart,
		length:     context.valueEnd - context.nameStart,
	}
	builder := completionBuilder{
		document: document,
		manager:  manager,
		context:  completionContext{kind: completionValue, declaration: &declaration, replace: context.replace, currentWord: text[context.replace.start:offset], offset: offset},
		options:  options,
	}
	return lsp.CompletionList{Items: builder.valueCompletions()}, true
}

type supportsCompletionContext struct {
	replace      selectionInterval
	propertyName string
	nameStart    int
	valueStart   int
	valueEnd     int
}

func supportsCompletionContextAt(text string, offset int) (supportsCompletionContext, bool) {
	if offset < 0 || offset > len(text) {
		return supportsCompletionContext{}, false
	}
	start := strings.LastIndex(strings.ToLower(text[:offset]), "@supports")
	if start == -1 {
		return supportsCompletionContext{}, false
	}
	blockStart := strings.Index(text[start:], "{")
	if blockStart != -1 && start+blockStart < offset {
		return supportsCompletionContext{}, false
	}
	open := strings.LastIndex(text[start:offset], "(")
	if open == -1 {
		return supportsCompletionContext{}, false
	}
	open += start
	close := strings.LastIndex(text[start:offset], ")")
	if close > open-start {
		return supportsCompletionContext{}, false
	}
	segment := text[open+1 : offset]
	replace := completionReplaceRange(text, offset, false)
	colon := strings.LastIndex(segment, ":")
	if colon == -1 {
		return supportsCompletionContext{replace: replace}, true
	}
	nameStart, nameEnd := trimRange(text, open+1, open+1+colon)
	if nameStart >= nameEnd {
		return supportsCompletionContext{}, false
	}
	valueStart := open + 1 + colon + 1
	valueEnd := offset
	for valueEnd < len(text) && text[valueEnd] != ')' && text[valueEnd] != '{' && text[valueEnd] != ';' {
		valueEnd++
	}
	return supportsCompletionContext{
		replace:      replace,
		propertyName: text[nameStart:nameEnd],
		nameStart:    nameStart,
		valueStart:   valueStart,
		valueEnd:     valueEnd,
	}, true
}

func completeMediaCondition(document *lsp.TextDocument, offset int, manager *languagefacts.DataManager) (lsp.CompletionList, bool) {
	text := document.Text()
	context, ok := mediaCompletionContextAt(text, offset)
	if !ok {
		return lsp.CompletionList{}, false
	}
	media, ok := manager.GetAtDirective("@media")
	if !ok {
		return lsp.CompletionList{}, true
	}
	if context.featureName != "" {
		for _, descriptor := range media.Descriptors {
			if descriptor.Name != context.featureName {
				continue
			}
			var items []lsp.CompletionItem
			for _, value := range descriptor.Values {
				edit := lsp.Replace(rangeFromOffsets(document, context.replace.start, context.replace.end), value.Name)
				item := lsp.CompletionItem{
					Label:    value.Name,
					Kind:     lsp.CompletionItemKindValue,
					TextEdit: &edit,
					SortText: " ",
				}
				items = append(items, item)
			}
			return lsp.CompletionList{Items: items}, true
		}
		return lsp.CompletionList{}, true
	}

	items := make([]lsp.CompletionItem, 0, len(media.Descriptors))
	for _, descriptor := range media.Descriptors {
		insertText := descriptor.Name
		item := lsp.CompletionItem{Label: descriptor.Name, Kind: lsp.CompletionItemKindKeyword}
		if descriptor.Type == "discrete" {
			insertText += ": "
			if len(descriptor.Values) > 0 {
				item.Command = &lsp.Command{Title: "Suggest", Command: "editor.action.triggerSuggest"}
			}
		}
		edit := lsp.Replace(rangeFromOffsets(document, context.replace.start, context.replace.end), insertText)
		item.TextEdit = &edit
		items = append(items, item)
	}
	return lsp.CompletionList{Items: dedupeCompletionItems(items)}, true
}

type mediaCompletionContext struct {
	replace     selectionInterval
	featureName string
}

func mediaCompletionContextAt(text string, offset int) (mediaCompletionContext, bool) {
	if offset < 0 || offset > len(text) {
		return mediaCompletionContext{}, false
	}
	start := strings.LastIndex(strings.ToLower(text[:offset]), "@media")
	if start == -1 {
		return mediaCompletionContext{}, false
	}
	blockStart := strings.Index(text[start:], "{")
	if blockStart != -1 && start+blockStart < offset {
		return mediaCompletionContext{}, false
	}
	open := strings.LastIndex(text[start:offset], "(")
	if open == -1 {
		return mediaCompletionContext{}, false
	}
	open += start
	close := strings.LastIndex(text[start:offset], ")")
	if close > open-start {
		return mediaCompletionContext{}, false
	}
	segment := text[open+1 : offset]
	colon := strings.LastIndex(segment, ":")
	replace := completionReplaceRange(text, offset, false)
	if colon == -1 {
		return mediaCompletionContext{replace: replace}, true
	}
	nameStart, nameEnd := trimRange(text, open+1, open+1+colon)
	if nameStart >= nameEnd {
		return mediaCompletionContext{}, false
	}
	return mediaCompletionContext{replace: replace, featureName: text[nameStart:nameEnd]}, true
}
