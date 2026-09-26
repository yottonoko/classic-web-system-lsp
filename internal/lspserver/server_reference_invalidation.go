package lspserver

import (
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) invalidateWorkspaceReferencesForParsedChange(previous, current *core.ParsedDocument) {
	if previous == nil || current == nil || !workspacepkg.SameFileIdentityURI(previous.URI, current.URI) {
		s.clearWorkspaceReferenceCache()
		return
	}
	s.mu.Lock()
	delete(s.referenceDeclarationPlans, previous)
	delete(s.referenceCountSummariesRestored, previous)
	s.mu.Unlock()
	if workspaceReferenceIncludeFingerprint(previous) != workspaceReferenceIncludeFingerprint(current) {
		s.logDebugSummaryEvent("referenceCache.invalidation", "[asp-lsp] referenceCache.invalidated reason=includeTopologyChanged uri="+current.URI, map[string]any{
			"reason": "includeTopologyChanged", "uri": current.URI,
		})
		s.refreshWorkspaceIncludeGraphFile(current)
		if !s.invalidateWorkspaceReferenceTopology(previous, current) {
			s.clearWorkspaceReferenceCache()
		}
		return
	}
	s.referenceWorkspaceIndex.update([]*core.ParsedDocument{previous})
	update := s.referenceWorkspaceIndex.update([]*core.ParsedDocument{current})
	if update.ChangedDocuments == 0 && len(update.AffectedNames) == 0 {
		s.logDebugSummaryEvent("referenceCache.invalidation", "[asp-lsp] referenceCache.preserved reason=semanticUnchanged uri="+current.URI, map[string]any{
			"reason": "semanticUnchanged", "semanticUnchangedDocuments": update.SemanticUnchangedDocuments, "uri": current.URI,
		})
		return
	}
	s.logDebugSummaryEvent("referenceCache.invalidation", "[asp-lsp] referenceCache.invalidated reason=referenceSemanticsChanged uri="+current.URI+", affectedNames="+strconv.Itoa(len(update.AffectedNames)), map[string]any{
		"affectedNames": len(update.AffectedNames), "reason": "referenceSemanticsChanged", "uri": current.URI,
	})
	s.invalidateWorkspaceReferenceNames(previous, current, update.AffectedNames)
}

func (s *Server) invalidateWorkspaceReferenceTopology(previous, current *core.ParsedDocument) bool {
	if previous == nil && current == nil {
		return false
	}
	changedURI := ""
	if current != nil {
		changedURI = current.URI
	} else {
		changedURI = previous.URI
	}
	changedPath := cleanFileURIPath(changedURI)
	if changedPath == "" {
		return false
	}
	seeds := []string{changedPath}
	for _, parsed := range []*core.ParsedDocument{previous, current} {
		if parsed == nil {
			continue
		}
		for _, include := range parsed.Includes {
			if details, ok := s.includeTargetDetailsForMode(parsed.URI, include.Path, include.Mode); ok && details.Path != "" {
				seeds = append(seeds, details.Path)
			}
		}
	}
	s.mu.Lock()
	if s.workspaceIncludeGraph == nil || !s.workspaceIncludeGraphComplete {
		s.mu.Unlock()
		return false
	}
	owners := s.workspaceIncludeGraph.AffectedScope(seeds)
	family := s.workspaceIncludeGraph.ForwardClosure(owners.FileNames)
	affected := family.IdentityMembership()
	for _, seed := range seeds {
		affected[workspacepkg.FileIdentityKeyFromFileName(seed)] = struct{}{}
	}
	s.invalidateWorkspaceReferenceFamilyLocked(affected)
	s.mu.Unlock()
	s.requestCodeLensRefresh("references.topology.invalidated")
	return true
}

func (s *Server) invalidateWorkspaceReferenceFamilyLocked(affected map[string]struct{}) {
	defer s.resetWorkspaceReferenceNameIndexLocked()
	for key := range s.referenceCounts {
		if _, ok := affected[workspacepkg.FileIdentityKeyFromURI(key.URI)]; ok {
			delete(s.referenceCounts, key)
		}
	}
	for key := range s.referencePartialCounts {
		if _, ok := affected[workspacepkg.FileIdentityKeyFromURI(key.URI)]; ok {
			delete(s.referencePartialCounts, key)
		}
	}
	for key := range s.referenceResults {
		if _, ok := affected[workspacepkg.FileIdentityKeyFromURI(key.URI)]; ok {
			delete(s.referenceResults, key)
		}
	}
	for key, inflight := range s.referenceInflight {
		if _, ok := affected[workspacepkg.FileIdentityKeyFromURI(key.URI)]; ok {
			inflight.stale = true
			delete(s.referenceInflight, key)
		}
	}
	for key, state := range s.referenceBatch {
		if _, ok := affected[key.DocumentKey]; ok {
			if state.cancel != nil {
				state.cancel()
			}
			delete(s.referenceBatch, key)
		}
	}
	for key := range s.referenceScopes {
		if _, ok := affected[key.DocumentKey]; ok {
			delete(s.referenceScopes, key)
		}
	}
	for key := range s.referenceImplicitPlans {
		if _, ok := affected[key.DocumentKey]; ok {
			delete(s.referenceImplicitPlans, key)
		}
	}
	for cacheKey := range s.referenceDocuments {
		for documentKey := range affected {
			if strings.HasPrefix(cacheKey, documentKey+"#") {
				delete(s.referenceDocuments, cacheKey)
				break
			}
		}
	}
	for cacheKey := range s.referenceDescriptorFingerprints {
		for documentKey := range affected {
			if strings.HasPrefix(cacheKey, documentKey+"#") {
				delete(s.referenceDescriptorFingerprints, cacheKey)
				break
			}
		}
	}
	for parsed := range s.referenceDeclarationPlans {
		if parsed != nil {
			if _, ok := affected[workspacepkg.FileIdentityKeyFromURI(parsed.URI)]; ok {
				delete(s.referenceDeclarationPlans, parsed)
			}
		}
	}
}

func (s *Server) invalidateWorkspaceReferenceNames(previous, current *core.ParsedDocument, names []string) {
	affected := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.ToLower(name)
		if name != "" {
			affected[name] = struct{}{}
		}
	}
	if len(affected) == 0 {
		return
	}
	s.mu.Lock()
	for name := range affected {
		s.referenceNameRevisions[name]++
	}
	s.invalidateWorkspaceReferenceTargetsByNameLocked(affected)
	// Cancel only batches that subscribed to an affected name. Unrelated names
	// keep both their final counts and their in-flight work.
	s.invalidateWorkspaceReferenceBatchesByNameLocked(affected)
	s.invalidateWorkspaceReferenceImplicitPlansByNameLocked(affected)
	if previous != nil && current != nil {
		for cacheKey, documents := range s.referenceDocuments {
			updated := false
			for index, document := range documents {
				if document != nil && workspacepkg.SameFileIdentityURI(document.URI, previous.URI) {
					documents[index] = current
					updated = true
				}
			}
			if updated {
				s.referenceDocuments[cacheKey] = documents
			}
		}
	}
	s.mu.Unlock()
	s.requestCodeLensRefresh("references.names.invalidated")
}

func workspaceReferenceTargetAffected(key workspaceReferenceTargetKey, affected map[string]struct{}) bool {
	if key.Name != "" {
		_, ok := affected[strings.ToLower(key.Name)]
		return ok
	}
	for name := range affected {
		if workspaceReferenceNameHash(name) == key.NameHash {
			return true
		}
	}
	return false
}

func workspaceReferenceIncludeFingerprint(parsed *core.ParsedDocument) string {
	if parsed == nil || len(parsed.Includes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		parts = append(parts, strings.ToLower(include.Mode)+"\x00"+include.Path)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x00")
}
