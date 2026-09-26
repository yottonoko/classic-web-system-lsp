package lspserver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestDidChangeConfigurationReadsVBScriptTypeChecking(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	params := json.RawMessage(`{"settings":{"aspLsp":{"vbscript":{"typeChecking":"strict"}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.settings.VBScriptTypeChecking != "strict" {
		t.Fatalf("VBScriptTypeChecking = %q, want strict", server.settings.VBScriptTypeChecking)
	}
}

func TestDidChangeConfigurationReadsVBScriptAssumeUndefinedGlobals(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	if server.settings.VBScriptAssumeUndefinedGlobals {
		t.Fatal("VBScriptAssumeUndefinedGlobals should default to false")
	}
	params := json.RawMessage(`{"settings":{"aspLsp":{"vbscript":{"assumeUndefinedGlobals":true}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if !server.settings.VBScriptAssumeUndefinedGlobals {
		t.Fatal("VBScriptAssumeUndefinedGlobals = false, want true")
	}
	params = json.RawMessage(`{"settings":{"aspLsp":{"vbscript":{"assumeUndefinedGlobals":false}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.settings.VBScriptAssumeUndefinedGlobals {
		t.Fatal("VBScriptAssumeUndefinedGlobals = true, want false")
	}
}

func TestDidChangeConfigurationReadsVBScriptAutoIncludes(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	if server.settings.VBScriptAutoIncludes {
		t.Fatal("VBScriptAutoIncludes should default to false")
	}
	params := json.RawMessage(`{"settings":{"aspLsp":{"vbscript":{"autoIncludes":true}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if !server.settings.VBScriptAutoIncludes {
		t.Fatal("VBScriptAutoIncludes = false, want true")
	}
	params = json.RawMessage(`{"settings":{"aspLsp":{"vbscript":{"autoIncludes":false}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.settings.VBScriptAutoIncludes {
		t.Fatal("VBScriptAutoIncludes = true, want false")
	}
}

func TestDidChangeConfigurationReadsVBScriptImplicitGlobalDiagnostics(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	if server.settings.VBScriptImplicitGlobalDiagnostics {
		t.Fatal("VBScriptImplicitGlobalDiagnostics should default to false")
	}
	params := json.RawMessage(`{"settings":{"aspLsp":{"vbscript":{"implicitGlobalDiagnostics":true}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if !server.settings.VBScriptImplicitGlobalDiagnostics {
		t.Fatal("VBScriptImplicitGlobalDiagnostics = false, want true")
	}
}

func TestDidChangeConfigurationReadsVBScriptIfSyntaxDiagnostics(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	params := json.RawMessage(`{"settings":{"aspLsp":{"vbscript":{"ifSyntaxDiagnostics":"strict"}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.settings.VBScriptIfSyntaxDiagnostics != "strict" {
		t.Fatalf("VBScriptIfSyntaxDiagnostics = %q, want strict", server.settings.VBScriptIfSyntaxDiagnostics)
	}
}

func TestDidChangeConfigurationFormattingPreservesWorkspaceIndexAndAnalysisCache(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 7
	server.graphGeneration = 11
	t.Cleanup(server.stopWorkspaceIndexWorkers)
	parsed := core.ParseDocument("file:///cached.asp", "<% value = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	server.rememberFileAnalysisSnapshot(parsed, &fileAnalysisSnapshot{})
	if server.cachedFileAnalysisSnapshot(parsed) == nil {
		t.Fatal("analysis snapshot was not cached")
	}

	params := json.RawMessage(`{"settings":{"aspLsp":{"format":{"printWidth":88}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.settings.FormatPrintWidth != 88 {
		t.Fatalf("FormatPrintWidth = %d, want 88", server.settings.FormatPrintWidth)
	}
	if server.workspaceIndexGeneration != 7 {
		t.Fatalf("format-only configuration advanced workspace generation to %d", server.workspaceIndexGeneration)
	}
	if server.graphGeneration != 11 {
		t.Fatalf("format-only configuration advanced graph generation to %d", server.graphGeneration)
	}
	if server.cachedFileAnalysisSnapshot(parsed) == nil {
		t.Fatal("format-only configuration cleared the analysis cache")
	}
}

func TestDidChangeConfigurationNoOpPreservesWorkspaceState(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 7
	server.graphGeneration = 11
	t.Cleanup(server.stopWorkspaceIndexWorkers)
	parsed := core.ParseDocument("file:///cached.asp", "<% value = 1 %>", core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	server.rememberFileAnalysisSnapshot(parsed, &fileAnalysisSnapshot{})

	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"cache": map[string]any{
					"enabled": server.settings.CacheEnabled, "directory": server.settings.CacheDirectory,
					"freshness": server.settings.CacheFreshness, "ttlHours": server.settings.CacheTTLHours,
					"maxSizeMb": server.settings.CacheMaxSizeMB, "gzip": server.settings.CacheGzip,
				},
				"network": map[string]any{
					"profile": server.settings.NetworkProfile, "statCacheTtlMs": server.settings.NetworkStatCacheTTLMS,
					"readdirCacheTtlMs":      server.settings.NetworkReadDirCacheTTLMS,
					"includeReadConcurrency": server.settings.NetworkIncludeReadConcurrency,
				},
				"workspace": map[string]any{
					"includes": server.settings.WorkspaceIncludeGlobs, "excludes": server.settings.WorkspaceExcludeGlobs,
					"scanChunkSize":           server.settings.WorkspaceScanChunkSize,
					"busyAnalysisConcurrency": server.settings.WorkspaceBusyAnalysisConcurrency,
					"respectGitIgnore":        server.settings.WorkspaceRespectGitIgnore,
				},
				"format": map[string]any{"printWidth": server.settings.FormatPrintWidth},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.workspaceIndexGeneration != 7 {
		t.Fatalf("no-op configuration advanced workspace generation to %d", server.workspaceIndexGeneration)
	}
	if server.graphGeneration != 11 {
		t.Fatalf("no-op configuration advanced graph generation to %d", server.graphGeneration)
	}
	if server.cachedFileAnalysisSnapshot(parsed) == nil {
		t.Fatal("no-op configuration cleared the analysis cache")
	}
}

func TestDidChangeConfigurationNoOpDoesNotCancelActiveWorkspaceIndex(t *testing.T) {
	root := t.TempDir()
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	t.Cleanup(server.stopWorkspaceIndexWorkers)

	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	cancelled := make(chan struct{})
	var cancelledOnce sync.Once
	server.workspaceIndexTestHook = func(ctx context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase != workspaceIndexTestPhaseStarted || generation != 1 {
			return
		}
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			cancelledOnce.Do(func() { close(cancelled) })
		}
	}
	server.activateWorkspaceIndexing(context.Background())
	server.scheduleWorkspaceIndex("test.noOpConfiguration")
	waitForWorkspaceIndexSignal(t, started)

	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"workspace": map[string]any{"includes": server.settings.WorkspaceIncludeGlobs},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.workspaceIndexGeneration != 1 {
		t.Fatalf("no-op configuration replaced active workspace generation with %d", server.workspaceIndexGeneration)
	}
	select {
	case <-cancelled:
		t.Fatal("no-op configuration cancelled the active workspace index")
	default:
	}
}

func TestDidChangeConfigurationDiskCacheTuningPreservesActiveWorkspaceState(t *testing.T) {
	root := t.TempDir()
	fileName := filepath.Join(root, "indexed.asp")
	if err := os.WriteFile(fileName, []byte("<% Dim IndexedValue %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = t.TempDir()
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.graphGeneration = 11
	server.configureFsGateway()
	server.configureDiskAnalysisCache()
	oldCache := server.diskCacheForUse()
	if oldCache == nil || !oldCache.Enabled() {
		t.Fatal("enabled disk cache was not configured")
	}

	parsed := core.ParseDocument("file:///cached.asp", "<% value = 1 %>", core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	server.rememberFileAnalysisSnapshot(parsed, &fileAnalysisSnapshot{})

	started := make(chan struct{})
	release := make(chan struct{})
	cancelled := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	var cancelledOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		server.stopWorkspaceIndexWorkers()
		server.waitForAsyncDiskCacheWrites()
		server.closeDiskAnalysisCache()
	})
	server.workspaceIndexTestHook = func(ctx context.Context, phase workspaceIndexTestPhase, generation uint64) {
		if phase != workspaceIndexTestPhaseAfterDiskEnqueueBeforeCommit || generation != 1 {
			return
		}
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
			cancelledOnce.Do(func() { close(cancelled) })
		}
	}
	server.activateWorkspaceIndexing(context.Background())
	server.scheduleWorkspaceIndex("test.diskCacheTuning")
	waitForWorkspaceIndexSignal(t, started)

	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"cache": map[string]any{"ttlHours": server.settings.CacheTTLHours + 1},
			},
		},
	})
	configured := make(chan error, 1)
	go func() {
		configured <- server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params)
	}()
	select {
	case err := <-configured:
		t.Fatalf("disk-cache configuration returned before the active transaction completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-configured:
		if err != nil {
			t.Fatalf("didChangeConfiguration failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disk-cache configuration did not finish after the active transaction completed")
	}
	waitForWorkspaceIndexCompletion(t, server)
	if oldCache.Enabled() {
		t.Fatal("disk-cache tuning left the previous cache open")
	}
	if cache := server.diskCacheForUse(); cache == nil || !cache.Enabled() || cache == oldCache {
		t.Fatal("disk-cache tuning did not install an enabled replacement cache")
	}
	if server.workspaceIndexGeneration != 1 {
		t.Fatalf("disk-cache tuning replaced active workspace generation with %d", server.workspaceIndexGeneration)
	}
	if server.graphGeneration != 11 {
		t.Fatalf("disk-cache tuning advanced graph generation to %d", server.graphGeneration)
	}
	if server.cachedFileAnalysisSnapshot(parsed) == nil {
		t.Fatal("disk-cache tuning cleared the in-memory analysis cache")
	}
	server.mu.Lock()
	includeGraphComplete := server.workspaceIncludeGraphComplete
	server.mu.Unlock()
	if !includeGraphComplete {
		t.Fatal("disk-cache tuning left workspace include-graph analysis incomplete")
	}
	if catalog := server.workspaceVBAutoIncludeSnapshot(); !catalog.Complete || catalog.Generation != 1 {
		t.Fatalf("disk-cache tuning left auto-include catalog incomplete: %#v", catalog)
	}
	select {
	case <-cancelled:
		t.Fatal("disk-cache tuning cancelled the active workspace index")
	default:
	}
	server.mu.Lock()
	_, indexed := server.workspace[filePathURI(fileName)]
	server.mu.Unlock()
	if !indexed {
		t.Fatal("disk-cache tuning lost the indexed workspace document")
	}
}

func TestConfigureDiskAnalysisCacheDoesNotRelabelGraphAfterWorkspaceSettingsChange(t *testing.T) {
	root := t.TempDir()
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = t.TempDir()
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureDiskAnalysisCache()
	t.Cleanup(server.waitForAsyncDiskCacheWrites)
	t.Cleanup(server.closeDiskAnalysisCache)

	oldSettingsKey := server.workspaceDiskSettingsKey()
	graph := workspacepkg.NewWorkspaceIncludeGraph()
	graph.Reset(oldSettingsKey)
	graph.Upsert(filepath.Join(root, "default.asp"), workspacepkg.SourceMetadata{FileName: filepath.Join(root, "default.asp")}, nil, "refs")
	server.mu.Lock()
	server.workspaceIncludeGraph = graph
	server.workspaceIncludeGraphComplete = true
	server.workspaceIncludeGraphRevision = 1
	server.settings.IncludePaths = []string{filepath.Join(root, "includes")}
	server.settings.CacheTTLHours++
	server.mu.Unlock()
	newSettingsKey := server.workspaceDiskSettingsKey()
	if newSettingsKey == oldSettingsKey {
		t.Fatal("workspace settings change did not change the disk settings key")
	}

	server.configureDiskAnalysisCache()
	server.mu.Lock()
	persisting := server.workspaceIncludeGraphPersisting
	server.mu.Unlock()
	if persisting {
		t.Fatal("cache replacement scheduled a stale include graph under the new settings key")
	}
	cache := server.diskCacheForUse()
	if cache == nil || !cache.Enabled() {
		t.Fatal("cache replacement did not install an enabled cache")
	}
	server.mu.Lock()
	server.workspaceIncludeGraphPersistKey = newSettingsKey
	server.mu.Unlock()
	server.persistWorkspaceIncludeGraphSnapshotAtRevision(newSettingsKey, 1)
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.ReadWorkspaceIncludeGraph(newSettingsKey); ok {
		t.Fatal("stale include graph was persisted under the new settings key")
	}
}

func TestConfigureDiskAnalysisCachePersistsMatchingCompleteGraphToReplacementCache(t *testing.T) {
	root := t.TempDir()
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = t.TempDir()
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureDiskAnalysisCache()
	t.Cleanup(server.closeDiskAnalysisCache)

	oldCache := server.diskCacheForUse()
	settingsKey := server.workspaceDiskSettingsKey()
	owner := filepath.Join(root, "default.asp")
	target := filepath.Join(root, "shared.inc")
	graph := workspacepkg.NewWorkspaceIncludeGraph()
	graph.Reset(settingsKey)
	graph.Upsert(owner, workspacepkg.SourceMetadata{FileName: owner}, []string{target}, "refs")
	replacementDirectory := t.TempDir()
	server.mu.Lock()
	server.workspaceIncludeGraph = graph
	server.workspaceIncludeGraphComplete = true
	server.workspaceIncludeGraphRevision = 1
	server.settings.CacheDirectory = replacementDirectory
	server.settings.CacheTTLHours++
	server.mu.Unlock()

	server.configureDiskAnalysisCache()
	replacementCache := server.diskCacheForUse()
	if replacementCache == nil || !replacementCache.Enabled() || replacementCache == oldCache {
		t.Fatal("cache replacement did not install a distinct enabled cache")
	}
	if replacementCache.Directory() != replacementDirectory {
		t.Fatalf("replacement cache directory = %q, want %q", replacementCache.Directory(), replacementDirectory)
	}
	server.waitForAsyncDiskCacheWrites()
	restored, ok := replacementCache.ReadWorkspaceIncludeGraph(settingsKey)
	if !ok || len(restored.Entries) != 1 {
		t.Fatalf("replacement cache include graph = %#v, ok=%t, want one entry", restored, ok)
	}
	entry := restored.Entries[0]
	if entry.FileName != owner || len(entry.TargetFileNames) != 1 || entry.TargetFileNames[0] != target {
		t.Fatalf("replacement cache include graph entry = %#v, want owner %q target %q", entry, owner, target)
	}
}

func TestDidChangeConfigurationRepeatedEquivalentDiskCacheSettingsAreNoOp(t *testing.T) {
	root := t.TempDir()
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = false
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 7
	server.graphGeneration = 11
	t.Cleanup(server.stopWorkspaceIndexWorkers)
	t.Cleanup(server.closeDiskAnalysisCache)

	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"cache": map[string]any{
					"directory": ".asp-lsp-cache",
					"ttlHours":  maxDiskCacheTTLHours + 1,
					"maxSizeMb": maxDiskCacheMaxSizeMB + 1,
				},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("first didChangeConfiguration failed: %v", err)
	}
	firstCache := server.diskCacheForUse()
	if firstCache == nil {
		t.Fatal("first disk-cache configuration did not install a cache instance")
	}
	if server.settings.CacheDirectory != filepath.Join(root, ".asp-lsp-cache") {
		t.Fatalf("CacheDirectory = %q, want workspace-relative path resolved once", server.settings.CacheDirectory)
	}
	if server.settings.CacheTTLHours != maxDiskCacheTTLHours || server.settings.CacheMaxSizeMB != maxDiskCacheMaxSizeMB {
		t.Fatalf("bounded cache settings = ttl %d, size %d", server.settings.CacheTTLHours, server.settings.CacheMaxSizeMB)
	}

	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("repeated didChangeConfiguration failed: %v", err)
	}
	if server.diskCacheForUse() != firstCache {
		t.Fatal("equivalent disk-cache configuration reopened the cache")
	}
	if server.workspaceIndexGeneration != 7 {
		t.Fatalf("equivalent disk-cache configuration advanced workspace generation to %d", server.workspaceIndexGeneration)
	}
	if server.graphGeneration != 11 {
		t.Fatalf("equivalent disk-cache configuration advanced graph generation to %d", server.graphGeneration)
	}
}

func TestDidChangeConfigurationWorkspaceMembershipChangeReindexes(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 7
	t.Cleanup(server.stopWorkspaceIndexWorkers)

	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"workspace": map[string]any{"includes": []string{"**/*.asp"}},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	waitForWorkspaceIndexCompletion(t, server)
	if server.workspaceIndexGeneration != 8 {
		t.Fatalf("workspace membership change generation = %d, want 8", server.workspaceIndexGeneration)
	}
}

func TestDidChangeConfigurationNetworkProfileReindexesOnlyWhenEffectiveCaseResolutionChanges(t *testing.T) {
	for _, test := range []struct {
		name                string
		caseResolution      string
		wantGeneration      uint64
		wantGraphGeneration uint64
	}{
		{name: "auto case resolution", caseResolution: "auto", wantGeneration: 8, wantGraphGeneration: 12},
		{name: "explicit full case resolution", caseResolution: "full", wantGeneration: 7, wantGraphGeneration: 11},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
			server.rootPath = root
			server.rootURI = filePathURI(root)
			server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
			server.settings.NetworkCaseResolution = test.caseResolution
			server.workspaceIndexEnabled = true
			server.workspaceIndexGeneration = 7
			server.graphGeneration = 11
			t.Cleanup(server.stopWorkspaceIndexWorkers)

			params := mustRaw(map[string]any{
				"settings": map[string]any{
					"aspLsp": map[string]any{
						"network": map[string]any{"profile": "network"},
					},
				},
			})
			if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
				t.Fatalf("didChangeConfiguration failed: %v", err)
			}
			waitForWorkspaceIndexCompletion(t, server)
			if server.workspaceIndexGeneration != test.wantGeneration {
				t.Fatalf("workspace generation = %d, want %d", server.workspaceIndexGeneration, test.wantGeneration)
			}
			if server.graphGeneration != test.wantGraphGeneration {
				t.Fatalf("graph generation = %d, want %d", server.graphGeneration, test.wantGraphGeneration)
			}
		})
	}
}

func TestDidChangeConfigurationGraphSettingInvalidatesGraphOnly(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	server.workspaceIndexEnabled = true
	server.workspaceIndexGeneration = 7
	server.graphGeneration = 11
	t.Cleanup(server.stopWorkspaceIndexWorkers)

	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"graph": map[string]any{"showRootNodes": false},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.workspaceIndexGeneration != 7 {
		t.Fatalf("graph-only configuration advanced workspace generation to %d", server.workspaceIndexGeneration)
	}
	if server.graphGeneration != 12 {
		t.Fatalf("graph-only configuration generation = %d, want 12", server.graphGeneration)
	}
}

func TestDidChangeConfigurationReadsExcelSettings(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	params := json.RawMessage(`{"settings":{"aspLsp":{"excel":{"locale":"en","includeRelatedIncludeTreesForUnresolved":false,"skipTypeInference":true}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	if server.settings.ExcelLocale != "en" ||
		server.settings.ExcelIncludeRelatedIncludeTrees ||
		!server.settings.ExcelSkipTypeInference {
		t.Fatalf("Excel settings = %#v", server.settings)
	}
}

func TestVBScriptTypeDiagnosticsStrictComMembers(t *testing.T) {
	server := New(bytes.NewReader(nil), bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	params := json.RawMessage(`{"settings":{"aspLsp":{"vbscript":{"typeChecking":"strict","comTypes":{"Custom.Widget":{"members":{"Child":"Custom.Child","Title":"String","Ping":{"kind":"method","returnType":"Boolean","parameters":[{"name":"name","type":"String"}]}}},"Custom.Child":{"members":{"Name":"String"}}}}}}}`)
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatalf("didChangeConfiguration failed: %v", err)
	}
	parsed := core.ParseDocument("file:///tmp/type.asp", `<%
Dim widget
Set widget = Server.CreateObject("Custom.Widget")
widget.
widget.Missing
widget.Ping("a", "b")
%>`, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want 2", diagnostics)
	}
}
