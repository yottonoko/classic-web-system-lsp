package lspserver

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const workspaceVBExportsArtifactKind = "vb-exports"

// WorkspaceVBAutoIncludeMember describes one public member of a top-level
// workspace VBScript export.
type WorkspaceVBAutoIncludeMember struct {
	Name       string
	Kind       string
	Range      lsp.Range
	TypeName   string
	Visibility string
	Members    []WorkspaceVBAutoIncludeMember
}

// WorkspaceVBAutoIncludeExport describes one explicit public top-level
// VBScript declaration that can be made visible by adding an include.
type WorkspaceVBAutoIncludeExport struct {
	URI        string
	Name       string
	Kind       string
	Range      lsp.Range
	TypeName   string
	Visibility string
	Members    []WorkspaceVBAutoIncludeMember
}

// WorkspaceVBAutoIncludeSnapshot is an immutable view of one complete
// workspace export-catalog generation.
type WorkspaceVBAutoIncludeSnapshot struct {
	Generation uint64
	Complete   bool
	catalog    *workspaceVBAutoIncludeCatalog
}

// Exports returns exact case-insensitive name matches in stable order.
func (snapshot WorkspaceVBAutoIncludeSnapshot) Exports(name string) []WorkspaceVBAutoIncludeExport {
	if !snapshot.Complete || snapshot.catalog == nil {
		return nil
	}
	return cloneWorkspaceVBAutoIncludeExports(snapshot.catalog.byName[strings.ToLower(name)])
}

// ExportsForPrefix returns case-insensitive prefix matches in stable order.
func (snapshot WorkspaceVBAutoIncludeSnapshot) ExportsForPrefix(prefix string) []WorkspaceVBAutoIncludeExport {
	if !snapshot.Complete || snapshot.catalog == nil {
		return nil
	}
	normalized := strings.ToLower(prefix)
	start := sort.SearchStrings(snapshot.catalog.names, normalized)
	result := make([]WorkspaceVBAutoIncludeExport, 0)
	for index := start; index < len(snapshot.catalog.names); index++ {
		name := snapshot.catalog.names[index]
		if !strings.HasPrefix(name, normalized) {
			break
		}
		result = append(result, snapshot.catalog.byName[name]...)
	}
	return cloneWorkspaceVBAutoIncludeExports(result)
}

type workspaceVBAutoIncludeDocumentExports struct {
	URI         string
	Fingerprint workspaceArtifactFingerprint
	Exports     []WorkspaceVBAutoIncludeExport
}

type workspaceVBAutoIncludeCatalog struct {
	generation uint64
	complete   bool
	byDocument map[workspaceDocumentID]workspaceVBAutoIncludeDocumentExports
	byName     map[string][]WorkspaceVBAutoIncludeExport
	names      []string
}

type workspaceVBAutoIncludeCatalogSource struct {
	DocumentID        workspaceDocumentID
	Document          *core.TextDocument
	SourceFingerprint workspaceArtifactFingerprint
}

type workspaceVBAutoIncludeCatalogBuild struct {
	catalog     *workspaceVBAutoIncludeCatalog
	cache       *workspacepkg.DiskAnalysisCache
	cacheDeltas []workspacepkg.DiskDocumentCacheDelta
	restored    int
	built       int
}

func newWorkspaceVBAutoIncludeCatalog(generation uint64, complete bool, documents map[workspaceDocumentID]workspaceVBAutoIncludeDocumentExports) *workspaceVBAutoIncludeCatalog {
	catalog := &workspaceVBAutoIncludeCatalog{
		generation: generation,
		complete:   complete,
		byDocument: make(map[workspaceDocumentID]workspaceVBAutoIncludeDocumentExports, len(documents)),
		byName:     map[string][]WorkspaceVBAutoIncludeExport{},
	}
	for documentID, document := range documents {
		owned := workspaceVBAutoIncludeDocumentExports{
			URI:         document.URI,
			Fingerprint: document.Fingerprint,
			Exports:     cloneWorkspaceVBAutoIncludeExports(document.Exports),
		}
		catalog.byDocument[documentID] = owned
		for _, export := range owned.Exports {
			name := strings.ToLower(export.Name)
			if name == "" {
				continue
			}
			catalog.byName[name] = append(catalog.byName[name], export)
		}
	}
	catalog.names = make([]string, 0, len(catalog.byName))
	for name, exports := range catalog.byName {
		sortWorkspaceVBAutoIncludeExports(exports)
		catalog.byName[name] = exports
		catalog.names = append(catalog.names, name)
	}
	sort.Strings(catalog.names)
	return catalog
}

func (s *Server) workspaceVBAutoIncludeSnapshot() WorkspaceVBAutoIncludeSnapshot {
	s.mu.Lock()
	catalog := s.workspaceVBAutoIncludeCatalog
	s.mu.Unlock()
	if catalog == nil {
		return WorkspaceVBAutoIncludeSnapshot{}
	}
	return WorkspaceVBAutoIncludeSnapshot{
		Generation: catalog.generation,
		Complete:   catalog.complete,
		catalog:    catalog,
	}
}

func (s *Server) markWorkspaceVBAutoIncludeCatalogIncompleteLocked(generation uint64) {
	s.workspaceVBAutoIncludeCatalog = newWorkspaceVBAutoIncludeCatalog(generation, false, nil)
}

func (s *Server) rebuildWorkspaceVBAutoIncludeCatalog(ctx context.Context, generation uint64) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	for ctx.Err() == nil {
		sources, ok := s.workspaceVBAutoIncludeCatalogSources(generation)
		if !ok {
			return false
		}
		build, ok := s.buildWorkspaceVBAutoIncludeCatalog(ctx, generation, sources)
		if !ok {
			return false
		}

		s.mu.Lock()
		current, currentOK := s.workspaceVBAutoIncludeCatalogSourcesLocked(generation)
		if !currentOK {
			s.mu.Unlock()
			return false
		}
		if !workspaceVBAutoIncludeCatalogSourcesEqual(sources, current) {
			s.mu.Unlock()
			continue
		}
		var cacheErr error
		if build.cache != nil && len(build.cacheDeltas) > 0 {
			cacheErr = build.cache.QueueDocumentCacheDeltas(build.cacheDeltas)
		}
		s.workspaceVBAutoIncludeCatalog = build.catalog
		s.mu.Unlock()
		if cacheErr != nil {
			s.logServerWarning("[asp-lsp] workspaceVBAutoIncludeCatalog.write.failed: " + cacheErr.Error())
		}
		s.logAnalysisDatabaseEvent("workspaceVBAutoIncludeCatalog", "publish", map[string]any{
			"built": build.built, "documents": len(sources), "generation": generation, "restored": build.restored,
		})
		return true
	}
	return false
}

func (s *Server) workspaceVBAutoIncludeCatalogSources(generation uint64) ([]workspaceVBAutoIncludeCatalogSource, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspaceVBAutoIncludeCatalogSourcesLocked(generation)
}

func (s *Server) workspaceVBAutoIncludeCatalogSourcesLocked(generation uint64) ([]workspaceVBAutoIncludeCatalogSource, bool) {
	if s.workspaceIndexClosed || s.workspaceIndexGeneration != generation {
		return nil, false
	}
	byDocument := make(map[workspaceDocumentID]*core.TextDocument, len(s.workspace))
	for _, document := range s.workspace {
		if document == nil {
			continue
		}
		documentID := workspaceDocumentIDFromURI(document.URI)
		if current := byDocument[documentID]; current == nil || document.URI < current.URI {
			byDocument[documentID] = document
		}
	}
	for _, document := range s.documents {
		if document == nil {
			continue
		}
		documentID := workspaceDocumentIDFromURI(document.URI)
		if _, belongsToWorkspace := byDocument[documentID]; belongsToWorkspace {
			byDocument[documentID] = document
		}
	}
	ids := make([]workspaceDocumentID, 0, len(byDocument))
	for documentID := range byDocument {
		ids = append(ids, documentID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	sources := make([]workspaceVBAutoIncludeCatalogSource, 0, len(ids))
	for _, documentID := range ids {
		document := byDocument[documentID]
		owned := core.NewTextDocument(document.URI, document.LanguageID, document.Version, document.Text)
		sources = append(sources, workspaceVBAutoIncludeCatalogSource{
			DocumentID:        documentID,
			Document:          owned,
			SourceFingerprint: workspaceFingerprint(document.Text),
		})
	}
	return sources, true
}

func workspaceVBAutoIncludeCatalogSourcesEqual(left, right []workspaceVBAutoIncludeCatalogSource) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].DocumentID != right[index].DocumentID ||
			left[index].Document.URI != right[index].Document.URI ||
			left[index].SourceFingerprint != right[index].SourceFingerprint {
			return false
		}
	}
	return true
}

func (s *Server) buildWorkspaceVBAutoIncludeCatalog(ctx context.Context, generation uint64, sources []workspaceVBAutoIncludeCatalogSource) (workspaceVBAutoIncludeCatalogBuild, bool) {
	build := workspaceVBAutoIncludeCatalogBuild{}
	documents := make(map[workspaceDocumentID]workspaceVBAutoIncludeDocumentExports, len(sources))
	s.mu.Lock()
	defaultLanguage := s.settings.DefaultLanguage
	s.mu.Unlock()
	build.cache = s.diskCacheForUse()
	if build.cache != nil && !build.cache.Enabled() {
		build.cache = nil
	}
	var artifacts []*workspacepkg.DiskDocumentArtifact
	if build.cache != nil {
		keys := make([]workspacepkg.DiskDocumentArtifactKey, len(sources))
		for index, source := range sources {
			keys[index] = workspacepkg.DiskDocumentArtifactKey{
				DocumentID: string(source.DocumentID), Kind: workspaceVBExportsArtifactKind,
				SchemaVersion: workspaceDocumentArtifactSchemaVersion,
			}
		}
		artifacts = build.cache.ReadDocumentArtifactsAligned(keys)
	}
	type sourceExports struct {
		summaries []vbExportSummary
		restored  bool
	}
	resolved := make([]sourceExports, len(sources))
	missing := make([]int, 0, len(sources))
	for index, source := range sources {
		if index < len(artifacts) {
			artifact := artifacts[index]
			var summaries []vbExportSummary
			if artifact != nil && artifact.SourceHash == workspaceVBExportsCacheSourceHash(source.SourceFingerprint, defaultLanguage) &&
				json.Unmarshal(artifact.Payload, &summaries) == nil && validWorkspaceVBExportSummaries(summaries) {
				resolved[index] = sourceExports{summaries: summaries, restored: true}
				continue
			}
		}
		missing = append(missing, index)
	}
	s.analysisWorkers.parallelForBulk(ctx, len(missing), func(workerCtx context.Context, missingIndex int) {
		if workerCtx.Err() != nil {
			return
		}
		index := missing[missingIndex]
		source := sources[index]
		parsed := s.cachedParsedText(source.Document.URI, source.Document.Text, defaultLanguage)
		if parsed == nil {
			// Warm startup can restore the include graph without hydrating full
			// parsed-document bundles. Reuse an existing cold-index parse when one
			// is available, but keep this fallback transient for lazy restoration.
			s.mu.Lock()
			transientParseHook := s.workspaceVBAutoIncludeTransientParseTestHook
			s.mu.Unlock()
			if transientParseHook != nil {
				transientParseHook(source.Document.URI)
			}
			parsed = core.ParseDocument(source.Document.URI, source.Document.Text, core.Settings{DefaultLanguage: defaultLanguage})
		}
		if workerCtx.Err() != nil {
			return
		}
		// The catalog only needs export summaries. Release the shard's
		// document CST afterwards so a cold workspace scan does not retain a
		// CST for every page; later requests rebuild it on demand.
		referenceShard := vbscript.BuildReferenceShard(parsed)
		resolved[index].summaries = workspaceTopLevelVBExportSummaries(summarizeVBScriptFileAnalysis(parsed).VBScript.Exports)
		referenceShard.ReleaseDocumentCST()
	})
	if ctx.Err() != nil {
		return workspaceVBAutoIncludeCatalogBuild{}, false
	}
	for index, source := range sources {
		summaries := resolved[index].summaries
		if !resolved[index].restored {
			build.built++
			payload, err := json.Marshal(summaries)
			if err == nil {
				build.cacheDeltas = append(build.cacheDeltas, workspacepkg.DiskDocumentCacheDelta{
					DocumentID: string(source.DocumentID),
					ArtifactUpserts: []workspacepkg.DiskDocumentArtifact{{
						Kind: workspaceVBExportsArtifactKind, SchemaVersion: workspaceDocumentArtifactSchemaVersion,
						SourceHash:  workspaceVBExportsCacheSourceHash(source.SourceFingerprint, defaultLanguage),
						Fingerprint: string(workspaceFingerprint(summaries)), Payload: payload,
					}},
				})
			}
		} else {
			build.restored++
		}
		exports := workspaceVBAutoIncludeExportsFromSummaries(source.Document.URI, summaries)
		documents[source.DocumentID] = workspaceVBAutoIncludeDocumentExports{
			URI: source.Document.URI, Fingerprint: workspaceFingerprint(summaries), Exports: exports,
		}
	}
	build.catalog = newWorkspaceVBAutoIncludeCatalog(generation, true, documents)
	return build, true
}

func workspaceVBExportsCacheSourceHash(sourceFingerprint workspaceArtifactFingerprint, defaultLanguage string) string {
	return string(workspaceFingerprint(struct {
		SourceFingerprint workspaceArtifactFingerprint
		DefaultLanguage   string
	}{
		SourceFingerprint: sourceFingerprint,
		DefaultLanguage:   strings.ToLower(strings.TrimSpace(defaultLanguage)),
	}))
}

func (s *Server) updateWorkspaceVBAutoIncludeCatalogLocked(previous, current *workspaceDocumentArtifactManifest) {
	catalog := s.workspaceVBAutoIncludeCatalog
	if catalog == nil || !catalog.complete || catalog.generation != s.workspaceIndexGeneration {
		return
	}
	var documentID workspaceDocumentID
	if current != nil {
		documentID = current.DocumentID
	} else if previous != nil {
		documentID = previous.DocumentID
	} else {
		return
	}
	nextDocument, keep := workspaceVBAutoIncludeDocumentExports{}, false
	if current != nil && s.workspaceDocumentByURILocked(current.URI) != nil {
		nextDocument = workspaceVBAutoIncludeDocumentExports{
			URI:         current.URI,
			Fingerprint: current.VBExportsFingerprint,
			Exports:     workspaceVBAutoIncludeExportsFromSummaries(current.URI, current.VBExports),
		}
		keep = true
	}
	before, existed := catalog.byDocument[documentID]
	if !keep && !existed {
		return
	}
	if keep && existed && before.URI == nextDocument.URI && before.Fingerprint == nextDocument.Fingerprint {
		return
	}
	documents := make(map[workspaceDocumentID]workspaceVBAutoIncludeDocumentExports, len(catalog.byDocument)+1)
	for key, document := range catalog.byDocument {
		documents[key] = document
	}
	if keep {
		documents[documentID] = nextDocument
	} else {
		delete(documents, documentID)
	}
	s.workspaceVBAutoIncludeCatalog = newWorkspaceVBAutoIncludeCatalog(catalog.generation, true, documents)
}

func (s *Server) removeWorkspaceVBAutoIncludeCatalogDocumentLocked(documentID workspaceDocumentID) {
	catalog := s.workspaceVBAutoIncludeCatalog
	if catalog == nil || !catalog.complete || catalog.generation != s.workspaceIndexGeneration {
		return
	}
	if _, exists := catalog.byDocument[documentID]; !exists {
		return
	}
	documents := make(map[workspaceDocumentID]workspaceVBAutoIncludeDocumentExports, len(catalog.byDocument)-1)
	for key, document := range catalog.byDocument {
		if key != documentID {
			documents[key] = document
		}
	}
	s.workspaceVBAutoIncludeCatalog = newWorkspaceVBAutoIncludeCatalog(catalog.generation, true, documents)
}

func (s *Server) queueWorkspaceVBExportArtifactTombstoneIfCurrent(documentID workspaceDocumentID, revision uint64) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		return
	}
	s.mu.Lock()
	if s.workspaceArtifacts[documentID] != nil || s.workspaceArtifactRevisions[documentID] != revision {
		s.mu.Unlock()
		return
	}
	err := cache.QueueDocumentCacheDeltas([]workspacepkg.DiskDocumentCacheDelta{{
		DocumentID: string(documentID),
		ArtifactDeletes: []workspacepkg.DiskDocumentArtifactKey{{
			Kind: workspaceVBExportsArtifactKind, SchemaVersion: workspaceDocumentArtifactSchemaVersion,
		}},
	}})
	s.mu.Unlock()
	if err != nil {
		s.logServerWarning("[asp-lsp] workspaceVBAutoIncludeCatalog.delete.failed: " + err.Error())
	}
}

func validWorkspaceVBExportSummaries(summaries []vbExportSummary) bool {
	memberRanges := workspaceVBExportMemberRanges(summaries)
	for _, summary := range summaries {
		if summary.Name == "" || summary.MemberOf != "" || strings.EqualFold(summary.Visibility, "private") {
			return false
		}
		if _, duplicateMember := memberRanges[workspaceVBExportRangeKey(summary)]; duplicateMember {
			return false
		}
	}
	return true
}

func workspaceTopLevelVBExportSummaries(summaries []vbExportSummary) []vbExportSummary {
	memberRanges := workspaceVBExportMemberRanges(summaries)
	result := make([]vbExportSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Name == "" || summary.MemberOf != "" || strings.EqualFold(summary.Visibility, "private") {
			continue
		}
		if _, duplicateMember := memberRanges[workspaceVBExportRangeKey(summary)]; duplicateMember {
			continue
		}
		cloned := summary
		cloned.Members = cloneVBExportSummaries(summary.Members)
		result = append(result, cloned)
	}
	return result
}

func workspaceVBExportMemberRanges(summaries []vbExportSummary) map[string]struct{} {
	ranges := map[string]struct{}{}
	var appendMembers func([]vbExportSummary)
	appendMembers = func(members []vbExportSummary) {
		for _, member := range members {
			ranges[workspaceVBExportRangeKey(member)] = struct{}{}
			appendMembers(member.Members)
		}
	}
	for _, summary := range summaries {
		appendMembers(summary.Members)
	}
	return ranges
}

func workspaceVBExportRangeKey(summary vbExportSummary) string {
	return strings.ToLower(summary.Name) + "\x00" + workspaceRangeKey(summary.Range)
}

func cloneVBExportSummaries(summaries []vbExportSummary) []vbExportSummary {
	if summaries == nil {
		return nil
	}
	result := make([]vbExportSummary, len(summaries))
	for index, summary := range summaries {
		result[index] = summary
		result[index].Members = cloneVBExportSummaries(summary.Members)
	}
	return result
}

func workspaceVBAutoIncludeExportsFromSummaries(uri string, summaries []vbExportSummary) []WorkspaceVBAutoIncludeExport {
	exports := make([]WorkspaceVBAutoIncludeExport, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Name == "" || summary.MemberOf != "" || strings.EqualFold(summary.Visibility, "private") {
			continue
		}
		exports = append(exports, WorkspaceVBAutoIncludeExport{
			URI:        uri,
			Name:       summary.Name,
			Kind:       summary.Kind,
			Range:      summary.Range,
			TypeName:   summary.TypeName,
			Visibility: summary.Visibility,
			Members:    workspaceVBAutoIncludeMembersFromSummaries(summary.Members),
		})
	}
	sortWorkspaceVBAutoIncludeExports(exports)
	return exports
}

func workspaceVBAutoIncludeMembersFromSummaries(summaries []vbExportSummary) []WorkspaceVBAutoIncludeMember {
	if summaries == nil {
		return nil
	}
	members := make([]WorkspaceVBAutoIncludeMember, 0, len(summaries))
	for _, summary := range summaries {
		members = append(members, WorkspaceVBAutoIncludeMember{
			Name:       summary.Name,
			Kind:       summary.Kind,
			Range:      summary.Range,
			TypeName:   summary.TypeName,
			Visibility: summary.Visibility,
			Members:    workspaceVBAutoIncludeMembersFromSummaries(summary.Members),
		})
	}
	return members
}

func cloneWorkspaceVBAutoIncludeExports(exports []WorkspaceVBAutoIncludeExport) []WorkspaceVBAutoIncludeExport {
	if exports == nil {
		return nil
	}
	result := make([]WorkspaceVBAutoIncludeExport, len(exports))
	for index, export := range exports {
		result[index] = export
		result[index].Members = cloneWorkspaceVBAutoIncludeMembers(export.Members)
	}
	return result
}

func cloneWorkspaceVBAutoIncludeMembers(members []WorkspaceVBAutoIncludeMember) []WorkspaceVBAutoIncludeMember {
	if members == nil {
		return nil
	}
	result := make([]WorkspaceVBAutoIncludeMember, len(members))
	for index, member := range members {
		result[index] = member
		result[index].Members = cloneWorkspaceVBAutoIncludeMembers(member.Members)
	}
	return result
}

func sortWorkspaceVBAutoIncludeExports(exports []WorkspaceVBAutoIncludeExport) {
	sort.SliceStable(exports, func(i, j int) bool {
		leftName, rightName := strings.ToLower(exports[i].Name), strings.ToLower(exports[j].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		leftURI, rightURI := strings.ToLower(exports[i].URI), strings.ToLower(exports[j].URI)
		if leftURI != rightURI {
			return leftURI < rightURI
		}
		if exports[i].URI != exports[j].URI {
			return exports[i].URI < exports[j].URI
		}
		if exports[i].Range.Start.Line != exports[j].Range.Start.Line {
			return exports[i].Range.Start.Line < exports[j].Range.Start.Line
		}
		if exports[i].Range.Start.Character != exports[j].Range.Start.Character {
			return exports[i].Range.Start.Character < exports[j].Range.Start.Character
		}
		if exports[i].Kind != exports[j].Kind {
			return exports[i].Kind < exports[j].Kind
		}
		return exports[i].Name < exports[j].Name
	})
}
