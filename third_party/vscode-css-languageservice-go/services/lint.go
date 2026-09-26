package services

import (
	"regexp"
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

// Level identifies the severity assigned to a lint rule.
type Level int

const (
	LevelIgnore  Level = 1
	LevelWarning Level = 2
	LevelError   Level = 4
)

// Rule describes a lint rule and its default severity.
type Rule struct {
	ID           string
	Message      string
	DefaultLevel Level
}

var (
	RuleAllVendorPrefixes = Rule{
		ID:           "compatibleVendorPrefixes",
		Message:      "When using a vendor-specific prefix make sure to also include all other vendor-specific properties",
		DefaultLevel: LevelIgnore,
	}
	RuleIncludeStandardPropertyWhenUsingVendorPrefix = Rule{
		ID:           "vendorPrefix",
		Message:      "When using a vendor-specific prefix also include the standard property",
		DefaultLevel: LevelWarning,
	}
	RuleDuplicateDeclarations = Rule{
		ID:           "duplicateProperties",
		Message:      "Do not use duplicate style definitions",
		DefaultLevel: LevelIgnore,
	}
	RuleEmptyRuleSet = Rule{
		ID:           "emptyRules",
		Message:      "Do not use empty rulesets",
		DefaultLevel: LevelWarning,
	}
	RuleUniversalSelector = Rule{
		ID:           "universalSelector",
		Message:      "The universal selector (*) is known to be slow",
		DefaultLevel: LevelIgnore,
	}
	RuleZeroWithUnit = Rule{
		ID:           "zeroUnits",
		Message:      "No unit for zero needed",
		DefaultLevel: LevelIgnore,
	}
	RuleRequiredPropertiesForFontFace = Rule{
		ID:           "fontFaceProperties",
		Message:      "@font-face rule must define 'src' and 'font-family' properties",
		DefaultLevel: LevelWarning,
	}
	RuleUnknownProperty = Rule{
		ID:           "unknownProperties",
		Message:      "Unknown property.",
		DefaultLevel: LevelWarning,
	}
	RuleUnknownVendorSpecificProperty = Rule{
		ID:           "unknownVendorSpecificProperties",
		Message:      "Unknown vendor specific property.",
		DefaultLevel: LevelIgnore,
	}
	RuleUnknownAtRule = Rule{
		ID:           "unknownAtRules",
		Message:      "Unknown at-rule.",
		DefaultLevel: LevelWarning,
	}
	RulePropertyIgnoredDueToDisplay = Rule{
		ID:           "propertyIgnoredDueToDisplay",
		Message:      "Property is ignored due to the display.",
		DefaultLevel: LevelWarning,
	}
	RuleBewareOfBoxModelSize = Rule{
		ID:           "boxModel",
		Message:      "Do not use width or height when using padding or border.",
		DefaultLevel: LevelIgnore,
	}
	RuleAvoidImportant = Rule{
		ID:           "important",
		Message:      "Avoid using !important. It is an indication that the specificity of the entire CSS has gotten out of control and needs to be refactored.",
		DefaultLevel: LevelIgnore,
	}
	RuleAvoidFloat = Rule{
		ID:           "float",
		Message:      "Avoid using 'float'. Floats lead to fragile CSS that is easy to break if one aspect of the layout changes.",
		DefaultLevel: LevelIgnore,
	}
	RuleAvoidIdSelector = Rule{
		ID:           "idSelector",
		Message:      "Selectors should not contain IDs because these rules are too tightly coupled with the HTML.",
		DefaultLevel: LevelIgnore,
	}
)

// LintConfiguration stores active lint rule severities.
type LintConfiguration struct {
	settings map[string]any
}

// LintEntry stores a lint rule match before conversion to diagnostics.
type LintEntry struct {
	Rule    Rule
	Level   Level
	Message string
	Range   lsp.Range
	Offset  int
	Length  int
}

type cssBlock struct {
	head      string
	context   string
	headStart int
	start     int
	bodyStart int
	bodyEnd   int
	end       int
}

type cssDeclaration struct {
	name       string
	lowerName  string
	value      string
	nameOffset int
	valueStart int
	offset     int
	length     int
}

type lintVendorGroup struct {
	names []string
	nodes []cssDeclaration
}

type lintKeyframeGroup struct {
	names []string
	nodes []cssBlock
}

var (
	zeroUnitPattern       = regexp.MustCompile(`(?i)(^|[^a-z0-9_.%-])0([a-z]+)\b`)
	boxModelNumberPattern = regexp.MustCompile(`[-+]?(?:\d+(?:\.\d*)?|\.\d+)`)
)

var vendorPrefixes = []string{"-ms-", "-moz-", "-o-", "-webkit-"}

var cssUnits = map[string]bool{
	"cap": true, "ch": true, "cm": true, "cqb": true, "cqh": true, "cqi": true, "cqmax": true,
	"cqmin": true, "cqw": true, "deg": true, "dpcm": true, "dpi": true, "dppx": true, "em": true,
	"ex": true, "fr": true, "grad": true, "hz": true, "ic": true, "in": true, "khz": true,
	"lh": true, "mm": true, "ms": true, "pc": true, "pt": true, "px": true, "rad": true,
	"rem": true, "rlh": true, "s": true, "turn": true, "vb": true, "vh": true, "vi": true,
	"vmax": true, "vmin": true, "vw": true,
}

func NewLintConfiguration(settings map[string]any) LintConfiguration {
	copied := map[string]any{}
	for key, value := range settings {
		copied[key] = value
	}
	return LintConfiguration{settings: copied}
}

func Validate(document *lsp.TextDocument, manager *languagefacts.DataManager, settings map[string]any) []lsp.Diagnostic {
	entries := LintEntries(document, manager, NewLintConfiguration(settings), LevelWarning|LevelError)
	diagnostics := make([]lsp.Diagnostic, 0, len(entries))
	for _, entry := range entries {
		if entry.Level == LevelIgnore {
			continue
		}
		severity := lsp.DiagnosticSeverityError
		if entry.Level == LevelWarning {
			severity = lsp.DiagnosticSeverityWarning
		}
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    entry.Range,
			Severity: severity,
			Code:     entry.Rule.ID,
			Source:   document.LanguageID,
			Message:  entry.Message,
		})
	}
	return diagnostics
}

func LintEntries(document *lsp.TextDocument, manager *languagefacts.DataManager, config LintConfiguration, levels Level) []LintEntry {
	if manager == nil {
		manager = languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
	}
	linter := lintVisitor{
		document:        document,
		text:            document.Text(),
		manager:         manager,
		config:          config,
		levels:          levels,
		validProperties: config.validProperties(),
	}
	linter.visit()
	return linter.entries
}

func (c LintConfiguration) level(rule Rule) Level {
	if raw, ok := c.settings[rule.ID]; ok {
		if level, ok := toLintLevel(raw); ok {
			return level
		}
	}
	return rule.DefaultLevel
}

func (c LintConfiguration) validProperties() map[string]bool {
	properties := map[string]bool{}
	raw, ok := c.settings["validProperties"]
	if !ok || raw == nil {
		return properties
	}
	switch values := raw.(type) {
	case []string:
		for _, value := range values {
			if value != "" {
				properties[strings.ToLower(value)] = true
			}
		}
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok && text != "" {
				properties[strings.ToLower(text)] = true
			}
		}
	}
	return properties
}

func toLintLevel(raw any) (Level, bool) {
	value, ok := raw.(string)
	if !ok {
		return 0, false
	}
	switch strings.ToLower(value) {
	case "ignore":
		return LevelIgnore, true
	case "warning":
		return LevelWarning, true
	case "error":
		return LevelError, true
	default:
		return 0, false
	}
}

type lintVisitor struct {
	document        *lsp.TextDocument
	text            string
	manager         *languagefacts.DataManager
	config          LintConfiguration
	levels          Level
	validProperties map[string]bool
	keyframes       map[string]*lintKeyframeGroup
	entries         []LintEntry
}

func (v *lintVisitor) visit() {
	v.keyframes = map[string]*lintKeyframeGroup{}
	v.visitUnknownAtRules()
	for _, block := range parseCSSBlocks(v.text) {
		headLower := strings.ToLower(block.head)
		declarations := parseDeclarations(v.text, block.bodyStart, block.bodyEnd)
		if strings.HasPrefix(headLower, "@font-face") {
			v.visitFontFace(block, declarations)
			continue
		}
		if v.visitKeyframe(block, headLower) {
			continue
		}
		if strings.HasPrefix(headLower, "@") {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(block.head), ":") {
			continue
		}
		v.visitRuleBlock(block, declarations)
	}
	v.validateKeyframes()
}

func (v *lintVisitor) visitUnknownAtRules() {
	if v.document.LanguageID != "css" {
		return
	}
	for i := 0; i < len(v.text); i++ {
		next := skipCSSIgnored(v.text, i)
		if next != i {
			i = next
			continue
		}
		if v.text[i] != '@' || i+1 >= len(v.text) || !isSelectorIdentifierByte(v.text[i+1]) && v.text[i+1] != '-' {
			continue
		}
		end := i + 2
		for end < len(v.text) && isSelectorIdentifierByte(v.text[end]) {
			end++
		}
		name := v.text[i:end]
		if v.isKnownAtDirective(name) {
			i = end - 1
			continue
		}
		v.add(i, end-i, RuleUnknownAtRule, "Unknown at rule "+name)
		i = end - 1
	}
}

func (v *lintVisitor) isKnownAtDirective(name string) bool {
	if _, ok := v.manager.GetAtDirective(name); ok {
		return true
	}
	if _, ok := v.manager.GetAtDirective(strings.ToLower(name)); ok {
		return true
	}
	return false
}

func (v *lintVisitor) visitRuleBlock(block cssBlock, declarations []cssDeclaration) {
	if stripped := strings.TrimSpace(stripComments(v.text[block.bodyStart:block.bodyEnd])); stripped == "" {
		v.add(block.headStart, max(1, block.end-block.headStart+1), RuleEmptyRuleSet, "")
	}
	if selectorContainsUniversal(block.head) {
		v.add(block.headStart, max(1, len(block.head)), RuleUniversalSelector, "")
	}
	if selectorContainsID(block.head) {
		v.add(block.headStart, max(1, len(block.head)), RuleAvoidIdSelector, "")
	}

	properties := map[string][]cssDeclaration{}
	for _, declaration := range declarations {
		properties[declaration.lowerName] = append(properties[declaration.lowerName], declaration)
	}

	v.visitDisplayDependencies(properties)
	v.visitAvoidFloat(properties)
	v.visitImportantAndZeroUnits(declarations)
	v.visitDuplicateDeclarations(properties)
	v.visitBoxModel(properties)
	v.visitUnknownProperties(block, declarations, properties)
}

func (v *lintVisitor) visitDisplayDependencies(properties map[string][]cssDeclaration) {
	for _, display := range properties["display"] {
		value := strings.ToLower(strings.TrimSpace(display.value))
		if value == "inline-block" {
			for _, floatDecl := range properties["float"] {
				if normalizedDeclarationValue(floatDecl.value) != "none" {
					v.add(floatDecl.nameOffset, len(floatDecl.name), RulePropertyIgnoredDueToDisplay, "inline-block is ignored due to the float. If 'float' has a value other than 'none', the box is floated and 'display' is treated as 'block'")
				}
			}
		}
		if value == "block" {
			for _, verticalAlign := range properties["vertical-align"] {
				v.add(verticalAlign.nameOffset, len(verticalAlign.name), RulePropertyIgnoredDueToDisplay, "Property is ignored due to the display. With 'display: block', vertical-align should not be used.")
			}
		}
	}
}

func (v *lintVisitor) visitAvoidFloat(properties map[string][]cssDeclaration) {
	for _, declaration := range properties["float"] {
		v.add(declaration.nameOffset, len(declaration.name), RuleAvoidFloat, "")
	}
}

func (v *lintVisitor) visitImportantAndZeroUnits(declarations []cssDeclaration) {
	for _, declaration := range declarations {
		valueLower := strings.ToLower(declaration.value)
		searchOffset := 0
		for {
			index := strings.Index(valueLower[searchOffset:], "!important")
			if index == -1 {
				break
			}
			offset := declaration.valueStart + searchOffset + index
			v.add(offset, len("!important"), RuleAvoidImportant, "")
			searchOffset += index + len("!important")
		}

		if strings.Contains(valueLower, "calc(") || v.validProperties[declaration.lowerName] {
			continue
		}
		matches := zeroUnitPattern.FindAllStringSubmatchIndex(declaration.value, -1)
		for _, match := range matches {
			unitStart := match[4]
			unitEnd := match[5]
			if unitStart == -1 || unitEnd == -1 {
				continue
			}
			unit := strings.ToLower(declaration.value[unitStart:unitEnd])
			if !cssUnits[unit] {
				continue
			}
			zeroStart := unitStart - 1
			v.add(declaration.valueStart+zeroStart, unitEnd-zeroStart, RuleZeroWithUnit, "")
		}
	}
}

func (v *lintVisitor) visitDuplicateDeclarations(properties map[string][]cssDeclaration) {
	for property, declarations := range properties {
		if property == "background" || v.validProperties[property] || len(declarations) < 2 {
			continue
		}
		for i, declaration := range declarations {
			if strings.HasPrefix(strings.TrimSpace(declaration.value), "-") {
				continue
			}
			for j, other := range declarations {
				if i == j || strings.HasPrefix(strings.TrimSpace(other.value), "-") {
					continue
				}
				v.add(declaration.nameOffset, len(declaration.name), RuleDuplicateDeclarations, "")
				break
			}
		}
	}
}

func (v *lintVisitor) visitBoxModel(properties map[string][]cssDeclaration) {
	if hasDeclarationValue(properties["box-sizing"], "border-box") {
		return
	}
	sizeDeclarations := map[string][]cssDeclaration{
		"height": properties["height"],
		"width":  properties["width"],
	}
	for axis, declarations := range sizeDeclarations {
		if len(declarations) == 0 {
			continue
		}
		for _, boxDeclaration := range boxModelDeclarationsForAxis(properties, axis) {
			if !boxModelValueAffectsSize(boxDeclaration, axis) {
				continue
			}
			if boxModelDeclarationOverridden(properties, boxDeclaration, axis) {
				continue
			}
			v.add(declarations[0].nameOffset, len(declarations[0].name), RuleBewareOfBoxModelSize, "")
			v.add(boxDeclaration.nameOffset, len(boxDeclaration.name), RuleBewareOfBoxModelSize, "")
		}
	}
}

func boxModelDeclarationsForAxis(properties map[string][]cssDeclaration, axis string) []cssDeclaration {
	names := []string{"border", "border-width", "border-style", "padding"}
	if axis == "height" {
		names = append(names, "border-top", "border-bottom", "border-top-width", "border-bottom-width", "border-top-style", "border-bottom-style", "padding-top", "padding-bottom")
	} else {
		names = append(names, "border-left", "border-right", "border-left-width", "border-right-width", "border-left-style", "border-right-style", "padding-left", "padding-right")
	}
	var result []cssDeclaration
	for _, name := range names {
		result = append(result, properties[name]...)
	}
	return result
}

func hasDeclarationValue(declarations []cssDeclaration, value string) bool {
	for _, declaration := range declarations {
		if strings.EqualFold(strings.TrimSpace(declaration.value), value) {
			return true
		}
	}
	return false
}

func boxModelValueAffectsSize(declaration cssDeclaration, axis string) bool {
	value := strings.ToLower(strings.TrimSpace(declaration.value))
	if value == "" || value == "initial" || value == "unset" || value == "none" || value == "hidden" {
		return false
	}
	if strings.Contains(value, " none") || strings.Contains(value, " hidden") || strings.HasPrefix(value, "none ") || strings.HasPrefix(value, "hidden ") {
		return false
	}
	switch declaration.lowerName {
	case "padding", "border-width":
		values := boxModelNumericValues(value)
		top, right, bottom, left := expandBoxModelValues(values)
		if declaration.lowerName == "padding" || declaration.lowerName == "border-width" {
			if axis == "height" {
				return top || bottom
			}
			return right || left
		}
	case "padding-top", "padding-bottom", "padding-left", "padding-right", "border-top-width", "border-bottom-width", "border-left-width", "border-right-width":
		return containsNonZeroNumericValue(value)
	case "border-style", "border-top-style", "border-bottom-style", "border-left-style", "border-right-style":
		return value != "none" && value != "hidden" && value != "initial" && value != "unset"
	default:
		return containsNonZeroNumericValue(value)
	}
	return false
}

func boxModelNumericValues(value string) []bool {
	fields := strings.Fields(value)
	result := make([]bool, 0, len(fields))
	for _, field := range fields {
		result = append(result, containsNonZeroNumericValue(field))
	}
	return result
}

func expandBoxModelValues(values []bool) (top, right, bottom, left bool) {
	switch len(values) {
	case 0:
		return false, false, false, false
	case 1:
		return values[0], values[0], values[0], values[0]
	case 2:
		return values[0], values[1], values[0], values[1]
	case 3:
		return values[0], values[1], values[2], values[1]
	default:
		return values[0], values[1], values[2], values[3]
	}
}

func containsNonZeroNumericValue(value string) bool {
	matches := boxModelNumberPattern.FindAllString(value, -1)
	for _, match := range matches {
		trimmed := strings.TrimLeft(match, "+-")
		trimmed = strings.Trim(trimmed, "0.")
		if trimmed != "" {
			return true
		}
	}
	return false
}

func boxModelDeclarationOverridden(properties map[string][]cssDeclaration, declaration cssDeclaration, axis string) bool {
	if declaration.lowerName != "border" {
		return false
	}
	var sideNames []string
	if axis == "height" {
		sideNames = []string{"border-top", "border-bottom"}
	} else {
		sideNames = []string{"border-left", "border-right"}
	}
	for _, sideName := range sideNames {
		sideDeclarations := properties[sideName]
		if len(sideDeclarations) == 0 {
			return false
		}
		overridesSide := false
		for _, sideDeclaration := range sideDeclarations {
			if !boxModelValueAffectsSize(sideDeclaration, axis) {
				overridesSide = true
				break
			}
		}
		if !overridesSide {
			return false
		}
	}
	return true
}

func normalizedDeclarationValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSpace(strings.ReplaceAll(value, "!important", ""))
	return value
}

func vendorPrefixMatchesSelectorContext(propertyName, selector string) bool {
	selector = strings.ToLower(selector)
	switch {
	case strings.HasPrefix(propertyName, "-webkit-"):
		return strings.Contains(selector, "::-webkit-")
	case strings.HasPrefix(propertyName, "-moz-"):
		return strings.Contains(selector, "::-moz-")
	case strings.HasPrefix(propertyName, "-ms-"):
		return strings.Contains(selector, "::-ms-") || strings.Contains(selector, "::-microsoft-")
	case strings.HasPrefix(propertyName, "-o-"):
		return strings.Contains(selector, "::-o-")
	default:
		return false
	}
}

func (v *lintVisitor) visitUnknownProperties(block cssBlock, declarations []cssDeclaration, properties map[string][]cssDeclaration) {
	if strings.TrimSpace(block.head) == ":export" {
		return
	}
	groups := map[string]*lintVendorGroup{}
	containsUnknowns := false
	for _, declaration := range declarations {
		name := declaration.lowerName
		if name == "" || strings.HasPrefix(name, "--") || v.validProperties[name] {
			continue
		}
		if isDynamicPropertyName(name) {
			containsUnknowns = true
			continue
		}
		if strings.HasPrefix(name, "-") && !strings.HasPrefix(name, "--") {
			if !v.manager.IsKnownProperty(name) {
				v.add(declaration.nameOffset, len(declaration.name), RuleUnknownVendorSpecificProperty, "")
			}
			v.addVendorGroup(groups, nonPrefixedPropertyName(name), name, declaration)
			continue
		}

		lookupName := name
		if strings.HasPrefix(lookupName, "*") || strings.HasPrefix(lookupName, "_") {
			lookupName = lookupName[1:]
		}
		if !v.manager.IsKnownProperty(declaration.lowerName) && !v.manager.IsKnownProperty(lookupName) {
			message := "Unknown property: '" + declaration.name + "'"
			v.add(declaration.nameOffset, len(declaration.name), RuleUnknownProperty, message)
		}
		v.addVendorGroup(groups, lookupName, name, cssDeclaration{})
	}
	if !containsUnknowns {
		v.validateVendorGroups(block, groups)
	}
}

func (v *lintVisitor) addVendorGroup(groups map[string]*lintVendorGroup, suffix, name string, declaration cssDeclaration) {
	group := groups[suffix]
	if group == nil {
		group = &lintVendorGroup{}
		groups[suffix] = group
	}
	group.names = append(group.names, name)
	if declaration.name != "" {
		group.nodes = append(group.nodes, declaration)
	}
}

func (v *lintVisitor) validateVendorGroups(block cssBlock, groups map[string]*lintVendorGroup) {
	for suffix, group := range groups {
		needsStandard := v.manager.IsStandardProperty(suffix) && !containsString(group.names, suffix)
		if !needsStandard && len(group.names) == 1 {
			continue
		}
		var expected []string
		for _, prefix := range vendorPrefixes {
			name := prefix + suffix
			if v.manager.IsStandardProperty(name) {
				expected = append(expected, name)
			}
		}
		missing := missingNames(expected, group.names)
		if len(missing) == 0 && !needsStandard {
			continue
		}
		for _, declaration := range group.nodes {
			if needsStandard && !vendorPrefixMatchesSelectorContext(declaration.lowerName, block.selectorContext()) {
				message := "Also define the standard property '" + suffix + "' for compatibility"
				v.add(declaration.nameOffset, len(declaration.name), RuleIncludeStandardPropertyWhenUsingVendorPrefix, message)
			}
			if len(missing) > 0 {
				message := "Always include all vendor specific properties: Missing: " + quotedList(missing)
				v.add(declaration.nameOffset, len(declaration.name), RuleAllVendorPrefixes, message)
			}
		}
	}
}

func (v *lintVisitor) visitFontFace(block cssBlock, declarations []cssDeclaration) {
	hasSrc := false
	hasFontFamily := false
	containsUnknowns := fontFaceContainsUnknowns(v.text[block.bodyStart:block.bodyEnd], declarations)
	for _, declaration := range declarations {
		switch declaration.lowerName {
		case "src":
			hasSrc = true
		case "font-family":
			hasFontFamily = true
		}
	}
	if !containsUnknowns && (!hasSrc || !hasFontFamily) {
		v.add(block.headStart, max(1, block.end-block.headStart+1), RuleRequiredPropertiesForFontFace, "")
	}
}

func fontFaceContainsUnknowns(body string, declarations []cssDeclaration) bool {
	if strings.Contains(body, "{") || strings.Contains(body, "@") {
		return true
	}
	for _, declaration := range declarations {
		if isDynamicPropertyName(declaration.lowerName) {
			return true
		}
	}
	return false
}

func (v *lintVisitor) visitKeyframe(block cssBlock, headLower string) bool {
	keyword, name, ok := keyframeParts(headLower)
	if !ok {
		return false
	}
	group := v.keyframes[name]
	if group == nil {
		group = &lintKeyframeGroup{}
		v.keyframes[name] = group
	}
	group.names = append(group.names, keyword)
	if keyword != "@keyframes" {
		group.nodes = append(group.nodes, block)
	}
	return true
}

func (v *lintVisitor) validateKeyframes() {
	expected := []string{"@-webkit-keyframes", "@-moz-keyframes", "@-o-keyframes"}
	for _, group := range v.keyframes {
		needsStandard := !containsString(group.names, "@keyframes")
		if !needsStandard && len(group.names) == 1 {
			continue
		}
		missing := missingNames(expected, group.names)
		if len(missing) == 0 && !needsStandard {
			continue
		}
		for _, block := range group.nodes {
			if needsStandard {
				v.add(block.headStart, len(keyframeKeyword(block.head)), RuleIncludeStandardPropertyWhenUsingVendorPrefix, "Always define standard rule '@keyframes' when defining keyframes.")
			}
			if len(missing) > 0 {
				v.add(block.headStart, len(keyframeKeyword(block.head)), RuleAllVendorPrefixes, "Always include all vendor specific rules: Missing: "+quotedList(missing))
			}
		}
	}
}

func (v *lintVisitor) add(byteOffset, byteLength int, rule Rule, message string) {
	level := v.config.level(rule)
	if level&v.levels == 0 {
		return
	}
	if message == "" {
		message = rule.Message
	}
	v.entries = append(v.entries, LintEntry{
		Rule:    rule,
		Level:   level,
		Message: message,
		Range:   byteRange(v.document, byteOffset, byteLength),
		Offset:  utf16OffsetAtByteOffset(v.document, byteOffset),
		Length:  utf16OffsetAtByteOffset(v.document, byteOffset+byteLength) - utf16OffsetAtByteOffset(v.document, byteOffset),
	})
}

func parseCSSBlocks(text string) []cssBlock {
	var blocks []cssBlock
	var stack []int
	for i := 0; i < len(text); i++ {
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
			if len(stack) == 0 {
				continue
			}
			start := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			headStart, headEnd := blockHeadRange(text, start)
			context := blockContext(text, stack)
			blocks = append(blocks, cssBlock{
				head:      strings.TrimSpace(text[headStart:headEnd]),
				context:   context,
				headStart: headStart,
				start:     start,
				bodyStart: start + 1,
				bodyEnd:   i,
				end:       i,
			})
		}
	}
	for i := 0; i < len(blocks)-1; i++ {
		for j := i + 1; j < len(blocks); j++ {
			if blocks[j].start < blocks[i].start {
				blocks[i], blocks[j] = blocks[j], blocks[i]
			}
		}
	}
	return blocks
}

func (b cssBlock) selectorContext() string {
	if b.context == "" {
		return b.head
	}
	return b.head + " " + b.context
}

func blockContext(text string, stack []int) string {
	var parts []string
	for _, brace := range stack {
		headStart, headEnd := blockHeadRange(text, brace)
		if headStart < headEnd {
			parts = append(parts, strings.TrimSpace(text[headStart:headEnd]))
		}
	}
	return strings.Join(parts, " ")
}

func blockHeadRange(text string, brace int) (int, int) {
	start := brace
	for start > 0 {
		switch text[start-1] {
		case '{', '}', ';':
			return trimRange(text, start, brace)
		}
		start--
	}
	return trimRange(text, start, brace)
}

func parseDeclarations(text string, start, end int) []cssDeclaration {
	var declarations []cssDeclaration
	segmentStart := start
	parenDepth := 0
	for i := start; i <= end; i++ {
		if i < end {
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
				if parenDepth == 0 {
					if declaration, ok := parseDeclarationSegment(text, segmentStart, i); ok {
						declarations = append(declarations, declaration)
					}
					segmentStart = i + 1
				}
			}
			continue
		}
		if declaration, ok := parseDeclarationSegment(text, segmentStart, end); ok {
			declarations = append(declarations, declaration)
		}
	}
	return declarations
}

func parseDeclarationSegment(text string, start, end int) (cssDeclaration, bool) {
	start, end = trimRange(text, start, end)
	start, end = trimLeadingCSSComments(text, start, end)
	if start >= end || strings.ContainsAny(text[start:end], "{}") {
		return cssDeclaration{}, false
	}
	colon := -1
	parenDepth := 0
	for i := start; i < end; i++ {
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
		case ':':
			if parenDepth == 0 {
				colon = i
				i = end
			}
		}
	}
	if colon == -1 {
		return cssDeclaration{}, false
	}
	nameStart, nameEnd := trimRange(text, start, colon)
	valueStart, valueEnd := trimRange(text, colon+1, end)
	if nameStart >= nameEnd {
		return cssDeclaration{}, false
	}
	name := text[nameStart:nameEnd]
	return cssDeclaration{
		name:       name,
		lowerName:  strings.ToLower(name),
		value:      text[valueStart:valueEnd],
		nameOffset: nameStart,
		valueStart: valueStart,
		offset:     nameStart,
		length:     valueEnd - nameStart,
	}, true
}

func trimLeadingCSSComments(text string, start, end int) (int, int) {
	for start < end {
		next := skipCSSIgnored(text, start)
		if next == start || !(start+1 < end && text[start] == '/' && text[start+1] == '*') {
			break
		}
		start = next + 1
		start, end = trimRange(text, start, end)
	}
	return start, end
}

func skipCSSIgnored(text string, index int) int {
	if index+1 < len(text) && text[index] == '/' && text[index+1] == '*' {
		end := strings.Index(text[index+2:], "*/")
		if end == -1 {
			return len(text) - 1
		}
		return index + 2 + end + 1
	}
	if text[index] == '"' || text[index] == '\'' {
		quote := text[index]
		for i := index + 1; i < len(text); i++ {
			if text[i] == '\\' {
				i++
				continue
			}
			if text[i] == quote {
				return i
			}
		}
		return len(text) - 1
	}
	return index
}

func trimRange(text string, start, end int) (int, int) {
	for start < end && isCSSSpace(text[start]) {
		start++
	}
	for end > start && isCSSSpace(text[end-1]) {
		end--
	}
	return start, end
}

func stripComments(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if i+1 < len(text) && text[i] == '/' && text[i+1] == '*' {
			next := skipCSSIgnored(text, i)
			i = next
			continue
		}
		out.WriteByte(text[i])
	}
	return out.String()
}

func selectorContainsUniversal(selector string) bool {
	for i := 0; i < len(selector); i++ {
		next := skipCSSIgnored(selector, i)
		if next != i {
			i = next
			continue
		}
		if selector[i] == '*' {
			return true
		}
	}
	return false
}

func selectorContainsID(selector string) bool {
	for i := 0; i < len(selector); i++ {
		next := skipCSSIgnored(selector, i)
		if next != i {
			i = next
			continue
		}
		if selector[i] == '#' && i+1 < len(selector) && isIdentStart(selector[i+1]) {
			return true
		}
	}
	return false
}

func nonPrefixedPropertyName(name string) string {
	for _, prefix := range []string{"-webkit-", "-moz-", "-ms-", "-o-"} {
		if strings.HasPrefix(name, prefix) {
			return name[len(prefix):]
		}
	}
	return name
}

func isDynamicPropertyName(name string) bool {
	return strings.Contains(name, "#{") || strings.HasSuffix(name, "+") || strings.HasSuffix(name, "+_")
}

func keyframeParts(head string) (keyword, name string, ok bool) {
	fields := strings.Fields(head)
	if len(fields) < 2 {
		return "", "", false
	}
	switch fields[0] {
	case "@keyframes", "@-webkit-keyframes", "@-moz-keyframes", "@-o-keyframes":
		return fields[0], fields[1], true
	default:
		return "", "", false
	}
}

func keyframeKeyword(head string) string {
	fields := strings.Fields(head)
	if len(fields) == 0 {
		return head
	}
	return fields[0]
}

func missingNames(expected, actual []string) []string {
	var missing []string
	for _, name := range expected {
		if !containsString(actual, name) {
			missing = append(missing, name)
		}
	}
	return missing
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func quotedList(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = "'" + value + "'"
	}
	return strings.Join(quoted, ", ")
}

func byteRange(document *lsp.TextDocument, byteOffset, byteLength int) lsp.Range {
	text := document.Text()
	if byteOffset < 0 {
		byteOffset = 0
	}
	if byteOffset > len(text) {
		byteOffset = len(text)
	}
	byteEnd := byteOffset + byteLength
	if byteEnd > len(text) {
		byteEnd = len(text)
	}
	return rangeFromOffsets(document, byteOffset, byteEnd)
}

func isCSSSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

func isIdentStart(b byte) bool {
	return b == '_' || b == '-' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
