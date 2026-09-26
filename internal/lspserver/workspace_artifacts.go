package lspserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type workspaceDocumentID string

func workspaceDocumentIDFromURI(uri string) workspaceDocumentID {
	return workspaceDocumentID(workspacepkg.SourceURIIdentityKey(uri))
}

type workspaceArtifactFingerprint string

// workspaceArtifactSnapshot contains only the facts needed to publish the
// workspace artifact manifest. It is intentionally separate from
// fileAnalysisSnapshot, whose additional fields are required by request
// features and disk persistence.
type workspaceArtifactSnapshot struct {
	URI                          string
	ReferenceShard               *vbscript.ReferenceShard
	Usage                        vbUsageDeclarations
	Summary                      vbFileAnalysisSummary
	VirtualDocuments             map[core.EmbeddedLanguage]core.VirtualDocument
	Includes                     []resolvedIncludeSnapshot
	IncludeResolutionFingerprint string
}

type workspaceDocumentArtifactManifest struct {
	DocumentID                workspaceDocumentID
	URI                       string
	SourceFingerprint         workspaceArtifactFingerprint
	ParserSettingsFingerprint workspaceArtifactFingerprint
	CST                       *core.ParsedDocument
	IncludeFingerprint        workspaceArtifactFingerprint
	ExecutionTapeFingerprint  workspaceArtifactFingerprint
	IncludeEdges              []workspaceIncludeArtifactEdge
	ExecutionTape             []workspaceDocumentExecutionEvent
	VBExports                 []vbExportSummary
	VBExportsFingerprint      workspaceArtifactFingerprint
	VBExportsCacheSourceHash  string
	PublicSymbols             map[string]workspaceArtifactFingerprint
	ExternalUsages            map[string]workspaceArtifactFingerprint
	ImplicitGlobalCandidates  map[string]workspaceArtifactFingerprint
	ObjectTagVariables        map[string]workspaceArtifactFingerprint
	References                map[string]workspaceReferenceArtifactSegment
	VirtualDocuments          map[core.EmbeddedLanguage]workspaceArtifactFingerprint
	LocalDiagnostics          map[string]workspaceArtifactFingerprint
}

type workspaceIncludeArtifactEdge struct {
	Path       string
	Mode       string
	DocumentID workspaceDocumentID
	Exists     bool
	Range      lsp.Range
}

type workspaceDocumentExecutionEventKind string

const (
	workspaceExecutionInclude             workspaceDocumentExecutionEventKind = "include"
	workspaceExecutionImplicitDeclaration workspaceDocumentExecutionEventKind = "implicit-declaration"
)

type workspaceDocumentExecutionEvent struct {
	Kind       workspaceDocumentExecutionEventKind
	Offset     int
	Include    workspaceIncludeArtifactEdge
	Name       string
	SymbolKind string
	Range      lsp.Range
}

type workspaceReferenceArtifactSegment struct {
	CountFingerprint    workspaceArtifactFingerprint
	LocationFingerprint workspaceArtifactFingerprint
}

type workspaceDocumentArtifactDelta struct {
	SourceChanged             bool
	ParserSettingsChanged     bool
	CSTChanged                bool
	IncludeEdgesChanged       bool
	ExecutionTapeChanged      bool
	VBExportsChanged          bool
	ChangedPublicNames        []string
	ChangedUsageNames         []string
	ChangedImplicitNames      []string
	ChangedObjectTagNames     []string
	ChangedReferenceCounts    []string
	ChangedReferenceLocations []string
	ChangedVirtualLanguages   []core.EmbeddedLanguage
	ChangedDiagnosticLayers   []string
}

func estimateWorkspaceDocumentArtifactBytes(manifest *workspaceDocumentArtifactManifest) int64 {
	if manifest == nil {
		return 0
	}
	bytes := int64(512 + len(manifest.DocumentID)*2 + len(manifest.URI)*2)
	bytes += int64(len(manifest.SourceFingerprint)+len(manifest.ParserSettingsFingerprint)+len(manifest.IncludeFingerprint)+len(manifest.ExecutionTapeFingerprint)) * 2
	bytes += int64(len(manifest.VBExportsFingerprint)+len(manifest.VBExportsCacheSourceHash)) * 2
	if parsed := manifest.CST; parsed != nil {
		// cloneWorkspaceArtifactCST owns an independent structural clone,
		// including its persisted Analysis payload. Its runtime maps are empty,
		// so the core estimate accounts for exactly the storage retained here.
		bytes += parsed.EstimateBytes()
	}
	for _, edge := range manifest.IncludeEdges {
		bytes += int64(160 + len(edge.Path)*2 + len(edge.Mode)*2 + len(edge.DocumentID)*2)
	}
	for _, event := range manifest.ExecutionTape {
		bytes += int64(224 + len(event.Kind)*2 + len(event.Name)*2 + len(event.SymbolKind)*2)
		bytes += int64(len(event.Include.Path)+len(event.Include.Mode)+len(event.Include.DocumentID)) * 2
	}
	for _, export := range manifest.VBExports {
		bytes += estimateVBExportSummaryBytes(export)
	}
	bytes += estimateWorkspaceArtifactFingerprintMapBytes(manifest.PublicSymbols)
	bytes += estimateWorkspaceArtifactFingerprintMapBytes(manifest.ExternalUsages)
	bytes += estimateWorkspaceArtifactFingerprintMapBytes(manifest.ImplicitGlobalCandidates)
	bytes += estimateWorkspaceArtifactFingerprintMapBytes(manifest.ObjectTagVariables)
	bytes += estimateWorkspaceArtifactFingerprintMapBytes(manifest.LocalDiagnostics)
	for name, segment := range manifest.References {
		bytes += int64(128+len(name)*2) + int64(len(segment.CountFingerprint)+len(segment.LocationFingerprint))*2
	}
	for language, fingerprint := range manifest.VirtualDocuments {
		bytes += int64(96 + len(language)*2 + len(fingerprint)*2)
	}
	return bytes
}

func estimateWorkspaceArtifactFingerprintMapBytes(values map[string]workspaceArtifactFingerprint) int64 {
	bytes := int64(64)
	for key, value := range values {
		bytes += int64(96 + len(key)*2 + len(value)*2)
	}
	return bytes
}

func buildWorkspaceDocumentArtifactManifest(parsed *core.ParsedDocument, snapshot *fileAnalysisSnapshot, parserSettingsFingerprint string) *workspaceDocumentArtifactManifest {
	if parsed == nil {
		return nil
	}
	if snapshot == nil {
		snapshot = buildWorkspaceArtifactSnapshot(parsed)
	}
	manifest := &workspaceDocumentArtifactManifest{
		DocumentID:        workspaceDocumentIDFromURI(parsed.URI),
		URI:               parsed.URI,
		SourceFingerprint: workspaceFingerprint(parsed.Text),
		ParserSettingsFingerprint: workspaceFingerprint(struct {
			Settings        string
			DefaultLanguage core.EmbeddedLanguage
		}{parserSettingsFingerprint, parsed.DefaultLanguage}),
		CST:                      cloneWorkspaceArtifactCST(parsed),
		VBExports:                workspaceTopLevelVBExportSummaries(snapshot.Summary.VBScript.Exports),
		PublicSymbols:            workspacePublicSymbolFingerprints(snapshot.Summary.VBScript.PublicSymbols),
		ExternalUsages:           workspaceExternalUsageFingerprints(snapshot.Summary.VBScript.ExternalRefUsages),
		ImplicitGlobalCandidates: workspaceNameFingerprints(snapshot.Summary.VBScript.ImplicitGlobalCandidateNames),
		ObjectTagVariables:       workspaceObjectTagFingerprints(parsed),
		References:               workspaceReferenceFingerprints(snapshot.ReferenceShard),
		VirtualDocuments:         workspaceVirtualDocumentFingerprints(snapshot.VirtualDocuments),
		LocalDiagnostics:         map[string]workspaceArtifactFingerprint{},
	}
	manifest.IncludeEdges = workspaceIncludeArtifactEdges(parsed, snapshot.Includes)
	manifest.ExecutionTape = workspaceExecutionTape(parsed, snapshot.Usage.Declarations, manifest.IncludeEdges)
	manifest.VBExportsFingerprint = workspaceFingerprint(manifest.VBExports)
	manifest.VBExportsCacheSourceHash = workspaceVBExportsCacheSourceHash(manifest.SourceFingerprint, string(parsed.DefaultLanguage))
	manifest.IncludeFingerprint = workspaceFingerprint(workspaceIncludeTopologyPayload(manifest.IncludeEdges))
	manifest.ExecutionTapeFingerprint = workspaceFingerprint(workspaceExecutionOrderPayload(manifest.ExecutionTape))
	return manifest
}

func buildWorkspaceArtifactSnapshot(parsed *core.ParsedDocument) *fileAnalysisSnapshot {
	return buildWorkspaceArtifactSnapshotContext(context.Background(), parsed)
}

func (s *Server) buildDocumentOpenWorkspaceArtifactSnapshot(parsed *core.ParsedDocument) *fileAnalysisSnapshot {
	return s.buildDocumentOpenWorkspaceArtifactSnapshotContext(context.Background(), parsed)
}

func (s *Server) buildDocumentOpenWorkspaceArtifactSnapshotContext(ctx context.Context, parsed *core.ParsedDocument) *fileAnalysisSnapshot {
	return workspaceArtifactSnapshotAsFileAnalysisSnapshot(s.buildDocumentOpenWorkspaceArtifactSnapshotReducedContext(ctx, parsed))
}

func (s *Server) buildDocumentOpenWorkspaceArtifactSnapshotReducedContext(ctx context.Context, parsed *core.ParsedDocument) *workspaceArtifactSnapshot {
	if parsed == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return nil
	}
	includeResolution := s.includeResolutionFingerprint(parsed)
	if snapshot := s.cachedWorkspaceArtifactSnapshot(parsed, includeResolution); snapshot != nil {
		return snapshot
	}
	s.mu.Lock()
	testHook := s.fileAnalysisSnapshotTestHook
	s.mu.Unlock()
	if testHook != nil {
		testHook()
	}
	snapshot := buildWorkspaceArtifactSnapshotReducedContext(ctx, parsed)
	if snapshot == nil {
		return nil
	}
	snapshot.IncludeResolutionFingerprint = includeResolution
	for _, include := range parsed.Includes {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		resolved := resolvedIncludeSnapshot{Path: include.Path, Mode: include.Mode, Range: include.Range}
		if details, ok := s.includeTargetDetailsForMode(parsed.URI, include.Path, include.Mode); ok {
			resolved.ResolvedPath = details.Path
			resolved.Exists = details.Exists
			if details.Path != "" {
				resolved.URI = filePathURI(details.Path)
			}
		}
		snapshot.Includes = append(snapshot.Includes, resolved)
	}
	if ctx != nil && ctx.Err() != nil {
		return nil
	}
	s.rememberWorkspaceArtifactSnapshot(parsed, includeResolution, snapshot)
	return snapshot
}

func buildWorkspaceArtifactSnapshotContext(ctx context.Context, parsed *core.ParsedDocument) *fileAnalysisSnapshot {
	return workspaceArtifactSnapshotAsFileAnalysisSnapshot(buildWorkspaceArtifactSnapshotReducedContext(ctx, parsed))
}

func buildWorkspaceArtifactSnapshotReducedContext(ctx context.Context, parsed *core.ParsedDocument) *workspaceArtifactSnapshot {
	if parsed == nil || ctx != nil && ctx.Err() != nil {
		return nil
	}
	referenceShard := vbscript.BuildReferenceShard(parsed)
	defer referenceShard.ReleaseDocumentCST()
	if ctx != nil && ctx.Err() != nil {
		return nil
	}
	usage := collectVBUsageDeclarations(parsed)
	if ctx != nil && ctx.Err() != nil {
		return nil
	}
	summary := summarizeVBScriptFileAnalysis(parsed)
	virtualDocuments := make(map[core.EmbeddedLanguage]core.VirtualDocument, 5)
	for _, language := range []core.EmbeddedLanguage{core.LanguageHTML, core.LanguageCSS, core.LanguageJavaScript, core.LanguageVBScript, core.LanguageJScript} {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		virtualDocuments[language] = core.BuildVirtualDocument(parsed, language)
	}
	return &workspaceArtifactSnapshot{
		URI:              parsed.URI,
		ReferenceShard:   referenceShard,
		Usage:            usage,
		Summary:          summary,
		VirtualDocuments: virtualDocuments,
	}
}

func workspaceArtifactSnapshotAsFileAnalysisSnapshot(snapshot *workspaceArtifactSnapshot) *fileAnalysisSnapshot {
	if snapshot == nil {
		return nil
	}
	return &fileAnalysisSnapshot{
		URI:                          snapshot.URI,
		ReferenceShard:               snapshot.ReferenceShard,
		Usage:                        snapshot.Usage,
		Summary:                      snapshot.Summary,
		Includes:                     snapshot.Includes,
		IncludeResolutionFingerprint: snapshot.IncludeResolutionFingerprint,
		VirtualDocuments:             snapshot.VirtualDocuments,
		runtimeBacked:                true,
	}
}

func estimateWorkspaceArtifactSnapshotBytes(snapshot *workspaceArtifactSnapshot) int64 {
	if snapshot == nil {
		return 0
	}
	bytes := int64(len(snapshot.URI)+len(snapshot.IncludeResolutionFingerprint))*2 + 256
	bytes += estimateAnalysisDeclarationsBytes(nil, snapshot.Usage.Declarations)
	bytes += int64(cap(snapshot.Usage.Declarations)-len(snapshot.Usage.Declarations)) * 160
	bytes += estimateVBFileAnalysisSummaryBytes(snapshot.Summary)
	bytes += estimateResolvedIncludeSnapshotBytes(snapshot.Includes)
	bytes += snapshot.ReferenceShard.EstimateBytes()
	for language, virtual := range snapshot.VirtualDocuments {
		bytes += int64(len(language)+len(virtual.URI)+len(virtual.LanguageID)+len(virtual.Text))*2 + int64(cap(virtual.Segments))*48 + 96
	}
	return bytes
}

func estimateVBFileAnalysisSummaryBytes(summary vbFileAnalysisSummary) int64 {
	bytes := int64(128) + int64(len(summary.Fingerprint)+len(summary.PublicSignatureHash))*2
	local := summary.VBScript
	bytes += int64(96) + int64(len(local.Fingerprint))*2
	bytes += int64(cap(local.PublicSymbols)) * 160
	for _, symbol := range local.PublicSymbols {
		bytes += int64(len(symbol.Name)+len(symbol.Kind)+len(symbol.TypeName)+len(symbol.MemberOf)+len(symbol.Visibility)) * 2
	}
	bytes += int64(cap(local.Exports)-len(local.Exports)) * 160
	for _, export := range local.Exports {
		bytes += estimateVBExportSummaryBytes(export)
	}
	bytes += int64(cap(local.ExternalRefs)) * 128
	for _, reference := range local.ExternalRefs {
		bytes += int64(len(reference.Name)+len(reference.KindHint)+len(reference.MemberName)) * 2
	}
	bytes += int64(cap(local.ExternalRefUsages)) * 128
	for _, usage := range local.ExternalRefUsages {
		bytes += int64(len(usage.Key))*2 + int64(cap(usage.Ranges))*32
	}
	bytes += int64(cap(local.ImplicitGlobalCandidateNames)) * 32
	for _, name := range local.ImplicitGlobalCandidateNames {
		bytes += int64(len(name)) * 2
	}
	return bytes
}

func estimateResolvedIncludeSnapshotBytes(includes []resolvedIncludeSnapshot) int64 {
	bytes := int64(cap(includes)-len(includes)) * 160
	for _, include := range includes {
		bytes += int64(160) + int64(len(include.Path)+len(include.Mode)+len(include.ResolvedPath)+len(include.URI))*2
	}
	return bytes
}

func workspaceDocumentArtifactManifestWithDiagnostics(manifest *workspaceDocumentArtifactManifest, layers map[string][]lsp.Diagnostic) *workspaceDocumentArtifactManifest {
	if manifest == nil {
		return nil
	}
	clone := *manifest
	clone.LocalDiagnostics = make(map[string]workspaceArtifactFingerprint, len(layers))
	for layer, diagnostics := range layers {
		clone.LocalDiagnostics[layer] = workspaceFingerprint(diagnostics)
	}
	return &clone
}

func compareWorkspaceDocumentArtifacts(previous, current *workspaceDocumentArtifactManifest) workspaceDocumentArtifactDelta {
	if previous == nil && current == nil {
		return workspaceDocumentArtifactDelta{}
	}
	if previous == nil {
		return workspaceAllArtifactDelta(current)
	}
	if current == nil {
		return workspaceAllArtifactDelta(previous)
	}
	delta := workspaceDocumentArtifactDelta{
		SourceChanged:         previous.SourceFingerprint != current.SourceFingerprint,
		ParserSettingsChanged: previous.ParserSettingsFingerprint != current.ParserSettingsFingerprint,
		IncludeEdgesChanged:   previous.IncludeFingerprint != current.IncludeFingerprint,
		ExecutionTapeChanged:  previous.ExecutionTapeFingerprint != current.ExecutionTapeFingerprint,
		VBExportsChanged: previous.VBExportsFingerprint != current.VBExportsFingerprint ||
			previous.VBExportsCacheSourceHash != current.VBExportsCacheSourceHash,
	}
	delta.CSTChanged = delta.SourceChanged || delta.ParserSettingsChanged
	delta.ChangedPublicNames = workspaceChangedFingerprintKeys(previous.PublicSymbols, current.PublicSymbols)
	delta.ChangedUsageNames = workspaceChangedFingerprintKeys(previous.ExternalUsages, current.ExternalUsages)
	delta.ChangedImplicitNames = workspaceChangedFingerprintKeys(previous.ImplicitGlobalCandidates, current.ImplicitGlobalCandidates)
	delta.ChangedObjectTagNames = workspaceChangedFingerprintKeys(previous.ObjectTagVariables, current.ObjectTagVariables)
	delta.ChangedReferenceCounts, delta.ChangedReferenceLocations = workspaceChangedReferences(previous.References, current.References)
	delta.ChangedVirtualLanguages = workspaceChangedVirtualLanguages(previous.VirtualDocuments, current.VirtualDocuments)
	delta.ChangedDiagnosticLayers = workspaceChangedFingerprintKeys(previous.LocalDiagnostics, current.LocalDiagnostics)
	return delta
}

func workspaceAllArtifactDelta(manifest *workspaceDocumentArtifactManifest) workspaceDocumentArtifactDelta {
	if manifest == nil {
		return workspaceDocumentArtifactDelta{}
	}
	counts := workspaceSortedKeys(manifest.References)
	return workspaceDocumentArtifactDelta{
		SourceChanged:             true,
		ParserSettingsChanged:     true,
		CSTChanged:                true,
		IncludeEdgesChanged:       len(manifest.IncludeEdges) > 0,
		ExecutionTapeChanged:      len(manifest.ExecutionTape) > 0,
		VBExportsChanged:          true,
		ChangedPublicNames:        workspaceSortedKeys(manifest.PublicSymbols),
		ChangedUsageNames:         workspaceSortedKeys(manifest.ExternalUsages),
		ChangedImplicitNames:      workspaceSortedKeys(manifest.ImplicitGlobalCandidates),
		ChangedObjectTagNames:     workspaceSortedKeys(manifest.ObjectTagVariables),
		ChangedReferenceCounts:    counts,
		ChangedReferenceLocations: append([]string(nil), counts...),
		ChangedVirtualLanguages:   workspaceSortedLanguages(manifest.VirtualDocuments),
		ChangedDiagnosticLayers:   workspaceSortedKeys(manifest.LocalDiagnostics),
	}
}

func estimateVBExportSummaryBytes(summary vbExportSummary) int64 {
	bytes := int64(160 + len(summary.Name)*2 + len(summary.Kind)*2 + len(summary.TypeName)*2 +
		len(summary.MemberOf)*2 + len(summary.Visibility)*2)
	bytes += int64(cap(summary.Members)-len(summary.Members)) * 160
	for _, member := range summary.Members {
		bytes += estimateVBExportSummaryBytes(member)
	}
	return bytes
}

func workspaceIncludeArtifactEdges(parsed *core.ParsedDocument, resolved []resolvedIncludeSnapshot) []workspaceIncludeArtifactEdge {
	byRange := make(map[string]resolvedIncludeSnapshot, len(resolved))
	for _, include := range resolved {
		byRange[workspaceRangeKey(include.Range)] = include
	}
	edges := make([]workspaceIncludeArtifactEdge, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		edge := workspaceIncludeArtifactEdge{Path: include.Path, Mode: strings.ToLower(include.Mode), Range: include.Range}
		if target, ok := byRange[workspaceRangeKey(include.Range)]; ok {
			edge.Exists = target.Exists
			if target.URI != "" {
				edge.DocumentID = workspaceDocumentIDFromURI(target.URI)
			} else if target.ResolvedPath != "" {
				edge.DocumentID = workspaceDocumentID(workspacepkg.FileIdentityKeyFromFileName(target.ResolvedPath))
			}
		}
		edges = append(edges, edge)
	}
	return edges
}

func workspaceExecutionTape(parsed *core.ParsedDocument, declarations []vbUsageDeclaration, edges []workspaceIncludeArtifactEdge) []workspaceDocumentExecutionEvent {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	events := make([]workspaceDocumentExecutionEvent, 0, len(edges)+len(declarations))
	for _, edge := range edges {
		events = append(events, workspaceDocumentExecutionEvent{Kind: workspaceExecutionInclude, Offset: doc.OffsetAt(edge.Range.Start), Include: edge, Range: edge.Range})
	}
	for _, declaration := range declarations {
		if !declaration.Implicit {
			continue
		}
		events = append(events, workspaceDocumentExecutionEvent{
			Kind: workspaceExecutionImplicitDeclaration, Offset: declaration.Start,
			Name: strings.ToLower(declaration.Name), SymbolKind: declaration.Kind, Range: declaration.Range,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Offset != events[j].Offset {
			return events[i].Offset < events[j].Offset
		}
		if events[i].Kind != events[j].Kind {
			return events[i].Kind == workspaceExecutionInclude
		}
		return workspaceExecutionEventKey(events[i]) < workspaceExecutionEventKey(events[j])
	})
	return events
}

func workspaceExecutionEventKey(event workspaceDocumentExecutionEvent) string {
	if event.Kind == workspaceExecutionInclude {
		return string(event.Include.DocumentID) + "\x00" + strings.ToLower(event.Include.Mode) + "\x00" + event.Include.Path
	}
	return event.Name + "\x00" + event.SymbolKind
}

func workspaceIncludeTopologyPayload(edges []workspaceIncludeArtifactEdge) []string {
	payload := make([]string, len(edges))
	for index, edge := range edges {
		payload[index] = strings.ToLower(edge.Mode) + "\x00" + string(edge.DocumentID) + "\x00" + edge.Path + "\x00" + strconv.FormatBool(edge.Exists)
	}
	return payload
}

func workspaceExecutionOrderPayload(events []workspaceDocumentExecutionEvent) []string {
	payload := make([]string, len(events))
	for index, event := range events {
		payload[index] = string(event.Kind) + "\x00" + workspaceExecutionEventKey(event)
	}
	return payload
}

func workspacePublicSymbolFingerprints(symbols []vbPublicSummarySymbol) map[string]workspaceArtifactFingerprint {
	groups := map[string][]vbPublicSummarySymbol{}
	for _, symbol := range symbols {
		key := strings.ToLower(symbol.Name)
		groups[key] = append(groups[key], symbol)
	}
	return workspaceFingerprintGroups(groups)
}

func workspaceExternalUsageFingerprints(usages []vbExternalRefUsage) map[string]workspaceArtifactFingerprint {
	result := make(map[string]workspaceArtifactFingerprint, len(usages))
	for _, usage := range usages {
		result[strings.ToLower(usage.Key)] = workspaceFingerprint(usage)
	}
	return result
}

func workspaceNameFingerprints(names []string) map[string]workspaceArtifactFingerprint {
	result := make(map[string]workspaceArtifactFingerprint, len(names))
	for _, name := range names {
		key := strings.ToLower(name)
		result[key] = workspaceFingerprint(key)
	}
	return result
}

func workspaceObjectTagFingerprints(parsed *core.ParsedDocument) map[string]workspaceArtifactFingerprint {
	groups := map[string][]vbUsageDeclaration{}
	for _, declaration := range serverObjectDeclarations(parsed) {
		key := strings.ToLower(declaration.Name)
		groups[key] = append(groups[key], declaration)
	}
	return workspaceFingerprintGroups(groups)
}

func workspaceReferenceFingerprints(shard *vbscript.ReferenceShard) map[string]workspaceReferenceArtifactSegment {
	result := map[string]workspaceReferenceArtifactSegment{}
	if shard == nil {
		return result
	}
	for _, name := range shard.NormalizedNames() {
		summary, ok := shard.SummaryFor(name)
		if !ok {
			continue
		}
		result[name] = workspaceReferenceArtifactSegment{
			CountFingerprint:    workspaceArtifactFingerprint(summary.CountFingerprint),
			LocationFingerprint: workspaceArtifactFingerprint(summary.LocationFingerprint),
		}
	}
	return result
}

func workspaceVirtualDocumentFingerprints(documents map[core.EmbeddedLanguage]core.VirtualDocument) map[core.EmbeddedLanguage]workspaceArtifactFingerprint {
	result := make(map[core.EmbeddedLanguage]workspaceArtifactFingerprint, len(documents))
	for language, document := range documents {
		result[language] = workspaceFingerprint(struct {
			LanguageID string
			Text       string
			Segments   []core.SourceMapSegment
		}{document.LanguageID, document.Text, document.Segments})
	}
	return result
}

func workspaceChangedReferences(previous, current map[string]workspaceReferenceArtifactSegment) ([]string, []string) {
	names := workspaceKeyUnion(previous, current)
	counts := make([]string, 0)
	locations := make([]string, 0)
	for _, name := range names {
		before, beforeOK := previous[name]
		after, afterOK := current[name]
		if !beforeOK || !afterOK || before.CountFingerprint != after.CountFingerprint {
			counts = append(counts, name)
		}
		if !beforeOK || !afterOK || before.LocationFingerprint != after.LocationFingerprint {
			locations = append(locations, name)
		}
	}
	return counts, locations
}

func workspaceChangedVirtualLanguages(previous, current map[core.EmbeddedLanguage]workspaceArtifactFingerprint) []core.EmbeddedLanguage {
	keys := map[core.EmbeddedLanguage]struct{}{}
	for key := range previous {
		keys[key] = struct{}{}
	}
	for key := range current {
		keys[key] = struct{}{}
	}
	changed := make([]core.EmbeddedLanguage, 0)
	for key := range keys {
		if previous[key] != current[key] {
			changed = append(changed, key)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i] < changed[j] })
	return changed
}

func workspaceChangedFingerprintKeys(previous, current map[string]workspaceArtifactFingerprint) []string {
	keys := workspaceKeyUnion(previous, current)
	changed := make([]string, 0)
	for _, key := range keys {
		if previous[key] != current[key] {
			changed = append(changed, key)
		}
	}
	return changed
}

func workspaceKeyUnion[T any](left, right map[string]T) []string {
	keys := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		keys[key] = struct{}{}
	}
	for key := range right {
		keys[key] = struct{}{}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func workspaceSortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func workspaceSortedLanguages(values map[core.EmbeddedLanguage]workspaceArtifactFingerprint) []core.EmbeddedLanguage {
	keys := make([]core.EmbeddedLanguage, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func workspaceFingerprintGroups[T any](groups map[string][]T) map[string]workspaceArtifactFingerprint {
	result := make(map[string]workspaceArtifactFingerprint, len(groups))
	for key, values := range groups {
		result[key] = workspaceFingerprint(values)
	}
	return result
}

func workspaceFingerprint(value any) workspaceArtifactFingerprint {
	var payload []byte
	switch value := value.(type) {
	case string:
		payload = []byte(value)
	case []byte:
		payload = value
	default:
		payload, _ = json.Marshal(value)
	}
	sum := sha256.Sum256(payload)
	return workspaceArtifactFingerprint(hex.EncodeToString(sum[:]))
}

func workspaceRangeKey(value lsp.Range) string {
	return strconv.Itoa(value.Start.Line) + ":" + strconv.Itoa(value.Start.Character) + ":" + strconv.Itoa(value.End.Line) + ":" + strconv.Itoa(value.End.Character)
}

func cloneWorkspaceArtifactCST(parsed *core.ParsedDocument) *core.ParsedDocument {
	return parsed.CloneStructural()
}
