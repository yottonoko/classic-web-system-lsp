package lspserver

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type workspaceReferenceTargetCacheKind uint8

const (
	workspaceReferenceCountCache workspaceReferenceTargetCacheKind = 1 << iota
	workspaceReferencePartialCountCache
	workspaceReferenceResultCache
	workspaceReferenceInflightCache
)

func (s *Server) ensureWorkspaceReferenceNameIndexLocked() {
	if s.referenceNameIndexReady {
		return
	}
	s.referenceTargetsByName = make(map[string]map[workspaceReferenceTargetKey]workspaceReferenceTargetCacheKind)
	s.referenceUnnamedTargets = make(map[workspaceReferenceTargetKey]workspaceReferenceTargetCacheKind)
	s.referenceBatchesByName = make(map[string]map[workspaceReferenceBatchKey]struct{})
	s.referenceImplicitPlansByName = make(map[string]map[workspaceReferenceImplicitPlanKey]struct{})
	s.referenceNameIndexReady = true
	for key := range s.referenceCounts {
		s.indexWorkspaceReferenceTargetLocked(key, workspaceReferenceCountCache)
	}
	for key := range s.referencePartialCounts {
		s.indexWorkspaceReferenceTargetLocked(key, workspaceReferencePartialCountCache)
	}
	for key := range s.referenceResults {
		s.indexWorkspaceReferenceTargetLocked(key, workspaceReferenceResultCache)
	}
	for key := range s.referenceInflight {
		s.indexWorkspaceReferenceTargetLocked(key, workspaceReferenceInflightCache)
	}
	for key, state := range s.referenceBatch {
		s.indexWorkspaceReferenceBatchLocked(key, state)
	}
	for key, plan := range s.referenceImplicitPlans {
		s.indexWorkspaceReferenceImplicitPlanLocked(key, plan)
	}
}

func (s *Server) resetWorkspaceReferenceNameIndexLocked() {
	s.referenceNameIndexReady = false
	s.referenceTargetsByName = nil
	s.referenceUnnamedTargets = nil
	s.referenceBatchesByName = nil
	s.referenceImplicitPlansByName = nil
}

func (s *Server) indexWorkspaceReferenceTargetLocked(key workspaceReferenceTargetKey, kind workspaceReferenceTargetCacheKind) {
	if !s.referenceNameIndexReady {
		return
	}
	name := strings.ToLower(key.Name)
	if name == "" {
		s.referenceUnnamedTargets[key] |= kind
		return
	}
	keys := s.referenceTargetsByName[name]
	if keys == nil {
		keys = make(map[workspaceReferenceTargetKey]workspaceReferenceTargetCacheKind)
		s.referenceTargetsByName[name] = keys
	}
	keys[key] |= kind
}

func (s *Server) unindexWorkspaceReferenceTargetLocked(key workspaceReferenceTargetKey, kind workspaceReferenceTargetCacheKind) {
	if !s.referenceNameIndexReady {
		return
	}
	name := strings.ToLower(key.Name)
	keys := s.referenceTargetsByName[name]
	if name == "" {
		keys = s.referenceUnnamedTargets
	}
	remaining := keys[key] &^ kind
	if remaining != 0 {
		keys[key] = remaining
		return
	}
	delete(keys, key)
	if name != "" && len(keys) == 0 {
		delete(s.referenceTargetsByName, name)
	}
}

func (s *Server) storeWorkspaceReferenceCountLocked(key workspaceReferenceTargetKey, count int) {
	s.referenceCounts[key] = count
	s.indexWorkspaceReferenceTargetLocked(key, workspaceReferenceCountCache)
}

func (s *Server) storeWorkspaceReferencePartialCountLocked(key workspaceReferenceTargetKey, count int) {
	s.referencePartialCounts[key] = count
	s.indexWorkspaceReferenceTargetLocked(key, workspaceReferencePartialCountCache)
}

func (s *Server) storeWorkspaceReferenceResultLocked(key workspaceReferenceTargetKey, locations []lsp.Location) {
	s.referenceResults[key] = locations
	s.indexWorkspaceReferenceTargetLocked(key, workspaceReferenceResultCache)
}

func (s *Server) storeWorkspaceReferenceInflightLocked(key workspaceReferenceTargetKey, inflight *workspaceReferenceInflight) {
	s.referenceInflight[key] = inflight
	s.indexWorkspaceReferenceTargetLocked(key, workspaceReferenceInflightCache)
}

func (s *Server) deleteWorkspaceReferenceTargetKindLocked(key workspaceReferenceTargetKey, kind workspaceReferenceTargetCacheKind) {
	s.unindexWorkspaceReferenceTargetLocked(key, kind)
	s.deleteWorkspaceReferenceTargetKindUnindexedLocked(key, kind)
}

func (s *Server) deleteWorkspaceReferenceTargetKindUnindexedLocked(key workspaceReferenceTargetKey, kind workspaceReferenceTargetCacheKind) {
	if kind&workspaceReferenceCountCache != 0 {
		delete(s.referenceCounts, key)
	}
	if kind&workspaceReferencePartialCountCache != 0 {
		delete(s.referencePartialCounts, key)
	}
	if kind&workspaceReferenceResultCache != 0 {
		delete(s.referenceResults, key)
	}
	if kind&workspaceReferenceInflightCache != 0 {
		if inflight := s.referenceInflight[key]; inflight != nil {
			inflight.stale = true
		}
		delete(s.referenceInflight, key)
	}
}

func (s *Server) indexWorkspaceReferenceBatchLocked(key workspaceReferenceBatchKey, state *workspaceReferenceBatchState) {
	if !s.referenceNameIndexReady || state == nil {
		return
	}
	for name := range state.nameRevisions {
		name = strings.ToLower(name)
		keys := s.referenceBatchesByName[name]
		if keys == nil {
			keys = make(map[workspaceReferenceBatchKey]struct{})
			s.referenceBatchesByName[name] = keys
		}
		keys[key] = struct{}{}
	}
}

func (s *Server) storeWorkspaceReferenceBatchLocked(key workspaceReferenceBatchKey, state *workspaceReferenceBatchState) {
	if previous := s.referenceBatch[key]; previous != nil {
		s.unindexWorkspaceReferenceBatchLocked(key, previous)
	}
	s.referenceBatch[key] = state
	s.indexWorkspaceReferenceBatchLocked(key, state)
}

func (s *Server) unindexWorkspaceReferenceBatchLocked(key workspaceReferenceBatchKey, state *workspaceReferenceBatchState) {
	if !s.referenceNameIndexReady || state == nil {
		return
	}
	for name := range state.nameRevisions {
		name = strings.ToLower(name)
		keys := s.referenceBatchesByName[name]
		delete(keys, key)
		if len(keys) == 0 {
			delete(s.referenceBatchesByName, name)
		}
	}
}

func (s *Server) deleteWorkspaceReferenceBatchLocked(key workspaceReferenceBatchKey) {
	state := s.referenceBatch[key]
	s.unindexWorkspaceReferenceBatchLocked(key, state)
	delete(s.referenceBatch, key)
}

func (s *Server) indexWorkspaceReferenceImplicitPlanLocked(key workspaceReferenceImplicitPlanKey, plan map[string]map[string]struct{}) {
	if !s.referenceNameIndexReady {
		return
	}
	for name := range plan {
		name = strings.ToLower(name)
		keys := s.referenceImplicitPlansByName[name]
		if keys == nil {
			keys = make(map[workspaceReferenceImplicitPlanKey]struct{})
			s.referenceImplicitPlansByName[name] = keys
		}
		keys[key] = struct{}{}
	}
}

func (s *Server) storeWorkspaceReferenceImplicitPlanLocked(key workspaceReferenceImplicitPlanKey, plan map[string]map[string]struct{}) {
	for previousKey, previous := range s.referenceImplicitPlans {
		if previousKey.DocumentKey != key.DocumentKey || previousKey == key {
			continue
		}
		s.unindexWorkspaceReferenceImplicitPlanLocked(previousKey, previous)
		delete(s.referenceImplicitPlans, previousKey)
	}
	if previous := s.referenceImplicitPlans[key]; previous != nil {
		s.unindexWorkspaceReferenceImplicitPlanLocked(key, previous)
	}
	plan = cloneWorkspaceReferenceImplicitPlan(plan)
	s.referenceImplicitPlans[key] = plan
	s.indexWorkspaceReferenceImplicitPlanLocked(key, plan)
}

func cloneWorkspaceReferenceImplicitPlan(plan map[string]map[string]struct{}) map[string]map[string]struct{} {
	if plan == nil {
		return nil
	}
	clone := make(map[string]map[string]struct{}, len(plan))
	for name, documents := range plan {
		documentClone := make(map[string]struct{}, len(documents))
		for documentKey := range documents {
			documentClone[documentKey] = struct{}{}
		}
		clone[name] = documentClone
	}
	return clone
}

func (s *Server) unindexWorkspaceReferenceImplicitPlanLocked(key workspaceReferenceImplicitPlanKey, plan map[string]map[string]struct{}) {
	if !s.referenceNameIndexReady {
		return
	}
	for name := range plan {
		name = strings.ToLower(name)
		keys := s.referenceImplicitPlansByName[name]
		delete(keys, key)
		if len(keys) == 0 {
			delete(s.referenceImplicitPlansByName, name)
		}
	}
}

func (s *Server) invalidateWorkspaceReferenceTargetsByNameLocked(affected map[string]struct{}) {
	s.ensureWorkspaceReferenceNameIndexLocked()
	for name := range affected {
		for key, kinds := range s.referenceTargetsByName[name] {
			s.deleteWorkspaceReferenceTargetKindUnindexedLocked(key, kinds)
		}
		delete(s.referenceTargetsByName, name)
	}
	// Name-less keys only exist for compatibility with old internal callers.
	// Keep their former hash-based semantics without making normal invalidation
	// scan the complete target caches.
	for key, kinds := range s.referenceUnnamedTargets {
		if workspaceReferenceTargetAffected(key, affected) {
			s.deleteWorkspaceReferenceTargetKindUnindexedLocked(key, kinds)
			delete(s.referenceUnnamedTargets, key)
		}
	}
}

func (s *Server) invalidateWorkspaceReferenceLocationsByNameLocked(affected map[string]struct{}) {
	s.ensureWorkspaceReferenceNameIndexLocked()
	kindsToDelete := workspaceReferenceResultCache | workspaceReferenceInflightCache
	for name := range affected {
		keys := s.referenceTargetsByName[name]
		for key, kinds := range keys {
			removed := kinds & kindsToDelete
			if removed == 0 {
				continue
			}
			s.deleteWorkspaceReferenceTargetKindUnindexedLocked(key, removed)
			remaining := kinds &^ removed
			if remaining == 0 {
				delete(keys, key)
			} else {
				keys[key] = remaining
			}
		}
		if len(keys) == 0 {
			delete(s.referenceTargetsByName, name)
		}
	}
	for key, kinds := range s.referenceUnnamedTargets {
		if !workspaceReferenceTargetAffected(key, affected) {
			continue
		}
		removed := kinds & kindsToDelete
		s.deleteWorkspaceReferenceTargetKindUnindexedLocked(key, removed)
		remaining := kinds &^ removed
		if remaining == 0 {
			delete(s.referenceUnnamedTargets, key)
		} else {
			s.referenceUnnamedTargets[key] = remaining
		}
	}
}

func (s *Server) invalidateWorkspaceReferenceBatchesByNameLocked(affected map[string]struct{}) {
	s.ensureWorkspaceReferenceNameIndexLocked()
	for name := range affected {
		keys := make([]workspaceReferenceBatchKey, 0, len(s.referenceBatchesByName[name]))
		for key := range s.referenceBatchesByName[name] {
			keys = append(keys, key)
		}
		for _, key := range keys {
			if state := s.referenceBatch[key]; state != nil && state.cancel != nil {
				state.cancel()
			}
			s.deleteWorkspaceReferenceBatchLocked(key)
		}
	}
}

func (s *Server) invalidateWorkspaceReferenceImplicitPlansByNameLocked(affected map[string]struct{}) {
	s.ensureWorkspaceReferenceNameIndexLocked()
	for name := range affected {
		keys := make([]workspaceReferenceImplicitPlanKey, 0, len(s.referenceImplicitPlansByName[name]))
		for key := range s.referenceImplicitPlansByName[name] {
			keys = append(keys, key)
		}
		for _, key := range keys {
			plan := s.referenceImplicitPlans[key]
			delete(plan, name)
			delete(s.referenceImplicitPlansByName[name], key)
			if len(plan) == 0 {
				delete(s.referenceImplicitPlans, key)
			}
		}
		if len(s.referenceImplicitPlansByName[name]) == 0 {
			delete(s.referenceImplicitPlansByName, name)
		}
	}
}
