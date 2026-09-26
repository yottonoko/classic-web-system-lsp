package lspserver

import (
	"io"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func newImplicitReferencePlanCacheTestServer(t *testing.T) (*Server, *core.ParsedDocument, string) {
	t.Helper()
	server := New(nil, io.Discard, io.Discard)
	root := t.TempDir()
	server.rootPath = root
	server.workspaceIncludeGraph.Reset("test")
	targetPath := filepath.Join(root, "target.asp")
	targetURI := filePathURI(targetPath)
	source := "<% SharedValue = 1 %>"
	server.workspace[targetURI] = core.NewTextDocument(targetURI, "classic-asp", 0, source)
	server.workspaceIncludeGraph.Upsert(targetPath, workspacepkg.SourceMetadata{FileName: targetPath}, nil, "")
	server.workspaceIncludeGraphComplete = true
	parsed := core.ParseDocument(targetURI, source, core.Settings{DefaultLanguage: "VBScript"})
	return server, parsed, workspacepkg.FileIdentityKeyFromFileName(targetPath)
}

func TestImplicitGlobalReferencePlansCacheKeyIncludesRevisions(t *testing.T) {
	server, parsed, documentKey := newImplicitReferencePlanCacheTestServer(t)

	first, complete := server.implicitGlobalReferencePlans(parsed)
	if !complete || first == nil {
		t.Fatalf("initial implicit reference plans = %#v, complete=%t", first, complete)
	}
	oldKey := workspaceReferenceImplicitPlanKey{DocumentKey: documentKey}
	server.mu.Lock()
	if _, ok := server.referenceImplicitPlans[oldKey]; !ok {
		server.mu.Unlock()
		t.Fatalf("initial cache key %#v was not stored", oldKey)
	}
	server.referenceImplicitPlans[oldKey] = map[string]map[string]struct{}{
		"stale": {"old-document": {}},
	}
	server.workspaceIncludeGraphRevision++
	server.mu.Unlock()

	server.referenceWorkspaceIndex.update([]*core.ParsedDocument{parsed})
	second, complete := server.implicitGlobalReferencePlans(parsed)
	if !complete || second == nil {
		t.Fatalf("refreshed implicit reference plans = %#v, complete=%t", second, complete)
	}
	if _, stale := second["stale"]; stale {
		t.Fatalf("stale implicit reference plan was returned after revisions changed: %#v", second)
	}

	graphRevision := server.workspaceIncludeGraphRevision
	indexRevision := server.referenceWorkspaceIndex.revisionNumber()
	currentKey := workspaceReferenceImplicitPlanKey{
		DocumentKey: documentKey, GraphRevision: graphRevision, IndexRevision: indexRevision,
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if _, ok := server.referenceImplicitPlans[currentKey]; !ok {
		t.Fatalf("refreshed cache key %#v was not stored; keys=%#v", currentKey, server.referenceImplicitPlans)
	}
}

func TestImplicitGlobalReferencePlansCacheCopySurvivesConcurrentInvalidation(t *testing.T) {
	server, parsed, documentKey := newImplicitReferencePlanCacheTestServer(t)
	key := workspaceReferenceImplicitPlanKey{DocumentKey: documentKey}
	server.mu.Lock()
	server.referenceImplicitPlans[key] = map[string]map[string]struct{}{
		"shared":   {"first": {}, "second": {}},
		"sentinel": {"unrelated": {}},
	}
	server.mu.Unlock()

	plans, complete := server.implicitGlobalReferencePlans(parsed)
	if !complete || plans == nil {
		t.Fatalf("cached implicit reference plans = %#v, complete=%t", plans, complete)
	}
	if _, ok := plans["shared"]; !ok {
		t.Fatalf("cached plan copy lost shared name: %#v", plans)
	}
	plans["shared"]["returned-only"] = struct{}{}
	server.mu.Lock()
	_, cacheMutated := server.referenceImplicitPlans[key]["shared"]["returned-only"]
	server.mu.Unlock()
	if cacheMutated {
		t.Fatal("mutating the returned nested plan changed the cache")
	}

	start := make(chan struct{})
	invalidEntry := make(chan struct{}, 1)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		for iteration := 0; iteration < 2_000; iteration++ {
			for name, documents := range plans {
				for documentKey := range documents {
					if name == "" || documentKey == "" {
						select {
						case invalidEntry <- struct{}{}:
						default:
						}
						return
					}
				}
			}
			runtime.Gosched()
		}
	}()
	go func() {
		defer wait.Done()
		<-start
		affected := map[string]struct{}{"shared": {}}
		for iteration := 0; iteration < 1_000; iteration++ {
			server.mu.Lock()
			plan := server.referenceImplicitPlans[key]
			plan["shared"] = map[string]struct{}{"updated": {}}
			server.invalidateWorkspaceReferenceImplicitPlansByNameLocked(affected)
			keys := server.referenceImplicitPlansByName["shared"]
			if keys == nil {
				keys = make(map[workspaceReferenceImplicitPlanKey]struct{})
				server.referenceImplicitPlansByName["shared"] = keys
			}
			keys[key] = struct{}{}
			server.mu.Unlock()
			runtime.Gosched()
		}
	}()
	close(start)
	wait.Wait()
	select {
	case <-invalidEntry:
		t.Fatal("cached plan contains an empty entry")
	default:
	}

	if _, ok := plans["shared"]; !ok {
		t.Fatalf("returned plan was mutated by cache invalidation: %#v", plans)
	}
}

func TestWorkspaceReferenceImplicitPlanStoreReplacesOlderRevisionKeys(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	const documentKey = "workspace/document.asp"

	server.mu.Lock()
	server.ensureWorkspaceReferenceNameIndexLocked()
	for revision := uint64(1); revision <= 100; revision++ {
		key := workspaceReferenceImplicitPlanKey{
			DocumentKey:   documentKey,
			GraphRevision: revision,
			IndexRevision: revision,
		}
		server.storeWorkspaceReferenceImplicitPlanLocked(key, map[string]map[string]struct{}{
			"shared": {documentKey: {}},
		})
	}

	if got := len(server.referenceImplicitPlans); got != 1 {
		server.mu.Unlock()
		t.Fatalf("implicit plan cache entries = %d, want 1", got)
	}
	latestKey := workspaceReferenceImplicitPlanKey{
		DocumentKey:   documentKey,
		GraphRevision: 100,
		IndexRevision: 100,
	}
	if _, ok := server.referenceImplicitPlans[latestKey]; !ok {
		server.mu.Unlock()
		t.Fatalf("latest implicit plan key %#v was not retained; keys=%#v", latestKey, server.referenceImplicitPlans)
	}
	if keys := server.referenceImplicitPlansByName["shared"]; len(keys) != 1 {
		server.mu.Unlock()
		t.Fatalf("implicit plan name index entries = %d, want 1", len(keys))
	} else if _, ok := keys[latestKey]; !ok {
		server.mu.Unlock()
		t.Fatalf("implicit plan name index lost latest key %#v: %#v", latestKey, keys)
	}
	server.mu.Unlock()
}
