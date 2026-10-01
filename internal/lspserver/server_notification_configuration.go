package lspserver

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type graphConfigurationSnapshot struct {
	initialViewMode               string
	showRootNodes                 bool
	showFileNodes                 bool
	showFunctionNodes             bool
	showSubNodes                  bool
	showClassNodes                bool
	showMethodNodes               bool
	showMethodFunctionNodes       bool
	showMethodSubNodes            bool
	showPropertyNodes             bool
	showMemberNodes               bool
	showGlobalVariableNodes       bool
	showGlobalConstantNodes       bool
	showLocalVariableNodes        bool
	showLocalConstantNodes        bool
	showParameterNodes            bool
	showUnresolvedNodes           bool
	hideSingleNodes               bool
	hideUnreferencedGlobalSymbols bool
	showOutgoingSelectionLinks    bool
	showIncludeLinks              bool
	showDeclareLinks              bool
	showReferenceLinks            bool
	showAssignmentLinks           bool
	showCallLinks                 bool
	showUnresolvedLinks           bool
	showMemberLinks               bool
	showIncomingDocumentIncludes  bool
	showIncomingFolderIncludes    bool
	includeRelatedIncludeTrees    bool
	useReverseIncludeIndex        bool
	workerSymbolExtraction        bool
}

func snapshotGraphConfiguration(settings serverSettings) graphConfigurationSnapshot {
	return graphConfigurationSnapshot{
		initialViewMode: settings.GraphInitialViewMode,
		showRootNodes:   settings.GraphShowRootNodes, showFileNodes: settings.GraphShowFileNodes,
		showFunctionNodes: settings.GraphShowFunctionNodes, showSubNodes: settings.GraphShowSubNodes,
		showClassNodes: settings.GraphShowClassNodes, showMethodNodes: settings.GraphShowMethodNodes,
		showMethodFunctionNodes: settings.GraphShowMethodFunctionNodes, showMethodSubNodes: settings.GraphShowMethodSubNodes,
		showPropertyNodes: settings.GraphShowPropertyNodes, showMemberNodes: settings.GraphShowMemberNodes,
		showGlobalVariableNodes: settings.GraphShowGlobalVariableNodes, showGlobalConstantNodes: settings.GraphShowGlobalConstantNodes,
		showLocalVariableNodes: settings.GraphShowLocalVariableNodes, showLocalConstantNodes: settings.GraphShowLocalConstantNodes,
		showParameterNodes: settings.GraphShowParameterNodes, showUnresolvedNodes: settings.GraphShowUnresolvedNodes,
		hideSingleNodes: settings.GraphHideSingleNodes, hideUnreferencedGlobalSymbols: settings.GraphHideUnreferencedGlobalSymbols,
		showOutgoingSelectionLinks: settings.GraphShowOutgoingSelectionLinks, showIncludeLinks: settings.GraphShowIncludeLinks,
		showDeclareLinks: settings.GraphShowDeclareLinks, showReferenceLinks: settings.GraphShowReferenceLinks,
		showAssignmentLinks: settings.GraphShowAssignmentLinks, showCallLinks: settings.GraphShowCallLinks,
		showUnresolvedLinks: settings.GraphShowUnresolvedLinks, showMemberLinks: settings.GraphShowMemberLinks,
		showIncomingDocumentIncludes: settings.GraphShowIncomingDocumentIncludes,
		showIncomingFolderIncludes:   settings.GraphShowIncomingFolderIncludes,
		includeRelatedIncludeTrees:   settings.GraphIncludeRelatedIncludeTrees,
		useReverseIncludeIndex:       settings.GraphUseReverseIncludeIndex,
		workerSymbolExtraction:       settings.GraphWorkerSymbolExtraction,
	}
}

func normalizeWorkspaceGlobList(values []string) []string {
	if values == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimPrefix(filepath.ToSlash(value), "./")
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized
}

func (s *Server) handleDidChangeConfiguration(params json.RawMessage) error {
	var p changeConfigurationParams
	if err := decodeNotificationParams(params, &p); err != nil {
		return err
	}
	previousResolvedNetworkProfile := s.resolveNetworkProfile()
	var normalizedCacheDirectory *string
	if p.Settings.AspLsp.Cache.Directory != nil {
		directory := s.normalizeConfiguredCacheDirectory(*p.Settings.AspLsp.Cache.Directory)
		normalizedCacheDirectory = &directory
	}
	reindexWorkspace := false
	includeResolutionChanged := false
	referenceSettingsChanged := false
	referenceCacheCleared := false
	runtimeCacheSettingsChanged := false
	filesystemSettingsChanged := false
	diagnosticsSettingsChanged := false
	semanticTokenCacheChanged := false
	analysisWorkersChanged := false
	networkProfileSettingChanged := false
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return nil
	}
	previousSettings := s.settings
	if p.Settings.AspLsp.DefaultLanguage != nil {
		defaultLanguage := normalizeDefaultLanguage(*p.Settings.AspLsp.DefaultLanguage)
		if defaultLanguage != s.settings.DefaultLanguage {
			s.settings.DefaultLanguage = defaultLanguage
			s.resetJavaScriptProjectLocked()
			reindexWorkspace = true
			semanticTokenCacheChanged = true
			referenceSettingsChanged = true
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Locale != nil {
		locale := normalizeConfiguredLocale(*p.Settings.AspLsp.Locale, s.clientLocale)
		if locale != s.settings.Locale {
			s.settings.Locale = locale
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Excel.Locale != nil {
		s.settings.ExcelLocale = normalizeExcelLocale(*p.Settings.AspLsp.Excel.Locale)
	}
	if p.Settings.AspLsp.Excel.IncludeRelatedIncludeTreesForUnresolved != nil {
		s.settings.ExcelIncludeRelatedIncludeTrees = *p.Settings.AspLsp.Excel.IncludeRelatedIncludeTreesForUnresolved
	}
	if p.Settings.AspLsp.Excel.SkipTypeInference != nil {
		s.settings.ExcelSkipTypeInference = *p.Settings.AspLsp.Excel.SkipTypeInference
	}
	if p.Settings.AspLsp.LegacyEncoding != nil {
		legacyEncoding := normalizeLegacyEncoding(*p.Settings.AspLsp.LegacyEncoding)
		if legacyEncoding != s.settings.LegacyEncoding {
			s.settings.LegacyEncoding = legacyEncoding
			reindexWorkspace = true
		}
	}
	if p.Settings.AspLsp.CheckJS != nil {
		if s.settings.CheckJS != *p.Settings.AspLsp.CheckJS {
			s.settings.CheckJS = *p.Settings.AspLsp.CheckJS
			s.resetJavaScriptProjectLocked()
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Debug.Output != nil || p.Settings.AspLsp.Debug.LogFile.Enabled != nil || p.Settings.AspLsp.Debug.LogFile.Path != nil {
		if p.Settings.AspLsp.Debug.Output != nil {
			s.settings.DebugOutput = normalizeDebugOutput(*p.Settings.AspLsp.Debug.Output)
		}
		if p.Settings.AspLsp.Debug.LogFile.Enabled != nil {
			s.settings.DebugLogFileEnabled = *p.Settings.AspLsp.Debug.LogFile.Enabled
		}
		if p.Settings.AspLsp.Debug.LogFile.Path != nil {
			s.settings.DebugLogFilePath = *p.Settings.AspLsp.Debug.LogFile.Path
		}
	}
	if p.Settings.AspLsp.Cache.Enabled != nil {
		if cacheEnabled := *p.Settings.AspLsp.Cache.Enabled; cacheEnabled != s.settings.CacheEnabled {
			s.settings.CacheEnabled = cacheEnabled
			runtimeCacheSettingsChanged = true
		}
	}
	if normalizedCacheDirectory != nil {
		if directory := *normalizedCacheDirectory; directory != s.settings.CacheDirectory {
			s.settings.CacheDirectory = directory
			runtimeCacheSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Cache.Freshness != nil {
		if freshness := strings.ToLower(strings.TrimSpace(*p.Settings.AspLsp.Cache.Freshness)); freshness != s.settings.CacheFreshness {
			s.settings.CacheFreshness = freshness
			runtimeCacheSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Cache.TTLHours != nil && *p.Settings.AspLsp.Cache.TTLHours > 0 {
		if ttlHours := boundedDiskCacheTTLHours(*p.Settings.AspLsp.Cache.TTLHours); ttlHours != s.settings.CacheTTLHours {
			s.settings.CacheTTLHours = ttlHours
			runtimeCacheSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Cache.MaxSizeMB != nil && *p.Settings.AspLsp.Cache.MaxSizeMB > 0 {
		if maxSizeMB := boundedDiskCacheMaxSizeMB(*p.Settings.AspLsp.Cache.MaxSizeMB); maxSizeMB != s.settings.CacheMaxSizeMB {
			s.settings.CacheMaxSizeMB = maxSizeMB
			runtimeCacheSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Cache.Gzip != nil {
		if gzip := *p.Settings.AspLsp.Cache.Gzip; gzip != s.settings.CacheGzip {
			s.settings.CacheGzip = gzip
			runtimeCacheSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Memory.MaxCacheBytes != nil && *p.Settings.AspLsp.Memory.MaxCacheBytes > 0 {
		s.settings.MemoryMaxCacheBytes = *p.Settings.AspLsp.Memory.MaxCacheBytes
	}
	if p.Settings.AspLsp.Memory.DebugTelemetry != nil {
		s.settings.MemoryDebugTelemetry = *p.Settings.AspLsp.Memory.DebugTelemetry
	}
	if p.Settings.AspLsp.Network.Profile != nil {
		if profile := strings.ToLower(strings.TrimSpace(*p.Settings.AspLsp.Network.Profile)); profile != s.settings.NetworkProfile {
			s.settings.NetworkProfile = profile
			filesystemSettingsChanged = true
			networkProfileSettingChanged = true
		}
	}
	if p.Settings.AspLsp.Network.StatCacheTTLMS != nil {
		if ttlMS := *p.Settings.AspLsp.Network.StatCacheTTLMS; ttlMS != s.settings.NetworkStatCacheTTLMS {
			s.settings.NetworkStatCacheTTLMS = ttlMS
			filesystemSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Network.ReadDirCacheTTLMS != nil {
		if ttlMS := *p.Settings.AspLsp.Network.ReadDirCacheTTLMS; ttlMS != s.settings.NetworkReadDirCacheTTLMS {
			s.settings.NetworkReadDirCacheTTLMS = ttlMS
			filesystemSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Network.IncludeReadConcurrency != nil {
		if concurrency := *p.Settings.AspLsp.Network.IncludeReadConcurrency; concurrency != s.settings.NetworkIncludeReadConcurrency {
			s.settings.NetworkIncludeReadConcurrency = concurrency
			filesystemSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Network.CaseResolution != nil {
		caseResolution := strings.ToLower(strings.TrimSpace(*p.Settings.AspLsp.Network.CaseResolution))
		if caseResolution != s.settings.NetworkCaseResolution {
			s.settings.NetworkCaseResolution = caseResolution
			filesystemSettingsChanged = true
			includeResolutionChanged = true
			referenceSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Diagnostics.DebounceMS != nil {
		if *p.Settings.AspLsp.Diagnostics.DebounceMS >= 0 {
			s.settings.DiagnosticsDebounceMS = *p.Settings.AspLsp.Diagnostics.DebounceMS
		}
	}
	if p.Settings.AspLsp.Incremental.Mode != nil {
		s.settings.IncrementalMode = normalizeIncrementalMode(*p.Settings.AspLsp.Incremental.Mode)
	}
	if p.Settings.AspLsp.Incremental.Analysis != nil {
		s.settings.IncrementalAnalysis = *p.Settings.AspLsp.Incremental.Analysis
	}
	if p.Settings.AspLsp.CodeLens.Includes != nil {
		s.settings.CodeLensIncludes = *p.Settings.AspLsp.CodeLens.Includes
	}
	if p.Settings.AspLsp.CodeLens.References != nil {
		s.settings.CodeLensReferences = *p.Settings.AspLsp.CodeLens.References
	}
	if value := p.Settings.AspLsp.CodeLens.ReferenceProcedures; value != nil && s.settings.CodeLensReferenceProcedures != *value {
		s.settings.CodeLensReferenceProcedures = *value
		referenceSettingsChanged = true
	}
	if value := p.Settings.AspLsp.CodeLens.ReferenceGlobals; value != nil && s.settings.CodeLensReferenceGlobals != *value {
		s.settings.CodeLensReferenceGlobals = *value
		referenceSettingsChanged = true
	}
	if value := p.Settings.AspLsp.CodeLens.ReferenceClasses; value != nil && s.settings.CodeLensReferenceClasses != *value {
		s.settings.CodeLensReferenceClasses = *value
		referenceSettingsChanged = true
	}
	if value := p.Settings.AspLsp.CodeLens.ReferenceClassMembers; value != nil && s.settings.CodeLensReferenceClassMembers != *value {
		s.settings.CodeLensReferenceClassMembers = *value
		referenceSettingsChanged = true
	}
	if p.Settings.AspLsp.CodeLens.IncludeRelatedIncludeTreesForUnresolved != nil {
		includeRelated := *p.Settings.AspLsp.CodeLens.IncludeRelatedIncludeTreesForUnresolved
		if includeRelated != s.settings.CodeLensIncludeRelatedIncludeTrees {
			s.settings.CodeLensIncludeRelatedIncludeTrees = includeRelated
			referenceSettingsChanged = true
		}
	}
	if referenceSettingsChanged {
		s.clearWorkspaceReferenceCacheLocked()
		referenceCacheCleared = true
	}
	if p.Settings.AspLsp.WindowsPathResolution != nil {
		if s.settings.WindowsPathResolution != *p.Settings.AspLsp.WindowsPathResolution {
			s.settings.WindowsPathResolution = *p.Settings.AspLsp.WindowsPathResolution
			includeResolutionChanged = true
			referenceSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.IncludePaths != nil {
		includePaths := normalizeVirtualRoots(p.Settings.AspLsp.IncludePaths)
		if !stringSlicesEqual(includePaths, s.settings.IncludePaths) {
			s.settings.IncludePaths = includePaths
			includeResolutionChanged = true
		}
	}
	if p.Settings.AspLsp.VirtualRoots != nil {
		virtualRoots := normalizeVirtualRoots(p.Settings.AspLsp.VirtualRoots)
		if !stringSlicesEqual(virtualRoots, s.settings.VirtualRoots) {
			s.settings.VirtualRoots = virtualRoots
			includeResolutionChanged = true
		}
	}
	if p.Settings.AspLsp.VirtualRoot != nil {
		virtualRoot := strings.TrimSpace(*p.Settings.AspLsp.VirtualRoot)
		if virtualRoot != s.settings.VirtualRoot {
			s.settings.VirtualRoot = virtualRoot
			includeResolutionChanged = true
		}
	}
	if p.Settings.AspLsp.Workspace.Includes != nil {
		includeGlobs := normalizeWorkspaceGlobList(p.Settings.AspLsp.Workspace.Includes)
		if len(includeGlobs) == 0 {
			includeGlobs = []string{"**/*.{asp,asa,inc,vbs}"}
		}
		if !stringSlicesEqual(includeGlobs, s.settings.WorkspaceIncludeGlobs) {
			s.settings.WorkspaceIncludeGlobs = append([]string(nil), includeGlobs...)
			reindexWorkspace = true
		}
	}
	if p.Settings.AspLsp.Workspace.Excludes != nil {
		excludeGlobs := normalizeWorkspaceGlobList(p.Settings.AspLsp.Workspace.Excludes)
		if !stringSlicesEqual(excludeGlobs, s.settings.WorkspaceExcludeGlobs) {
			s.settings.WorkspaceExcludeGlobs = append([]string(nil), excludeGlobs...)
			reindexWorkspace = true
		}
	}
	if p.Settings.AspLsp.Workspace.ScanChunkSize != nil && *p.Settings.AspLsp.Workspace.ScanChunkSize > 0 {
		if scanChunkSize := *p.Settings.AspLsp.Workspace.ScanChunkSize; scanChunkSize != s.settings.WorkspaceScanChunkSize {
			s.settings.WorkspaceScanChunkSize = scanChunkSize
			reindexWorkspace = true
		}
	}
	if p.Settings.AspLsp.Workspace.BusyAnalysisConcurrency != nil && *p.Settings.AspLsp.Workspace.BusyAnalysisConcurrency >= 0 {
		if concurrency := *p.Settings.AspLsp.Workspace.BusyAnalysisConcurrency; concurrency != s.settings.WorkspaceBusyAnalysisConcurrency {
			s.settings.WorkspaceBusyAnalysisConcurrency = concurrency
			analysisWorkersChanged = true
		}
	}
	if p.Settings.AspLsp.Workspace.RespectGitIgnore != nil {
		if respectGitIgnore := *p.Settings.AspLsp.Workspace.RespectGitIgnore; respectGitIgnore != s.settings.WorkspaceRespectGitIgnore {
			s.settings.WorkspaceRespectGitIgnore = respectGitIgnore
			s.invalidateGitIgnoreGlobs()
			reindexWorkspace = true
		}
	}
	if p.Settings.AspLsp.Format.OnSave != nil {
		s.settings.FormatOnSave = *p.Settings.AspLsp.Format.OnSave
	}
	if p.Settings.AspLsp.Format.IndentSize != nil && *p.Settings.AspLsp.Format.IndentSize > 0 {
		s.settings.FormatTabSize = *p.Settings.AspLsp.Format.IndentSize
	}
	if p.Settings.AspLsp.Format.IndentStyle != nil {
		s.settings.FormatInsertSpaces = *p.Settings.AspLsp.Format.IndentStyle != "tab"
	}
	if p.Settings.AspLsp.Format.PrintWidth != nil && *p.Settings.AspLsp.Format.PrintWidth > 0 {
		s.settings.FormatPrintWidth = *p.Settings.AspLsp.Format.PrintWidth
	}
	if p.Settings.AspLsp.Format.EndOfLine != nil {
		s.settings.FormatEndOfLine = normalizeFormatEndOfLine(*p.Settings.AspLsp.Format.EndOfLine)
	}
	if p.Settings.AspLsp.Format.EmbeddedLanguageFormatting != nil {
		s.settings.FormatEmbeddedLanguageFormatting = normalizeEmbeddedLanguageFormatting(*p.Settings.AspLsp.Format.EmbeddedLanguageFormatting)
	}
	if p.Settings.AspLsp.Format.FragmentMode != nil {
		s.settings.FormatFragmentMode = normalizeFormatFragmentMode(*p.Settings.AspLsp.Format.FragmentMode)
	}
	if p.Settings.AspLsp.Format.InsertFinalNewline != nil {
		s.settings.FormatInsertFinalNewline = *p.Settings.AspLsp.Format.InsertFinalNewline
	}
	if p.Settings.AspLsp.Format.PreserveNewLines != nil {
		s.settings.FormatPreserveNewLines = p.Settings.AspLsp.Format.PreserveNewLines
	}
	if p.Settings.AspLsp.Format.MaxPreserveNewLines != nil && *p.Settings.AspLsp.Format.MaxPreserveNewLines >= 0 {
		s.settings.FormatMaxPreserveNewLines = p.Settings.AspLsp.Format.MaxPreserveNewLines
	}
	if p.Settings.AspLsp.Format.IndentEmptyLines != nil {
		s.settings.FormatIndentEmptyLines = p.Settings.AspLsp.Format.IndentEmptyLines
	}
	if p.Settings.AspLsp.Format.EnabledLanguages != nil {
		s.settings.FormatEnabledLanguages = append([]string(nil), p.Settings.AspLsp.Format.EnabledLanguages...)
	}
	if p.Settings.AspLsp.Format.HTMLIndentSize != nil && *p.Settings.AspLsp.Format.HTMLIndentSize > 0 {
		s.settings.FormatHTMLTabSize = *p.Settings.AspLsp.Format.HTMLIndentSize
	}
	if p.Settings.AspLsp.Format.HTMLIndentStyle != nil {
		insertSpaces := *p.Settings.AspLsp.Format.HTMLIndentStyle != "tab"
		s.settings.FormatHTMLInsertSpaces = &insertSpaces
	}
	if p.Settings.AspLsp.Format.HTMLWrapLineLength != nil && *p.Settings.AspLsp.Format.HTMLWrapLineLength >= 0 {
		s.settings.FormatHTMLWrapLineLength = *p.Settings.AspLsp.Format.HTMLWrapLineLength
	}
	if p.Settings.AspLsp.Format.HTMLWrapAttributes != nil {
		s.settings.FormatHTMLWrapAttributes = *p.Settings.AspLsp.Format.HTMLWrapAttributes
	}
	if p.Settings.AspLsp.Format.HTMLWrapAttributesIndentSize != nil && *p.Settings.AspLsp.Format.HTMLWrapAttributesIndentSize > 0 {
		s.settings.FormatHTMLWrapAttributesIndentSize = *p.Settings.AspLsp.Format.HTMLWrapAttributesIndentSize
	}
	if p.Settings.AspLsp.Format.HTMLIndentInnerHTML != nil {
		s.settings.FormatHTMLIndentInnerHTML = p.Settings.AspLsp.Format.HTMLIndentInnerHTML
	}
	if p.Settings.AspLsp.Format.HTMLUnformatted != nil {
		s.settings.FormatHTMLUnformatted = *p.Settings.AspLsp.Format.HTMLUnformatted
	}
	if p.Settings.AspLsp.Format.HTMLContentUnformatted != nil {
		s.settings.FormatHTMLContentUnformatted = *p.Settings.AspLsp.Format.HTMLContentUnformatted
	}
	if p.Settings.AspLsp.Format.HTMLExtraLiners != nil {
		s.settings.FormatHTMLExtraLiners = *p.Settings.AspLsp.Format.HTMLExtraLiners
	}
	if p.Settings.AspLsp.Format.CSSIndentSize != nil && *p.Settings.AspLsp.Format.CSSIndentSize > 0 {
		s.settings.FormatCSSTabSize = *p.Settings.AspLsp.Format.CSSIndentSize
	}
	if p.Settings.AspLsp.Format.CSSIndentStyle != nil {
		insertSpaces := *p.Settings.AspLsp.Format.CSSIndentStyle != "tab"
		s.settings.FormatCSSInsertSpaces = &insertSpaces
	}
	if p.Settings.AspLsp.Format.CSSWrapLineLength != nil && *p.Settings.AspLsp.Format.CSSWrapLineLength >= 0 {
		s.settings.FormatCSSWrapLineLength = *p.Settings.AspLsp.Format.CSSWrapLineLength
	}
	if p.Settings.AspLsp.Format.CSSNewlineBetweenRules != nil {
		s.settings.FormatCSSNewlineBetweenRules = p.Settings.AspLsp.Format.CSSNewlineBetweenRules
	}
	if p.Settings.AspLsp.Format.CSSNewlineBetweenSelectors != nil {
		s.settings.FormatCSSNewlineBetweenSelectors = p.Settings.AspLsp.Format.CSSNewlineBetweenSelectors
	}
	if p.Settings.AspLsp.Format.CSSSpaceAroundSelectorSeparator != nil {
		s.settings.FormatCSSSpaceAroundSelectorSeparator = p.Settings.AspLsp.Format.CSSSpaceAroundSelectorSeparator
	}
	if p.Settings.AspLsp.Format.CSSBraceStyle != nil {
		s.settings.FormatCSSBraceStyle = normalizeCSSBraceStyle(*p.Settings.AspLsp.Format.CSSBraceStyle)
	}
	if p.Settings.AspLsp.Format.CSSTagIndentMode != nil {
		s.settings.FormatCSSTagIndentMode = normalizeFormatTagIndentMode(*p.Settings.AspLsp.Format.CSSTagIndentMode)
	}
	if p.Settings.AspLsp.Format.IgnoreCSSTagIndent != nil {
		s.settings.FormatIgnoreCSSTagIndent = *p.Settings.AspLsp.Format.IgnoreCSSTagIndent
	}
	if p.Settings.AspLsp.Format.JavaScriptIndentSize != nil && *p.Settings.AspLsp.Format.JavaScriptIndentSize > 0 {
		s.settings.FormatJavaScriptTabSize = *p.Settings.AspLsp.Format.JavaScriptIndentSize
	}
	if p.Settings.AspLsp.Format.JavaScriptIndentStyle != nil {
		insertSpaces := *p.Settings.AspLsp.Format.JavaScriptIndentStyle != "tab"
		s.settings.FormatJavaScriptInsertSpaces = &insertSpaces
	}
	if p.Settings.AspLsp.Format.JScriptIndentSize != nil && *p.Settings.AspLsp.Format.JScriptIndentSize > 0 {
		s.settings.FormatJScriptTabSize = *p.Settings.AspLsp.Format.JScriptIndentSize
	}
	if p.Settings.AspLsp.Format.JScriptIndentStyle != nil {
		insertSpaces := *p.Settings.AspLsp.Format.JScriptIndentStyle != "tab"
		s.settings.FormatJScriptInsertSpaces = &insertSpaces
	}
	if p.Settings.AspLsp.Format.JavaScriptSemicolons != nil {
		s.settings.FormatJavaScriptSemicolons = normalizeJavaScriptSemicolons(*p.Settings.AspLsp.Format.JavaScriptSemicolons)
	}
	if p.Settings.AspLsp.Format.JavaScriptIndentSwitchCase != nil {
		s.settings.FormatJavaScriptIndentSwitchCase = p.Settings.AspLsp.Format.JavaScriptIndentSwitchCase
	}
	if p.Settings.AspLsp.Format.JavaScriptPlaceOpenBraceOnNewLineForFunctions != nil {
		s.settings.FormatJavaScriptBraceFunctionsNewLine = p.Settings.AspLsp.Format.JavaScriptPlaceOpenBraceOnNewLineForFunctions
	}
	if p.Settings.AspLsp.Format.JavaScriptPlaceOpenBraceOnNewLineForControlBlocks != nil {
		s.settings.FormatJavaScriptBraceControlNewLine = p.Settings.AspLsp.Format.JavaScriptPlaceOpenBraceOnNewLineForControlBlocks
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterCommaDelimiter != nil {
		s.settings.FormatJavaScriptSpaceAfterComma = p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterCommaDelimiter
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterSemicolonInForStatements != nil {
		s.settings.FormatJavaScriptSpaceAfterForSemicolon = p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterSemicolonInForStatements
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceBeforeAndAfterBinaryOperators != nil {
		s.settings.FormatJavaScriptSpaceAroundBinaryOps = p.Settings.AspLsp.Format.JavaScriptInsertSpaceBeforeAndAfterBinaryOperators
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions != nil {
		s.settings.FormatJavaScriptSpaceAfterAnonFunction = p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceBeforeFunctionParenthesis != nil {
		s.settings.FormatJavaScriptSpaceAfterNamedFunction = p.Settings.AspLsp.Format.JavaScriptInsertSpaceBeforeFunctionParenthesis
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterKeywordsInControlFlowStatements != nil {
		s.settings.FormatJavaScriptSpaceBeforeConditional = p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterKeywordsInControlFlowStatements
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis != nil {
		s.settings.FormatJavaScriptSpaceInsideParentheses = p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets != nil {
		s.settings.FormatJavaScriptSpaceInsideBrackets = p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces != nil {
		s.settings.FormatJavaScriptSpaceInsideBraces = p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces
	}
	if p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces != nil {
		s.settings.FormatJavaScriptSpaceInsideEmptyBraces = p.Settings.AspLsp.Format.JavaScriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces
	}
	if p.Settings.AspLsp.Format.JavaScriptTagIndentMode != nil {
		s.settings.FormatJavaScriptTagIndentMode = normalizeFormatTagIndentMode(*p.Settings.AspLsp.Format.JavaScriptTagIndentMode)
	}
	if p.Settings.AspLsp.Format.IgnoreJavaScriptTagIndent != nil {
		s.settings.FormatIgnoreJavaScriptTagIndent = *p.Settings.AspLsp.Format.IgnoreJavaScriptTagIndent
	}
	if p.Settings.AspLsp.Format.NestedASPInCSSJS != nil {
		s.settings.FormatNestedASPInCSSJS = normalizeNestedASPInCSSJS(*p.Settings.AspLsp.Format.NestedASPInCSSJS)
	}
	if p.Settings.AspLsp.Format.VBScriptIndentSize != nil && *p.Settings.AspLsp.Format.VBScriptIndentSize > 0 {
		s.settings.FormatVBScriptTabSize = *p.Settings.AspLsp.Format.VBScriptIndentSize
	}
	if p.Settings.AspLsp.Format.VBScriptIndentStyle != nil {
		insertSpaces := *p.Settings.AspLsp.Format.VBScriptIndentStyle != "tab"
		s.settings.FormatVBScriptInsertSpaces = &insertSpaces
	}
	if p.Settings.AspLsp.Format.VBScriptKeywordCase != nil {
		s.settings.FormatVBScriptKeywordCase = *p.Settings.AspLsp.Format.VBScriptKeywordCase
	}
	if p.Settings.AspLsp.Format.VBScriptLineContinuationIndentSize != nil && *p.Settings.AspLsp.Format.VBScriptLineContinuationIndentSize >= 0 {
		s.settings.FormatVBScriptLineContinuationIndentSize = *p.Settings.AspLsp.Format.VBScriptLineContinuationIndentSize
	}
	if p.Settings.AspLsp.Format.VBScriptSelectCaseIndent != nil {
		s.settings.FormatVBScriptSelectCaseIndent = *p.Settings.AspLsp.Format.VBScriptSelectCaseIndent
	}
	if p.Settings.AspLsp.Format.VBScriptBlockIndent != nil {
		s.settings.FormatVBScriptBlockIndent = *p.Settings.AspLsp.Format.VBScriptBlockIndent
	}
	if p.Settings.AspLsp.Format.VBScriptTagIndentMode != nil {
		s.settings.FormatVBScriptTagIndentMode = normalizeFormatTagIndentMode(*p.Settings.AspLsp.Format.VBScriptTagIndentMode)
	}
	if p.Settings.AspLsp.Format.IgnoreVBScriptTagIndent != nil {
		s.settings.FormatIgnoreVBScriptTagIndent = *p.Settings.AspLsp.Format.IgnoreVBScriptTagIndent
	}
	if p.Settings.AspLsp.Format.UppercaseKeywords != nil {
		s.settings.FormatUppercaseKeywords = *p.Settings.AspLsp.Format.UppercaseKeywords
		if *p.Settings.AspLsp.Format.UppercaseKeywords && p.Settings.AspLsp.Format.VBScriptKeywordCase == nil {
			s.settings.FormatVBScriptKeywordCase = ""
		}
	}
	if p.Settings.AspLsp.Format.AlignAssignments != nil {
		s.settings.FormatAlignAssignments = *p.Settings.AspLsp.Format.AlignAssignments
	}
	if p.Settings.AspLsp.Format.ASPDelimiterSpacing != nil {
		s.settings.FormatASPDelimiterSpacing = *p.Settings.AspLsp.Format.ASPDelimiterSpacing
	}
	if p.Settings.AspLsp.Format.ASPBlockNewline != nil {
		s.settings.FormatASPBlockNewline = *p.Settings.AspLsp.Format.ASPBlockNewline
	}
	if p.Settings.AspLsp.Format.RespectDisableRegions != nil {
		s.settings.FormatRespectDisableRegions = *p.Settings.AspLsp.Format.RespectDisableRegions
	}
	if p.Settings.AspLsp.Graph.InitialViewMode != nil {
		if *p.Settings.AspLsp.Graph.InitialViewMode == "3d" {
			s.settings.GraphInitialViewMode = "3d"
		} else {
			s.settings.GraphInitialViewMode = "2d"
		}
	}
	if p.Settings.AspLsp.Graph.ShowRootNodes != nil {
		s.settings.GraphShowRootNodes = *p.Settings.AspLsp.Graph.ShowRootNodes
	}
	if p.Settings.AspLsp.Graph.ShowFileNodes != nil {
		s.settings.GraphShowFileNodes = *p.Settings.AspLsp.Graph.ShowFileNodes
	}
	if p.Settings.AspLsp.Graph.ShowFunctionNodes != nil {
		s.settings.GraphShowFunctionNodes = *p.Settings.AspLsp.Graph.ShowFunctionNodes
	}
	if p.Settings.AspLsp.Graph.ShowSubNodes != nil {
		s.settings.GraphShowSubNodes = *p.Settings.AspLsp.Graph.ShowSubNodes
	}
	if p.Settings.AspLsp.Graph.ShowClassNodes != nil {
		s.settings.GraphShowClassNodes = *p.Settings.AspLsp.Graph.ShowClassNodes
	}
	if p.Settings.AspLsp.Graph.ShowMethodNodes != nil {
		s.settings.GraphShowMethodNodes = *p.Settings.AspLsp.Graph.ShowMethodNodes
	}
	if p.Settings.AspLsp.Graph.ShowMethodFunctionNodes != nil {
		s.settings.GraphShowMethodFunctionNodes = *p.Settings.AspLsp.Graph.ShowMethodFunctionNodes
	}
	if p.Settings.AspLsp.Graph.ShowMethodSubNodes != nil {
		s.settings.GraphShowMethodSubNodes = *p.Settings.AspLsp.Graph.ShowMethodSubNodes
	}
	if p.Settings.AspLsp.Graph.ShowPropertyNodes != nil {
		s.settings.GraphShowPropertyNodes = *p.Settings.AspLsp.Graph.ShowPropertyNodes
	}
	if p.Settings.AspLsp.Graph.ShowMemberNodes != nil {
		s.settings.GraphShowMemberNodes = *p.Settings.AspLsp.Graph.ShowMemberNodes
	}
	if p.Settings.AspLsp.Graph.ShowGlobalVariableNodes != nil {
		s.settings.GraphShowGlobalVariableNodes = *p.Settings.AspLsp.Graph.ShowGlobalVariableNodes
	}
	if p.Settings.AspLsp.Graph.ShowGlobalConstantNodes != nil {
		s.settings.GraphShowGlobalConstantNodes = *p.Settings.AspLsp.Graph.ShowGlobalConstantNodes
	}
	if p.Settings.AspLsp.Graph.ShowLocalVariableNodes != nil {
		s.settings.GraphShowLocalVariableNodes = *p.Settings.AspLsp.Graph.ShowLocalVariableNodes
	}
	if p.Settings.AspLsp.Graph.ShowLocalConstantNodes != nil {
		s.settings.GraphShowLocalConstantNodes = *p.Settings.AspLsp.Graph.ShowLocalConstantNodes
	}
	if p.Settings.AspLsp.Graph.ShowParameterNodes != nil {
		s.settings.GraphShowParameterNodes = *p.Settings.AspLsp.Graph.ShowParameterNodes
	}
	if p.Settings.AspLsp.Graph.ShowUnresolvedNodes != nil {
		s.settings.GraphShowUnresolvedNodes = *p.Settings.AspLsp.Graph.ShowUnresolvedNodes
	}
	if p.Settings.AspLsp.Graph.HideSingleNodes != nil {
		s.settings.GraphHideSingleNodes = *p.Settings.AspLsp.Graph.HideSingleNodes
	}
	if p.Settings.AspLsp.Graph.HideUnreferencedGlobalSymbols != nil {
		s.settings.GraphHideUnreferencedGlobalSymbols = *p.Settings.AspLsp.Graph.HideUnreferencedGlobalSymbols
	}
	if p.Settings.AspLsp.Graph.ShowOutgoingSelectionLinks != nil {
		s.settings.GraphShowOutgoingSelectionLinks = *p.Settings.AspLsp.Graph.ShowOutgoingSelectionLinks
	}
	if p.Settings.AspLsp.Graph.ShowIncludeLinks != nil {
		s.settings.GraphShowIncludeLinks = *p.Settings.AspLsp.Graph.ShowIncludeLinks
	}
	if p.Settings.AspLsp.Graph.ShowDeclareLinks != nil {
		s.settings.GraphShowDeclareLinks = *p.Settings.AspLsp.Graph.ShowDeclareLinks
	}
	if p.Settings.AspLsp.Graph.ShowReferenceLinks != nil {
		s.settings.GraphShowReferenceLinks = *p.Settings.AspLsp.Graph.ShowReferenceLinks
	}
	if p.Settings.AspLsp.Graph.ShowAssignmentLinks != nil {
		s.settings.GraphShowAssignmentLinks = *p.Settings.AspLsp.Graph.ShowAssignmentLinks
	}
	if p.Settings.AspLsp.Graph.ShowCallLinks != nil {
		s.settings.GraphShowCallLinks = *p.Settings.AspLsp.Graph.ShowCallLinks
	}
	if p.Settings.AspLsp.Graph.ShowUnresolvedLinks != nil {
		s.settings.GraphShowUnresolvedLinks = *p.Settings.AspLsp.Graph.ShowUnresolvedLinks
	}
	if p.Settings.AspLsp.Graph.ShowMemberLinks != nil {
		s.settings.GraphShowMemberLinks = *p.Settings.AspLsp.Graph.ShowMemberLinks
	}
	if p.Settings.AspLsp.Graph.ShowIncomingDocumentIncludes != nil {
		s.settings.GraphShowIncomingDocumentIncludes = *p.Settings.AspLsp.Graph.ShowIncomingDocumentIncludes
	}
	if p.Settings.AspLsp.Graph.ShowIncomingFolderIncludes != nil {
		s.settings.GraphShowIncomingFolderIncludes = *p.Settings.AspLsp.Graph.ShowIncomingFolderIncludes
	}
	if p.Settings.AspLsp.Graph.IncludeRelatedIncludeTreesForUnresolved != nil {
		s.settings.GraphIncludeRelatedIncludeTrees = *p.Settings.AspLsp.Graph.IncludeRelatedIncludeTreesForUnresolved
	}
	if p.Settings.AspLsp.Graph.UseReverseIncludeIndex != nil {
		s.settings.GraphUseReverseIncludeIndex = *p.Settings.AspLsp.Graph.UseReverseIncludeIndex
	}
	if p.Settings.AspLsp.Graph.WorkerSymbolExtraction != nil {
		s.settings.GraphWorkerSymbolExtraction = *p.Settings.AspLsp.Graph.WorkerSymbolExtraction
	}
	if p.Settings.AspLsp.Flowchart.LabelLineLength != nil && *p.Settings.AspLsp.Flowchart.LabelLineLength >= 8 {
		s.settings.FlowchartLabelLineLength = *p.Settings.AspLsp.Flowchart.LabelLineLength
	}
	if p.Settings.AspLsp.Flowchart.LabelMode != nil {
		s.settings.FlowchartLabelMode = normalizeFlowchartLabelMode(*p.Settings.AspLsp.Flowchart.LabelMode)
	}
	if p.Settings.AspLsp.InlayHints.ParameterNames != nil {
		s.settings.InlayParameterNames = *p.Settings.AspLsp.InlayHints.ParameterNames
	}
	if p.Settings.AspLsp.InlayHints.ImplicitByRef != nil {
		s.settings.InlayImplicitByRef = *p.Settings.AspLsp.InlayHints.ImplicitByRef
	}
	if p.Settings.AspLsp.InlayHints.FunctionReturnTypes != nil {
		s.settings.InlayFunctionReturnTypes = *p.Settings.AspLsp.InlayHints.FunctionReturnTypes
	}
	if p.Settings.AspLsp.InlayHints.VariableTypes != nil {
		s.settings.InlayVariableTypes = *p.Settings.AspLsp.InlayHints.VariableTypes
	}
	if p.Settings.AspLsp.InlayHints.ScopeMarkers.Global != nil {
		s.settings.InlayScopeMarkers.Global = *p.Settings.AspLsp.InlayHints.ScopeMarkers.Global
	}
	if p.Settings.AspLsp.InlayHints.ScopeMarkers.Local != nil {
		s.settings.InlayScopeMarkers.Local = *p.Settings.AspLsp.InlayHints.ScopeMarkers.Local
	}
	if p.Settings.AspLsp.InlayHints.ScopeMarkers.Uncertain != nil {
		s.settings.InlayScopeMarkers.Uncertain = *p.Settings.AspLsp.InlayHints.ScopeMarkers.Uncertain
	}
	if p.Settings.AspLsp.JavaScript.AutoImports != nil {
		s.settings.JavaScriptAutoImports = *p.Settings.AspLsp.JavaScript.AutoImports
	}
	if p.Settings.AspLsp.JavaScript.IgnoreProjectConfig != nil {
		ignoreProjectConfig := *p.Settings.AspLsp.JavaScript.IgnoreProjectConfig
		if ignoreProjectConfig != s.settings.JavaScriptIgnoreProjectConfig {
			s.settings.JavaScriptIgnoreProjectConfig = ignoreProjectConfig
			s.resetJavaScriptProjectLocked()
		}
	}
	if p.Settings.AspLsp.JavaScript.UnusedDiagnostics != nil {
		if *p.Settings.AspLsp.JavaScript.UnusedDiagnostics != s.settings.JavaScriptUnusedDiagnostics {
			s.settings.JavaScriptUnusedDiagnostics = *p.Settings.AspLsp.JavaScript.UnusedDiagnostics
			s.resetJavaScriptProjectLocked()
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.JavaScript.CompilerOptions != nil {
		compilerOptions := cloneJavaScriptCompilerOptions(p.Settings.AspLsp.JavaScript.CompilerOptions)
		if !reflect.DeepEqual(compilerOptions, s.settings.JavaScriptCompilerOptions) {
			s.settings.JavaScriptCompilerOptions = compilerOptions
			s.settings.JavaScriptCompilerOptionTypes = nil
			if types, configured := javaScriptCompilerOptionTypes(s.settings.JavaScriptCompilerOptions); configured {
				s.settings.JavaScriptCompilerOptionTypes = types
			}
			s.resetJavaScriptProjectLocked()
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.StyleExtraction.InsertionMode != nil {
		s.settings.StyleExtractionInsertionMode = *p.Settings.AspLsp.StyleExtraction.InsertionMode
	}
	if p.Settings.AspLsp.VBScript.InitializedDimQuickFixStyle != nil {
		s.settings.InitializedDimQuickFixStyle = *p.Settings.AspLsp.VBScript.InitializedDimQuickFixStyle
	}
	if p.Settings.AspLsp.VBScript.IdentifierCase != nil {
		identifierCase := normalizeVBIdentifierCase(*p.Settings.AspLsp.VBScript.IdentifierCase)
		if identifierCase != s.settings.IdentifierCase {
			s.settings.IdentifierCase = identifierCase
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.IdentifierCaseByKind != nil {
		identifierCaseByKind := normalizeVBIdentifierCaseByKind(p.Settings.AspLsp.VBScript.IdentifierCaseByKind)
		if !reflect.DeepEqual(identifierCaseByKind, s.settings.IdentifierCaseByKind) {
			s.settings.IdentifierCaseByKind = identifierCaseByKind
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.ShowUnresolvedSymbolsInCompletion != nil {
		s.settings.ShowUnresolvedSymbolsInCompletion = *p.Settings.AspLsp.VBScript.ShowUnresolvedSymbolsInCompletion
	}
	if p.Settings.AspLsp.VBScript.AssumeUndefinedGlobals != nil {
		if *p.Settings.AspLsp.VBScript.AssumeUndefinedGlobals != s.settings.VBScriptAssumeUndefinedGlobals {
			s.settings.VBScriptAssumeUndefinedGlobals = *p.Settings.AspLsp.VBScript.AssumeUndefinedGlobals
			diagnosticsSettingsChanged = true
			referenceSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.SyntaxSnippets != nil {
		s.settings.SyntaxSnippets = *p.Settings.AspLsp.VBScript.SyntaxSnippets
	}
	if p.Settings.AspLsp.VBScript.SyntaxKeywords != nil {
		s.settings.VBScriptSyntaxKeywords = *p.Settings.AspLsp.VBScript.SyntaxKeywords
	}
	if p.Settings.AspLsp.VBScript.AutoIncludes != nil {
		s.settings.VBScriptAutoIncludes = *p.Settings.AspLsp.VBScript.AutoIncludes
	}
	if p.Settings.AspLsp.VBScript.UnusedDiagnostics != nil {
		if *p.Settings.AspLsp.VBScript.UnusedDiagnostics != s.settings.VBScriptUnusedDiagnostics {
			s.settings.VBScriptUnusedDiagnostics = *p.Settings.AspLsp.VBScript.UnusedDiagnostics
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.ImplicitGlobalDiagnostics != nil {
		if *p.Settings.AspLsp.VBScript.ImplicitGlobalDiagnostics != s.settings.VBScriptImplicitGlobalDiagnostics {
			s.settings.VBScriptImplicitGlobalDiagnostics = *p.Settings.AspLsp.VBScript.ImplicitGlobalDiagnostics
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.DeadCodeDiagnostics != nil {
		if *p.Settings.AspLsp.VBScript.DeadCodeDiagnostics != s.settings.VBScriptDeadCodeDiagnostics {
			s.settings.VBScriptDeadCodeDiagnostics = *p.Settings.AspLsp.VBScript.DeadCodeDiagnostics
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.SQLInjectionDiagnostics != nil {
		sqlInjectionDiagnostics := normalizeVBScriptSQLInjectionDiagnostics(*p.Settings.AspLsp.VBScript.SQLInjectionDiagnostics)
		if sqlInjectionDiagnostics != s.settings.VBScriptSQLInjectionDiagnostics {
			s.settings.VBScriptSQLInjectionDiagnostics = sqlInjectionDiagnostics
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.IfSyntaxDiagnostics != nil {
		ifSyntaxDiagnostics := normalizeVBScriptIfSyntaxDiagnostics(*p.Settings.AspLsp.VBScript.IfSyntaxDiagnostics)
		if ifSyntaxDiagnostics != s.settings.VBScriptIfSyntaxDiagnostics {
			s.settings.VBScriptIfSyntaxDiagnostics = ifSyntaxDiagnostics
			diagnosticsSettingsChanged = true
		}
	}
	if typeChecking, ok := jsonStringAtPath(params, "settings", "aspLsp", "vbscript", "typeChecking"); ok {
		typeChecking = strings.ToLower(strings.TrimSpace(typeChecking))
		if typeChecking != s.settings.VBScriptTypeChecking {
			s.settings.VBScriptTypeChecking = typeChecking
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.TypeChecking != nil {
		typeChecking := strings.ToLower(strings.TrimSpace(*p.Settings.AspLsp.VBScript.TypeChecking))
		if typeChecking != s.settings.VBScriptTypeChecking {
			s.settings.VBScriptTypeChecking = typeChecking
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.Globals != nil {
		globals := cloneVBScriptGlobals(p.Settings.AspLsp.VBScript.Globals)
		if !reflect.DeepEqual(globals, s.settings.VBScriptGlobals) {
			s.settings.VBScriptGlobals = globals
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.VBScript.ComTypes != nil {
		comTypes := cloneVBScriptComTypes(p.Settings.AspLsp.VBScript.ComTypes)
		if !reflect.DeepEqual(comTypes, s.settings.VBScriptComTypes) {
			s.settings.VBScriptComTypes = comTypes
			diagnosticsSettingsChanged = true
		}
	}
	if p.Settings.AspLsp.Rename.UpdateIncludesOnFileRename != nil {
		s.settings.UpdateIncludesOnFileRename = *p.Settings.AspLsp.Rename.UpdateIncludesOnFileRename
	}
	if p.Settings.AspLsp.Rename.WorkspaceSymbolRename != nil {
		s.settings.WorkspaceSymbolRename = *p.Settings.AspLsp.Rename.WorkspaceSymbolRename
	}
	settingsChanged := !reflect.DeepEqual(previousSettings, s.settings)
	graphConfigurationChanged := snapshotGraphConfiguration(previousSettings) != snapshotGraphConfiguration(s.settings)
	if includeResolutionChanged {
		s.resetTrustedFilesystemRootCacheLocked()
	}
	s.mu.Unlock()
	if networkProfileSettingChanged && previousResolvedNetworkProfile.CaseResolution != s.resolveNetworkProfile().CaseResolution {
		includeResolutionChanged = true
		referenceSettingsChanged = true
		s.mu.Lock()
		s.resetTrustedFilesystemRootCacheLocked()
		s.mu.Unlock()
	}
	if !settingsChanged {
		s.logDebugTrace("configuration.unchanged", "[asp-lsp] configuration.unchanged")
		return nil
	}
	workspaceIndexSettingsChanged := reindexWorkspace || includeResolutionChanged
	if workspaceIndexSettingsChanged {
		s.cancelWorkspaceIndexWorker()
		s.workspaceIndexStateMu.Lock()
		defer s.workspaceIndexStateMu.Unlock()
	}
	if semanticTokenCacheChanged {
		s.clearSemanticTokenCache()
	}
	if analysisWorkersChanged {
		s.configureAnalysisWorkers()
	}
	s.logDebugTrace("configuration.changed", "[asp-lsp] configuration.changed")
	if reindexWorkspace || includeResolutionChanged || referenceSettingsChanged && !referenceCacheCleared {
		s.clearWorkspaceReferenceCache()
	} else if referenceSettingsChanged {
		s.requestCodeLensRefresh("references.configuration.changed")
	}
	if includeResolutionChanged {
		s.logDebugSummary("[asp-lsp] invalidation.includeResolution")
	}
	if workspaceIndexSettingsChanged || diagnosticsSettingsChanged {
		s.clearAnalysisCache()
	}
	if diagnosticsSettingsChanged || includeResolutionChanged {
		s.clearWorkspaceDiagnosticsCaches(true)
		for _, uri := range s.openDocumentURIs() {
			if err := s.publishDiagnostics(uri); err != nil {
				return err
			}
		}
	}
	if runtimeCacheSettingsChanged {
		s.configureDiskAnalysisCache()
	}
	if filesystemSettingsChanged || includeResolutionChanged {
		s.configureFsGateway()
	}
	if settingsChanged {
		if err := s.requestVisualRefresh("configuration.changed"); err != nil {
			return err
		}
	}
	graphInvalidationNeeded := graphConfigurationChanged || reindexWorkspace || includeResolutionChanged || referenceSettingsChanged || diagnosticsSettingsChanged
	if graphInvalidationNeeded {
		s.invalidateGraphBackground()
	}
	if workspaceIndexSettingsChanged {
		s.scheduleWorkspaceIndex("configuration.changed")
	}
	s.checkMemoryPressure("configuration.changed")
	return nil
}
