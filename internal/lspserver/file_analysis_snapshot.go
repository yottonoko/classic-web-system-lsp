package lspserver

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

// fileAnalysisSnapshotSchemaVersion invalidates complete file-analysis payloads
// when their persisted semantics change. Version 9 rejects v8 snapshots because
// duplicate procedure signatures now use the first declaration; the disk cache
// tool version remains unchanged so unrelated cache components stay reusable.
const fileAnalysisSnapshotSchemaVersion = 9

type fileAnalysisSnapshot struct {
	SchemaVersion                int
	URI                          string
	ReferenceShard               *vbscript.ReferenceShard
	ReferenceFacts               vbReferenceDocumentFacts
	Symbols                      vbscript.SymbolIndex `json:"-"`
	Signatures                   map[string]vbscript.Signature
	SignatureList                []vbscript.Signature
	Usage                        vbUsageDeclarations
	GraphDeclarations            []vbUsageDeclaration
	Assignments                  []vbAssignment
	AnalysisTypes                vbGraphAnalysisTypes
	Members                      []graphMemberOccurrence
	Summary                      vbFileAnalysisSummary
	Includes                     []resolvedIncludeSnapshot
	IncludeResolutionFingerprint string
	SymbolFacts                  map[string]symbolAnalysisFact
	VirtualDocuments             map[core.EmbeddedLanguage]core.VirtualDocument
	VBDocumentSymbols            []lsp.DocumentSymbol
	VBFoldingRanges              []lsp.FoldingRange
	DocumentColors               []lsp.ColorInformation
	NamingDeclarations           []vbUsageDeclaration
	VBClassLines                 map[int]struct{}
	VBProcedureLines             map[int]struct{}
	// runtimeBacked is true for snapshots assembled from a live ParsedDocument.
	// It is deliberately unexported so persisted JSON snapshots remain
	// schema-compatible; decoded snapshots retain the zero value and own their
	// typed payloads apart from explicitly seeded runtime values.
	runtimeBacked bool
}

type resolvedIncludeSnapshot struct {
	Path         string
	Mode         string
	Range        lsp.Range
	ResolvedPath string
	URI          string
	Exists       bool
}

type symbolAnalysisFact struct {
	Name           string
	NormalizedName string
	Kind           string
	Scope          string
	MemberOf       string
	Declaration    lsp.Range
	Occurrences    []symbolOccurrenceFact
	ReferenceCount int
	ReadCount      int
	WriteCount     int
	CallCount      int
}

type symbolOccurrenceFact struct {
	Range lsp.Range
	Role  string
}

func (s *Server) buildFileAnalysisSnapshot(parsed *core.ParsedDocument) *fileAnalysisSnapshot {
	return s.buildFileAnalysisSnapshotContext(context.Background(), parsed)
}

func (s *Server) buildFileAnalysisSnapshotContext(ctx context.Context, parsed *core.ParsedDocument) *fileAnalysisSnapshot {
	if parsed == nil {
		return nil
	}
	s.mu.Lock()
	testHook := s.fileAnalysisSnapshotTestHook
	s.mu.Unlock()
	if testHook != nil {
		testHook()
	}
	referenceShard := vbscript.BuildReferenceShard(parsed)
	symbols := vbscript.BuildSymbolIndex(parsed)
	referenceFacts := vbReferenceDocumentFactsFor(parsed)
	usage := collectVBUsageDeclarations(parsed)
	assignments := vbscriptAssignments(parsed)
	snapshot := &fileAnalysisSnapshot{
		SchemaVersion:                fileAnalysisSnapshotSchemaVersion,
		URI:                          parsed.URI,
		ReferenceShard:               referenceShard,
		ReferenceFacts:               referenceFacts,
		Symbols:                      symbols,
		Signatures:                   vbscript.BuildSignatures(parsed),
		SignatureList:                vbscript.Signatures(parsed),
		Usage:                        usage,
		GraphDeclarations:            graphVBDeclarations(parsed),
		Assignments:                  assignments,
		AnalysisTypes:                graphAnalysisTypes(parsed),
		Members:                      graphMemberOccurrences(parsed),
		Summary:                      summarizeVBScriptFileAnalysis(parsed),
		SymbolFacts:                  buildSymbolAnalysisFacts(parsed, symbols, usage, assignments),
		IncludeResolutionFingerprint: s.includeResolutionFingerprintContext(ctx, parsed),
		VirtualDocuments:             map[core.EmbeddedLanguage]core.VirtualDocument{},
		VBDocumentSymbols:            vbscript.DocumentSymbols(parsed),
		VBFoldingRanges:              vbscript.FoldingRanges(parsed),
		DocumentColors:               core.DocumentColors(parsed),
		NamingDeclarations:           collectVBNamingDeclarations(parsed),
		VBClassLines:                 vbClassLineSet(parsed),
		VBProcedureLines:             vbProcedureLineSet(parsed),
		runtimeBacked:                true,
	}
	for _, language := range []core.EmbeddedLanguage{core.LanguageHTML, core.LanguageCSS, core.LanguageJavaScript, core.LanguageVBScript, core.LanguageJScript} {
		snapshot.VirtualDocuments[language] = core.BuildVirtualDocument(parsed, language)
	}
	for _, include := range parsed.Includes {
		resolved := resolvedIncludeSnapshot{Path: include.Path, Mode: include.Mode, Range: include.Range}
		if details, ok := s.includeTargetDetailsForModeContext(ctx, parsed.URI, include.Path, include.Mode); ok {
			resolved.ResolvedPath = details.Path
			resolved.Exists = details.Exists
			if details.Path != "" {
				resolved.URI = filePathURI(details.Path)
			}
		}
		snapshot.Includes = append(snapshot.Includes, resolved)
	}
	return snapshot
}

func (s *Server) scheduleFileAnalysisSnapshot(doc *core.TextDocument, parsed *core.ParsedDocument, defaultLanguage string) {
	if doc == nil || parsed == nil {
		return
	}
	document := core.NewTextDocument(doc.URI, doc.LanguageID, doc.Version, doc.Text)
	key := "file-analysis\x00" + workspacepkg.FileIdentityKeyFromURI(document.URI)
	s.runAsyncDiskCacheWriteKey(key, func() {
		// The fingerprint and the snapshot resolve the same includes.
		ctx := withIncludeResolutionMemo(context.Background())
		cache := s.diskCacheForUse()
		if cache == nil || !cache.Enabled() {
			return
		}
		if !s.fileAnalysisSnapshotTaskCurrent(document, parsed, defaultLanguage) {
			return
		}
		lookup := s.parsedDiskLookup(document, defaultLanguage)
		validateSource := s.diskCacheUsesDefaultDirectory()
		includeResolution := s.includeResolutionFingerprintContext(ctx, parsed)
		snapshot := s.cachedFileAnalysisSnapshot(parsed)
		if snapshot == nil || snapshot.IncludeResolutionFingerprint != includeResolution {
			snapshot = s.buildFileAnalysisSnapshotContext(ctx, parsed)
			s.rememberFileAnalysisSnapshot(parsed, snapshot)
		}
		s.writeDiskParsedDocumentToCache(cache, document, defaultLanguage, parsed, snapshot, lookup, validateSource)
	})
}

func (s *Server) fileAnalysisSnapshotTaskCurrent(doc *core.TextDocument, parsed *core.ParsedDocument, defaultLanguage string) bool {
	if doc == nil || parsed == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.openDocumentByURILocked(doc.URI)
	if current == nil {
		current = s.workspace[doc.URI]
	}
	if current == nil && strings.HasPrefix(strings.ToLower(doc.URI), "file:") {
		if candidates := s.workspaceURIsForIdentityLocked(doc.URI); len(candidates) > 0 {
			current = s.workspace[candidates[0]]
		}
	}
	if current != nil && (current.Text != doc.Text || current.Version != doc.Version) {
		return false
	}
	if cached, ok := s.parsedCache[parsedDocumentCacheKey(doc.URI)]; ok {
		return cached.Parsed == parsed && cached.Version == doc.Version && cached.Text == doc.Text && cached.DefaultLanguage == defaultLanguage
	}
	if cached, ok := s.parsedCache[parsedTextCacheKey(doc.URI)]; ok {
		return cached.Parsed == parsed && cached.Text == doc.Text && cached.DefaultLanguage == defaultLanguage
	}
	return true
}

func buildSymbolAnalysisFacts(parsed *core.ParsedDocument, symbols vbscript.SymbolIndex, usage vbUsageDeclarations, assignments []vbAssignment) map[string]symbolAnalysisFact {
	mergedByRange := map[string]vbUsageDeclaration{}
	for _, declaration := range usage.Declarations {
		key := strings.ToLower(declaration.Name) + "\x00" + snapshotRangeKey(declaration.Range)
		if current, ok := mergedByRange[key]; ok {
			mergedByRange[key] = mergeSnapshotDeclaration(current, declaration)
		} else {
			mergedByRange[key] = declaration
		}
	}
	merged := make([]vbUsageDeclaration, 0, len(mergedByRange))
	for _, declaration := range mergedByRange {
		merged = append(merged, declaration)
	}
	facts := make(map[string]symbolAnalysisFact, len(merged))
	declarations := map[string][]vbUsageDeclaration{}
	for _, declaration := range merged {
		key := strings.ToLower(declaration.Name)
		declarations[key] = append(declarations[key], declaration)
		identity := symbolDeclarationIdentity(declaration)
		facts[identity] = symbolAnalysisFact{
			Name:           declaration.Name,
			NormalizedName: key,
			Kind:           declaration.Kind,
			Scope:          declaration.Scope,
			MemberOf:       declaration.MemberOf,
			Declaration:    declaration.Range,
		}
	}
	writes := map[string]map[string]struct{}{}
	for _, assignment := range assignments {
		key := strings.ToLower(assignment.Name)
		if writes[key] == nil {
			writes[key] = map[string]struct{}{}
		}
		writes[key][snapshotRangeKey(assignment.NameRange)] = struct{}{}
	}
	doc := core.SourceDocument(parsed)
	scopes := vbScopeIntervalsForDocument(parsed)
	for key, occurrences := range symbols.Occurrences {
		for _, occurrence := range occurrences {
			identity, declaration, declared := resolveSymbolOccurrenceDeclaration(key, occurrence.Range, declarations[key], vbScopeAtOffset(scopes, doc.OffsetAt(occurrence.Range.Start)))
			if !declared {
				identity = "unresolved:" + key
			}
			fact, ok := facts[identity]
			if !ok {
				fact = symbolAnalysisFact{Name: occurrence.Name, NormalizedName: key}
				if symbol, exists := symbols.Declarations[key]; exists {
					fact.Name = symbol.Name
					fact.Kind = symbol.Kind
					fact.Declaration = symbol.Range
				}
			}
			role := "read"
			if declared && occurrence.Range == declaration.Range {
				role = "declaration"
			} else if _, ok := writes[key][snapshotRangeKey(occurrence.Range)]; ok {
				role = "write"
			} else if end := doc.OffsetAt(occurrence.Range.End); nextNonSpaceByte(parsed.Text, end) >= 0 && parsed.Text[nextNonSpaceByte(parsed.Text, end)] == '(' {
				role = "call"
			}
			fact.Occurrences = append(fact.Occurrences, symbolOccurrenceFact{Range: occurrence.Range, Role: role})
			switch role {
			case "read":
				fact.ReadCount++
				fact.ReferenceCount++
			case "write":
				fact.WriteCount++
				fact.ReferenceCount++
			case "call":
				fact.CallCount++
				fact.ReferenceCount++
			}
			facts[identity] = fact
		}
	}
	return facts
}

func mergeSnapshotDeclaration(current, candidate vbUsageDeclaration) vbUsageDeclaration {
	if current.Kind == "" || current.Kind == "variable" && candidate.Kind != "" && candidate.Kind != "variable" {
		current.Kind = candidate.Kind
	}
	if candidate.Scope != "" {
		current.Scope = candidate.Scope
	}
	if candidate.MemberOf != "" {
		current.MemberOf = candidate.MemberOf
	}
	if candidate.ProcedureKind != "" {
		current.ProcedureKind = candidate.ProcedureKind
	}
	if candidate.TypeName != "" {
		current.TypeName = candidate.TypeName
	}
	if candidate.AssignedValue != "" {
		current.AssignedValue = candidate.AssignedValue
	}
	current.Local = current.Local || candidate.Local
	current.Implicit = current.Implicit && candidate.Implicit
	current.Uncertain = current.Uncertain || candidate.Uncertain
	if candidate.Start != 0 || current.Start == 0 {
		current.Start = candidate.Start
		current.End = candidate.End
		current.Line = candidate.Line
	}
	return current
}

type vbLexicalScope struct {
	Procedure string
	Class     string
}

type vbScopeInterval struct {
	Start int
	End   int
	Scope vbLexicalScope
}

func vbScopeIntervalsForDocument(parsed *core.ParsedDocument) []vbScopeInterval {
	intervals := []vbScopeInterval{}
	current := vbLexicalScope{}
	procedureScopes := vbProcedureScopes(parsed)
	procedureScopeIndex := newVBProcedureScopeIndex(procedureScopes)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			for _, statement := range splitVBStatementSegments(parsed.Text[lineStart:lineEnd], lineStart) {
				trimmed := strings.TrimSpace(statement.Text)
				lower := strings.ToLower(trimmed)
				if scope := procedureScopeIndex.at(statement.Start); scope != "" {
					current.Procedure = scope
				} else if className := vbClassDeclarationName(trimmed); className != "" {
					current.Class = className
				}
				intervals = append(intervals, vbScopeInterval{Start: statement.Start, End: statement.End, Scope: current})
				if lower == "end sub" || lower == "end function" || lower == "end property" {
					current.Procedure = ""
				} else if lower == "end class" {
					current = vbLexicalScope{}
				}
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
	return intervals
}

func vbScopeAtOffset(intervals []vbScopeInterval, offset int) vbLexicalScope {
	for _, interval := range intervals {
		if offset >= interval.Start && offset <= interval.End {
			return interval.Scope
		}
	}
	return vbLexicalScope{}
}

func vbClassDeclarationName(statement string) string {
	fields := strings.Fields(statement)
	for index := 0; index+1 < len(fields); index++ {
		if strings.EqualFold(fields[index], "Class") {
			end := readVBIdentifier(fields[index+1], 0)
			if end > 0 {
				return strings.ToLower(fields[index+1][:end])
			}
		}
	}
	return ""
}

func resolveSymbolOccurrenceDeclaration(name string, occurrence lsp.Range, declarations []vbUsageDeclaration, scope vbLexicalScope) (string, vbUsageDeclaration, bool) {
	for _, declaration := range declarations {
		if declaration.Range == occurrence {
			return symbolDeclarationIdentity(declaration), declaration, true
		}
	}
	for _, declaration := range declarations {
		if scope.Procedure != "" && strings.EqualFold(declaration.Scope, scope.Procedure) && (declaration.MemberOf == "" || strings.EqualFold(declaration.MemberOf, scope.Class)) {
			return symbolDeclarationIdentity(declaration), declaration, true
		}
	}
	for _, declaration := range declarations {
		if scope.Class != "" && strings.EqualFold(declaration.MemberOf, scope.Class) && declaration.Scope == "" {
			return symbolDeclarationIdentity(declaration), declaration, true
		}
	}
	for _, declaration := range declarations {
		if !declaration.Local && declaration.Scope == "" && declaration.MemberOf == "" {
			return symbolDeclarationIdentity(declaration), declaration, true
		}
	}
	return "unresolved:" + name, vbUsageDeclaration{}, false
}

func symbolDeclarationIdentity(declaration vbUsageDeclaration) string {
	return strings.Join([]string{
		strings.ToLower(declaration.Kind),
		strings.ToLower(declaration.MemberOf),
		strings.ToLower(declaration.Scope),
		strings.ToLower(declaration.Name),
		strconv.Itoa(declaration.Start),
		strconv.Itoa(declaration.End),
	}, "\x00")
}

func snapshotRangeKey(value lsp.Range) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func (s *Server) includeResolutionFingerprint(parsed *core.ParsedDocument) string {
	return s.includeResolutionFingerprintContext(context.Background(), parsed)
}

func (s *Server) includeResolutionFingerprintContext(ctx context.Context, parsed *core.ParsedDocument) string {
	if parsed == nil || len(parsed.Includes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		part := include.Mode + "\x00" + include.Path
		if details, ok := s.includeTargetDetailsForModeContext(ctx, parsed.URI, include.Path, include.Mode); ok {
			part += "\x00" + details.Path + "\x00" + strconv.FormatBool(details.Exists)
		}
		parts = append(parts, part)
	}
	return workspacepkg.DiskContentHash(strings.Join(parts, "\x01"))
}

func (s *Server) readDiskFileAnalysisSnapshot(doc *core.TextDocument, parsed *core.ParsedDocument, defaultLanguage string) (*fileAnalysisSnapshot, bool) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || doc == nil {
		return nil, false
	}
	entry, ok := cache.ReadFileBundle(s.parsedDiskLookup(doc, defaultLanguage))
	if !ok {
		return nil, false
	}
	var snapshot fileAnalysisSnapshot
	if err := json.Unmarshal(entry.AnalysisSnapshot, &snapshot); err != nil || snapshot.SchemaVersion != fileAnalysisSnapshotSchemaVersion || snapshot.URI != doc.URI || snapshot.IncludeResolutionFingerprint != s.includeResolutionFingerprint(parsed) {
		return nil, false
	}
	s.logAnalysisDatabaseEvent("fileAnalysis", "hit", map[string]any{
		"settingsKey": shortLogKey(entry.SettingsKey), "symbols": len(snapshot.SymbolFacts), "uri": doc.URI,
	})
	return &snapshot, true
}

func (s *Server) ensureFileAnalysisSnapshot(doc *core.TextDocument, parsed *core.ParsedDocument, defaultLanguage string) *fileAnalysisSnapshot {
	if parsed == nil || doc == nil {
		return nil
	}
	ctx := withIncludeResolutionMemo(context.Background())
	includeResolution := s.includeResolutionFingerprintContext(ctx, parsed)
	if snapshot := s.cachedFileAnalysisSnapshot(parsed); snapshot != nil && snapshot.IncludeResolutionFingerprint == includeResolution {
		return snapshot
	}
	if snapshot, ok := s.readDiskFileAnalysisSnapshot(doc, parsed, defaultLanguage); ok {
		seedParsedAnalysis(parsed, snapshot)
		s.rememberFileAnalysisSnapshot(parsed, snapshot)
		return snapshot
	}
	snapshot := s.buildFileAnalysisSnapshotContext(ctx, parsed)
	s.rememberFileAnalysisSnapshot(parsed, snapshot)
	s.writeDiskParsedDocument(doc, defaultLanguage, parsed, snapshot)
	return snapshot
}

func seedParsedAnalysis(parsed *core.ParsedDocument, snapshot *fileAnalysisSnapshot) {
	if parsed == nil || snapshot == nil {
		return
	}
	// Facts that are only read back through runtime analysis are seeded as
	// runtime values; encoding them again as JSON would duplicate the restored
	// snapshot in memory without ever being decoded.
	vbscript.SeedReferenceShard(parsed, snapshot.ReferenceShard)
	snapshot.Symbols = vbscript.BuildSymbolIndex(parsed)
	parsed.StoreAnalysis(vbReferenceDocumentFactsAnalysisKey, snapshot.ReferenceFacts)
	parsed.StoreAnalysis("vbscript.signatures-by-name.v2", snapshot.Signatures)
	parsed.StoreAnalysis("vbscript.signatures.v1", snapshot.SignatureList)
	parsed.StoreAnalysis("lspserver.vb-usage.v2", snapshot.Usage)
	parsed.StoreAnalysis("lspserver.graph-vb-declarations.v2", snapshot.GraphDeclarations)
	parsed.StoreAnalysis("lspserver.vb-assignments.v2", snapshot.Assignments)
	parsed.StoreAnalysis("lspserver.graph-analysis-types.v6", snapshot.AnalysisTypes)
	parsed.StoreAnalysis("lspserver.graph-member-occurrences.v1", snapshot.Members)
	parsed.StoreRuntimeAnalysis(vbFileAnalysisSummaryAnalysisKey, snapshot.Summary)
	parsed.StoreAnalysis("vbscript.document-symbols.v1", snapshot.VBDocumentSymbols)
	parsed.StoreAnalysis("vbscript.folding-ranges.v1", snapshot.VBFoldingRanges)
	parsed.StoreAnalysis("core.document-colors.v1", snapshot.DocumentColors)
	parsed.StoreAnalysis("lspserver.vb-naming-declarations.v1", snapshot.NamingDeclarations)
	parsed.StoreAnalysis("lspserver.vb-class-lines.v1", snapshot.VBClassLines)
	parsed.StoreAnalysis("lspserver.vb-procedure-lines.v1", snapshot.VBProcedureLines)
	for language, virtual := range snapshot.VirtualDocuments {
		parsed.StoreAnalysis("core.virtual-document.v1."+string(language), virtual)
	}
}
