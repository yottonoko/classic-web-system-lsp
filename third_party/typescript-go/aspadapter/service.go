package aspadapter

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	tsjson "github.com/microsoft/typescript-go/internal/json"
	"github.com/microsoft/typescript-go/internal/ls"
	"github.com/microsoft/typescript-go/internal/ls/autoimport"
	"github.com/microsoft/typescript-go/internal/ls/lsconv"
	"github.com/microsoft/typescript-go/internal/ls/lsutil"
	"github.com/microsoft/typescript-go/internal/lsp/lsproto"
	"github.com/microsoft/typescript-go/internal/sourcemap"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs"
	"github.com/microsoft/typescript-go/internal/vfs/vfsmatch"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
)

// Project owns a reusable TypeScript language-service project. Files are replaced
// atomically; the compiler program is rebuilt only when their contents change.
type Project struct {
	mu                  sync.RWMutex
	diagnosticMu        sync.Mutex
	root                string
	files               map[string]string
	options             ProjectOptions
	service             *ls.LanguageService
	host                *projectHost
	fs                  vfs.FS
	rootFiles           []string
	capabilities        lsproto.ResolvedClientCapabilities
	generation          uint64
	builds              uint64
	diagnosticResponses map[diagnosticResponseCacheKey][]byte
}

type diagnosticResponseCacheKey struct {
	generation uint64
	method     string
	uri        lsproto.DocumentUri
}

// ProjectOptions controls compiler behavior that affects the embedded project.
type ProjectOptions struct {
	Types           []string
	TypesConfigured bool
	// CompilerOptions contains tsconfig/jsconfig compilerOptions values in their JSON form.
	CompilerOptions map[string]any
}

// ProjectState describes the real compiler project currently backing requests.
type ProjectState struct {
	Generation  uint64
	Builds      uint64
	Rebuilt     bool
	Incremental bool
}

// NewProject creates an initially empty reusable JavaScript project.
func NewProject(root string) *Project {
	root, _ = normalizeProjectInputPath(root)
	capabilities := (&lsproto.ClientCapabilities{}).Resolve()
	capabilities.TextDocument.Hover.ContentFormat = []lsproto.MarkupKind{lsproto.MarkupKindMarkdown, lsproto.MarkupKindPlainText}
	capabilities.TextDocument.Completion.CompletionItem.DocumentationFormat = []lsproto.MarkupKind{lsproto.MarkupKindMarkdown, lsproto.MarkupKindPlainText}
	capabilities.TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport = true
	capabilities.TextDocument.SemanticTokens.TokenTypes = []string{"namespace", "class", "enum", "interface", "struct", "typeParameter", "type", "parameter", "variable", "property", "enumMember", "decorator", "event", "function", "method", "macro", "label", "comment", "string", "keyword", "number", "regexp", "operator"}
	capabilities.TextDocument.SemanticTokens.TokenModifiers = []string{"declaration", "definition", "readonly", "static", "deprecated", "abstract", "async", "modification", "documentation", "defaultLibrary", "local"}
	return &Project{
		root:                filepath.ToSlash(root),
		files:               map[string]string{},
		capabilities:        capabilities,
		diagnosticResponses: map[diagnosticResponseCacheKey][]byte{},
	}
}

func normalizeProjectInputPath(path string) (string, error) {
	path = normalizeWindowsPathPrefix(path)
	if classifyProjectPath(path) != projectPathUnknown {
		return tspath.RemoveTrailingDirectorySeparators(tspath.NormalizePath(path)), nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = normalizeWindowsPathPrefix(absolute)
	return tspath.RemoveTrailingDirectorySeparators(tspath.NormalizePath(absolute)), nil
}

type projectPathKind uint8

const (
	projectPathUnknown projectPathKind = iota
	projectPathPOSIX
	projectPathDrive
	projectPathUNC
)

func normalizeWindowsPathPrefix(path string) string {
	path = filepath.ToSlash(path)
	if strings.HasPrefix(path, `\\`) || len(path) >= 2 && tspath.IsVolumeCharacter(path[0]) && path[1] == ':' {
		path = strings.ReplaceAll(path, `\`, "/")
	}
	lowerPath := strings.ToLower(path)
	if strings.HasPrefix(lowerPath, "//?/unc/") {
		path = "//" + path[len("//?/unc/"):]
	} else if strings.HasPrefix(lowerPath, "//?/") {
		path = path[len("//?/"):]
	}
	if len(path) >= 2 && tspath.IsVolumeCharacter(path[0]) && path[1] == ':' {
		path = strings.ToLower(path[:1]) + path[1:]
	}
	return path
}

func classifyProjectPath(path string) projectPathKind {
	path = normalizeWindowsPathPrefix(path)
	switch {
	case strings.HasPrefix(path, "//"):
		return projectPathUNC
	case len(path) >= 3 && tspath.IsVolumeCharacter(path[0]) && path[1] == ':' && path[2] == '/':
		return projectPathDrive
	case strings.HasPrefix(path, "/"):
		return projectPathPOSIX
	default:
		return projectPathUnknown
	}
}

// Keep the in-memory compiler file map in the path family selected by its project root.
func normalizeProjectPathForRoot(root, path string) string {
	root = normalizeWindowsPathPrefix(root)
	path = normalizeWindowsPathPrefix(path)
	rootKind := classifyProjectPath(root)
	pathKind := classifyProjectPath(path)
	if (rootKind == projectPathDrive || rootKind == projectPathUNC) && pathKind != rootKind && tspath.IsRootedDiskPath(path) {
		relativePath := strings.TrimLeft(path, "/")
		if pathKind == projectPathDrive {
			relativePath = strings.TrimLeft(path[2:], "/")
		}
		path = tspath.CombinePaths(root, relativePath)
	}
	return tspath.RemoveTrailingDirectorySeparators(tspath.NormalizePath(path))
}

func normalizeProjectFiles(root string, files map[string]string) (map[string]string, error) {
	normalized := make(map[string]string, len(files))
	for path, text := range files {
		normalizedPath := normalizeProjectPathForRoot(root, path)
		if _, exists := normalized[normalizedPath]; exists {
			return nil, fmt.Errorf("project paths normalize to the same file: %q", normalizedPath)
		}
		normalized[normalizedPath] = text
	}
	return normalized, nil
}

func (p *Project) normalizeFileName(name string) (string, error) {
	absolute, err := normalizeProjectInputPath(name)
	if err != nil {
		return "", err
	}
	return normalizeProjectPathForRoot(p.root, absolute), nil
}

// UpdateFiles replaces all project roots and rebuilds the compiler program when needed.
func (p *Project) UpdateFiles(files map[string]string) (ProjectState, error) {
	return p.Update(files, ProjectOptions{})
}

// Update replaces all project files and compiler options atomically.
func (p *Project) Update(files map[string]string, options ProjectOptions) (ProjectState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	normalized := make(map[string]string, len(files))
	for name, text := range files {
		absolute, err := p.normalizeFileName(name)
		if err != nil {
			return ProjectState{}, err
		}
		normalized[absolute] = text
	}
	optionsUnchanged := projectOptionsEqual(p.options, options)
	if mapsEqual(p.files, normalized) && optionsUnchanged && p.service != nil {
		return ProjectState{Generation: p.generation, Builds: p.builds}, nil
	}
	changedFile, canUpdateIncrementally := singleChangedFile(p.files, normalized)
	canUpdateIncrementally = canUpdateIncrementally && optionsUnchanged && p.service != nil
	p.files = normalized
	p.options = ProjectOptions{Types: append([]string(nil), options.Types...), TypesConfigured: options.TypesConfigured, CompilerOptions: cloneCompilerOptions(options.CompilerOptions)}
	p.generation++
	p.diagnosticResponses = map[diagnosticResponseCacheKey][]byte{}
	incremental, err := p.rebuildLocked(changedFile, canUpdateIncrementally)
	if err != nil {
		return ProjectState{}, err
	}
	p.builds++
	return ProjectState{Generation: p.generation, Builds: p.builds, Rebuilt: true, Incremental: incremental}, nil
}

// UpdateDelta applies file upserts and deletions while preserving all
// unaffected project roots. A single changed existing file follows the same
// incremental UpdateProgram path as Update.
func (p *Project) UpdateDelta(upserts map[string]string, deletes []string, options ProjectOptions) (ProjectState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	normalizedUpserts := make(map[string]string, len(upserts))
	for name, text := range upserts {
		absolute, err := p.normalizeFileName(name)
		if err != nil {
			return ProjectState{}, err
		}
		normalizedUpserts[absolute] = text
	}
	normalizedDeletes := make([]string, 0, len(deletes))
	for _, name := range deletes {
		absolute, err := p.normalizeFileName(name)
		if err != nil {
			return ProjectState{}, err
		}
		normalizedDeletes = append(normalizedDeletes, absolute)
	}
	optionsUnchanged := projectOptionsEqual(p.options, options)
	changedFile := ""
	changedFiles := 0
	changedExistingFile := false
	for name, text := range normalizedUpserts {
		if previous, ok := p.files[name]; !ok || previous != text {
			changedFile = name
			changedFiles++
			changedExistingFile = ok
		}
	}
	for _, name := range normalizedDeletes {
		if _, ok := p.files[name]; ok {
			changedFile = name
			changedFiles++
			changedExistingFile = false
		}
	}
	if changedFiles == 0 && optionsUnchanged && p.service != nil {
		return ProjectState{Generation: p.generation, Builds: p.builds}, nil
	}
	canUpdateIncrementally := changedFiles == 1 && changedExistingFile && optionsUnchanged && p.service != nil
	for name, text := range normalizedUpserts {
		p.files[name] = text
	}
	for _, name := range normalizedDeletes {
		delete(p.files, name)
	}
	p.options = ProjectOptions{Types: append([]string(nil), options.Types...), TypesConfigured: options.TypesConfigured, CompilerOptions: cloneCompilerOptions(options.CompilerOptions)}
	p.generation++
	p.diagnosticResponses = map[diagnosticResponseCacheKey][]byte{}
	incremental, err := p.rebuildLocked(changedFile, canUpdateIncrementally)
	if err != nil {
		return ProjectState{}, err
	}
	p.builds++
	return ProjectState{Generation: p.generation, Builds: p.builds, Rebuilt: true, Incremental: incremental}, nil
}

// State reports compiler-project lifecycle counters without changing the project.
func (p *Project) State() ProjectState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ProjectState{Generation: p.generation, Builds: p.builds}
}

// Request invokes a real TypeScript language-service operation. params is the
// standard JSON LSP params object for method; the result is standard LSP JSON.
func (p *Project) Request(ctx context.Context, method string, params []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	diagnosticCacheable := isDiagnosticMethod(method)
	if diagnosticCacheable {
		if !lockProjectRequestContext(ctx, p.mu.TryRLock) {
			return nil, ctx.Err()
		}
		defer p.mu.RUnlock()
	} else {
		if !lockProjectRequestContext(ctx, p.mu.TryLock) {
			return nil, ctx.Err()
		}
		defer p.mu.Unlock()
	}
	if p.service == nil {
		return nil, errors.New("typescript project has not been initialized")
	}
	ctx = lsproto.WithClientCapabilities(ctx, &p.capabilities)
	var diagnosticKey diagnosticResponseCacheKey
	var diagnosticParams lsproto.DocumentDiagnosticParams
	if diagnosticCacheable {
		if err := tsjson.Unmarshal(params, &diagnosticParams); err != nil {
			return nil, err
		}
		diagnosticKey = diagnosticResponseCacheKey{generation: p.generation, method: method, uri: diagnosticParams.TextDocument.Uri}
		if ctx.Err() == nil {
			p.diagnosticMu.Lock()
			if cached, ok := p.diagnosticResponses[diagnosticKey]; ok {
				p.diagnosticMu.Unlock()
				return slices.Clone(cached), nil
			}
			p.diagnosticMu.Unlock()
		}
	}
	var result any
	var err error
	switch method {
	case "textDocument/completion":
		var value lsproto.CompletionParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideCompletion(ctx, value.TextDocument.Uri, value.Position, value.Context)
		}
	case "completionItem/resolve":
		var value lsproto.CompletionItem
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ResolveCompletionItem(ctx, &value, value.Data)
		}
	case "textDocument/hover":
		var value lsproto.HoverParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideHover(ctx, &value)
		}
	case "textDocument/diagnostic":
		value := diagnosticParams
		if !diagnosticCacheable {
			err = tsjson.Unmarshal(params, &value)
		}
		if err == nil {
			result, err = p.service.ProvideDiagnostics(ctx, value.TextDocument.Uri)
		}
	case "textDocument/syntacticDiagnostic", "textDocument/semanticDiagnostic", "textDocument/suggestionDiagnostic":
		value := diagnosticParams
		if !diagnosticCacheable {
			err = tsjson.Unmarshal(params, &value)
		}
		if err == nil {
			program := p.service.GetProgram()
			file := program.GetSourceFile(value.TextDocument.Uri.FileName())
			if file == nil {
				err = fmt.Errorf("typescript file not found: %s", value.TextDocument.Uri.FileName())
				break
			}
			var diagnostics []*ast.Diagnostic
			if method == "textDocument/syntacticDiagnostic" {
				diagnostics = program.GetSyntacticDiagnostics(ctx, file)
			} else if method == "textDocument/semanticDiagnostic" {
				diagnostics = program.GetSemanticDiagnostics(ctx, file)
			} else {
				diagnostics = program.GetSuggestionDiagnostics(ctx, file)
			}
			items := make([]*lsproto.Diagnostic, 0, len(diagnostics))
			for _, diagnostic := range diagnostics {
				items = append(items, lsconv.DiagnosticToLSPPull(ctx, p.host.converters, diagnostic, false))
			}
			result = lsproto.RelatedFullDocumentDiagnosticReportOrUnchangedDocumentDiagnosticReport{
				FullDocumentDiagnosticReport: &lsproto.RelatedFullDocumentDiagnosticReport{Items: items},
			}
		}
	case "textDocument/definition":
		var value lsproto.DefinitionParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideDefinition(ctx, value.TextDocument.Uri, value.Position)
		}
	case "textDocument/typeDefinition":
		var value lsproto.TypeDefinitionParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideTypeDefinition(ctx, value.TextDocument.Uri, value.Position)
		}
	case "textDocument/implementation":
		var value lsproto.ImplementationParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideImplementations(ctx, &value, nil)
		}
	case "textDocument/references":
		var value lsproto.ReferenceParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideReferences(ctx, &value, nil)
		}
	case "textDocument/rename":
		var value lsproto.RenameParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideRename(ctx, &value, nil)
		}
	case "textDocument/prepareRename":
		var value lsproto.PrepareRenameParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			info := p.service.GetRenameInfo(ctx, "", value.TextDocument.Uri, value.Position)
			if info.CanRename {
				result = info.TriggerSpan
			}
		}
	case "textDocument/signatureHelp":
		var value lsproto.SignatureHelpParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideSignatureHelp(ctx, value.TextDocument.Uri, value.Position, value.Context)
		}
	case "textDocument/documentHighlight":
		var value lsproto.DocumentHighlightParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideDocumentHighlights(ctx, value.TextDocument.Uri, value.Position)
		}
	case "textDocument/documentSymbol":
		var value lsproto.DocumentSymbolParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideDocumentSymbols(ctx, value.TextDocument.Uri)
		}
	case "textDocument/foldingRange":
		var value lsproto.FoldingRangeParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideFoldingRange(ctx, value.TextDocument.Uri)
		}
	case "textDocument/selectionRange":
		var value lsproto.SelectionRangeParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideSelectionRanges(ctx, &value)
		}
	case "textDocument/codeAction":
		var value lsproto.CodeActionParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideCodeActions(ctx, &value)
		}
	case "textDocument/prepareCallHierarchy":
		var value lsproto.CallHierarchyPrepareParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvidePrepareCallHierarchy(ctx, value.TextDocument.Uri, value.Position)
		}
	case "callHierarchy/incomingCalls":
		var value lsproto.CallHierarchyIncomingCallsParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideCallHierarchyIncomingCalls(ctx, value.Item, nil)
		}
	case "callHierarchy/outgoingCalls":
		var value lsproto.CallHierarchyOutgoingCallsParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideCallHierarchyOutgoingCalls(ctx, value.Item)
		}
	case "textDocument/inlayHint":
		var value lsproto.InlayHintParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideInlayHint(ctx, &value)
		}
	case "textDocument/semanticTokens/full":
		var value lsproto.SemanticTokensParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideSemanticTokens(ctx, value.TextDocument.Uri)
		}
	case "textDocument/semanticTokens/range":
		var value lsproto.SemanticTokensRangeParams
		if err = tsjson.Unmarshal(params, &value); err == nil {
			result, err = p.service.ProvideSemanticTokensRange(ctx, value.TextDocument.Uri, value.Range)
		}
	default:
		return nil, fmt.Errorf("unsupported typescript language-service method %q", method)
	}
	if err != nil {
		return nil, err
	}
	raw, err := tsjson.Marshal(result)
	if err != nil {
		return nil, err
	}
	if diagnosticCacheable && ctx.Err() == nil {
		p.diagnosticMu.Lock()
		p.diagnosticResponses[diagnosticKey] = slices.Clone(raw)
		p.diagnosticMu.Unlock()
	}
	return raw, nil
}

func lockProjectRequestContext(ctx context.Context, tryLock func() bool) bool {
	for {
		if ctx.Err() != nil {
			return false
		}
		if tryLock() {
			return true
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false
		case <-timer.C:
		}
	}
}

func isDiagnosticMethod(method string) bool {
	switch method {
	case "textDocument/diagnostic", "textDocument/syntacticDiagnostic", "textDocument/semanticDiagnostic", "textDocument/suggestionDiagnostic":
		return true
	default:
		return false
	}
}

func (p *Project) rebuildLocked(changedFile string, canUpdateIncrementally bool) (bool, error) {
	normalizedFiles, err := normalizeProjectFiles(p.root, p.files)
	if err != nil {
		return false, err
	}
	p.files = normalizedFiles

	if canUpdateIncrementally && p.fs != nil && p.host != nil {
		if text, ok := p.files[changedFile]; ok {
			if err := p.fs.WriteFile(changedFile, text); err != nil {
				return false, err
			}
			p.host.invalidateFile(changedFile)
			oldProgram := p.service.GetProgram()
			if oldProgram.GetSourceFile(changedFile) != nil {
				changedPath := tspath.ToPath(changedFile, p.root, p.fs.UseCaseSensitiveFileNames())
				host := compiler.NewCompilerHost(p.root, p.fs, bundled.LibPath(), nil, nil)
				program, _, incremental := oldProgram.UpdateProgram(changedPath, host, nil)
				if program != nil {
					p.service = ls.NewLanguageService(
						tspath.Path(filepath.ToSlash(filepath.Join(p.root, "jsconfig.json"))),
						program,
						p.host,
						firstOrEmpty(p.rootFiles),
					)
					return incremental, nil
				}
			}
		}
	}

	fs := bundled.WrapFS(vfstest.FromMap(p.files, true))
	fileNames := slices.DeleteFunc(slices.Sorted(mapKeys(p.files)), func(fileName string) bool {
		return !isProjectRootFile(fileName)
	})
	options := &core.CompilerOptions{
		AllowJs:          core.TSTrue,
		NoEmit:           core.TSTrue,
		Target:           core.ScriptTargetESNext,
		Module:           core.ModuleKindCommonJS,
		ModuleResolution: core.ModuleResolutionKindNode10,
	}
	if configured := p.options.CompilerOptions; configured != nil {
		for key, value := range configured {
			tsoptions.ParseCompilerOptions(key, normalizeCompilerOptionValue(key, value), options)
		}
	}
	options.AllowJs = core.TSTrue
	options.NoEmit = core.TSTrue
	options.NoLib = core.TSFalse
	options.Lib = ensureBrowserJavaScriptLibs(options.Lib)
	if p.options.TypesConfigured {
		options.Types = append([]string{}, p.options.Types...)
	}
	compare := tspath.ComparePathsOptions{UseCaseSensitiveFileNames: true, CurrentDirectory: p.root}
	config := tsoptions.NewParsedCommandLine(options, fileNames, compare)
	host := compiler.NewCompilerHost(p.root, fs, bundled.LibPath(), nil, nil)
	program := compiler.NewProgram(compiler.ProgramOptions{Host: host, Config: config, SingleThreaded: core.TSFalse})
	lsHost := newProjectHost(fs)
	p.fs = fs
	p.rootFiles = slices.Clone(fileNames)
	p.host = lsHost
	p.service = ls.NewLanguageService(tspath.Path(filepath.ToSlash(filepath.Join(p.root, "jsconfig.json"))), program, lsHost, firstOrEmpty(fileNames))
	return false, nil
}

func ensureBrowserJavaScriptLibs(libs []string) []string {
	result := append([]string(nil), libs...)
	seen := make(map[string]struct{}, len(result)+3)
	for _, lib := range result {
		seen[strings.ToLower(lib)] = struct{}{}
	}
	for _, lib := range []string{"lib.esnext.d.ts", "lib.dom.d.ts", "lib.dom.iterable.d.ts"} {
		if _, ok := seen[lib]; ok {
			continue
		}
		result = append(result, lib)
		seen[lib] = struct{}{}
	}
	return result
}

func normalizeCompilerOptionValue(key string, value any) any {
	text, ok := value.(string)
	if !ok {
		if values, ok := value.([]any); ok && (key == "lib" || key == "types" || key == "typeRoots") {
			stringsValue := make([]string, 0, len(values))
			for _, item := range values {
				if item, ok := item.(string); ok {
					stringsValue = append(stringsValue, item)
				}
			}
			return stringsValue
		}
		return value
	}
	values := map[string]map[string]float64{
		"target": {
			"es5": 1, "es2015": 2, "es6": 2, "es2016": 3, "es2017": 4, "es2018": 5, "es2019": 6,
			"es2020": 7, "es2021": 8, "es2022": 9, "es2023": 10, "es2024": 11, "es2025": 12, "esnext": 99,
		},
		"module": {
			"commonjs": 1, "amd": 2, "umd": 3, "system": 4, "es2015": 5, "es6": 5, "es2020": 6,
			"es2022": 7, "esnext": 99, "node16": 100, "node18": 101, "node20": 102, "nodenext": 199, "preserve": 200,
		},
		"moduleResolution": {"classic": 1, "node": 2, "node10": 2, "node16": 3, "nodenext": 99, "bundler": 100},
		"moduleDetection":  {"auto": 1, "legacy": 2, "force": 3},
		"jsx":              {"preserve": 1, "react-native": 2, "react": 3, "react-jsx": 4, "react-jsxdev": 5},
	}
	if mapped, ok := values[key][strings.ToLower(strings.TrimSpace(text))]; ok {
		return mapped
	}
	return value
}

type projectHost struct {
	fs          vfs.FS
	converters  *lsconv.Converters
	preferences lsutil.UserPreferences
	registry    *autoimport.Registry
	lineMaps    sync.Map
	ecmaLines   sync.Map
}

func (h *projectHost) invalidateFile(fileName string) {
	h.lineMaps.Delete(fileName)
	h.ecmaLines.Delete(fileName)
}

func newProjectHost(fs vfs.FS) *projectHost {
	h := &projectHost{fs: fs, preferences: lsutil.NewDefaultUserPreferences()}
	h.preferences.IncludeCompletionsForModuleExports = core.TSFalse
	h.preferences.InlayHints.IncludeInlayParameterNameHints = lsutil.IncludeInlayParameterNameHintsAll
	h.preferences.InlayHints.IncludeInlayVariableTypeHints = core.TSTrue
	h.preferences.InlayHints.IncludeInlayFunctionLikeReturnTypeHints = core.TSTrue
	h.converters = lsconv.NewConverters(lsproto.PositionEncodingKindUTF16, func(fileName string) *lsconv.LSPLineMap {
		if cached, ok := h.lineMaps.Load(fileName); ok {
			return cached.(*lsconv.LSPLineMap)
		}
		if text, ok := fs.ReadFile(fileName); ok {
			lineMap := lsconv.ComputeLSPLineStarts(text)
			h.lineMaps.Store(fileName, lineMap)
			return lineMap
		}
		return nil
	})
	h.registry = autoimport.NewRegistry(func(fileName string) tspath.Path {
		return tspath.ToPath(fileName, "/", fs.UseCaseSensitiveFileNames())
	}, h.preferences)
	return h
}

func (h *projectHost) UseCaseSensitiveFileNames() bool              { return h.fs.UseCaseSensitiveFileNames() }
func (h *projectHost) ReadFile(path string) (string, bool)          { return h.fs.ReadFile(path) }
func (h *projectHost) Converters() *lsconv.Converters               { return h.converters }
func (h *projectHost) GetPreferences(string) lsutil.UserPreferences { return h.preferences }
func (h *projectHost) GetECMALineInfo(fileName string) *sourcemap.ECMALineInfo {
	if cached, ok := h.ecmaLines.Load(fileName); ok {
		return cached.(*sourcemap.ECMALineInfo)
	}
	text, ok := h.fs.ReadFile(fileName)
	if !ok {
		return nil
	}
	lineInfo := sourcemap.CreateECMALineInfo(text, core.ComputeECMALineStarts(text))
	h.ecmaLines.Store(fileName, lineInfo)
	return lineInfo
}
func (h *projectHost) AutoImportRegistry() *autoimport.Registry { return h.registry }
func (h *projectHost) ReadDirectory(currentDir, path string, extensions, excludes, includes []string, depth int) []string {
	return vfsmatch.ReadDirectory(h.fs, currentDir, path, extensions, excludes, includes, depth)
}
func (h *projectHost) GetDirectories(path string) []string {
	return h.fs.GetAccessibleEntries(path).Directories
}
func (h *projectHost) DirectoryExists(path string) bool { return h.fs.DirectoryExists(path) }
func (h *projectHost) FileExists(path string) bool      { return h.fs.FileExists(path) }

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func singleChangedFile(previous, next map[string]string) (string, bool) {
	if len(previous) != len(next) || len(previous) == 0 {
		return "", false
	}
	changedFile := ""
	for fileName, previousText := range previous {
		nextText, ok := next[fileName]
		if !ok {
			return "", false
		}
		if previousText == nextText {
			continue
		}
		if changedFile != "" {
			return "", false
		}
		changedFile = fileName
	}
	return changedFile, changedFile != ""
}

func projectOptionsEqual(left, right ProjectOptions) bool {
	return left.TypesConfigured == right.TypesConfigured && slices.Equal(left.Types, right.Types) && reflect.DeepEqual(left.CompilerOptions, right.CompilerOptions)
}

func cloneCompilerOptions(options map[string]any) map[string]any {
	if options == nil {
		return nil
	}
	cloned := make(map[string]any, len(options))
	for key, value := range options {
		cloned[key] = value
	}
	return cloned
}

func isProjectRootFile(fileName string) bool {
	lower := strings.ToLower(fileName)
	for _, extension := range []string{".d.ts", ".d.mts", ".d.cts", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"} {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return false
}

func mapKeys(values map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range values {
			if !yield(key) {
				return
			}
		}
	}
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
