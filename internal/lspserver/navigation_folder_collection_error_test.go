package lspserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestNavigationFolderDocumentsPropagatesWalkFailure(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	walkErr := errors.New("injected workspace walk failure")
	server := newFolderCollectionErrorTestServer(t, root)
	server.workspaceWalkDirTestHook = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(sourcePath) {
			return walkErr
		}
		return nil
	}

	result := server.navigationFolderDocumentsContextWithProgressResult(context.Background(), filePathURI(root), nil)
	if result.complete || result.documents != nil || !errors.Is(result.err, walkErr) {
		t.Fatalf("folder collection = %#v; want incomplete result with walk failure and no documents", result)
	}
}

func TestNavigationFolderDocumentsPropagatesTrustedReadFailure(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	movedPath := sourcePath + ".moved"
	if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newFolderCollectionErrorTestServer(t, root)
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) != filepath.Clean(sourcePath) {
			return
		}
		if err := os.Rename(sourcePath, movedPath); err != nil {
			t.Errorf("rename source for injected read failure: %v", err)
		}
	}
	defer func() {
		if _, err := os.Stat(movedPath); err == nil {
			if renameErr := os.Rename(movedPath, sourcePath); renameErr != nil {
				t.Errorf("restore source after injected read failure: %v", renameErr)
			}
		}
	}()

	result := server.navigationFolderDocumentsContextWithProgressResult(context.Background(), filePathURI(root), nil)
	if result.complete || result.documents != nil || !errors.Is(result.err, os.ErrNotExist) {
		t.Fatalf("folder collection = %#v; want incomplete trusted read failure and no documents", result)
	}
}

func TestNavigationFolderDocumentsFilteredReadIsSkipped(t *testing.T) {
	root := t.TempDir()
	includedPath := filepath.Join(root, "included.asp")
	excludedPath := filepath.Join(root, "excluded.asp")
	for _, path := range []string{includedPath, excludedPath} {
		if err := os.WriteFile(path, []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := newFolderCollectionErrorTestServer(t, root)
	server.settings.WorkspaceExcludeGlobs = []string{"excluded.asp"}
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(excludedPath) {
			t.Errorf("filtered document was read: %s", path)
		}
	}

	result := server.navigationFolderDocumentsContextWithProgressResult(context.Background(), filePathURI(root), nil)
	if !result.complete || result.err != nil {
		t.Fatalf("filtered folder collection = %#v; want complete success", result)
	}
	if len(result.documents) != 1 || result.documents[0] == nil || filepath.Clean(fileURIPath(result.documents[0].URI)) != filepath.Clean(includedPath) {
		t.Fatalf("filtered folder documents = %#v; want only %q", result.documents, includedPath)
	}
}

func TestNavigationFolderDocumentsReadsAcceptedFilesInParallelAndKeepsOrder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.asp", "b.asp", "c.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(`<% Dim Value %>`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := newFolderCollectionErrorTestServer(t, root)
	server.analysisWorkers.setWorkers(3)
	release := make(chan struct{})
	started := make(chan struct{}, 3)
	var active atomic.Int64
	var maximum atomic.Int64
	server.workspaceFileReadTestHook = func(string) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
	}

	result := make(chan graphDocumentCollectionResult, 1)
	go func() {
		result <- server.navigationFolderDocumentsContextWithProgressResult(context.Background(), filePathURI(root), nil)
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("accepted workspace files were not read concurrently")
		}
	}
	close(release)
	collection := <-result
	if !collection.complete || collection.err != nil {
		t.Fatalf("parallel folder collection = %#v", collection)
	}
	if maximum.Load() < 2 {
		t.Fatalf("maximum concurrent reads = %d, want at least 2", maximum.Load())
	}
	got := make([]string, 0, len(collection.documents))
	for _, document := range collection.documents {
		got = append(got, filepath.Base(fileURIPath(document.URI)))
	}
	if strings.Join(got, ",") != "a.asp,b.asp,c.asp" {
		t.Fatalf("parallel folder document order = %#v", got)
	}
}

func TestNavigationFolderDocumentsCancellationReturnsNoPrefix(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newFolderCollectionErrorTestServer(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.workspaceWalkDirTestHook = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(sourcePath) {
			cancel()
		}
		return nil
	}

	result := server.navigationFolderDocumentsContextWithProgressResult(ctx, filePathURI(root), nil)
	if result.complete || result.documents != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("cancelled folder collection = %#v; want cancellation and no documents", result)
	}
}

func TestConfiguredRootDiskUniversePropagatesFolderWalkFailure(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	walkErr := errors.New("configured-root walk failure")
	server := newFolderCollectionErrorTestServer(t, root)
	server.workspaceWalkDirTestHook = func(path string) error {
		if filepath.Clean(path) == filepath.Clean(sourcePath) {
			return walkErr
		}
		return nil
	}

	result := server.workspaceGraphSourceDocumentsContextWithConfiguredRoots(context.Background(), server.graphGenerationSnapshot())
	if result.complete || result.documents != nil || !errors.Is(result.err, walkErr) {
		t.Fatalf("configured-root collection = %#v; want incomplete walk failure and no documents", result)
	}
}

func TestWorkspaceIndexRejectsWalkAndReadFailuresWithoutPartialDocuments(t *testing.T) {
	for _, failure := range []string{"walk", "read"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			sourcePath := filepath.Join(root, "source.asp")
			if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
				t.Fatal(err)
			}
			server := newFolderCollectionErrorTestServer(t, root)
			server.settings.CacheEnabled = false
			server.workspaceIndexEnabled = true
			server.workspaceIndexGeneration = 1
			movedPath := sourcePath + ".moved"
			if failure == "walk" {
				walkErr := errors.New("workspace index walk failure")
				server.workspaceWalkDirTestHook = func(path string) error {
					if filepath.Clean(path) == filepath.Clean(sourcePath) {
						return walkErr
					}
					return nil
				}
			} else {
				server.workspaceFileReadTestHook = func(path string) {
					if filepath.Clean(path) == filepath.Clean(sourcePath) {
						if err := os.Rename(sourcePath, movedPath); err != nil {
							t.Errorf("rename source for injected index read failure: %v", err)
						}
					}
				}
				defer func() {
					if _, err := os.Stat(movedPath); err == nil {
						if renameErr := os.Rename(movedPath, sourcePath); renameErr != nil {
							t.Errorf("restore source after injected index read failure: %v", renameErr)
						}
					}
				}()
			}

			documents, _, ok := server.buildWorkspaceIndex(context.Background(), 1, workspaceIndexRunSettings{
				roots: []workspaceRoot{{Path: root, URI: filePathURI(root)}},
			})
			if ok || documents != nil {
				t.Fatalf("workspace index result = ok:%t documents:%#v; want incomplete failure", ok, documents)
			}
		})
	}
}

func TestIncomingIncludeDocumentsReadFailureHasNoPartialOwnerForBothIndexModes(t *testing.T) {
	for _, useReverseIndex := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[useReverseIndex], func(t *testing.T) {
			root := t.TempDir()
			targetPath := filepath.Join(root, "target.inc")
			ownerPath := filepath.Join(root, "owner.asp")
			if err := os.WriteFile(targetPath, []byte(`<% Dim TargetValue %>`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ownerPath, []byte(`<!-- #include file="target.inc" -->`), 0o600); err != nil {
				t.Fatal(err)
			}
			server := newFolderCollectionErrorTestServer(t, root)
			server.settings.GraphUseReverseIncludeIndex = useReverseIndex
			server.workspace = map[string]*core.TextDocument{}
			targetURI := filePathURI(targetPath)
			server.documents[targetURI] = core.NewTextDocument(targetURI, "classic-asp", 1, "<% Dim OpenTargetValue %>")
			server.workspaceIncludeGraph.Reset("test")
			server.workspaceIncludeGraph.Upsert(ownerPath, workspacepkg.SourceMetadata{FileName: ownerPath}, []string{targetPath}, "test")
			server.workspaceIncludeGraphComplete = true
			beforeGraph := server.workspaceIncludeGraph
			beforeRevision := server.workspaceIncludeGraphRevision
			movedPath := ownerPath + ".moved"
			server.workspaceFileReadTestHook = func(path string) {
				if filepath.Clean(path) == filepath.Clean(ownerPath) {
					if err := os.Rename(ownerPath, movedPath); err != nil {
						t.Errorf("rename owner for injected read failure: %v", err)
					}
				}
			}
			defer func() {
				if _, err := os.Stat(movedPath); err == nil {
					if renameErr := os.Rename(movedPath, ownerPath); renameErr != nil {
						t.Errorf("restore owner after injected read failure: %v", renameErr)
					}
				}
			}()

			result := server.incomingIncludeDocumentsForTargetsContextResult(context.Background(),
				map[string]struct{}{targetPath: {}}, map[string]struct{}{targetURI: {}})
			if result.complete || result.documents != nil || !errors.Is(result.err, os.ErrNotExist) {
				t.Fatalf("incoming result = %#v; want incomplete trusted read failure and no owners", result)
			}
			if server.workspaceIncludeGraph != beforeGraph || server.workspaceIncludeGraphRevision != beforeRevision || !server.workspaceIncludeGraphComplete {
				t.Fatalf("include graph mutated after read failure: pointerChanged=%t revision=%d/%d complete=%t", server.workspaceIncludeGraph != beforeGraph, server.workspaceIncludeGraphRevision, beforeRevision, server.workspaceIncludeGraphComplete)
			}
		})
	}
}

func TestExportAnalysisExcelFolderReadFailurePreservesTarget(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.asp")
	if err := os.WriteFile(sourcePath, []byte(`<% Dim Value %>`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newFolderCollectionErrorTestServer(t, root)
	targetPath := filepath.Join(root, "analysis.xlsx")
	previous := []byte("previous workbook")
	if err := os.WriteFile(targetPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	movedPath := sourcePath + ".moved"
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(sourcePath) {
			if err := os.Rename(sourcePath, movedPath); err != nil {
				t.Errorf("rename source for injected Excel read failure: %v", err)
			}
		}
	}
	defer func() {
		if _, err := os.Stat(movedPath); err == nil {
			if renameErr := os.Rename(movedPath, sourcePath); renameErr != nil {
				t.Errorf("restore source after injected Excel read failure: %v", renameErr)
			}
		}
	}()

	result, rpcErr := server.exportAnalysisExcelRequest(context.Background(), executeCommandParams{Arguments: []any{
		analysisExcelExportArg{Scope: "folder", URI: filePathURI(root), TargetPath: targetPath},
	}})
	message := ""
	if rpcErr != nil {
		message = strings.ToLower(rpcErr.Message)
	}
	if result != nil || rpcErr == nil || rpcErr.Code != -32603 ||
		!strings.Contains(message, "no such file") && !strings.Contains(message, "cannot find the file") {
		t.Fatalf("folder Excel read failure = result:%#v error:%#v; want -32603 operational error", result, rpcErr)
	}
	contents, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(previous) {
		t.Fatalf("Excel target contents = %q, want %q", contents, previous)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".analysis.xlsx.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary Excel files = %#v, want none", matches)
	}
}

func newFolderCollectionErrorTestServer(t *testing.T, root string) *Server {
	t.Helper()
	server := New(nil, io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	t.Cleanup(server.shutdownRuntimeCaches)
	return server
}
