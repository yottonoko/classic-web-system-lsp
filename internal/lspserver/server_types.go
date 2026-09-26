package lspserver

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type pendingClientRequest struct {
	method    string
	startedAt time.Time
	response  chan rpcMessage
}

type workspaceReferenceInflight struct {
	done         chan struct{}
	locations    []lsp.Location
	generation   uint64
	nameRevision uint64
	stale        bool
}

type workspaceReferenceTargetKey struct {
	URI                string
	Name               string
	NameHash           uint64
	Line               int
	Character          int
	SymbolKind         string
	IncludeDeclaration bool
	Generation         uint64
}

type workspaceReferencePreviousKey struct {
	DocumentKey  string
	NameHash     uint64
	Kind         string
	MemberOfHash uint64
	ScopeHash    uint64
}

type workspaceReferenceBatchKey struct {
	DocumentKey string
	Generation  uint64
}

type workspaceReferenceScopeCacheKey struct {
	DocumentKey    string
	GraphRevision  uint64
	IncludeRelated bool
}

type workspaceReferenceScopeSnapshot struct {
	DocumentKeys []string
	FileNames    []string
	Membership   map[string]struct{}
	Fingerprint  string
}

type workspaceReferenceImplicitPlanKey struct {
	DocumentKey   string
	GraphRevision uint64
	IndexRevision uint64
}

type workspaceReferenceBatchState struct {
	generation       uint64
	done             chan struct{}
	doneOnce         sync.Once
	ctx              context.Context
	cancel           context.CancelFunc
	nameRevisions    map[string]uint64
	complete         bool
	total            int
	warmed           int
	cacheHits        int
	dbRestored       int
	examinedSegments int
	queryDescriptors []workspaceReferenceQueryDescriptor
	started          time.Time
	progressTaskID   string
	documentProgress bool
	declarations     []vbUsageDeclaration
	finalCounts      []int
}

type workspaceReferenceDeclarationPlan struct {
	generation   uint64
	catalog      *LegacyUndefinedGlobalCatalog
	settings     referenceCodeLensFilterSettings
	declarations []vbUsageDeclaration
	memberOwners map[int]string
}

type workspaceReferenceDescriptorFingerprintCache struct {
	scope string
	names map[string]workspaceReferenceCachedNameFingerprint
}

type workspaceReferenceCachedNameFingerprint struct {
	revision    uint64
	fingerprint string
}

type workspaceReferenceCountPromotion struct {
	workspaceGeneration uint64
	fingerprint         string
	counts              map[workspaceReferenceTargetKey]int
}

const workspaceReferenceQuerySchemaVersion = 1
const workspaceReferenceDocumentSchemaVersion = 3

type persistedWorkspaceReferenceQuery struct {
	SchemaVersion          int    `cbor:"schemaVersion"`
	DeclarationFingerprint string `cbor:"declarationFingerprint"`
	NameFingerprint        string `cbor:"nameFingerprint"`
	ScopeFingerprint       string `cbor:"scopeFingerprint"`
	Count                  int    `cbor:"count"`
}

type workspaceReferenceQueryDescriptor struct {
	key                    []byte
	declaration            vbUsageDeclaration
	declarationFingerprint string
	nameFingerprint        string
	scopeFingerprint       string
}

type persistedWorkspaceReferenceDocument struct {
	SchemaVersion       int                                                `cbor:"schemaVersion"`
	DocumentKey         string                                             `cbor:"documentKey"`
	SourceHash          string                                             `cbor:"sourceHash"`
	Declarations        map[string]vbscript.Symbol                         `cbor:"declarations"`
	Scopes              []vbscript.ReferenceScope                          `cbor:"scopes"`
	PostingKeys         []string                                           `cbor:"postingKeys"`
	PostingFingerprints map[string]string                                  `cbor:"postingFingerprints,omitempty"`
	CountSummaries      map[string]persistedWorkspaceReferenceCountSummary `cbor:"countSummaries,omitempty"`
}

type persistedWorkspaceReferenceCountSummary struct {
	CountFingerprint    string                                       `cbor:"countFingerprint"`
	LocationFingerprint string                                       `cbor:"locationFingerprint"`
	Counts              workspaceReferenceCountSummary               `cbor:"counts"`
	DeclarationRanges   []lsp.Range                                  `cbor:"declarationRanges,omitempty"`
	ImplicitAdjustments []persistedWorkspaceReferenceCountAdjustment `cbor:"implicitAdjustments,omitempty"`
}

type persistedWorkspaceReferenceCountAdjustment struct {
	Range                         lsp.Range `cbor:"range"`
	CodeLensReferences            int       `cbor:"codeLensReferences,omitempty"`
	CodeLensCalls                 int       `cbor:"codeLensCalls,omitempty"`
	UnqualifiedCodeLensReferences int       `cbor:"unqualifiedCodeLensReferences,omitempty"`
	UnqualifiedCodeLensCalls      int       `cbor:"unqualifiedCodeLensCalls,omitempty"`
}

type persistedWorkspaceReferencePosting struct {
	SchemaVersion int                         `cbor:"schemaVersion"`
	Name          string                      `cbor:"name"`
	Postings      []vbscript.ReferencePosting `cbor:"postings"`
}

type semanticTokenCache struct {
	Version int
	Tokens  lsp.SemanticTokens
}

type requestCancellationEntry struct {
	cancel   context.CancelFunc
	sequence uint64
}

type diagnosticRevisionJob struct {
	generation uint64
	key        string
	version    int
	cancel     context.CancelFunc
	supersede  context.CancelFunc
	done       chan struct{}
	fastTimer  *time.Timer
	finalTimer *time.Timer
}

type serverSettings struct {
	CacheEnabled                             bool
	CacheDirectory                           string
	CacheFreshness                           string
	CacheTTLHours                            int
	CacheMaxSizeMB                           int
	CacheGzip                                bool
	CheckJS                                  bool
	CodeLensIncludes                         bool
	CodeLensReferences                       bool
	CodeLensReferenceProcedures              bool
	CodeLensReferenceGlobals                 bool
	CodeLensReferenceClasses                 bool
	CodeLensReferenceClassMembers            bool
	CodeLensIncludeRelatedIncludeTrees       bool
	DebugOutput                              string
	DebugLogFileEnabled                      bool
	DebugLogFilePath                         string
	DiagnosticsDebounceMS                    int
	DefaultLanguage                          string
	FormatCSSBraceStyle                      string
	FormatCSSInsertSpaces                    *bool
	FormatCSSNewlineBetweenRules             *bool
	FormatCSSNewlineBetweenSelectors         *bool
	FormatCSSSpaceAroundSelectorSeparator    *bool
	FormatCSSTabSize                         int
	FormatCSSTagIndentMode                   string
	FormatIgnoreCSSTagIndent                 bool
	FormatCSSWrapLineLength                  int
	FormatEndOfLine                          string
	FormatEmbeddedLanguageFormatting         string
	FormatFragmentMode                       string
	FormatIndentEmptyLines                   *bool
	FormatHTMLInsertSpaces                   *bool
	FormatHTMLContentUnformatted             string
	FormatHTMLExtraLiners                    string
	FormatHTMLIndentInnerHTML                *bool
	FormatHTMLTabSize                        int
	FormatHTMLUnformatted                    string
	FormatHTMLWrapAttributes                 string
	FormatHTMLWrapAttributesIndentSize       int
	FormatHTMLWrapLineLength                 int
	FormatInsertSpaces                       bool
	FormatEnabledLanguages                   []string
	FormatInsertFinalNewline                 bool
	FormatMaxPreserveNewLines                *int
	FormatNestedASPInCSSJS                   string
	FormatOnSave                             bool
	FormatPreserveNewLines                   *bool
	FormatPrintWidth                         int
	FormatRespectDisableRegions              bool
	FormatTabSize                            int
	FormatVBScriptTabSize                    int
	FormatVBScriptInsertSpaces               *bool
	FormatVBScriptKeywordCase                string
	FormatVBScriptLineContinuationIndentSize int
	FormatVBScriptSelectCaseIndent           string
	FormatVBScriptBlockIndent                string
	FormatVBScriptTagIndentMode              string
	FormatIgnoreVBScriptTagIndent            bool
	FormatUppercaseKeywords                  bool
	FormatAlignAssignments                   bool
	FormatASPDelimiterSpacing                string
	FormatASPBlockNewline                    string
	GraphUseReverseIncludeIndex              bool
	GraphWorkerSymbolExtraction              bool
	GraphInitialViewMode                     string
	GraphShowRootNodes                       bool
	GraphShowFileNodes                       bool
	GraphShowFunctionNodes                   bool
	GraphShowSubNodes                        bool
	GraphShowClassNodes                      bool
	GraphShowMethodNodes                     bool
	GraphShowMethodFunctionNodes             bool
	GraphShowMethodSubNodes                  bool
	GraphShowPropertyNodes                   bool
	GraphShowMemberNodes                     bool
	GraphShowGlobalVariableNodes             bool
	GraphShowGlobalConstantNodes             bool
	GraphShowLocalVariableNodes              bool
	GraphShowLocalConstantNodes              bool
	GraphShowParameterNodes                  bool
	GraphShowUnresolvedNodes                 bool
	GraphHideSingleNodes                     bool
	GraphHideUnreferencedGlobalSymbols       bool
	GraphShowOutgoingSelectionLinks          bool
	GraphShowIncludeLinks                    bool
	GraphShowDeclareLinks                    bool
	GraphShowReferenceLinks                  bool
	GraphShowAssignmentLinks                 bool
	GraphShowCallLinks                       bool
	GraphShowUnresolvedLinks                 bool
	GraphShowMemberLinks                     bool
	GraphShowIncomingDocumentIncludes        bool
	GraphShowIncomingFolderIncludes          bool
	GraphIncludeRelatedIncludeTrees          bool
	FlowchartLabelLineLength                 int
	FlowchartLabelMode                       string
	InitializedDimQuickFixStyle              string
	IdentifierCase                           string
	IdentifierCaseByKind                     map[string]string
	InlayImplicitByRef                       bool
	InlayFunctionReturnTypes                 bool
	InlayParameterNames                      bool
	InlayScopeMarkers                        inlayScopeMarkerSettings
	InlayVariableTypes                       bool
	JavaScriptAutoImports                    bool
	JavaScriptCompilerOptions                map[string]any
	JavaScriptCompilerOptionTypes            []string
	JavaScriptIgnoreProjectConfig            bool
	JavaScriptUnusedDiagnostics              bool
	VBScriptAutoIncludes                     bool
	VBScriptDeadCodeDiagnostics              bool
	VBScriptUnusedDiagnostics                bool
	VBScriptImplicitGlobalDiagnostics        bool
	VBScriptSyntaxKeywords                   bool
	VBScriptAssumeUndefinedGlobals           bool
	VBScriptIfSyntaxDiagnostics              string
	VBScriptTypeChecking                     string
	VBScriptGlobals                          map[string]vbscriptGlobalSetting
	VBScriptComTypes                         map[string]vbscriptComTypeSetting
	FormatJavaScriptBraceStyle               string
	FormatJavaScriptBraceFunctionsNewLine    *bool
	FormatJavaScriptBraceControlNewLine      *bool
	FormatJavaScriptInsertSpaces             *bool
	FormatJavaScriptTagIndentMode            string
	FormatIgnoreJavaScriptTagIndent          bool
	FormatJavaScriptSemicolons               string
	FormatJavaScriptIndentSwitchCase         *bool
	FormatJavaScriptSpaceAfterComma          *bool
	FormatJavaScriptSpaceAfterForSemicolon   *bool
	FormatJavaScriptSpaceAroundBinaryOps     *bool
	FormatJavaScriptSpaceAfterAnonFunction   *bool
	FormatJavaScriptSpaceAfterNamedFunction  *bool
	FormatJavaScriptSpaceBeforeConditional   *bool
	FormatJavaScriptSpaceInsideParentheses   *bool
	FormatJavaScriptSpaceInsideBrackets      *bool
	FormatJavaScriptSpaceInsideBraces        *bool
	FormatJavaScriptSpaceInsideEmptyBraces   *bool
	FormatJavaScriptTabSize                  int
	FormatJScriptInsertSpaces                *bool
	FormatJScriptTabSize                     int
	LegacyEncoding                           string
	Locale                                   string
	MemoryMaxCacheBytes                      int64
	MemoryDebugTelemetry                     bool
	NetworkProfile                           string
	NetworkStatCacheTTLMS                    int
	NetworkReadDirCacheTTLMS                 int
	NetworkIncludeReadConcurrency            int
	NetworkCaseResolution                    string
	ExcelLocale                              string
	ExcelIncludeRelatedIncludeTrees          bool
	ExcelSkipTypeInference                   bool
	ShowUnresolvedSymbolsInCompletion        bool
	StyleExtractionInsertionMode             string
	SyntaxSnippets                           bool
	IncludePaths                             []string
	VirtualRoots                             []string
	VirtualRoot                              string
	WorkspaceSymbolRename                    bool
	WorkspaceIncludeGlobs                    []string
	WorkspaceExcludeGlobs                    []string
	WorkspaceScanChunkSize                   int
	WorkspaceBusyAnalysisConcurrency         int
	WorkspaceRespectGitIgnore                bool
	IncrementalMode                          string
	IncrementalAnalysis                      bool
	UpdateIncludesOnFileRename               bool
	WindowsPathResolution                    bool
}

type vbscriptGlobalSetting struct {
	Type string `json:"type"`
	Kind string `json:"kind"`
}

func (s *vbscriptGlobalSetting) UnmarshalJSON(data []byte) error {
	var shorthand string
	if err := json.Unmarshal(data, &shorthand); err == nil {
		s.Type = strings.TrimSpace(shorthand)
		s.Kind = ""
		return nil
	}
	var value struct {
		Type string `json:"type"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.Type = strings.TrimSpace(value.Type)
	s.Kind = strings.ToLower(strings.TrimSpace(value.Kind))
	return nil
}

type vbscriptComTypeSetting struct {
	Members map[string]vbscriptComMemberSetting `json:"members"`
}

type vbscriptComMemberSetting struct {
	Kind       string                        `json:"kind"`
	Type       string                        `json:"type"`
	ReturnType string                        `json:"returnType"`
	Parameters []vbscriptComParameterSetting `json:"parameters"`
}

type vbscriptComParameterSetting struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Mode     string `json:"mode"`
	Optional bool   `json:"optional"`
	ByRef    bool   `json:"byRef"`
	ByVal    bool   `json:"byVal"`
}

func (s *vbscriptComParameterSetting) UnmarshalJSON(data []byte) error {
	var shorthand string
	if err := json.Unmarshal(data, &shorthand); err == nil {
		s.Name = ""
		s.Type = strings.TrimSpace(shorthand)
		s.Mode = "ByRef"
		s.Optional = false
		s.ByRef = false
		s.ByVal = false
		return nil
	}
	var value struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Mode     string `json:"mode"`
		Optional bool   `json:"optional"`
		ByRef    bool   `json:"byRef"`
		ByVal    bool   `json:"byVal"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.Name = strings.TrimSpace(value.Name)
	s.Type = strings.TrimSpace(value.Type)
	s.Mode = strings.TrimSpace(value.Mode)
	s.Optional = value.Optional
	s.ByRef = value.ByRef
	s.ByVal = value.ByVal
	return nil
}

func (s vbscriptComParameterSetting) modeName() string {
	mode := strings.ToLower(strings.TrimSpace(s.Mode))
	switch mode {
	case "byval", "by-value", "value":
		return "byval"
	case "byref", "by-reference", "reference":
		return "byref"
	}
	if s.ByVal {
		return "byval"
	}
	// VBScript parameters are ByRef when no passing mode is specified.
	return "byref"
}

func (s *vbscriptComMemberSetting) UnmarshalJSON(data []byte) error {
	var shorthand string
	if err := json.Unmarshal(data, &shorthand); err == nil {
		s.Type = strings.TrimSpace(shorthand)
		s.Kind = "property"
		return nil
	}
	var value struct {
		Kind       string                        `json:"kind"`
		Type       string                        `json:"type"`
		ReturnType string                        `json:"returnType"`
		Parameters []vbscriptComParameterSetting `json:"parameters"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.Kind = strings.ToLower(strings.TrimSpace(value.Kind))
	s.Type = strings.TrimSpace(value.Type)
	s.ReturnType = strings.TrimSpace(value.ReturnType)
	s.Parameters = value.Parameters
	return nil
}

func (s vbscriptGlobalSetting) typeName() string {
	return strings.TrimSpace(s.Type)
}

func (s vbscriptComMemberSetting) typeName() string {
	return s.declaredTypeName()
}

func (s vbscriptComMemberSetting) declaredTypeName() string {
	if s.ReturnType != "" {
		return strings.TrimSpace(s.ReturnType)
	}
	return strings.TrimSpace(s.Type)
}

func (s vbscriptComMemberSetting) declarationKind() string {
	switch s.Kind {
	case "method":
		return "method"
	case "field":
		return "field"
	default:
		if len(s.Parameters) > 0 && s.Kind == "" {
			return "method"
		}
		return "property"
	}
}

func firstVBTypeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parts, err := splitVBScriptTypeUnion(value); err == nil && len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	parts := strings.Split(value, "|")
	return strings.TrimSpace(parts[0])
}

func cloneVBScriptGlobals(values map[string]vbscriptGlobalSetting) map[string]vbscriptGlobalSetting {
	if values == nil {
		return nil
	}
	cloned := make(map[string]vbscriptGlobalSetting, len(values))
	for name, value := range values {
		cloned[name] = value
	}
	return cloned
}

func cloneVBScriptComTypes(values map[string]vbscriptComTypeSetting) map[string]vbscriptComTypeSetting {
	if values == nil {
		return nil
	}
	cloned := make(map[string]vbscriptComTypeSetting, len(values))
	for typeName, value := range values {
		memberClones := make(map[string]vbscriptComMemberSetting, len(value.Members))
		for memberName, member := range value.Members {
			if member.Parameters != nil {
				params := make([]vbscriptComParameterSetting, len(member.Parameters))
				copy(params, member.Parameters)
				member.Parameters = params
			}
			memberClones[memberName] = member
		}
		value.Members = memberClones
		cloned[typeName] = value
	}
	return cloned
}

const (
	defaultDiagnosticsDebounceMS    = 250
	defaultWorkspaceScanChunkSize   = 200
	defaultFlowchartLabelLineLength = 34
)

func defaultServerSettings() serverSettings {
	settings := serverSettings{
		CacheEnabled:                       true,
		CacheFreshness:                     "auto",
		CacheTTLHours:                      24 * 14,
		CacheMaxSizeMB:                     16384,
		CacheGzip:                          false,
		CodeLensReferences:                 true,
		CodeLensReferenceProcedures:        true,
		CodeLensReferenceGlobals:           true,
		CodeLensReferenceClasses:           true,
		CodeLensReferenceClassMembers:      true,
		CodeLensIncludeRelatedIncludeTrees: true,
		DefaultLanguage:                    "VBScript",
		DiagnosticsDebounceMS:              defaultDiagnosticsDebounceMS,
		GraphUseReverseIncludeIndex:        true,
		GraphWorkerSymbolExtraction:        false,
		FlowchartLabelLineLength:           defaultFlowchartLabelLineLength,
		GraphInitialViewMode:               "2d",
		GraphShowRootNodes:                 true,
		GraphShowFileNodes:                 true,
		GraphShowFunctionNodes:             true,
		GraphShowSubNodes:                  true,
		GraphShowClassNodes:                true,
		GraphShowGlobalVariableNodes:       true,
		GraphShowGlobalConstantNodes:       true,
		GraphShowUnresolvedNodes:           true,
		GraphHideSingleNodes:               true,
		GraphHideUnreferencedGlobalSymbols: true,
		GraphShowOutgoingSelectionLinks:    true,
		GraphShowIncludeLinks:              true,
		GraphShowDeclareLinks:              true,
		GraphShowReferenceLinks:            true,
		GraphShowAssignmentLinks:           true,
		GraphShowCallLinks:                 true,
		GraphShowUnresolvedLinks:           true,
		GraphShowMemberLinks:               true,
		GraphIncludeRelatedIncludeTrees:    true,
		InlayParameterNames:                true,
		MemoryMaxCacheBytes:                workspacepkg.DefaultMemoryMaxCacheBytes,
		NetworkProfile:                     "auto",
		NetworkStatCacheTTLMS:              -1,
		NetworkReadDirCacheTTLMS:           -1,
		NetworkCaseResolution:              "auto",
		SyntaxSnippets:                     true,
		VBScriptAutoIncludes:               false,
		VBScriptDeadCodeDiagnostics:        true,
		VBScriptUnusedDiagnostics:          true,
		VBScriptImplicitGlobalDiagnostics:  false,
		VBScriptSyntaxKeywords:             true,
		VBScriptAssumeUndefinedGlobals:     false,
		JavaScriptUnusedDiagnostics:        true,
		WindowsPathResolution:              true,
		ExcelIncludeRelatedIncludeTrees:    true,
		WorkspaceIncludeGlobs:              []string{"**/*.{asp,asa,inc,vbs}"},
		WorkspaceExcludeGlobs:              []string{},
		WorkspaceScanChunkSize:             defaultWorkspaceScanChunkSize,
		IncrementalMode:                    "full",
		IncrementalAnalysis:                true,
	}
	settings.resetFormatSettings()
	return settings
}

func (settings *serverSettings) resetFormatSettings() {
	settings.FormatCSSBraceStyle = ""
	settings.FormatCSSInsertSpaces = nil
	settings.FormatCSSNewlineBetweenRules = nil
	settings.FormatCSSNewlineBetweenSelectors = nil
	settings.FormatCSSSpaceAroundSelectorSeparator = nil
	settings.FormatCSSTabSize = 0
	settings.FormatCSSTagIndentMode = ""
	settings.FormatIgnoreCSSTagIndent = false
	settings.FormatCSSWrapLineLength = 0
	settings.FormatEndOfLine = ""
	settings.FormatEmbeddedLanguageFormatting = ""
	settings.FormatFragmentMode = ""
	settings.FormatIndentEmptyLines = nil
	settings.FormatHTMLInsertSpaces = nil
	settings.FormatHTMLContentUnformatted = ""
	settings.FormatHTMLExtraLiners = ""
	settings.FormatHTMLIndentInnerHTML = nil
	settings.FormatHTMLTabSize = 0
	settings.FormatHTMLUnformatted = ""
	settings.FormatHTMLWrapAttributes = ""
	settings.FormatHTMLWrapAttributesIndentSize = 0
	settings.FormatHTMLWrapLineLength = 0
	settings.FormatInsertSpaces = true
	settings.FormatEnabledLanguages = nil
	settings.FormatInsertFinalNewline = false
	settings.FormatMaxPreserveNewLines = nil
	settings.FormatNestedASPInCSSJS = "skipRegion"
	settings.FormatOnSave = false
	settings.FormatPreserveNewLines = nil
	settings.FormatPrintWidth = 0
	settings.FormatRespectDisableRegions = true
	settings.FormatTabSize = 2
	settings.FormatVBScriptTabSize = 0
	settings.FormatVBScriptInsertSpaces = nil
	settings.FormatVBScriptKeywordCase = ""
	settings.FormatVBScriptLineContinuationIndentSize = 0
	settings.FormatVBScriptSelectCaseIndent = ""
	settings.FormatVBScriptBlockIndent = ""
	settings.FormatVBScriptTagIndentMode = ""
	settings.FormatIgnoreVBScriptTagIndent = false
	settings.FormatUppercaseKeywords = false
	settings.FormatAlignAssignments = false
	settings.FormatASPDelimiterSpacing = ""
	settings.FormatASPBlockNewline = ""
	settings.FormatJavaScriptBraceStyle = ""
	settings.FormatJavaScriptBraceFunctionsNewLine = nil
	settings.FormatJavaScriptBraceControlNewLine = nil
	settings.FormatJavaScriptInsertSpaces = nil
	settings.FormatJavaScriptTagIndentMode = ""
	settings.FormatIgnoreJavaScriptTagIndent = false
	settings.FormatJavaScriptSemicolons = ""
	settings.FormatJavaScriptIndentSwitchCase = nil
	settings.FormatJavaScriptSpaceAfterComma = nil
	settings.FormatJavaScriptSpaceAfterForSemicolon = nil
	settings.FormatJavaScriptSpaceAroundBinaryOps = nil
	settings.FormatJavaScriptSpaceAfterAnonFunction = nil
	settings.FormatJavaScriptSpaceAfterNamedFunction = nil
	settings.FormatJavaScriptSpaceBeforeConditional = nil
	settings.FormatJavaScriptSpaceInsideParentheses = nil
	settings.FormatJavaScriptSpaceInsideBrackets = nil
	settings.FormatJavaScriptSpaceInsideBraces = nil
	settings.FormatJavaScriptSpaceInsideEmptyBraces = nil
	settings.FormatJavaScriptTabSize = 0
	settings.FormatJScriptInsertSpaces = nil
	settings.FormatJScriptTabSize = 0
}
