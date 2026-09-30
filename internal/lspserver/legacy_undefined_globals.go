package lspserver

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const legacyUndefinedGlobalCatalogSchemaVersion = 1

// LegacyUndefinedGlobalKind describes the conservative role inferred for an
// undeclared identifier when legacy workspace-global compatibility is enabled.
type LegacyUndefinedGlobalKind string

const (
	LegacyUndefinedGlobalVariableOrConstant LegacyUndefinedGlobalKind = "variableOrConstant"
	LegacyUndefinedGlobalFunction           LegacyUndefinedGlobalKind = "function"
	LegacyUndefinedGlobalClass              LegacyUndefinedGlobalKind = "class"
)

// LegacyUndefinedGlobalSymbol is one legacy workspace-global symbol and every
// undeclared VBScript occurrence that contributed to it.
type LegacyUndefinedGlobalSymbol struct {
	Name      string                    `json:"name"`
	Kind      LegacyUndefinedGlobalKind `json:"kind"`
	OriginURI string                    `json:"originUri"`
	Range     lsp.Range                 `json:"range"`
	Locations []lsp.Location            `json:"locations"`
}

// LegacyUndefinedGlobalCatalog is the fingerprinted workspace-wide legacy
// symbol catalog used by feature-specific adapters.
type LegacyUndefinedGlobalCatalog struct {
	SchemaVersion         int                           `json:"schemaVersion"`
	SourceFingerprint     string                        `json:"sourceFingerprint"`
	SettingsFingerprint   string                        `json:"settingsFingerprint"`
	Symbols               []LegacyUndefinedGlobalSymbol `json:"symbols"`
	RestoredFromDatabase  bool                          `json:"-"`
	symbolByName          map[string]int
	symbolsByURI          map[string][]legacyUndefinedGlobalLocationIndex
	configuredGlobalCount int
	incrementalIndex      *legacyUndefinedGlobalIndex
}

type legacyUndefinedGlobalDocumentSnapshot struct {
	DocumentID        workspaceDocumentID
	URI               string
	SourceFingerprint workspaceArtifactFingerprint
	Facts             legacyUndefinedGlobalDocumentFacts
}

// legacyUndefinedGlobalIndex is immutable after publication. Document updates
// copy only the top-level maps and the affected name buckets.
type legacyUndefinedGlobalIndex struct {
	documents         map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot
	globalsByName     map[string]map[workspaceDocumentID]struct{}
	occurrencesByName map[string]map[workspaceDocumentID][]legacyUndefinedGlobalOccurrence
	configuredNames   map[string]struct{}
	symbolsByName     map[string]LegacyUndefinedGlobalSymbol
}

type legacyUndefinedGlobalLocationIndex struct {
	symbolIndex int
	rangeValue  lsp.Range
}

type legacyUndefinedGlobalDocumentFacts struct {
	GlobalNames []string                          `json:"globalNames"`
	LocalNames  map[string][]string               `json:"localNames"`
	Occurrences []legacyUndefinedGlobalOccurrence `json:"occurrences"`
}

type legacyUndefinedGlobalOccurrence struct {
	Name         string                    `json:"name"`
	KindEvidence LegacyUndefinedGlobalKind `json:"kindEvidence"`
	Scope        string                    `json:"scope,omitempty"`
	Range        lsp.Range                 `json:"range"`
}

type legacyUndefinedGlobalBuildKey struct {
	generation          uint64
	settingsFingerprint string
}

type legacyUndefinedGlobalBuild struct {
	done    chan struct{}
	catalog LegacyUndefinedGlobalCatalog
	ok      bool
}

// WorkspaceLegacyUndefinedGlobals returns the fingerprinted compatibility
// catalog. It is inactive unless vbscript.assumeUndefinedGlobals is enabled.
func (s *Server) WorkspaceLegacyUndefinedGlobals(ctx context.Context) (LegacyUndefinedGlobalCatalog, bool) {
	catalog, ok := s.workspaceLegacyUndefinedGlobals(ctx)
	if !ok {
		return LegacyUndefinedGlobalCatalog{}, false
	}
	return cloneLegacyUndefinedGlobalCatalog(catalog), true
}

func (s *Server) workspaceLegacyUndefinedGlobals(ctx context.Context) (LegacyUndefinedGlobalCatalog, bool) {
	if ctx.Err() != nil {
		return LegacyUndefinedGlobalCatalog{}, false
	}
	s.mu.Lock()
	enabled := s.settings.VBScriptAssumeUndefinedGlobals
	generation := s.graphGeneration
	configuredGlobalCount := len(s.settings.VBScriptGlobals)
	if cached := s.legacyUndefinedGlobalCatalog; enabled && configuredGlobalCount == 0 &&
		cached != nil && cached.configuredGlobalCount == 0 &&
		s.legacyUndefinedGlobalCatalogGeneration == generation {
		catalog := *cached
		s.mu.Unlock()
		return catalog, true
	}
	configuredGlobals := cloneVBScriptGlobals(s.settings.VBScriptGlobals)
	s.mu.Unlock()
	if !enabled {
		return LegacyUndefinedGlobalCatalog{}, false
	}
	settingsFingerprint := s.legacyUndefinedGlobalSettingsFingerprint(configuredGlobals)
	s.mu.Lock()
	previousCatalog := s.legacyUndefinedGlobalCatalog
	if cached := s.legacyUndefinedGlobalCatalog; cached != nil &&
		s.legacyUndefinedGlobalCatalogGeneration == generation &&
		s.legacyUndefinedGlobalCatalogSettingsFingerprint == settingsFingerprint {
		catalog := *cached
		s.mu.Unlock()
		return catalog, true
	}
	buildKey := legacyUndefinedGlobalBuildKey{generation: generation, settingsFingerprint: settingsFingerprint}
	if s.legacyUndefinedGlobalBuilds == nil {
		s.legacyUndefinedGlobalBuilds = map[legacyUndefinedGlobalBuildKey]*legacyUndefinedGlobalBuild{}
	}
	if build := s.legacyUndefinedGlobalBuilds[buildKey]; build != nil {
		done := build.done
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return LegacyUndefinedGlobalCatalog{}, false
		case <-done:
			return build.catalog, build.ok
		}
	}
	build := &legacyUndefinedGlobalBuild{done: make(chan struct{})}
	s.legacyUndefinedGlobalBuilds[buildKey] = build
	s.mu.Unlock()

	taskID, _ := s.beginProgressTask(
		"legacy.undefinedGlobals", "analyzing", "legacyUndefinedGlobals",
		"legacyUndefinedGlobals.collectDocuments", "", s.workspaceGraphSourceCount(), false,
	)
	progressState := "cancelled"
	defer func() {
		s.finishProgressTask(taskID, "legacyUndefinedGlobals", progressState)
		s.mu.Lock()
		delete(s.legacyUndefinedGlobalBuilds, buildKey)
		close(build.done)
		s.mu.Unlock()
	}()
	configuredNames := legacyUndefinedGlobalConfiguredNames(configuredGlobals)
	if previousCatalog != nil && previousCatalog.incrementalIndex != nil {
		if snapshots, complete := s.legacyUndefinedGlobalArtifactSnapshots(previousCatalog.incrementalIndex); complete {
			catalog := updateLegacyUndefinedGlobalCatalog(
				ctx, previousCatalog.incrementalIndex, snapshots, settingsFingerprint, configuredNames,
			)
			if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
				return LegacyUndefinedGlobalCatalog{}, false
			}
			catalog.configuredGlobalCount = len(configuredGlobals)
			s.rememberWorkspaceLegacyUndefinedGlobals(catalog, generation, settingsFingerprint)
			prepareLegacyUndefinedGlobalCatalog(&catalog)
			build.catalog, build.ok = catalog, true
			progressState = "completed"
			return build.catalog, true
		}
	}
	collection := s.workspaceGraphDocumentsContextWithProgressResult(ctx, true, func(_ string, detail string, current, total int) {
		s.updateLegacyUndefinedGlobalProgress(taskID, "legacyUndefinedGlobals.collectDocuments", detail, current, total)
	})
	if !collection.complete || ctx.Err() != nil || collection.generation != generation || !s.graphGenerationCurrent(ctx, generation) {
		return LegacyUndefinedGlobalCatalog{}, false
	}
	documents := dedupeParsedDocumentsByFileIdentity(collection.documents)
	sourceFingerprint := legacyUndefinedGlobalSourceFingerprint(documents, func(detail string, current, total int) {
		s.updateLegacyUndefinedGlobalProgress(taskID, "legacyUndefinedGlobals.fingerprintDocuments", detail, current, total)
	})
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		return LegacyUndefinedGlobalCatalog{}, false
	}
	s.updateLegacyUndefinedGlobalProgress(taskID, "legacyUndefinedGlobals.databaseRead", "lookup", 0, 1)
	if catalog, ok := s.restoreWorkspaceLegacyUndefinedGlobals(sourceFingerprint, settingsFingerprint); ok {
		s.updateLegacyUndefinedGlobalProgress(taskID, "legacyUndefinedGlobals.databaseRead", "hit", 1, 1)
		catalog.RestoredFromDatabase = true
		catalog.configuredGlobalCount = len(configuredGlobals)
		s.rememberWorkspaceLegacyUndefinedGlobals(catalog, generation, settingsFingerprint)
		prepareLegacyUndefinedGlobalCatalog(&catalog)
		build.catalog, build.ok = catalog, true
		progressState = "completed"
		return build.catalog, true
	}
	s.updateLegacyUndefinedGlobalProgress(taskID, "legacyUndefinedGlobals.databaseRead", "miss", 1, 1)
	catalog := collectLegacyUndefinedGlobalCatalog(
		ctx, documents, sourceFingerprint, settingsFingerprint,
		configuredNames,
		func(detail string, current, total int) {
			s.updateLegacyUndefinedGlobalProgress(taskID, "legacyUndefinedGlobals.scanDocuments", detail, current, total)
		},
	)
	catalog.configuredGlobalCount = len(configuredGlobals)
	if ctx.Err() != nil || !s.graphGenerationCurrent(ctx, generation) {
		return LegacyUndefinedGlobalCatalog{}, false
	}
	s.updateLegacyUndefinedGlobalProgress(taskID, "legacyUndefinedGlobals.databaseWrite", "catalog", 0, 1)
	s.persistWorkspaceLegacyUndefinedGlobals(catalog)
	s.updateLegacyUndefinedGlobalProgress(taskID, "legacyUndefinedGlobals.databaseWrite", "catalog", 1, 1)
	s.rememberWorkspaceLegacyUndefinedGlobals(catalog, generation, settingsFingerprint)
	prepareLegacyUndefinedGlobalCatalog(&catalog)
	build.catalog, build.ok = catalog, true
	progressState = "completed"
	return build.catalog, true
}

// workspaceLegacyUndefinedGlobalsInteractive never joins a workspace-wide
// catalog build already owned by background analysis. A later editor request
// observes the catalog after the owner publishes it.
func (s *Server) workspaceLegacyUndefinedGlobalsInteractive(ctx context.Context) (LegacyUndefinedGlobalCatalog, bool) {
	fastCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	catalog, ok := s.workspaceLegacyUndefinedGlobals(fastCtx)
	cancel()
	if ok || ctx.Err() != nil {
		return catalog, ok
	}
	s.scheduleLegacyUndefinedGlobals()
	return LegacyUndefinedGlobalCatalog{}, false
}

func (s *Server) scheduleLegacyUndefinedGlobals() bool {
	s.mu.Lock()
	if s.shutdown || !s.settings.VBScriptAssumeUndefinedGlobals || s.legacyUndefinedGlobalBackgroundCancel != nil {
		s.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.legacyUndefinedGlobalBackgroundCancel = cancel
	s.backgroundAnalysisWorkers.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.backgroundAnalysisWorkers.Done()
		defer func() {
			cancel()
			s.mu.Lock()
			s.legacyUndefinedGlobalBackgroundCancel = nil
			s.mu.Unlock()
		}()
		_, _ = s.workspaceLegacyUndefinedGlobals(ctx)
	}()
	return true
}

func (s *Server) workspaceLegacyUndefinedGlobalAtInteractive(ctx context.Context, uri string, position lsp.Position) (LegacyUndefinedGlobalSymbol, bool) {
	catalog, ok := s.workspaceLegacyUndefinedGlobalsInteractive(ctx)
	if !ok {
		return LegacyUndefinedGlobalSymbol{}, false
	}
	if entries, indexed := catalog.symbolsByURI[workspacepkg.FileIdentityKeyFromURI(uri)]; indexed {
		for _, entry := range entries {
			if lspPositionInRange(position, entry.rangeValue) {
				return cloneLegacyUndefinedGlobalSymbol(catalog.Symbols[entry.symbolIndex]), true
			}
		}
	}
	return LegacyUndefinedGlobalSymbol{}, false
}

// WorkspaceLegacyUndefinedGlobalNamed resolves a catalog symbol by its
// case-insensitive VBScript name.
func (s *Server) WorkspaceLegacyUndefinedGlobalNamed(ctx context.Context, name string) (LegacyUndefinedGlobalSymbol, bool) {
	catalog, ok := s.workspaceLegacyUndefinedGlobals(ctx)
	if !ok {
		return LegacyUndefinedGlobalSymbol{}, false
	}
	if index, found := catalog.symbolByName[strings.ToLower(name)]; found {
		return cloneLegacyUndefinedGlobalSymbol(catalog.Symbols[index]), true
	}
	for _, symbol := range catalog.Symbols {
		if strings.EqualFold(symbol.Name, name) {
			return cloneLegacyUndefinedGlobalSymbol(symbol), true
		}
	}
	return LegacyUndefinedGlobalSymbol{}, false
}

// WorkspaceLegacyUndefinedGlobalAt resolves only recorded VBScript
// occurrences; HTML text and attributes never participate in the catalog.
func (s *Server) WorkspaceLegacyUndefinedGlobalAt(ctx context.Context, uri string, position lsp.Position) (LegacyUndefinedGlobalSymbol, bool) {
	catalog, ok := s.workspaceLegacyUndefinedGlobals(ctx)
	if !ok {
		return LegacyUndefinedGlobalSymbol{}, false
	}
	if entries, indexed := catalog.symbolsByURI[workspacepkg.FileIdentityKeyFromURI(uri)]; indexed {
		for _, entry := range entries {
			if lspPositionInRange(position, entry.rangeValue) {
				return cloneLegacyUndefinedGlobalSymbol(catalog.Symbols[entry.symbolIndex]), true
			}
		}
		return LegacyUndefinedGlobalSymbol{}, false
	}
	for _, symbol := range catalog.Symbols {
		for _, location := range symbol.Locations {
			if workspacepkg.SameFileIdentityURI(location.URI, uri) && lspPositionInRange(position, location.Range) {
				return cloneLegacyUndefinedGlobalSymbol(symbol), true
			}
		}
	}
	return LegacyUndefinedGlobalSymbol{}, false
}

func collectLegacyUndefinedGlobalCatalog(
	ctx context.Context,
	documents []*core.ParsedDocument,
	sourceFingerprint, settingsFingerprint string,
	configuredGlobals map[string]struct{},
	report func(detail string, current, total int),
) LegacyUndefinedGlobalCatalog {
	snapshots := make(map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot, len(documents))
	for index, parsed := range documents {
		if ctx.Err() != nil || parsed == nil {
			continue
		}
		documentID := workspaceDocumentIDFromURI(parsed.URI)
		snapshots[documentID] = legacyUndefinedGlobalDocumentSnapshot{
			DocumentID: documentID, URI: parsed.URI,
			SourceFingerprint: workspaceFingerprint(parsed.Text),
			Facts:             legacyUndefinedGlobalFacts(parsed),
		}
		if report != nil {
			report(progressDetailForURI(parsed.URI), index+1, len(documents))
		}
	}
	index := buildLegacyUndefinedGlobalIndex(snapshots, configuredGlobals)
	return LegacyUndefinedGlobalCatalog{
		SchemaVersion:     legacyUndefinedGlobalCatalogSchemaVersion,
		SourceFingerprint: sourceFingerprint, SettingsFingerprint: settingsFingerprint,
		Symbols: legacyUndefinedGlobalIndexSymbols(index), incrementalIndex: index,
	}
}

func (s *Server) legacyUndefinedGlobalArtifactSnapshots(previous *legacyUndefinedGlobalIndex) (map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot, bool) {
	s.mu.Lock()
	if !s.workspaceIncludeGraphComplete || len(s.workspaceArtifacts) == 0 {
		s.mu.Unlock()
		return nil, false
	}
	manifests := make([]*workspaceDocumentArtifactManifest, 0, len(s.workspaceArtifacts))
	for _, manifest := range s.workspaceArtifacts {
		if manifest != nil && manifest.CST != nil {
			manifests = append(manifests, manifest)
		}
	}
	s.mu.Unlock()
	if len(manifests) == 0 {
		return nil, false
	}
	snapshots := make(map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot, len(manifests))
	for _, manifest := range manifests {
		if old, ok := previous.documents[manifest.DocumentID]; ok && old.SourceFingerprint == manifest.SourceFingerprint {
			snapshots[manifest.DocumentID] = old
			continue
		}
		snapshots[manifest.DocumentID] = legacyUndefinedGlobalDocumentSnapshot{
			DocumentID: manifest.DocumentID, URI: manifest.URI,
			SourceFingerprint: manifest.SourceFingerprint,
			Facts:             legacyUndefinedGlobalFacts(manifest.CST),
		}
	}
	return snapshots, true
}

func buildLegacyUndefinedGlobalIndex(
	documents map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot,
	configuredNames map[string]struct{},
) *legacyUndefinedGlobalIndex {
	index := &legacyUndefinedGlobalIndex{
		documents:         make(map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot, len(documents)),
		globalsByName:     map[string]map[workspaceDocumentID]struct{}{},
		occurrencesByName: map[string]map[workspaceDocumentID][]legacyUndefinedGlobalOccurrence{},
		configuredNames:   cloneLegacyUndefinedGlobalNameSet(configuredNames),
		symbolsByName:     map[string]LegacyUndefinedGlobalSymbol{},
	}
	affected := map[string]struct{}{}
	for documentID, snapshot := range documents {
		index.documents[documentID] = snapshot
		legacyUndefinedGlobalAddDocument(index, snapshot, affected)
	}
	for name := range configuredNames {
		affected[strings.ToLower(name)] = struct{}{}
	}
	for name := range affected {
		legacyUndefinedGlobalRecomputeName(index, name)
	}
	return index
}

func updateLegacyUndefinedGlobalCatalog(
	ctx context.Context,
	previous *legacyUndefinedGlobalIndex,
	documents map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot,
	settingsFingerprint string,
	configuredNames map[string]struct{},
) LegacyUndefinedGlobalCatalog {
	index := &legacyUndefinedGlobalIndex{
		documents:         make(map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot, len(documents)),
		globalsByName:     cloneLegacyUndefinedGlobalDocumentSets(previous.globalsByName),
		occurrencesByName: cloneLegacyUndefinedGlobalOccurrenceBuckets(previous.occurrencesByName),
		configuredNames:   cloneLegacyUndefinedGlobalNameSet(configuredNames),
		symbolsByName:     cloneLegacyUndefinedGlobalSymbols(previous.symbolsByName),
	}
	for documentID, snapshot := range previous.documents {
		index.documents[documentID] = snapshot
	}
	affected := map[string]struct{}{}
	for documentID, old := range previous.documents {
		current, exists := documents[documentID]
		if exists && current.SourceFingerprint == old.SourceFingerprint && current.URI == old.URI {
			continue
		}
		legacyUndefinedGlobalRemoveDocument(index, old, affected)
		delete(index.documents, documentID)
	}
	for documentID, current := range documents {
		if ctx.Err() != nil {
			return LegacyUndefinedGlobalCatalog{}
		}
		old, exists := previous.documents[documentID]
		if exists && current.SourceFingerprint == old.SourceFingerprint && current.URI == old.URI {
			continue
		}
		index.documents[documentID] = current
		legacyUndefinedGlobalAddDocument(index, current, affected)
	}
	for name := range previous.configuredNames {
		if _, ok := configuredNames[name]; !ok {
			affected[name] = struct{}{}
		}
	}
	for name := range configuredNames {
		if _, ok := previous.configuredNames[name]; !ok {
			affected[name] = struct{}{}
		}
	}
	for name := range affected {
		legacyUndefinedGlobalRecomputeName(index, name)
	}
	return LegacyUndefinedGlobalCatalog{
		SchemaVersion:       legacyUndefinedGlobalCatalogSchemaVersion,
		SourceFingerprint:   legacyUndefinedGlobalSnapshotFingerprint(documents),
		SettingsFingerprint: settingsFingerprint,
		Symbols:             legacyUndefinedGlobalIndexSymbols(index), incrementalIndex: index,
	}
}

func legacyUndefinedGlobalAddDocument(index *legacyUndefinedGlobalIndex, snapshot legacyUndefinedGlobalDocumentSnapshot, affected map[string]struct{}) {
	for _, rawName := range snapshot.Facts.GlobalNames {
		name := strings.ToLower(rawName)
		bucket := cloneLegacyUndefinedGlobalDocumentSet(index.globalsByName[name])
		bucket[snapshot.DocumentID] = struct{}{}
		index.globalsByName[name] = bucket
		affected[name] = struct{}{}
	}
	localNames := legacyUndefinedGlobalLocalNameSet(snapshot.Facts.LocalNames)
	byName := map[string][]legacyUndefinedGlobalOccurrence{}
	for _, occurrence := range snapshot.Facts.Occurrences {
		name := strings.ToLower(occurrence.Name)
		if _, local := localNames[legacyUndefinedGlobalLocalKey(occurrence.Scope, name)]; local {
			continue
		}
		byName[name] = append(byName[name], occurrence)
	}
	for name, occurrences := range byName {
		bucket := cloneLegacyUndefinedGlobalOccurrenceBucket(index.occurrencesByName[name])
		bucket[snapshot.DocumentID] = append([]legacyUndefinedGlobalOccurrence(nil), occurrences...)
		index.occurrencesByName[name] = bucket
		affected[name] = struct{}{}
	}
}

func legacyUndefinedGlobalRemoveDocument(index *legacyUndefinedGlobalIndex, snapshot legacyUndefinedGlobalDocumentSnapshot, affected map[string]struct{}) {
	for _, rawName := range snapshot.Facts.GlobalNames {
		name := strings.ToLower(rawName)
		bucket := cloneLegacyUndefinedGlobalDocumentSet(index.globalsByName[name])
		delete(bucket, snapshot.DocumentID)
		if len(bucket) == 0 {
			delete(index.globalsByName, name)
		} else {
			index.globalsByName[name] = bucket
		}
		affected[name] = struct{}{}
	}
	localNames := legacyUndefinedGlobalLocalNameSet(snapshot.Facts.LocalNames)
	seen := map[string]struct{}{}
	for _, occurrence := range snapshot.Facts.Occurrences {
		name := strings.ToLower(occurrence.Name)
		if _, local := localNames[legacyUndefinedGlobalLocalKey(occurrence.Scope, name)]; local {
			continue
		}
		seen[name] = struct{}{}
	}
	for name := range seen {
		bucket := cloneLegacyUndefinedGlobalOccurrenceBucket(index.occurrencesByName[name])
		delete(bucket, snapshot.DocumentID)
		if len(bucket) == 0 {
			delete(index.occurrencesByName, name)
		} else {
			index.occurrencesByName[name] = bucket
		}
		affected[name] = struct{}{}
	}
}

type legacyUndefinedGlobalLocatedOccurrence struct {
	uri        string
	occurrence legacyUndefinedGlobalOccurrence
}

func legacyUndefinedGlobalRecomputeName(index *legacyUndefinedGlobalIndex, name string) {
	name = strings.ToLower(name)
	if _, configured := index.configuredNames[name]; configured || len(index.globalsByName[name]) > 0 {
		delete(index.symbolsByName, name)
		return
	}
	var located []legacyUndefinedGlobalLocatedOccurrence
	for documentID, occurrences := range index.occurrencesByName[name] {
		snapshot, ok := index.documents[documentID]
		if !ok {
			continue
		}
		for _, occurrence := range occurrences {
			located = append(located, legacyUndefinedGlobalLocatedOccurrence{uri: snapshot.URI, occurrence: occurrence})
		}
	}
	if len(located) == 0 {
		delete(index.symbolsByName, name)
		return
	}
	sort.SliceStable(located, func(i, j int) bool {
		left, right := located[i], located[j]
		if left.uri != right.uri {
			return left.uri < right.uri
		}
		if left.occurrence.Range.Start.Line != right.occurrence.Range.Start.Line {
			return left.occurrence.Range.Start.Line < right.occurrence.Range.Start.Line
		}
		if left.occurrence.Range.Start.Character != right.occurrence.Range.Start.Character {
			return left.occurrence.Range.Start.Character < right.occurrence.Range.Start.Character
		}
		if left.occurrence.Range.End.Line != right.occurrence.Range.End.Line {
			return left.occurrence.Range.End.Line < right.occurrence.Range.End.Line
		}
		return left.occurrence.Range.End.Character < right.occurrence.Range.End.Character
	})
	first := located[0]
	symbol := LegacyUndefinedGlobalSymbol{Name: first.occurrence.Name, OriginURI: first.uri, Range: first.occurrence.Range}
	locations := map[string]struct{}{}
	classEvidence, callEvidence := false, false
	for _, item := range located {
		location := lsp.Location{URI: item.uri, Range: item.occurrence.Range}
		key := legacyUndefinedGlobalLocationKey(location)
		if _, duplicate := locations[key]; duplicate {
			continue
		}
		locations[key] = struct{}{}
		symbol.Locations = append(symbol.Locations, location)
		classEvidence = classEvidence || item.occurrence.KindEvidence == LegacyUndefinedGlobalClass
		callEvidence = callEvidence || item.occurrence.KindEvidence == LegacyUndefinedGlobalFunction
	}
	switch {
	case classEvidence && !callEvidence:
		symbol.Kind = LegacyUndefinedGlobalClass
	case callEvidence && !classEvidence:
		symbol.Kind = LegacyUndefinedGlobalFunction
	default:
		symbol.Kind = LegacyUndefinedGlobalVariableOrConstant
	}
	index.symbolsByName[name] = symbol
}

func legacyUndefinedGlobalIndexSymbols(index *legacyUndefinedGlobalIndex) []LegacyUndefinedGlobalSymbol {
	keys := make([]string, 0, len(index.symbolsByName))
	for key := range index.symbolsByName {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	symbols := make([]LegacyUndefinedGlobalSymbol, 0, len(keys))
	for _, key := range keys {
		symbols = append(symbols, index.symbolsByName[key])
	}
	return symbols
}

func legacyUndefinedGlobalSnapshotFingerprint(documents map[workspaceDocumentID]legacyUndefinedGlobalDocumentSnapshot) string {
	parts := make([]string, 0, len(documents))
	for documentID, snapshot := range documents {
		parts = append(parts, string(documentID)+"\x00"+string(snapshot.SourceFingerprint))
	}
	sort.Strings(parts)
	return workspacepkg.DiskContentHash(strings.Join(parts, "\x00"))
}

func cloneLegacyUndefinedGlobalNameSet(source map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(source))
	for name := range source {
		cloned[strings.ToLower(name)] = struct{}{}
	}
	return cloned
}

func cloneLegacyUndefinedGlobalDocumentSet(source map[workspaceDocumentID]struct{}) map[workspaceDocumentID]struct{} {
	cloned := make(map[workspaceDocumentID]struct{}, len(source)+1)
	for documentID := range source {
		cloned[documentID] = struct{}{}
	}
	return cloned
}

func cloneLegacyUndefinedGlobalDocumentSets(source map[string]map[workspaceDocumentID]struct{}) map[string]map[workspaceDocumentID]struct{} {
	cloned := make(map[string]map[workspaceDocumentID]struct{}, len(source))
	for name, bucket := range source {
		cloned[name] = bucket
	}
	return cloned
}

func cloneLegacyUndefinedGlobalOccurrenceBucket(source map[workspaceDocumentID][]legacyUndefinedGlobalOccurrence) map[workspaceDocumentID][]legacyUndefinedGlobalOccurrence {
	cloned := make(map[workspaceDocumentID][]legacyUndefinedGlobalOccurrence, len(source)+1)
	for documentID, occurrences := range source {
		cloned[documentID] = occurrences
	}
	return cloned
}

func cloneLegacyUndefinedGlobalOccurrenceBuckets(source map[string]map[workspaceDocumentID][]legacyUndefinedGlobalOccurrence) map[string]map[workspaceDocumentID][]legacyUndefinedGlobalOccurrence {
	cloned := make(map[string]map[workspaceDocumentID][]legacyUndefinedGlobalOccurrence, len(source))
	for name, bucket := range source {
		cloned[name] = bucket
	}
	return cloned
}

func cloneLegacyUndefinedGlobalSymbols(source map[string]LegacyUndefinedGlobalSymbol) map[string]LegacyUndefinedGlobalSymbol {
	cloned := make(map[string]LegacyUndefinedGlobalSymbol, len(source))
	for name, symbol := range source {
		cloned[name] = symbol
	}
	return cloned
}

func legacyUndefinedGlobalFacts(parsed *core.ParsedDocument) legacyUndefinedGlobalDocumentFacts {
	var cached legacyUndefinedGlobalDocumentFacts
	if parsed.LoadAnalysis("lspserver.legacy-undefined-global-facts.v2", &cached) {
		return cached
	}
	facts := legacyUndefinedGlobalDocumentFacts{LocalNames: map[string][]string{}}
	declarationOffsets := map[offsetRange]struct{}{}
	localDeclarationOffsets := map[offsetRange]struct{}{}
	declarations := collectVBUsageDeclarations(parsed).Declarations
	for _, declaration := range declarations {
		if declaration.Local {
			localDeclarationOffsets[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
		}
	}
	for _, declaration := range declarations {
		declarationOffsets[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
		lower := strings.ToLower(declaration.Name)
		if declaration.Local {
			scope := strings.ToLower(declaration.Scope)
			facts.LocalNames[scope] = append(facts.LocalNames[scope], lower)
			continue
		}
		if _, localDuplicate := localDeclarationOffsets[offsetRangeKey(declaration.Start, declaration.End)]; !localDuplicate {
			facts.GlobalNames = append(facts.GlobalNames, lower)
		}
	}
	for _, declaration := range serverObjectDeclarations(parsed) {
		facts.GlobalNames = append(facts.GlobalNames, strings.ToLower(declaration.Name))
		declarationOffsets[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
	}
	procedures := graphVBProcedureRanges(parsed)
	document := core.SourceDocument(parsed)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, span := range vbIdentifierSpans(text) {
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			if _, declaration := declarationOffsets[offsetRangeKey(start, end)]; declaration {
				continue
			}
			name := parsed.Text[start:end]
			lower := strings.ToLower(name)
			if isDeclaredVBBuiltinOrKeywordForDocument(parsed, lower) || previousNonSpace(parsed.Text, start) == '.' || legacyUndefinedGlobalNamedArgument(parsed.Text, end) {
				continue
			}
			position := document.PositionAt(start)
			facts.Occurrences = append(facts.Occurrences, legacyUndefinedGlobalOccurrence{
				Name: name, KindEvidence: legacyUndefinedGlobalKindEvidence(parsed.Text, start, end),
				Scope: legacyUndefinedGlobalScopeAt(procedures, position), Range: document.Range(start, end),
			})
		}
	}
	sort.Strings(facts.GlobalNames)
	for scope := range facts.LocalNames {
		sort.Strings(facts.LocalNames[scope])
	}
	parsed.StoreAnalysis("lspserver.legacy-undefined-global-facts.v2", facts)
	return facts
}

func legacyUndefinedGlobalScopeAt(procedures []graphVBProcedureRange, position lsp.Position) string {
	for _, procedure := range procedures {
		if position.Line >= procedure.startLine && position.Line <= procedure.endLine {
			return procedure.key()
		}
	}
	return ""
}

func legacyUndefinedGlobalKindEvidence(text string, start, end int) LegacyUndefinedGlobalKind {
	if strings.EqualFold(legacyUndefinedGlobalPreviousIdentifier(text, start), "new") {
		return LegacyUndefinedGlobalClass
	}
	if next := nextNonSpaceByte(text, end); next >= 0 && text[next] == '(' {
		return LegacyUndefinedGlobalFunction
	}
	if legacyUndefinedGlobalExplicitCall(text, start, end) {
		return LegacyUndefinedGlobalFunction
	}
	return LegacyUndefinedGlobalVariableOrConstant
}

func legacyUndefinedGlobalPreviousIdentifier(text string, start int) string {
	end := start
	for end > 0 && isVBWhitespace(text[end-1]) && text[end-1] != '\r' && text[end-1] != '\n' {
		end--
	}
	begin := end
	for begin > 0 && isVBIdentifier(text[begin-1]) {
		begin--
	}
	return text[begin:end]
}

func legacyUndefinedGlobalExplicitCall(text string, start, end int) bool {
	lineStart := strings.LastIndexAny(text[:start], "\r\n")
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}
	lineEnd := end
	for lineEnd < len(text) && text[lineEnd] != '\r' && text[lineEnd] != '\n' {
		lineEnd++
	}
	for _, statement := range splitVBStatementSegments(text[lineStart:lineEnd], lineStart) {
		if start < statement.Start || start >= statement.End {
			continue
		}
		name := text[start:end]
		statementText := statement.Text
		if strings.HasPrefix(strings.ToLower(statementText), "call ") {
			rest := strings.TrimSpace(statementText[len("call "):])
			identifierEnd := readVBIdentifier(rest, 0)
			return identifierEnd > 0 && strings.EqualFold(rest[:identifierEnd], name)
		}
		firstEnd := readVBIdentifier(statementText, 0)
		if firstEnd > 0 && strings.EqualFold(statementText[:firstEnd], name) && statement.Start == start {
			rest := strings.TrimSpace(statementText[firstEnd:])
			return rest != "" && !strings.HasPrefix(rest, "=")
		}
		return false
	}
	return false
}

func legacyUndefinedGlobalNamedArgument(text string, end int) bool {
	next := nextNonSpaceByte(text, end)
	if next < 0 || text[next] != ':' {
		return false
	}
	afterColon := nextNonSpaceByte(text, next+1)
	return afterColon >= 0 && text[afterColon] == '='
}

func legacyUndefinedGlobalLocalNameSet(names map[string][]string) map[string]struct{} {
	set := map[string]struct{}{}
	for scope, values := range names {
		for _, name := range values {
			set[legacyUndefinedGlobalLocalKey(scope, name)] = struct{}{}
		}
	}
	return set
}

func legacyUndefinedGlobalLocalKey(scope, name string) string {
	return strings.ToLower(scope) + "\x00" + strings.ToLower(name)
}

func legacyUndefinedGlobalLocationKey(location lsp.Location) string {
	return workspacepkg.FileIdentityKeyFromURI(location.URI) + "\x00" + diagnosticRangeKey(location.Range)
}

func cloneLegacyUndefinedGlobalCatalog(catalog LegacyUndefinedGlobalCatalog) LegacyUndefinedGlobalCatalog {
	cloned := catalog
	cloned.symbolByName = nil
	cloned.symbolsByURI = nil
	cloned.Symbols = append([]LegacyUndefinedGlobalSymbol(nil), catalog.Symbols...)
	for index := range cloned.Symbols {
		cloned.Symbols[index].Locations = append([]lsp.Location(nil), catalog.Symbols[index].Locations...)
	}
	return cloned
}

func cloneLegacyUndefinedGlobalSymbol(symbol LegacyUndefinedGlobalSymbol) LegacyUndefinedGlobalSymbol {
	symbol.Locations = append([]lsp.Location(nil), symbol.Locations...)
	return symbol
}

func prepareLegacyUndefinedGlobalCatalog(catalog *LegacyUndefinedGlobalCatalog) {
	if catalog == nil {
		return
	}
	catalog.symbolByName = make(map[string]int, len(catalog.Symbols))
	catalog.symbolsByURI = make(map[string][]legacyUndefinedGlobalLocationIndex)
	for symbolIndex, symbol := range catalog.Symbols {
		catalog.symbolByName[strings.ToLower(symbol.Name)] = symbolIndex
		for _, location := range symbol.Locations {
			key := workspacepkg.FileIdentityKeyFromURI(location.URI)
			catalog.symbolsByURI[key] = append(catalog.symbolsByURI[key], legacyUndefinedGlobalLocationIndex{
				symbolIndex: symbolIndex,
				rangeValue:  location.Range,
			})
		}
	}
}

func (s *Server) rememberWorkspaceLegacyUndefinedGlobals(catalog LegacyUndefinedGlobalCatalog, generation uint64, settingsFingerprint string) {
	cloned := cloneLegacyUndefinedGlobalCatalog(catalog)
	prepareLegacyUndefinedGlobalCatalog(&cloned)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.graphGeneration != generation || !s.settings.VBScriptAssumeUndefinedGlobals {
		return
	}
	s.legacyUndefinedGlobalCatalog = &cloned
	s.legacyUndefinedGlobalCatalogGeneration = generation
	s.legacyUndefinedGlobalCatalogSettingsFingerprint = settingsFingerprint
}

func legacyUndefinedGlobalSourceFingerprint(documents []*core.ParsedDocument, report func(detail string, current, total int)) string {
	parts := make([]string, 0, len(documents))
	for index, document := range documents {
		if document == nil {
			continue
		}
		parts = append(parts, workspacepkg.FileIdentityKeyFromURI(document.URI)+"\x00"+workspacepkg.DiskContentHash(document.Text))
		if report != nil {
			report(progressDetailForURI(document.URI), index+1, len(documents))
		}
	}
	sort.Strings(parts)
	return workspacepkg.DiskContentHash(strings.Join(parts, "\x00"))
}

func (s *Server) legacyUndefinedGlobalSettingsFingerprint(configuredGlobals map[string]vbscriptGlobalSetting) string {
	payload, _ := json.Marshal(configuredGlobals)
	configuredGlobalsFingerprint := workspacepkg.DiskContentHash(string(payload))
	return workspacepkg.DiskContentHash(s.workspaceDiskSettingsKey() + "\x00assumeUndefinedGlobals=true\x00schema=1\x00globals=" + configuredGlobalsFingerprint)
}

func legacyUndefinedGlobalConfiguredNames(configuredGlobals map[string]vbscriptGlobalSetting) map[string]struct{} {
	names := make(map[string]struct{}, len(configuredGlobals))
	for name := range configuredGlobals {
		name = strings.TrimSpace(name)
		if name != "" {
			names[strings.ToLower(name)] = struct{}{}
		}
	}
	return names
}

func (s *Server) updateLegacyUndefinedGlobalProgress(taskID, label, detail string, current, total int) {
	activeItems := []string(nil)
	if detail != "" {
		activeItems = []string{detail}
	}
	s.updateProgressTask(taskID, "legacyUndefinedGlobals", label, detail, current, total, activeItems, "running")
	s.mu.Lock()
	hook := s.legacyUndefinedGlobalProgressTestHook
	s.mu.Unlock()
	if hook != nil {
		hook(label, detail, current, total)
	}
}

func legacyUndefinedGlobalPersistentKey(sourceFingerprint, settingsFingerprint string) string {
	return workspacepkg.DiskContentHash("workspaceLegacyUndefinedGlobals\x00" + settingsFingerprint + "\x00" + sourceFingerprint)
}

func (s *Server) restoreWorkspaceLegacyUndefinedGlobals(sourceFingerprint, settingsFingerprint string) (LegacyUndefinedGlobalCatalog, bool) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		return LegacyUndefinedGlobalCatalog{}, false
	}
	settingsKey := legacyUndefinedGlobalPersistentKey(sourceFingerprint, settingsFingerprint)
	entry, ok := cache.ReadWorkspaceLegacyUndefinedGlobals(settingsKey)
	if !ok {
		s.logAnalysisDatabaseEvent("workspaceLegacyUndefinedGlobals", "miss", map[string]any{"settingsKey": shortLogKey(settingsKey)})
		return LegacyUndefinedGlobalCatalog{}, false
	}
	var catalog LegacyUndefinedGlobalCatalog
	if json.Unmarshal(entry.Payload, &catalog) != nil || catalog.SchemaVersion != legacyUndefinedGlobalCatalogSchemaVersion || catalog.SourceFingerprint != sourceFingerprint || catalog.SettingsFingerprint != settingsFingerprint {
		s.logAnalysisDatabaseEvent("workspaceLegacyUndefinedGlobals", "stale", map[string]any{"settingsKey": shortLogKey(settingsKey)})
		return LegacyUndefinedGlobalCatalog{}, false
	}
	s.logAnalysisDatabaseEvent("workspaceLegacyUndefinedGlobals", "hit", map[string]any{"settingsKey": shortLogKey(settingsKey), "symbols": len(catalog.Symbols)})
	return catalog, true
}

func (s *Server) persistWorkspaceLegacyUndefinedGlobals(catalog LegacyUndefinedGlobalCatalog) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		return
	}
	payload, err := json.Marshal(catalog)
	if err != nil {
		return
	}
	settingsKey := legacyUndefinedGlobalPersistentKey(catalog.SourceFingerprint, catalog.SettingsFingerprint)
	if err := cache.WriteWorkspaceLegacyUndefinedGlobals(workspacepkg.DiskWorkspaceLegacyUndefinedGlobalsCacheEntry{SettingsKey: settingsKey, Payload: payload}); err != nil {
		s.logServerWarning("[asp-lsp] database.workspaceLegacyUndefinedGlobals.write.failed: " + err.Error())
		return
	}
	s.logAnalysisDatabaseEvent("workspaceLegacyUndefinedGlobals", "write", map[string]any{"bytes": len(payload), "settingsKey": shortLogKey(settingsKey), "symbols": len(catalog.Symbols)})
}
