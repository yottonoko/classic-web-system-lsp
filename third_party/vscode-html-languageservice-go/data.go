package htmlservice

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed data/webCustomData.json
var defaultHTMLDataJSON []byte

var defaultHTMLDataCache struct {
	sync.Once
	data HTMLDataV1
}

type staticHTMLDataProvider struct {
	id               string
	tags             []TagData
	tagMap           map[string]TagData
	globalAttributes []AttributeData
	valueSetMap      map[string][]ValueData
}

func NewHTMLDataProvider(id string, customData HTMLDataV1) HTMLDataProvider {
	p := &staticHTMLDataProvider{
		id:               id,
		tags:             append([]TagData(nil), customData.Tags...),
		tagMap:           map[string]TagData{},
		globalAttributes: append([]AttributeData(nil), customData.GlobalAttributes...),
		valueSetMap:      map[string][]ValueData{},
	}
	for _, tag := range p.tags {
		p.tagMap[strings.ToLower(tag.Name)] = tag
	}
	for _, set := range customData.ValueSets {
		p.valueSetMap[set.Name] = append([]ValueData(nil), set.Values...)
	}
	return p
}

func (p *staticHTMLDataProvider) GetID() string { return p.id }
func (p *staticHTMLDataProvider) IsApplicable(languageID string) bool {
	return true
}
func (p *staticHTMLDataProvider) ProvideTags() []TagData {
	return append([]TagData(nil), p.tags...)
}
func (p *staticHTMLDataProvider) ProvideAttributes(tag string) []AttributeData {
	var result []AttributeData
	if entry, ok := p.tagMap[strings.ToLower(tag)]; ok {
		result = append(result, entry.Attributes...)
	}
	result = append(result, p.globalAttributes...)
	return result
}
func (p *staticHTMLDataProvider) ProvideValues(tag, attribute string) []ValueData {
	attribute = strings.ToLower(attribute)
	var result []ValueData
	process := func(attrs []AttributeData) {
		for _, attr := range attrs {
			if strings.ToLower(attr.Name) != attribute {
				continue
			}
			result = append(result, attr.Values...)
			if attr.ValueSet != "" {
				result = append(result, p.valueSetMap[attr.ValueSet]...)
			}
		}
	}
	if entry, ok := p.tagMap[strings.ToLower(tag)]; ok {
		process(entry.Attributes)
	}
	process(p.globalAttributes)
	return result
}

// HTMLDataManager stores and resolves the active HTML data providers.
type HTMLDataManager struct {
	mu                  sync.RWMutex
	dataProviders       []HTMLDataProvider
	voidElementsCache   map[string][]string
	voidElementSetCache map[string]map[string]bool
}

func NewHTMLDataManager(options LanguageServiceOptions) *HTMLDataManager {
	m := &HTMLDataManager{}
	useDefault := true
	if options.UseDefaultDataProvider != nil {
		useDefault = *options.UseDefaultDataProvider
	}
	m.SetDataProviders(useDefault, options.CustomDataProviders)
	return m
}

func (m *HTMLDataManager) SetDataProviders(builtIn bool, providers []HTMLDataProvider) {
	dataProviders := []HTMLDataProvider{}
	if builtIn {
		dataProviders = append(dataProviders, NewHTMLDataProvider("html5", defaultHTMLData()))
	}
	dataProviders = append(dataProviders, providers...)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.dataProviders = dataProviders
	m.voidElementsCache = nil
	m.voidElementSetCache = nil
}

func (m *HTMLDataManager) GetDataProviders() []HTMLDataProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]HTMLDataProvider(nil), m.dataProviders...)
}

func (m *HTMLDataManager) IsVoidElement(element string, voidElements []string) bool {
	element = strings.ToLower(element)
	i := sort.SearchStrings(voidElements, element)
	return i < len(voidElements) && voidElements[i] == element
}

func (m *HTMLDataManager) GetVoidElements(languageID string) []string {
	m.mu.RLock()
	if m.voidElementsCache != nil {
		if cached, ok := m.voidElementsCache[languageID]; ok {
			m.mu.RUnlock()
			return cached
		}
	}
	providers := append([]HTMLDataProvider(nil), m.dataProviders...)
	m.mu.RUnlock()

	voidElements := voidElementsForProviders(providers, languageID)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.voidElementsCache == nil {
		m.voidElementsCache = map[string][]string{}
	}
	if cached, ok := m.voidElementsCache[languageID]; ok {
		return cached
	}
	m.voidElementsCache[languageID] = voidElements
	return voidElements
}

func (m *HTMLDataManager) getVoidElementSet(languageID string) map[string]bool {
	m.mu.RLock()
	if m.voidElementSetCache != nil {
		if cached, ok := m.voidElementSetCache[languageID]; ok {
			m.mu.RUnlock()
			return cached
		}
	}
	m.mu.RUnlock()

	voidElements := m.GetVoidElements(languageID)
	set := make(map[string]bool, len(voidElements))
	for _, element := range voidElements {
		set[element] = true
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.voidElementSetCache == nil {
		m.voidElementSetCache = map[string]map[string]bool{}
	}
	if cached, ok := m.voidElementSetCache[languageID]; ok {
		return cached
	}
	m.voidElementSetCache[languageID] = set
	return set
}

func voidElementsForProviders(providers []HTMLDataProvider, languageID string) []string {
	var voidTags []string
	for _, provider := range providers {
		if !provider.IsApplicable(languageID) {
			continue
		}
		for _, tag := range provider.ProvideTags() {
			if tag.Void {
				voidTags = append(voidTags, tag.Name)
			}
		}
	}
	sort.Strings(voidTags)
	return voidTags
}

func (m *HTMLDataManager) IsPathAttribute(tag, attr string) bool {
	if attr == "src" || attr == "href" {
		return true
	}
	if attrs, ok := pathTagAndAttr[tag]; ok {
		for _, allowed := range attrs {
			if allowed == attr {
				return true
			}
		}
	}
	return false
}

var pathTagAndAttr = map[string][]string{
	"a":          {"href"},
	"area":       {"href"},
	"body":       {"background"},
	"blockquote": {"cite"},
	"del":        {"cite"},
	"form":       {"action"},
	"frame":      {"src", "longdesc"},
	"img":        {"src", "longdesc"},
	"ins":        {"cite"},
	"link":       {"href"},
	"object":     {"data"},
	"q":          {"cite"},
	"script":     {"src"},
	"audio":      {"src"},
	"button":     {"formaction"},
	"command":    {"icon"},
	"embed":      {"src"},
	"html":       {"manifest"},
	"input":      {"src", "formaction"},
	"source":     {"src"},
	"track":      {"src"},
	"video":      {"src", "poster"},
}

func GetDefaultHTMLDataProvider() HTMLDataProvider {
	return NewHTMLDataProvider("default", defaultHTMLData())
}

var documentationCache sync.Map

type documentationCacheEntry struct {
	doc *MarkupContent
}

func GenerateDocumentation(item any, settings HoverSettings, supportsMarkdown bool) *MarkupContent {
	result := &MarkupContent{Kind: MarkupKindPlainText}
	if supportsMarkdown {
		result.Kind = MarkupKindMarkdown
	}
	description, references, status, browsers := documentationParts(item)
	includeDocumentation := boolOption(settings.Documentation, true)
	includeReferences := boolOption(settings.References, true)
	if key, ok := documentationCacheKey(item, description, references, status, browsers, includeDocumentation, includeReferences, supportsMarkdown); ok {
		if cached, exists := documentationCache.Load(key); exists {
			entry := cached.(documentationCacheEntry)
			if entry.doc == nil {
				return nil
			}
			doc := *entry.doc
			return &doc
		}
		defer func() {
			if result.Value == "" {
				documentationCache.Store(key, documentationCacheEntry{})
				return
			}
			doc := *result
			documentationCache.Store(key, documentationCacheEntry{doc: &doc})
		}()
	}
	if description != nil && includeDocumentation {
		result.Value += normalizeDescription(description)
	}
	if status != nil && includeDocumentation {
		if result.Value != "" {
			result.Value += "\n\n"
		}
		text := getEntryBaselineStatus(status, browsers)
		if supportsMarkdown {
			result.Value += fmt.Sprintf("%s _%s_", getEntryBaselineImage(status), text)
		} else {
			result.Value += text
		}
	}
	if len(references) > 0 && includeReferences {
		if result.Value != "" {
			result.Value += "\n\n"
		}
		parts := make([]string, 0, len(references))
		for _, ref := range references {
			if supportsMarkdown {
				parts = append(parts, fmt.Sprintf("[%s](%s)", ref.Name, ref.URL))
			} else {
				parts = append(parts, fmt.Sprintf("%s: %s", ref.Name, ref.URL))
			}
		}
		if supportsMarkdown {
			result.Value += strings.Join(parts, " | ")
		} else {
			result.Value += strings.Join(parts, "\n")
		}
	}
	if result.Value == "" {
		return nil
	}
	return result
}

func documentationCacheKey(item any, description any, references []Reference, status *BaselineStatus, browsers []string, includeDocumentation, includeReferences, supportsMarkdown bool) (string, bool) {
	var kind, name string
	switch v := item.(type) {
	case TagData:
		kind, name = "tag", v.Name
	case AttributeData:
		kind, name = "attr", v.Name
	case ValueData:
		kind, name = "value", v.Name
	default:
		return "", false
	}
	var builder strings.Builder
	builder.WriteString(kind)
	builder.WriteByte(0)
	builder.WriteString(name)
	builder.WriteByte(0)
	if supportsMarkdown {
		builder.WriteByte('m')
	} else {
		builder.WriteByte('p')
	}
	if includeDocumentation {
		builder.WriteByte('d')
		builder.WriteString(normalizeDescription(description))
		if status != nil {
			builder.WriteString(fmt.Sprint(status.Baseline))
			builder.WriteByte(0)
			builder.WriteString(status.BaselineLowDate)
			builder.WriteByte(0)
			builder.WriteString(status.BaselineHighDate)
		}
		builder.WriteString(strings.Join(browsers, ","))
	}
	if includeReferences {
		builder.WriteByte('r')
		for _, ref := range references {
			builder.WriteString(ref.Name)
			builder.WriteByte(0)
			builder.WriteString(ref.URL)
			builder.WriteByte(0)
		}
	}
	return builder.String(), true
}

func documentationParts(item any) (any, []Reference, *BaselineStatus, []string) {
	switch v := item.(type) {
	case TagData:
		return v.Description, v.References, v.Status, v.Browsers
	case AttributeData:
		return v.Description, v.References, v.Status, v.Browsers
	case ValueData:
		return v.Description, v.References, v.Status, v.Browsers
	default:
		return nil, nil, nil, nil
	}
}

func normalizeDescription(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case MarkupContent:
		return v.Value
	case map[string]any:
		if s, ok := v["value"].(string); ok {
			return s
		}
	}
	return ""
}

func boolOption(v *bool, dflt bool) bool {
	if v == nil {
		return dflt
	}
	return *v
}

const baselineLimitedImage = "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTgiIGhlaWdodD0iMTAiIHZpZXdCb3g9IjAgMCA1NDAgMzAwIiBmaWxsPSJub25lIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPgogIDxzdHlsZT4KICAgIC5ncmF5LXNoYXBlIHsKICAgICAgZmlsbDogI0M2QzZDNjsgLyogTGlnaHQgbW9kZSAqLwogICAgfQoKICAgIEBtZWRpYSAocHJlZmVycy1jb2xvci1zY2hlbWU6IGRhcmspIHsKICAgICAgLmdyYXktc2hhcGUgewogICAgICAgIGZpbGw6ICM1NjU2NTY7IC8qIERhcmsgbW9kZSAqLwogICAgICB9CiAgICB9CiAgPC9zdHlsZT4KICA8cGF0aCBkPSJNMTUwIDBMMjQwIDkwTDIxMCAxMjBMMTIwIDMwTDE1MCAwWiIgZmlsbD0iI0YwOTQwOSIvPgogIDxwYXRoIGQ9Ik00MjAgMzBMNTQwIDE1MEw0MjAgMjcwTDM5MCAyNDBMNDgwIDE1MEwzOTAgNjBMNDIwIDMwWiIgY2xhc3M9ImdyYXktc2hhcGUiLz4KICA8cGF0aCBkPSJNMzMwIDE4MEwzMDAgMjEwTDM5MCAzMDBMNDIwIDI3MEwzMzAgMTgwWiIgZmlsbD0iI0YwOTQwOSIvPgogIDxwYXRoIGQ9Ik0xMjAgMzBMMTUwIDYwTDYwIDE1MEwxNTAgMjQwTDEyMCAyNzBMMCAxNTBMMTIwIDMwWiIgY2xhc3M9ImdyYXktc2hhcGUiLz4KICA8cGF0aCBkPSJNMzkwIDBMNDIwIDMwTDE1MCAzMDBMMTIwIDI3MEwzOTAgMFoiIGZpbGw9IiNGMDk0MDkiLz4KPC9zdmc+"
const baselineLowImage = "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTgiIGhlaWdodD0iMTAiIHZpZXdCb3g9IjAgMCA1NDAgMzAwIiBmaWxsPSJub25lIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPgogIDxzdHlsZT4KICAgIC5ibHVlLXNoYXBlIHsKICAgICAgZmlsbDogI0E4QzdGQTsgLyogTGlnaHQgbW9kZSAqLwogICAgfQoKICAgIEBtZWRpYSAocHJlZmVycy1jb2xvci1zY2hlbWU6IGRhcmspIHsKICAgICAgLmJsdWUtc2hhcGUgewogICAgICAgIGZpbGw6ICMyRDUwOUU7IC8qIERhcmsgbW9kZSAqLwogICAgICB9CiAgICB9CgogICAgLmRhcmtlci1ibHVlLXNoYXBlIHsKICAgICAgICBmaWxsOiAjMUI2RUYzOwogICAgfQoKICAgIEBtZWRpYSAocHJlZmVycy1jb2xvci1zY2hlbWU6IGRhcmspIHsKICAgICAgICAuZGFya2VyLWJsdWUtc2hhcGUgewogICAgICAgICAgICBmaWxsOiAjNDE4NUZGOwogICAgICAgIH0KICAgIH0KCiAgPC9zdHlsZT4KICA8cGF0aCBkPSJNMTUwIDBMMTgwIDMwTDE1MCA2MEwxMjAgMzBMMTUwIDBaIiBjbGFzcz0iYmx1ZS1zaGFwZSIvPgogIDxwYXRoIGQ9Ik0yMTAgNjBMMjQwIDkwTDIxMCAxMjBMMTgwIDkwTDIxMCA2MFoiIGNsYXNzPSJibHVlLXNoYXBlIi8+CiAgPHBhdGggZD0iTTQ1MCA2MEw0ODAgOTBMNDUwIDEyMEw0MjAgOTBMNDUwIDYwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNNTEwIDEyMEw1NDAgMTUwTDUxMCAxODBMNDgwIDE1MEw1MTAgMTIwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNNDUwIDE4MEw0ODAgMjEwTDQ1MCAyNDBMNDIwIDIxMEw0NTAgMTgwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNMzkwIDI0MEw0MjAgMjcwTDM5MCAzMDBMMzYwIDI3MEwzOTAgMjQwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNMzMwIDE4MEwzNjAgMjEwTDMzMCAyNDBMMzAwIDIxMEwzMzAgMTgwWiIgY2xhc3M9ImJsdWUtc2hhcGUiLz4KICA8cGF0aCBkPSJNOTAgNjBMMTIwIDkwTDkwIDEyMEw2MCA5MEw5MCA2MFoiIGNsYXNzPSJibHVlLXNoYXBlIi8+CiAgPHBhdGggZD0iTTM5MCAwTDQyMCAzMEwxNTAgMzAwTDAgMTUwTDMwIDEyMEwxNTAgMjQwTDM5MCAwWiIgY2xhc3M9ImRhcmtlci1ibHVlLXNoYXBlIi8+Cjwvc3ZnPg=="
const baselineHighImage = "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMTgiIGhlaWdodD0iMTAiIHZpZXdCb3g9IjAgMCA1NDAgMzAwIiBmaWxsPSJub25lIiB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPgogIDxzdHlsZT4KICAgIC5ncmVlbi1zaGFwZSB7CiAgICAgIGZpbGw6ICNDNEVFRDA7IC8qIExpZ2h0IG1vZGUgKi8KICAgIH0KCiAgICBAbWVkaWEgKHByZWZlcnMtY29sb3Itc2NoZW1lOiBkYXJrKSB7CiAgICAgIC5ncmVlbi1zaGFwZSB7CiAgICAgICAgZmlsbDogIzEyNTIyNTsgLyogRGFyayBtb2RlICovCiAgICAgIH0KICAgIH0KICA8L3N0eWxlPgogIDxwYXRoIGQ9Ik00MjAgMzBMMzkwIDYwTDQ4MCAxNTBMMzkwIDI0MEwzMzAgMTgwTDMwMCAyMTBMMzkwIDMwMEw1NDAgMTUwTDQyMCAzMFoiIGNsYXNzPSJncmVlbi1zaGFwZSIvPgogIDxwYXRoIGQ9Ik0xNTAgMEwzMCAxMjBMNjAgMTUwTDE1MCA2MEwyMTAgMTIwTDI0MCA5MEwxNTAgMFoiIGNsYXNzPSJncmVlbi1zaGFwZSIvPgogIDxwYXRoIGQ9Ik0zOTAgMEw0MjAgMzBMMTUwIDMwMEwwIDE1MEwzMCAxMjBMMTUwIDI0MEwzOTAgMFoiIGZpbGw9IiMxRUE0NDYiLz4KPC9zdmc+"

func getEntryBaselineImage(status *BaselineStatus) string {
	if status == nil {
		return ""
	}
	switch status.Baseline {
	case "low":
		return "![Baseline icon](" + baselineLowImage + ")"
	case "high":
		return "![Baseline icon](" + baselineHighImage + ")"
	default:
		return "![Baseline icon](" + baselineLimitedImage + ")"
	}
}

var shortCompatPattern = regexp.MustCompile(`(E|FFA|FF|SM|S|CA|C|IE|O)([\d|.]+)?`)

func getEntryBaselineStatus(status *BaselineStatus, browsers []string) string {
	if status == nil {
		return ""
	}
	if b, ok := status.Baseline.(bool); ok && !b {
		missing := GetMissingBaselineBrowsers(browsers)
		text := "Limited availability across major browsers"
		if missing != "" {
			text += " (Not fully implemented in " + missing + ")"
		}
		return text
	}
	year := ""
	if status.BaselineLowDate != "" {
		year = strings.Split(status.BaselineLowDate, "-")[0]
	}
	prefix := "Widely"
	if status.Baseline == "low" {
		prefix = "Newly"
	}
	return fmt.Sprintf("%s available across major browsers (Baseline since %s)", prefix, year)
}

func GetMissingBaselineBrowsers(browsers []string) string {
	if browsers == nil {
		return ""
	}
	type browserInfo struct {
		id       string
		name     string
		platform string
	}
	browserOrder := []browserInfo{
		{"C", "Chrome", "desktop"},
		{"CA", "Chrome", "Android"},
		{"E", "Edge", "desktop"},
		{"FF", "Firefox", "desktop"},
		{"FFA", "Firefox", "Android"},
		{"S", "Safari", "macOS"},
		{"SM", "Safari", "iOS"},
	}
	missing := map[string]browserInfo{}
	for _, browser := range browserOrder {
		missing[browser.id] = browser
	}
	for _, short := range browsers {
		match := shortCompatPattern.FindStringSubmatch(short)
		if len(match) == 0 {
			continue
		}
		delete(missing, match[1])
	}
	valuesByName := map[string]string{}
	var names []string
	appendName := func(name string) {
		if _, ok := valuesByName[name]; !ok {
			names = append(names, name)
		}
	}
	for _, browser := range browserOrder {
		if _, ok := missing[browser.id]; !ok {
			continue
		}
		_, seen := valuesByName[browser.name]
		appendName(browser.name)
		if seen || browser.id == "E" {
			valuesByName[browser.name] = browser.name
		} else {
			valuesByName[browser.name] = browser.name + " on " + browser.platform
		}
	}
	var result []string
	for _, name := range names {
		result = append(result, valuesByName[name])
	}
	return joinEnglishDisjunction(result)
}

func joinEnglishDisjunction(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " or " + values[1]
	default:
		return strings.Join(values[:len(values)-1], ", ") + ", or " + values[len(values)-1]
	}
}

func defaultHTMLData() HTMLDataV1 {
	defaultHTMLDataCache.Do(func() {
		if err := json.Unmarshal(defaultHTMLDataJSON, &defaultHTMLDataCache.data); err != nil {
			defaultHTMLDataCache.data = fallbackHTMLData()
		}
	})
	return cloneHTMLData(defaultHTMLDataCache.data)
}

func cloneHTMLData(data HTMLDataV1) HTMLDataV1 {
	clone := HTMLDataV1{
		Version:          cloneJSONValue(data.Version),
		Tags:             make([]TagData, len(data.Tags)),
		GlobalAttributes: cloneAttributes(data.GlobalAttributes),
		ValueSets:        make([]ValueSet, len(data.ValueSets)),
	}
	for i, tag := range data.Tags {
		clone.Tags[i] = tag
		clone.Tags[i].Description = cloneJSONValue(tag.Description)
		clone.Tags[i].Attributes = cloneAttributes(tag.Attributes)
		clone.Tags[i].References = append([]Reference(nil), tag.References...)
		clone.Tags[i].Browsers = append([]string(nil), tag.Browsers...)
		clone.Tags[i].Status = cloneBaselineStatus(tag.Status)
	}
	for i, set := range data.ValueSets {
		clone.ValueSets[i] = set
		clone.ValueSets[i].Values = cloneValues(set.Values)
	}
	return clone
}

func cloneAttributes(attributes []AttributeData) []AttributeData {
	clone := make([]AttributeData, len(attributes))
	for i, attribute := range attributes {
		clone[i] = attribute
		clone[i].Description = cloneJSONValue(attribute.Description)
		clone[i].Values = cloneValues(attribute.Values)
		clone[i].References = append([]Reference(nil), attribute.References...)
		clone[i].Browsers = append([]string(nil), attribute.Browsers...)
		clone[i].Status = cloneBaselineStatus(attribute.Status)
	}
	return clone
}

func cloneValues(values []ValueData) []ValueData {
	clone := make([]ValueData, len(values))
	for i, value := range values {
		clone[i] = value
		clone[i].Description = cloneJSONValue(value.Description)
		clone[i].References = append([]Reference(nil), value.References...)
		clone[i].Browsers = append([]string(nil), value.Browsers...)
		clone[i].Status = cloneBaselineStatus(value.Status)
	}
	return clone
}

func cloneBaselineStatus(status *BaselineStatus) *BaselineStatus {
	if status == nil {
		return nil
	}
	clone := *status
	clone.Baseline = cloneJSONValue(status.Baseline)
	return &clone
}

func cloneJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(value))
		for key, item := range value {
			clone[key] = cloneJSONValue(item)
		}
		return clone
	case []any:
		clone := make([]any, len(value))
		for i, item := range value {
			clone[i] = cloneJSONValue(item)
		}
		return clone
	default:
		return value
	}
}

func fallbackHTMLData() HTMLDataV1 {
	attrs := func(names ...string) []AttributeData {
		out := make([]AttributeData, 0, len(names))
		for _, name := range names {
			out = append(out, AttributeData{Name: name})
		}
		return out
	}
	valueSet := func(name string, values ...string) ValueSet {
		out := ValueSet{Name: name}
		for _, v := range values {
			out.Values = append(out.Values, ValueData{Name: v})
		}
		return out
	}
	tags := []TagData{
		{Name: "!DOCTYPE", Description: "A preamble for HTML documents.", Attributes: nil},
		{Name: "html", Description: "The html element represents the root of an HTML document.", Attributes: attrs("manifest", "version", "xmlns")},
		{Name: "head", Description: "The head element represents a collection of metadata for the Document.", Attributes: attrs("profile")},
		{Name: "title", Description: "The title element represents the document's title or name.", Attributes: nil},
		{Name: "body", Attributes: attrs("background")},
		{Name: "div", Description: "The div element has no special meaning at all.", Attributes: nil},
		{Name: "span", Description: "The span element is a generic phrasing container.", Attributes: nil},
		{Name: "h1", Attributes: nil},
		{Name: "header", Attributes: nil},
		{Name: "p", Attributes: nil},
		{Name: "a", Attributes: attrs("href", "target", "download", "rel")},
		{Name: "ul", Attributes: nil},
		{Name: "li", Attributes: nil},
		{Name: "form", Attributes: attrs("action", "method")},
		{Name: "label", Attributes: attrs("for")},
		{Name: "input", Void: true, Attributes: attrs("type", "src", "size", "disabled", "formaction")},
		{Name: "img", Void: true, Attributes: attrs("src", "alt", "longdesc")},
		{Name: "br", Void: true, Attributes: nil},
		{Name: "hr", Void: true, Attributes: nil},
		{Name: "meta", Void: true, Attributes: attrs("charset", "name", "content")},
		{Name: "link", Void: true, Attributes: attrs("href", "rel", "type")},
		{Name: "script", Attributes: attrs("src", "type")},
		{Name: "style", Attributes: attrs("type")},
		{Name: "iframe", Attributes: attrs("src", "sandbox")},
		{Name: "source", Void: true, Attributes: attrs("src", "type")},
		{Name: "video", Attributes: attrs("src", "poster")},
		{Name: "audio", Attributes: attrs("src")},
	}
	globalAttrs := attrs("id", "class", "style", "dir", "tabindex", "hidden", "title", "aria-describedby", "onmousemove")
	for i, attr := range globalAttrs {
		switch attr.Name {
		case "dir":
			globalAttrs[i].Values = []ValueData{{Name: "ltr"}, {Name: "rtl"}, {Name: "auto"}}
		}
	}
	for i := range tags {
		for j := range tags[i].Attributes {
			switch tags[i].Attributes[j].Name {
			case "type":
				tags[i].Attributes[j].Values = []ValueData{{Name: "text"}, {Name: "checkbox"}, {Name: "color"}, {Name: "number"}, {Name: "button"}}
			case "sandbox":
				tags[i].Attributes[j].Values = []ValueData{{Name: "allow-forms"}, {Name: "allow-modals"}, {Name: "allow-scripts"}, {Name: "allow-same-origin"}}
			case "rel":
				tags[i].Attributes[j].Values = []ValueData{{Name: "stylesheet"}, {Name: "icon"}, {Name: "preload"}}
			case "method":
				tags[i].Attributes[j].Values = []ValueData{{Name: "get"}, {Name: "post"}}
			}
		}
	}
	return HTMLDataV1{
		Version:          1.1,
		Tags:             tags,
		GlobalAttributes: globalAttrs,
		ValueSets: []ValueSet{
			valueSet("inputtypes", "text", "checkbox", "color", "number", "button"),
		},
	}
}
