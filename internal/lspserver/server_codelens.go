package lspserver

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) codeLens(uri string) []lsp.CodeLens {
	return s.codeLensContext(context.Background(), uri)
}

func (s *Server) codeLensContext(ctx context.Context, uri string) []lsp.CodeLens {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	var lenses []lsp.CodeLens
	if s.settings.CodeLensReferences {
		plan := s.workspaceReferenceCodeLensPlanContext(ctx, parsed)
		if ctx.Err() != nil || len(parsed.Includes) > 0 && plan.declarations == nil {
			return nil
		}
		memberOwners := plan.memberOwners
		declarations := plan.declarations
		countStates := s.snapshotWorkspaceReferenceCodeLensCounts(parsed, declarations)
		if ctx.Err() != nil {
			return nil
		}
		for declarationIndex, declaration := range declarations {
			if ctx.Err() != nil {
				return nil
			}
			data := map[string]any{
				"kind":       "vbscript-reference",
				"uri":        uri,
				"name":       declaration.Name,
				"line":       declaration.Range.Start.Line,
				"character":  declaration.Range.Start.Character,
				"symbolKind": declaration.Kind,
			}
			if memberOf := memberOwners[declaration.Range.Start.Line]; memberOf != "" {
				data["memberOf"] = memberOf
			}
			lens := lsp.CodeLens{
				Range: declaration.Range,
				Data:  data,
			}
			countState := countStates[declarationIndex]
			if countState.final {
				lens.Command = &lsp.Command{
					Title: referenceCodeLensTitle(countState.count, s.isJapanese()), Command: "aspLsp.showReferences",
					Arguments: []any{uri, declaration.Range.Start},
				}
			} else if countState.partial {
				var previous *int
				if countState.previous {
					previous = &countState.previousCount
				}
				lens.Command = &lsp.Command{
					Title: referenceCodeLensCalculatingTitle(&countState.partialCount, previous, s.isJapanese()), Command: "aspLsp.showReferences",
					Arguments: []any{uri, declaration.Range.Start},
				}
			} else if countState.previous {
				lens.Command = &lsp.Command{
					Title: referenceCodeLensCalculatingTitle(nil, &countState.previousCount, s.isJapanese()), Command: "aspLsp.showReferences",
					Arguments: []any{uri, declaration.Range.Start},
				}
			} else {
				lens.Command = &lsp.Command{
					Title: referenceCodeLensCalculatingTitle(nil, nil, s.isJapanese()), Command: "aspLsp.showReferences",
					Arguments: []any{uri, declaration.Range.Start},
				}
			}
			lenses = append(lenses, lens)
		}
		if doc != nil && len(declarations) > 0 {
			if ctx.Err() != nil {
				return nil
			}
			s.logWorkspaceReferenceBatch(uri, doc.Version, parsed, "", declarations)
		}
	}
	if s.settings.CodeLensIncludes {
		for _, include := range parsed.Includes {
			if ctx.Err() != nil {
				return nil
			}
			targetURI := include.Path
			if details, ok := s.includeTargetDetailsForModeContext(ctx, uri, include.Path, include.Mode); ok && details.Path != "" {
				if ctx.Err() != nil {
					return nil
				}
				targetURI = filePathURI(details.Path)
			}
			lenses = append(lenses, lsp.CodeLens{
				Range: include.Range,
				Command: &lsp.Command{
					Title:     include.Path,
					Command:   "vscode.open",
					Arguments: []any{targetURI},
				},
			})
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return lenses
}

type workspaceReferenceCodeLensCountState struct {
	count         int
	partialCount  int
	previousCount int
	final         bool
	partial       bool
	previous      bool
}

func (s *Server) snapshotWorkspaceReferenceCodeLensCounts(parsed *core.ParsedDocument, declarations []vbUsageDeclaration) []workspaceReferenceCodeLensCountState {
	states := make([]workspaceReferenceCodeLensCountState, len(declarations))
	if parsed == nil || len(declarations) == 0 {
		return states
	}
	documentKey := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceReady := s.workspaceReferenceIndexReadyLocked()
	batchKey := referenceBatchCacheKey(parsed.URI, 0, s.referenceGeneration)
	if state := s.referenceBatch[batchKey]; workspaceReady && workspaceReferenceBatchCountsMatch(state, declarations) {
		for index, count := range state.finalCounts {
			states[index].count, states[index].final = count, true
			previousKey := workspaceReferencePreviousCountKeyFor(documentKey, declarations[index])
			if previous, ok := s.referencePreviousCounts[previousKey]; ok {
				states[index].previousCount, states[index].previous = previous, true
			}
			if previousKey.DocumentKey != "" {
				s.referencePreviousCounts[previousKey] = count
			}
		}
		return states
	}
	for index, declaration := range declarations {
		key := workspaceReferenceRequestKey(parsed.URI, declaration.Range.Start, false, declaration.Kind, s.referenceGeneration, declaration.Name)
		state := &states[index]
		if workspaceReady {
			if count, ok := s.referenceCounts[key]; ok {
				state.count, state.final = count, true
			} else if locations, ok := s.referenceResults[key]; ok {
				state.count, state.final = len(locations), true
			}
		}
		if partial, ok := s.referencePartialCounts[key]; ok {
			state.partialCount, state.partial = partial, true
		}
		previousKey := workspaceReferencePreviousCountKeyFor(documentKey, declaration)
		if previous, ok := s.referencePreviousCounts[previousKey]; ok {
			state.previousCount, state.previous = previous, true
		}
		if state.final && previousKey.DocumentKey != "" {
			s.referencePreviousCounts[previousKey] = state.count
		}
	}
	return states
}

func workspaceReferenceBatchCountsMatch(state *workspaceReferenceBatchState, declarations []vbUsageDeclaration) bool {
	if state == nil || !state.complete || len(state.finalCounts) != len(declarations) || len(state.declarations) != len(declarations) {
		return false
	}
	for index := range declarations {
		if state.declarations[index] != declarations[index] {
			return false
		}
	}
	return true
}

func (s *Server) workspaceReferenceCodeLensPlan(parsed *core.ParsedDocument) workspaceReferenceDeclarationPlan {
	return s.workspaceReferenceCodeLensPlanContext(context.Background(), parsed)
}

func (s *Server) workspaceReferenceCodeLensPlanContext(ctx context.Context, parsed *core.ParsedDocument) workspaceReferenceDeclarationPlan {
	if parsed == nil {
		return workspaceReferenceDeclarationPlan{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return workspaceReferenceDeclarationPlan{}
	}
	s.mu.Lock()
	generation := s.referenceGeneration
	catalog := s.legacyUndefinedGlobalCatalog
	settings := referenceCodeLensFilterSettings{
		procedures:   s.settings.CodeLensReferenceProcedures,
		globals:      s.settings.CodeLensReferenceGlobals,
		classes:      s.settings.CodeLensReferenceClasses,
		classMembers: s.settings.CodeLensReferenceClassMembers,
	}
	if cached, ok := s.referenceDeclarationPlans[parsed]; ok && cached.generation == generation && cached.catalog == catalog && cached.settings == settings {
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()
	if ctx.Err() != nil {
		return workspaceReferenceDeclarationPlan{}
	}

	declarations := s.vbscriptReferenceCodeLensDeclarationsContext(ctx, parsed)
	if ctx.Err() != nil || len(parsed.Includes) > 0 && declarations == nil {
		return workspaceReferenceDeclarationPlan{}
	}
	plan := workspaceReferenceDeclarationPlan{
		generation:   generation,
		catalog:      catalog,
		settings:     settings,
		declarations: declarations,
		memberOwners: vbClassMemberLineOwnersContext(ctx, parsed),
	}
	if ctx.Err() != nil {
		return workspaceReferenceDeclarationPlan{}
	}
	s.mu.Lock()
	currentSettings := referenceCodeLensFilterSettings{
		procedures:   s.settings.CodeLensReferenceProcedures,
		globals:      s.settings.CodeLensReferenceGlobals,
		classes:      s.settings.CodeLensReferenceClasses,
		classMembers: s.settings.CodeLensReferenceClassMembers,
	}
	if s.referenceGeneration == generation && s.legacyUndefinedGlobalCatalog == catalog && currentSettings == settings {
		s.referenceDeclarationPlans[parsed] = plan
		s.mu.Unlock()
		return plan
	}
	s.mu.Unlock()
	if ctx.Err() != nil {
		return workspaceReferenceDeclarationPlan{}
	}
	return s.workspaceReferenceCodeLensPlanContext(ctx, parsed)
}

func (s *Server) cachedWorkspaceReferenceCodeLensCount(parsed *core.ParsedDocument, declaration vbUsageDeclaration) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := workspaceReferenceRequestKey(parsed.URI, declaration.Range.Start, false, declaration.Kind, s.referenceGeneration, declaration.Name)
	if count, ok := s.referenceCounts[key]; ok {
		return count, true
	}
	locations, ok := s.referenceResults[key]
	return len(locations), ok
}

func workspaceReferencePreviousCountKey(parsed *core.ParsedDocument, declaration vbUsageDeclaration) workspaceReferencePreviousKey {
	if parsed == nil {
		return workspaceReferencePreviousKey{}
	}
	return workspaceReferencePreviousCountKeyFor(workspacepkg.FileIdentityKeyFromURI(parsed.URI), declaration)
}

func workspaceReferencePreviousCountKeyFor(documentKey string, declaration vbUsageDeclaration) workspaceReferencePreviousKey {
	return workspaceReferencePreviousKey{
		DocumentKey: documentKey, NameHash: workspaceReferenceNameHash(declaration.Name), Kind: declaration.Kind,
		MemberOfHash: workspaceReferenceNameHash(declaration.MemberOf), ScopeHash: workspaceReferenceNameHash(declaration.Scope),
	}
}

func (s *Server) rememberWorkspaceReferenceCount(parsed *core.ParsedDocument, declaration vbUsageDeclaration, count int) {
	key := workspaceReferencePreviousCountKey(parsed, declaration)
	if key.DocumentKey == "" {
		return
	}
	s.mu.Lock()
	s.referencePreviousCounts[key] = count
	s.mu.Unlock()
}

func vbClassMemberLineOwners(parsed *core.ParsedDocument) map[int]string {
	return vbClassMemberLineOwnersContext(context.Background(), parsed)
}

const vbClassMemberLineOwnersAnalysisKey = "lspserver.vb-class-member-line-owners.v1"

func vbClassMemberLineOwnersContext(ctx context.Context, parsed *core.ParsedDocument) map[int]string {
	if parsed == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(vbClassMemberLineOwnersAnalysisKey); ok {
		if cached, ok := value.(map[int]string); ok {
			return cached
		}
	}
	source := vbTextDocument(parsed)
	owners := map[int]string{}
	for _, region := range parsed.Regions {
		if ctx.Err() != nil {
			return nil
		}
		if region.Language != core.LanguageVBScript {
			continue
		}
		inClass := false
		className := ""
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			if ctx.Err() != nil {
				return nil
			}
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			trimmed := strings.TrimSpace(line)
			lowerTrimmed := strings.ToLower(trimmed)
			if matches := vbClassDeclarationLinePattern.FindStringSubmatchIndex(line); matches != nil {
				inClass = true
				className = line[matches[2]:matches[3]]
			} else if inClass {
				owners[source.PositionAt(lineStart).Line] = className
			}
			if lowerTrimmed == "end class" {
				inClass = false
				className = ""
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	parsed.StoreRuntimeAnalysis(vbClassMemberLineOwnersAnalysisKey, owners)
	return owners
}

func (s *Server) vbscriptReferenceCodeLensDeclarations(parsed *core.ParsedDocument) []vbUsageDeclaration {
	return s.vbscriptReferenceCodeLensDeclarationsContext(context.Background(), parsed)
}

func (s *Server) vbscriptReferenceCodeLensDeclarationsContext(ctx context.Context, parsed *core.ParsedDocument) []vbUsageDeclaration {
	if parsed == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	declarations := collectVBNamingDeclarations(parsed)
	if ctx.Err() != nil {
		return nil
	}
	declarations = append(declarations, serverObjectDeclarations(parsed)...)
	if ctx.Err() != nil {
		return nil
	}
	var includedGlobalNames map[string]struct{}
	if len(parsed.Includes) > 0 {
		included, complete := s.includedDocumentsContextResult(ctx, parsed)
		if !complete || ctx.Err() != nil {
			return nil
		}
		includedGlobalNames = includedVBVariableInlayGlobalNamesContext(ctx, included)
		if ctx.Err() != nil {
			return nil
		}
	}
	for _, declaration := range implicitAssignmentInlayDeclarations(parsed, true, includedGlobalNames) {
		if ctx.Err() != nil {
			return nil
		}
		if declaration.Implicit && !declaration.Local {
			declarations = append(declarations, declaration)
		}
	}
	classLines := vbClassLineSet(parsed)
	procedureLines := vbProcedureLineSet(parsed)
	settings := s.referenceCodeLensFilterSettings()
	if ctx.Err() != nil {
		return nil
	}
	result := make([]vbUsageDeclaration, 0, len(declarations))
	seen := map[string]struct{}{}
	for _, declaration := range declarations {
		if ctx.Err() != nil {
			return nil
		}
		if declaration.Local {
			continue
		}
		declaration.Kind = vbCodeLensSymbolKind(declaration, classLines)
		if !shouldShowVBReferenceCodeLens(declaration, classLines, procedureLines, settings) {
			continue
		}
		key := strings.ToLower(declaration.Name) + ":" + strconv.Itoa(declaration.Range.Start.Line) + ":" + strconv.Itoa(declaration.Range.Start.Character)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, declaration)
	}
	if ctx.Err() != nil {
		return nil
	}
	return result
}

func vbCodeLensSymbolKind(declaration vbUsageDeclaration, classLines map[int]struct{}) string {
	if _, ok := classLines[declaration.Line]; !ok {
		if declaration.Kind == "const" {
			return "constant"
		}
		return declaration.Kind
	}
	switch declaration.Kind {
	case "sub", "function":
		return "method"
	case "variable":
		return "field"
	case "const":
		return "constant"
	default:
		return declaration.Kind
	}
}

type referenceCodeLensFilterSettings struct {
	procedures   bool
	globals      bool
	classes      bool
	classMembers bool
}

func (s *Server) referenceCodeLensFilterSettings() referenceCodeLensFilterSettings {
	s.mu.Lock()
	settings := referenceCodeLensFilterSettings{
		procedures: s.settings.CodeLensReferenceProcedures, globals: s.settings.CodeLensReferenceGlobals,
		classes: s.settings.CodeLensReferenceClasses, classMembers: s.settings.CodeLensReferenceClassMembers,
	}
	s.mu.Unlock()
	return settings
}

func shouldShowVBReferenceCodeLens(declaration vbUsageDeclaration, classLines map[int]struct{}, procedureLines map[int]struct{}, settings referenceCodeLensFilterSettings) bool {
	inClass := false
	if _, ok := classLines[declaration.Line]; ok {
		inClass = true
	}
	inProcedure := false
	if _, ok := procedureLines[declaration.Line]; ok {
		inProcedure = true
	}
	switch declaration.Kind {
	case "sub", "function", "method", "property":
		return settings.procedures
	case "class":
		return settings.classes
	case "variable", "constant":
		if inProcedure && !inClass {
			return false
		}
		if inClass {
			return settings.classMembers
		}
		return settings.globals
	case "field":
		return settings.classMembers
	default:
		return true
	}
}

func vbProcedureLineSet(parsed *core.ParsedDocument) map[int]struct{} {
	var cached map[int]struct{}
	if parsed.LoadAnalysis("lspserver.vb-procedure-lines.v1", &cached) {
		return cached
	}
	lines := map[int]struct{}{}
	procedureScopes := vbProcedureScopes(parsed)
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			if vbProcedureScopeAtLine(procedureScopes, doc.PositionAt(lineStart).Line) != "" {
				lines[doc.PositionAt(lineStart).Line] = struct{}{}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	parsed.StoreAnalysis("lspserver.vb-procedure-lines.v1", lines)
	return lines
}

func vbClassLineSet(parsed *core.ParsedDocument) map[int]struct{} {
	var cached map[int]struct{}
	if parsed.LoadAnalysis("lspserver.vb-class-lines.v1", &cached) {
		return cached
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	lines := map[int]struct{}{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		inClass := false
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			lowerTrimmed := strings.ToLower(strings.TrimSpace(line))
			if strings.HasPrefix(lowerTrimmed, "class ") {
				inClass = true
			}
			if inClass {
				lines[doc.PositionAt(lineStart).Line] = struct{}{}
			}
			if lowerTrimmed == "end class" {
				inClass = false
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	parsed.StoreAnalysis("lspserver.vb-class-lines.v1", lines)
	return lines
}

type workspaceReferenceProgressContextKey struct{}

type workspaceReferenceLocationSinkContextKey struct{}

type workspaceReferenceProgressReporter func(current, total int, uri string)
type workspaceReferenceLocationSink func([]lsp.Location)

func (s *Server) beginWorkspaceReferenceProgress(ctx context.Context, name, uri string, version int) (context.Context, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	taskID, _ := s.beginDocumentProgressTask("references.count", "analyzing", "references", "references.workspace", name, uri, version, true, 0, false)
	var progressMu sync.Mutex
	lastImmediateCurrent := 0
	lastImmediateAt := time.Now()
	reporter := workspaceReferenceProgressReporter(func(current, total int, uri string) {
		detail := progressDetailForURI(uri)
		activeItems := []string(nil)
		if detail != "" {
			activeItems = []string{detail}
		}
		progressMu.Lock()
		immediate := current == 0 || total > 0 && current >= total || current-lastImmediateCurrent >= workspaceReferenceProgressSegmentBatch || time.Since(lastImmediateAt) >= 100*time.Millisecond
		if immediate {
			lastImmediateCurrent = current
			lastImmediateAt = time.Now()
		}
		progressMu.Unlock()
		if immediate {
			if done := s.updateProgressTaskImmediate(taskID, "references", "references.workspace", detail, current, total, activeItems, "running"); done != nil {
				<-done
			}
			return
		}
		s.updateProgressTask(taskID, "references", "references.workspace", detail, current, total, activeItems, "running")
	})
	return context.WithValue(ctx, workspaceReferenceProgressContextKey{}, reporter), taskID
}

func workspaceReferenceProgressFromContext(ctx context.Context) workspaceReferenceProgressReporter {
	if ctx == nil {
		return nil
	}
	reporter, _ := ctx.Value(workspaceReferenceProgressContextKey{}).(workspaceReferenceProgressReporter)
	return reporter
}

func workspaceReferenceLocationSinkFromContext(ctx context.Context) workspaceReferenceLocationSink {
	if ctx == nil {
		return nil
	}
	sink, _ := ctx.Value(workspaceReferenceLocationSinkContextKey{}).(workspaceReferenceLocationSink)
	return sink
}

func emitWorkspaceReferenceLocations(ctx context.Context, locations []lsp.Location) {
	if len(locations) == 0 {
		return
	}
	if sink := workspaceReferenceLocationSinkFromContext(ctx); sink != nil {
		sink(locations)
	}
}
