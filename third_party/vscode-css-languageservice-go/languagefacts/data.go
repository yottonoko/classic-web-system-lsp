package languagefacts

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yottonoko/vscode-css-languageservice-go/internal/data"
)

var shortCompatPattern = regexp.MustCompile(`^(E|FFA|FF|SM|S|CA|C|IE|O)([\d.]+)?$`)

// EntryStatus describes the standards status of a CSS data entry.
type EntryStatus string

const (
	EntryStatusStandard     EntryStatus = "standard"
	EntryStatusExperimental EntryStatus = "experimental"
	EntryStatusNonstandard  EntryStatus = "nonstandard"
	EntryStatusObsolete     EntryStatus = "obsolete"
)

// Baseline describes the web-platform baseline support level for a CSS entry.
type Baseline string

const (
	BaselineFalse Baseline = "false"
	BaselineLow   Baseline = "low"
	BaselineHigh  Baseline = "high"
)

// BaselineStatus stores baseline availability metadata for a CSS entry.
type BaselineStatus struct {
	Status           Baseline `json:"status"`
	BaselineLowDate  string   `json:"baseline_low_date,omitempty"`
	BaselineHighDate string   `json:"baseline_high_date,omitempty"`
}

// Reference links a CSS data entry to external documentation.
type Reference struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ValueData describes a CSS property or descriptor value.
type ValueData struct {
	Name        string          `json:"name"`
	Description json.RawMessage `json:"description,omitempty"`
	Browsers    []string        `json:"browsers,omitempty"`
	Baseline    *BaselineStatus `json:"baseline,omitempty"`
	Status      EntryStatus     `json:"status,omitempty"`
	References  []Reference     `json:"references,omitempty"`
}

// DescriptorData describes an at-rule descriptor.
type DescriptorData struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	References  []Reference     `json:"references,omitempty"`
	Syntax      string          `json:"syntax,omitempty"`
	Type        string          `json:"type,omitempty"`
	Values      []ValueData     `json:"values,omitempty"`
	Browsers    []string        `json:"browsers,omitempty"`
	Baseline    *BaselineStatus `json:"baseline,omitempty"`
	Status      EntryStatus     `json:"status,omitempty"`
}

// PropertyData describes a CSS property from custom or built-in data.
type PropertyData struct {
	Name         string          `json:"name"`
	Description  json.RawMessage `json:"description,omitempty"`
	Browsers     []string        `json:"browsers,omitempty"`
	Baseline     *BaselineStatus `json:"baseline,omitempty"`
	Restrictions []string        `json:"restrictions,omitempty"`
	Status       EntryStatus     `json:"status,omitempty"`
	Syntax       string          `json:"syntax,omitempty"`
	Values       []ValueData     `json:"values,omitempty"`
	References   []Reference     `json:"references,omitempty"`
	Relevance    *float64        `json:"relevance,omitempty"`
	AtRule       string          `json:"atRule,omitempty"`
}

// AtDirectiveData describes a CSS at-rule entry.
type AtDirectiveData struct {
	Name        string           `json:"name"`
	Description json.RawMessage  `json:"description,omitempty"`
	Browsers    []string         `json:"browsers,omitempty"`
	Baseline    *BaselineStatus  `json:"baseline,omitempty"`
	Status      EntryStatus      `json:"status,omitempty"`
	References  []Reference      `json:"references,omitempty"`
	Descriptors []DescriptorData `json:"descriptors,omitempty"`
}

// PseudoClassData describes a CSS pseudo-class entry.
type PseudoClassData struct {
	Name        string          `json:"name"`
	Description json.RawMessage `json:"description,omitempty"`
	Browsers    []string        `json:"browsers,omitempty"`
	Baseline    *BaselineStatus `json:"baseline,omitempty"`
	Status      EntryStatus     `json:"status,omitempty"`
	References  []Reference     `json:"references,omitempty"`
}

// PseudoElementData describes a CSS pseudo-element entry.
type PseudoElementData struct {
	Name        string          `json:"name"`
	Description json.RawMessage `json:"description,omitempty"`
	Browsers    []string        `json:"browsers,omitempty"`
	Baseline    *BaselineStatus `json:"baseline,omitempty"`
	Status      EntryStatus     `json:"status,omitempty"`
	References  []Reference     `json:"references,omitempty"`
}

// CSSDataV1 is the custom data schema accepted by the language service.
type CSSDataV1 struct {
	Version        float64             `json:"version"`
	Properties     []PropertyData      `json:"properties,omitempty"`
	AtDirectives   []AtDirectiveData   `json:"atDirectives,omitempty"`
	PseudoClasses  []PseudoClassData   `json:"pseudoClasses,omitempty"`
	PseudoElements []PseudoElementData `json:"pseudoElements,omitempty"`
}

// CSSDataProvider supplies CSS metadata to completion, hover, and validation.
type CSSDataProvider interface {
	ProvideProperties() []PropertyData
	ProvideAtDirectives() []AtDirectiveData
	ProvidePseudoClasses() []PseudoClassData
	ProvidePseudoElements() []PseudoElementData
}

// StaticDataProvider is an in-memory CSSDataProvider implementation.
type StaticDataProvider struct {
	properties     []PropertyData
	atDirectives   []AtDirectiveData
	pseudoClasses  []PseudoClassData
	pseudoElements []PseudoElementData
}

func NewCSSDataProvider(cssData CSSDataV1) CSSDataProvider {
	provider := &StaticDataProvider{}
	provider.addData(cssData)
	return provider
}

var defaultCSSDataProvider = sync.OnceValue(func() CSSDataProvider {
	var cssData CSSDataV1
	if err := json.Unmarshal(data.WebCustomData(), &cssData); err != nil {
		panic(err)
	}
	return NewCSSDataProvider(cssData)
})

func GetDefaultCSSDataProvider() CSSDataProvider {
	return defaultCSSDataProvider()
}

func GetMissingBaselineBrowsers(browsers []string) string {
	if browsers == nil {
		return ""
	}
	missing := map[string]browserInfo{
		"C":   {Name: "Chrome", Platform: "desktop"},
		"CA":  {Name: "Chrome", Platform: "Android"},
		"E":   {Name: "Edge", Platform: "desktop"},
		"FF":  {Name: "Firefox", Platform: "desktop"},
		"FFA": {Name: "Firefox", Platform: "Android"},
		"S":   {Name: "Safari", Platform: "macOS"},
		"SM":  {Name: "Safari", Platform: "iOS"},
	}
	for _, compat := range browsers {
		match := shortCompatPattern.FindStringSubmatch(compat)
		if match == nil {
			continue
		}
		delete(missing, match[1])
	}
	if len(missing) == 0 {
		return ""
	}
	missingByName := map[string][]browserInfo{}
	for _, browser := range missing {
		missingByName[browser.Name] = append(missingByName[browser.Name], browser)
	}
	totalByName := map[string]int{
		"Chrome":  2,
		"Edge":    1,
		"Firefox": 2,
		"Safari":  2,
	}
	var names []string
	for _, id := range []string{"C", "CA", "E", "FF", "FFA", "S", "SM"} {
		browser, ok := missing[id]
		if !ok {
			continue
		}
		name := browser.Name
		if len(missingByName[browser.Name]) == totalByName[browser.Name] {
			if containsString(names, name) {
				continue
			}
		} else if id != "E" {
			name += " on " + browser.Platform
		}
		names = append(names, name)
	}
	return englishDisjunction(names)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type browserInfo struct {
	Name     string
	Platform string
}

func englishDisjunction(values []string) string {
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

func (p *StaticDataProvider) ProvideProperties() []PropertyData {
	return append([]PropertyData(nil), p.properties...)
}

func (p *StaticDataProvider) ProvideAtDirectives() []AtDirectiveData {
	return append([]AtDirectiveData(nil), p.atDirectives...)
}

func (p *StaticDataProvider) ProvidePseudoClasses() []PseudoClassData {
	return append([]PseudoClassData(nil), p.pseudoClasses...)
}

func (p *StaticDataProvider) ProvidePseudoElements() []PseudoElementData {
	return append([]PseudoElementData(nil), p.pseudoElements...)
}

func (p *StaticDataProvider) addData(cssData CSSDataV1) {
	for _, prop := range cssData.Properties {
		if prop.Name != "" {
			p.properties = append(p.properties, prop)
		}
	}
	for _, directive := range cssData.AtDirectives {
		if directive.Name != "" {
			p.atDirectives = append(p.atDirectives, directive)
		}
	}
	for _, pseudoClass := range cssData.PseudoClasses {
		if pseudoClass.Name != "" {
			p.pseudoClasses = append(p.pseudoClasses, pseudoClass)
		}
	}
	for _, pseudoElement := range cssData.PseudoElements {
		if pseudoElement.Name != "" {
			p.pseudoElements = append(p.pseudoElements, pseudoElement)
		}
	}
}

// DataManager indexes CSS data providers for fast lookup.
type DataManager struct {
	state atomic.Pointer[dataManagerState]
}

type dataManagerState struct {
	dataProviders    []CSSDataProvider
	propertySet      map[string]PropertyData
	atDirectiveSet   map[string]AtDirectiveData
	pseudoClassSet   map[string]PseudoClassData
	pseudoElementSet map[string]PseudoElementData
	properties       []PropertyData
	atDirectives     []AtDirectiveData
	pseudoClasses    []PseudoClassData
	pseudoElements   []PseudoElementData
}

var emptyDataManagerState = collectData(nil)
var defaultDataManagerState = sync.OnceValue(func() *dataManagerState {
	return collectData([]CSSDataProvider{GetDefaultCSSDataProvider()})
})

// DataManagerOptions configures the providers used by a DataManager.
type DataManagerOptions struct {
	UseDefaultDataProvider *bool
	CustomDataProviders    []CSSDataProvider
}

func NewDataManager(options DataManagerOptions) *DataManager {
	manager := &DataManager{}
	useDefault := true
	if options.UseDefaultDataProvider != nil {
		useDefault = *options.UseDefaultDataProvider
	}
	manager.SetDataProviders(useDefault, options.CustomDataProviders)
	return manager
}

func (m *DataManager) SetDataProviders(builtIn bool, providers []CSSDataProvider) {
	if builtIn && len(providers) == 0 {
		m.state.Store(defaultDataManagerState())
		return
	}
	dataProviders := []CSSDataProvider(nil)
	if builtIn {
		dataProviders = append(dataProviders, GetDefaultCSSDataProvider())
	}
	dataProviders = append(dataProviders, providers...)
	m.state.Store(collectData(dataProviders))
}

func collectData(dataProviders []CSSDataProvider) *dataManagerState {
	state := &dataManagerState{
		dataProviders:    append([]CSSDataProvider(nil), dataProviders...),
		propertySet:      map[string]PropertyData{},
		atDirectiveSet:   map[string]AtDirectiveData{},
		pseudoClassSet:   map[string]PseudoClassData{},
		pseudoElementSet: map[string]PseudoElementData{},
	}
	for _, provider := range dataProviders {
		for _, property := range provider.ProvideProperties() {
			key := strings.ToLower(property.Name)
			if _, ok := state.propertySet[key]; !ok {
				state.propertySet[key] = property
				state.properties = append(state.properties, property)
			}
		}
		for _, directive := range provider.ProvideAtDirectives() {
			if _, ok := state.atDirectiveSet[directive.Name]; !ok {
				state.atDirectiveSet[directive.Name] = directive
				state.atDirectives = append(state.atDirectives, directive)
			}
		}
		for _, pseudoClass := range provider.ProvidePseudoClasses() {
			if _, ok := state.pseudoClassSet[pseudoClass.Name]; !ok {
				state.pseudoClassSet[pseudoClass.Name] = pseudoClass
				state.pseudoClasses = append(state.pseudoClasses, pseudoClass)
			}
		}
		for _, pseudoElement := range provider.ProvidePseudoElements() {
			if _, ok := state.pseudoElementSet[pseudoElement.Name]; !ok {
				state.pseudoElementSet[pseudoElement.Name] = pseudoElement
				state.pseudoElements = append(state.pseudoElements, pseudoElement)
			}
		}
	}
	return state
}

func (m *DataManager) currentState() *dataManagerState {
	state := m.state.Load()
	if state == nil {
		return emptyDataManagerState
	}
	return state
}

func (m *DataManager) GetProperty(name string) (PropertyData, bool) {
	state := m.currentState()
	property, ok := state.propertySet[strings.ToLower(name)]
	return property, ok
}

func (m *DataManager) GetAtDirective(name string) (AtDirectiveData, bool) {
	state := m.currentState()
	directive, ok := state.atDirectiveSet[name]
	return directive, ok
}

func (m *DataManager) GetPseudoClass(name string) (PseudoClassData, bool) {
	state := m.currentState()
	pseudoClass, ok := state.pseudoClassSet[name]
	return pseudoClass, ok
}

func (m *DataManager) GetPseudoElement(name string) (PseudoElementData, bool) {
	state := m.currentState()
	pseudoElement, ok := state.pseudoElementSet[name]
	return pseudoElement, ok
}

func (m *DataManager) GetProperties() []PropertyData {
	return append([]PropertyData(nil), m.currentState().properties...)
}

func (m *DataManager) GetAtDirectives() []AtDirectiveData {
	return append([]AtDirectiveData(nil), m.currentState().atDirectives...)
}

func (m *DataManager) GetPseudoClasses() []PseudoClassData {
	return append([]PseudoClassData(nil), m.currentState().pseudoClasses...)
}

func (m *DataManager) GetPseudoElements() []PseudoElementData {
	return append([]PseudoElementData(nil), m.currentState().pseudoElements...)
}

func (m *DataManager) IsKnownProperty(name string) bool {
	_, ok := m.currentState().propertySet[strings.ToLower(name)]
	return ok
}

func (m *DataManager) IsStandardProperty(name string) bool {
	property, ok := m.GetProperty(name)
	return ok && (property.Status == "" || property.Status == EntryStatusStandard)
}
