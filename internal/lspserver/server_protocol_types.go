package lspserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func isJavaScriptPosition(doc *core.TextDocument, parsed *core.ParsedDocument, position lsp.Position) bool {
	region := core.RegionAt(parsed, doc.OffsetAt(position))
	return region != nil && (region.Language == core.LanguageJavaScript || region.Language == core.LanguageJScript)
}

var _ = os.Stderr

var vbClassDeclarationLinePattern = regexp.MustCompile(`(?i)^\s*Class\s+([A-Za-z_][A-Za-z0-9_]*)`)

type initializeParams struct {
	RootURI      string `json:"rootUri"`
	RootPath     string `json:"rootPath"`
	Locale       string `json:"locale"`
	Capabilities struct {
		Workspace struct {
			Configuration  bool `json:"configuration"`
			SemanticTokens struct {
				RefreshSupport bool `json:"refreshSupport"`
			} `json:"semanticTokens"`
			InlayHint struct {
				RefreshSupport bool `json:"refreshSupport"`
			} `json:"inlayHint"`
			CodeLens struct {
				RefreshSupport bool `json:"refreshSupport"`
			} `json:"codeLens"`
		} `json:"workspace"`
	} `json:"capabilities"`
	WorkspaceFolders []struct {
		URI string `json:"uri"`
	} `json:"workspaceFolders"`
}

type textDocumentPositionParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Position     lsp.Position               `json:"position"`
}

type completionParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Position     lsp.Position               `json:"position"`
	Context      *completionContext         `json:"context,omitempty"`
}

type completionContext struct {
	TriggerKind      int    `json:"triggerKind,omitempty"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

type selectionRangeParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Positions    []lsp.Position             `json:"positions"`
}

type inlayHintParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Range        lsp.Range                  `json:"range"`
}

type inlineValueParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Range        lsp.Range                  `json:"range"`
}

type codeActionParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Range        lsp.Range                  `json:"range"`
	Context      struct {
		Diagnostics []lsp.Diagnostic `json:"diagnostics"`
		Only        []string         `json:"only,omitempty"`
	} `json:"context"`
}

type referenceParams struct {
	TextDocument       lsp.TextDocumentIdentifier `json:"textDocument"`
	Position           lsp.Position               `json:"position"`
	PartialResultToken any                        `json:"partialResultToken,omitempty"`
	Context            struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type renameParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Position     lsp.Position               `json:"position"`
	NewName      string                     `json:"newName"`
}

type workspaceSymbolParams struct {
	Query string `json:"query"`
}

type onTypeFormattingParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Position     lsp.Position               `json:"position"`
	Ch           string                     `json:"ch"`
	Options      core.FormattingOptions     `json:"options"`
}

type willRenameFilesParams struct {
	Files []struct {
		OldURI string `json:"oldUri"`
		NewURI string `json:"newUri"`
	} `json:"files"`
}

type willSaveWaitUntilParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Reason       int                        `json:"reason"`
}

type changeConfigurationParams struct {
	Settings struct {
		AspLsp struct {
			DefaultLanguage *string `json:"defaultLanguage"`
			Locale          *string `json:"locale"`
			LegacyEncoding  *string `json:"legacyEncoding"`
			CheckJS         *bool   `json:"checkJs"`
			Debug           struct {
				Output  *string `json:"output"`
				LogFile struct {
					Enabled *bool   `json:"enabled"`
					Path    *string `json:"path"`
				} `json:"logFile"`
			} `json:"debug"`
			Cache struct {
				Enabled   *bool   `json:"enabled"`
				Directory *string `json:"directory"`
				Freshness *string `json:"freshness"`
				TTLHours  *int    `json:"ttlHours"`
				MaxSizeMB *int    `json:"maxSizeMb"`
				Gzip      *bool   `json:"gzip"`
			} `json:"cache"`
			Memory struct {
				MaxCacheBytes  *int64 `json:"maxCacheBytes"`
				DebugTelemetry *bool  `json:"debugTelemetry"`
			} `json:"memory"`
			Network struct {
				Profile                *string `json:"profile"`
				StatCacheTTLMS         *int    `json:"statCacheTtlMs"`
				ReadDirCacheTTLMS      *int    `json:"readdirCacheTtlMs"`
				IncludeReadConcurrency *int    `json:"includeReadConcurrency"`
				CaseResolution         *string `json:"caseResolution"`
			} `json:"network"`
			Diagnostics struct {
				DebounceMS *int `json:"debounceMs"`
			} `json:"diagnostics"`
			Incremental struct {
				Mode     *string `json:"mode"`
				Analysis *bool   `json:"analysis"`
			} `json:"incremental"`
			CodeLens struct {
				Includes                                *bool `json:"includes"`
				References                              *bool `json:"references"`
				ReferenceProcedures                     *bool `json:"referenceProcedures"`
				ReferenceGlobals                        *bool `json:"referenceGlobals"`
				ReferenceClasses                        *bool `json:"referenceClasses"`
				ReferenceClassMembers                   *bool `json:"referenceClassMembers"`
				IncludeRelatedIncludeTreesForUnresolved *bool `json:"includeRelatedIncludeTreesForUnresolved"`
			} `json:"codeLens"`
			IncludePaths          []string `json:"includePaths"`
			VirtualRoot           *string  `json:"virtualRoot"`
			VirtualRoots          []string `json:"virtualRoots"`
			WindowsPathResolution *bool    `json:"windowsPathResolution"`
			Workspace             struct {
				Includes                []string `json:"includes"`
				Excludes                []string `json:"excludes"`
				ScanChunkSize           *int     `json:"scanChunkSize"`
				BusyAnalysisConcurrency *int     `json:"busyAnalysisConcurrency"`
				RespectGitIgnore        *bool    `json:"respectGitIgnore"`
			} `json:"workspace"`
			Excel struct {
				Locale                                  *string `json:"locale"`
				IncludeRelatedIncludeTreesForUnresolved *bool   `json:"includeRelatedIncludeTreesForUnresolved"`
				SkipTypeInference                       *bool   `json:"skipTypeInference"`
			} `json:"excel"`
			NavigationGraph struct{} `json:"navigationGraph"`
			Format          struct {
				OnSave                                                               *bool    `json:"onSave"`
				IndentSize                                                           *int     `json:"indentSize"`
				IndentStyle                                                          *string  `json:"indentStyle"`
				PrintWidth                                                           *int     `json:"printWidth"`
				EndOfLine                                                            *string  `json:"endOfLine"`
				EmbeddedLanguageFormatting                                           *string  `json:"embeddedLanguageFormatting"`
				FragmentMode                                                         *string  `json:"fragmentMode"`
				InsertFinalNewline                                                   *bool    `json:"insertFinalNewline"`
				PreserveNewLines                                                     *bool    `json:"preserveNewLines"`
				MaxPreserveNewLines                                                  *int     `json:"maxPreserveNewLines"`
				IndentEmptyLines                                                     *bool    `json:"indentEmptyLines"`
				EnabledLanguages                                                     []string `json:"enabledLanguages"`
				HTMLIndentSize                                                       *int     `json:"htmlIndentSize"`
				HTMLIndentStyle                                                      *string  `json:"htmlIndentStyle"`
				HTMLWrapLineLength                                                   *int     `json:"htmlWrapLineLength"`
				HTMLWrapAttributes                                                   *string  `json:"htmlWrapAttributes"`
				HTMLWrapAttributesIndentSize                                         *int     `json:"htmlWrapAttributesIndentSize"`
				HTMLIndentInnerHTML                                                  *bool    `json:"htmlIndentInnerHtml"`
				HTMLUnformatted                                                      *string  `json:"htmlUnformatted"`
				HTMLContentUnformatted                                               *string  `json:"htmlContentUnformatted"`
				HTMLExtraLiners                                                      *string  `json:"htmlExtraLiners"`
				CSSIndentSize                                                        *int     `json:"cssIndentSize"`
				CSSIndentStyle                                                       *string  `json:"cssIndentStyle"`
				CSSWrapLineLength                                                    *int     `json:"cssWrapLineLength"`
				CSSNewlineBetweenRules                                               *bool    `json:"cssNewlineBetweenRules"`
				CSSNewlineBetweenSelectors                                           *bool    `json:"cssNewlineBetweenSelectors"`
				CSSSpaceAroundSelectorSeparator                                      *bool    `json:"cssSpaceAroundSelectorSeparator"`
				CSSBraceStyle                                                        *string  `json:"cssBraceStyle"`
				CSSTagIndentMode                                                     *string  `json:"cssTagIndentMode"`
				IgnoreCSSTagIndent                                                   *bool    `json:"ignoreCssTagIndent"`
				JavaScriptIndentSize                                                 *int     `json:"javascriptIndentSize"`
				JavaScriptIndentStyle                                                *string  `json:"javascriptIndentStyle"`
				JScriptIndentSize                                                    *int     `json:"jscriptIndentSize"`
				JScriptIndentStyle                                                   *string  `json:"jscriptIndentStyle"`
				JavaScriptSemicolons                                                 *string  `json:"javascriptSemicolons"`
				JavaScriptIndentSwitchCase                                           *bool    `json:"javascriptIndentSwitchCase"`
				JavaScriptPlaceOpenBraceOnNewLineForFunctions                        *bool    `json:"javascriptPlaceOpenBraceOnNewLineForFunctions"`
				JavaScriptPlaceOpenBraceOnNewLineForControlBlocks                    *bool    `json:"javascriptPlaceOpenBraceOnNewLineForControlBlocks"`
				JavaScriptInsertSpaceAfterCommaDelimiter                             *bool    `json:"javascriptInsertSpaceAfterCommaDelimiter"`
				JavaScriptInsertSpaceAfterSemicolonInForStatements                   *bool    `json:"javascriptInsertSpaceAfterSemicolonInForStatements"`
				JavaScriptInsertSpaceBeforeAndAfterBinaryOperators                   *bool    `json:"javascriptInsertSpaceBeforeAndAfterBinaryOperators"`
				JavaScriptInsertSpaceAfterKeywordsInControlFlowStatements            *bool    `json:"javascriptInsertSpaceAfterKeywordsInControlFlowStatements"`
				JavaScriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions       *bool    `json:"javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions"`
				JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis *bool    `json:"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis"`
				JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets    *bool    `json:"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets"`
				JavaScriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces      *bool    `json:"javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces"`
				JavaScriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces         *bool    `json:"javascriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces"`
				JavaScriptInsertSpaceBeforeFunctionParenthesis                       *bool    `json:"javascriptInsertSpaceBeforeFunctionParenthesis"`
				JavaScriptTagIndentMode                                              *string  `json:"javascriptTagIndentMode"`
				IgnoreJavaScriptTagIndent                                            *bool    `json:"ignoreJavaScriptTagIndent"`
				NestedASPInCSSJS                                                     *string  `json:"nestedAspInCssJs"`
				RespectDisableRegions                                                *bool    `json:"respectDisableRegions"`
				VBScriptIndentSize                                                   *int     `json:"vbscriptIndentSize"`
				VBScriptIndentStyle                                                  *string  `json:"vbscriptIndentStyle"`
				VBScriptKeywordCase                                                  *string  `json:"vbscriptKeywordCase"`
				VBScriptLineContinuationIndentSize                                   *int     `json:"vbscriptLineContinuationIndentSize"`
				VBScriptSelectCaseIndent                                             *string  `json:"vbscriptSelectCaseIndent"`
				VBScriptBlockIndent                                                  *string  `json:"vbscriptBlockIndent"`
				VBScriptTagIndentMode                                                *string  `json:"vbscriptTagIndentMode"`
				IgnoreVBScriptTagIndent                                              *bool    `json:"ignoreVbscriptTagIndent"`
				UppercaseKeywords                                                    *bool    `json:"uppercaseKeywords"`
				AlignAssignments                                                     *bool    `json:"alignAssignments"`
				ASPDelimiterSpacing                                                  *string  `json:"aspDelimiterSpacing"`
				ASPBlockNewline                                                      *string  `json:"aspBlockNewline"`
			} `json:"format"`
			Graph struct {
				InitialViewMode                         *string `json:"initialViewMode"`
				ShowRootNodes                           *bool   `json:"showRootNodes"`
				ShowFileNodes                           *bool   `json:"showFileNodes"`
				ShowFunctionNodes                       *bool   `json:"showFunctionNodes"`
				ShowSubNodes                            *bool   `json:"showSubNodes"`
				ShowClassNodes                          *bool   `json:"showClassNodes"`
				ShowMethodNodes                         *bool   `json:"showMethodNodes"`
				ShowMethodFunctionNodes                 *bool   `json:"showMethodFunctionNodes"`
				ShowMethodSubNodes                      *bool   `json:"showMethodSubNodes"`
				ShowPropertyNodes                       *bool   `json:"showPropertyNodes"`
				ShowMemberNodes                         *bool   `json:"showMemberNodes"`
				ShowGlobalVariableNodes                 *bool   `json:"showGlobalVariableNodes"`
				ShowGlobalConstantNodes                 *bool   `json:"showGlobalConstantNodes"`
				ShowLocalVariableNodes                  *bool   `json:"showLocalVariableNodes"`
				ShowLocalConstantNodes                  *bool   `json:"showLocalConstantNodes"`
				ShowParameterNodes                      *bool   `json:"showParameterNodes"`
				ShowUnresolvedNodes                     *bool   `json:"showUnresolvedNodes"`
				HideSingleNodes                         *bool   `json:"hideSingleNodes"`
				HideUnreferencedGlobalSymbols           *bool   `json:"hideUnreferencedGlobalSymbols"`
				ShowOutgoingSelectionLinks              *bool   `json:"showOutgoingSelectionLinks"`
				ShowIncludeLinks                        *bool   `json:"showIncludeLinks"`
				ShowDeclareLinks                        *bool   `json:"showDeclareLinks"`
				ShowReferenceLinks                      *bool   `json:"showReferenceLinks"`
				ShowAssignmentLinks                     *bool   `json:"showAssignmentLinks"`
				ShowCallLinks                           *bool   `json:"showCallLinks"`
				ShowUnresolvedLinks                     *bool   `json:"showUnresolvedLinks"`
				ShowMemberLinks                         *bool   `json:"showMemberLinks"`
				ShowIncomingDocumentIncludes            *bool   `json:"showIncomingDocumentIncludes"`
				ShowIncomingFolderIncludes              *bool   `json:"showIncomingFolderIncludes"`
				IncludeRelatedIncludeTreesForUnresolved *bool   `json:"includeRelatedIncludeTreesForUnresolved"`
				UseReverseIncludeIndex                  *bool   `json:"useReverseIncludeIndex"`
				WorkerSymbolExtraction                  *bool   `json:"workerSymbolExtraction"`
			} `json:"graph"`
			Flowchart struct {
				LabelLineLength *int    `json:"labelLineLength"`
				LabelMode       *string `json:"labelMode"`
			} `json:"flowchart"`
			InlayHints struct {
				FunctionReturnTypes *bool `json:"functionReturnTypes"`
				ImplicitByRef       *bool `json:"implicitByRef"`
				ParameterNames      *bool `json:"parameterNames"`
				VariableTypes       *bool `json:"variableTypes"`
				ScopeMarkers        struct {
					Global    *bool `json:"global"`
					Local     *bool `json:"local"`
					Uncertain *bool `json:"uncertain"`
				} `json:"scopeMarkers"`
			} `json:"inlayHints"`
			JavaScript struct {
				AutoImports         *bool          `json:"autoImports"`
				IgnoreProjectConfig *bool          `json:"ignoreProjectConfig"`
				UnusedDiagnostics   *bool          `json:"unusedDiagnostics"`
				CompilerOptions     map[string]any `json:"compilerOptions"`
			} `json:"javascript"`
			Rename struct {
				UpdateIncludesOnFileRename *bool `json:"updateIncludesOnFileRename"`
				WorkspaceSymbolRename      *bool `json:"workspaceSymbolRename"`
			} `json:"rename"`
			StyleExtraction struct {
				InsertionMode *string `json:"insertionMode"`
			} `json:"styleExtraction"`
			VBScript struct {
				AutoIncludes                      *bool                             `json:"autoIncludes"`
				InitializedDimQuickFixStyle       *string                           `json:"initializedDimQuickFixStyle"`
				IdentifierCase                    *string                           `json:"identifierCase"`
				IdentifierCaseByKind              map[string]string                 `json:"identifierCaseByKind"`
				AssumeUndefinedGlobals            *bool                             `json:"assumeUndefinedGlobals"`
				ShowUnresolvedSymbolsInCompletion *bool                             `json:"showUnresolvedSymbolsInCompletion"`
				SyntaxSnippets                    *bool                             `json:"syntaxSnippets"`
				SyntaxKeywords                    *bool                             `json:"syntaxKeywords"`
				UnusedDiagnostics                 *bool                             `json:"unusedDiagnostics"`
				ImplicitGlobalDiagnostics         *bool                             `json:"implicitGlobalDiagnostics"`
				DeadCodeDiagnostics               *bool                             `json:"deadCodeDiagnostics"`
				IfSyntaxDiagnostics               *string                           `json:"ifSyntaxDiagnostics"`
				TypeChecking                      *string                           `json:"typeChecking"`
				Globals                           map[string]vbscriptGlobalSetting  `json:"globals"`
				ComTypes                          map[string]vbscriptComTypeSetting `json:"comTypes"`
			} `json:"vbscript"`
		} `json:"aspLsp"`
	} `json:"settings"`
}

func jsonStringAtPath(raw json.RawMessage, path ...string) (string, bool) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	for _, part := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return "", false
		}
		value, ok = object[part]
		if !ok {
			return "", false
		}
	}
	text, ok := value.(string)
	return text, ok
}

func normalizeDefaultLanguage(value string) string {
	if value == "JScript" {
		return "JScript"
	}
	return "VBScript"
}

func normalizeIncrementalMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "full", "off", "legacy":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "full"
	}
}

func normalizeDebugOutput(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "summary", "verbose":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "off"
	}
}

func normalizeLegacyEncoding(value string) string {
	switch value {
	case "utf8", "shift_jis", "cp932":
		return value
	default:
		return "auto"
	}
}

func normalizeFormatEndOfLine(value string) string {
	switch value {
	case "lf", "crlf", "auto":
		return value
	default:
		return ""
	}
}

func normalizeEmbeddedLanguageFormatting(value string) string {
	switch value {
	case "auto", "off":
		return value
	default:
		return ""
	}
}

func normalizeFormatFragmentMode(value string) string {
	switch value {
	case "auto", "fragment", "document":
		return value
	default:
		return "auto"
	}
}

func normalizeFormatTagIndentMode(value string) string {
	switch value {
	case "relativeToTag", "ignoreTag", "preserveExisting":
		return value
	default:
		return ""
	}
}

func normalizeCSSBraceStyle(value string) string {
	switch value {
	case "collapse", "expand":
		return value
	default:
		return ""
	}
}

func normalizeJavaScriptSemicolons(value string) string {
	switch value {
	case "ignore", "insert", "remove":
		return value
	default:
		return ""
	}
}

func normalizeNestedASPInCSSJS(value string) string {
	switch value {
	case "skipRegion", "protectAspOnly", "formatAroundAsp":
		return value
	default:
		return "skipRegion"
	}
}

func normalizeFlowchartLabelMode(value string) string {
	switch value {
	case "normal", "raw", "description":
		return value
	default:
		return ""
	}
}

func normalizeVirtualRoots(values []string) []string {
	roots := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		path := value
		if strings.HasPrefix(value, "file:") {
			path = fileURIPath(value)
		}
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			absolute = path
		}
		cleaned := filepath.Clean(absolute)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		roots = append(roots, cleaned)
	}
	return roots
}

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type callHierarchyItemParams struct {
	Item lsp.CallHierarchyItem `json:"item"`
}

type typeHierarchyItemParams struct {
	Item lsp.TypeHierarchyItem `json:"item"`
}

type textDocumentIdentifierParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
}

type lineCommentEditsParams struct {
	TextDocument lsp.VersionedTextDocumentIdentifier `json:"textDocument"`
	Selections   []lsp.Range                         `json:"selections"`
}

type lineCommentEditsResult struct {
	Version    int            `json:"version"`
	Edits      []lsp.TextEdit `json:"edits"`
	NoOpReason string         `json:"noOpReason,omitempty"`
}

type semanticTokensRangeParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Range        lsp.Range                  `json:"range"`
}

type semanticTokensDeltaParams struct {
	TextDocument     lsp.TextDocumentIdentifier `json:"textDocument"`
	PreviousResultID string                     `json:"previousResultId"`
}

type didOpenParams struct {
	TextDocument lsp.TextDocumentItem `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument   lsp.VersionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []struct {
		Range *lsp.Range `json:"range,omitempty"`
		Text  string     `json:"text"`
	} `json:"contentChanges"`
}

type didChangeWatchedFilesParams struct {
	Changes []fileEvent `json:"changes"`
}

type fileEvent struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

const (
	fileChangeCreated = 1
	fileChangeChanged = 2
	fileChangeDeleted = 3
)

type workspaceFolderEvent struct {
	URI string `json:"uri"`
}

type didChangeWorkspaceFoldersParams struct {
	Event struct {
		Added   []workspaceFolderEvent `json:"added"`
		Removed []workspaceFolderEvent `json:"removed"`
	} `json:"event"`
}

type didRenameFilesParams struct {
	Files []struct {
		OldURI string `json:"oldUri"`
		NewURI string `json:"newUri"`
	} `json:"files"`
}

type didFileOperationParams struct {
	Files []struct {
		URI string `json:"uri"`
	} `json:"files"`
}

type formattingParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Options      core.FormattingOptions     `json:"options"`
}

type rangeFormattingParams struct {
	TextDocument lsp.TextDocumentIdentifier `json:"textDocument"`
	Range        lsp.Range                  `json:"range"`
	Options      core.FormattingOptions     `json:"options"`
}

type executeCommandParams struct {
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}
