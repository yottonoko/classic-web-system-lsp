package workspace

import "testing"

func TestDiskWorkspaceIndexWriteTransactionCommitIfConcurrentClose(t *testing.T) {
	for iteration := range 16 {
		cache, err := NewDiskAnalysisCache(testDiskCacheOptions(t.TempDir(), 0, 1, false))
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := cache.BeginWorkspaceIndexWrite()
		if err != nil {
			_ = cache.Close()
			t.Fatal(err)
		}
		settingsKey := "transaction-close"
		if err := transaction.QueueWorkspaceMembershipManifest(DiskWorkspaceMembershipManifest{
			SettingsKey: settingsKey,
			DocumentIDs: []string{"file:///site/transaction.asp"},
		}); err != nil {
			_ = cache.Close()
			t.Fatal(err)
		}
		if err := transaction.WriteWorkspaceIndex(DiskWorkspaceIndexCacheEntry{
			SettingsKey: settingsKey,
			Entries: []DiskWorkspaceIndexedDocument{{
				URI:      "file:///site/transaction.asp",
				FileName: "/site/transaction.asp",
				Text:     "transaction payload",
			}},
		}); err != nil {
			_ = cache.Close()
			t.Fatal(err)
		}

		allowEntered := make(chan struct{})
		allowRelease := make(chan struct{})
		commitResult := make(chan struct {
			committed bool
			err       error
		}, 1)
		go func() {
			committed, commitErr := transaction.CommitIf(func() bool {
				close(allowEntered)
				<-allowRelease
				return true
			})
			commitResult <- struct {
				committed bool
				err       error
			}{committed: committed, err: commitErr}
		}()
		<-allowEntered

		closeResult := make(chan error, 1)
		go func() { closeResult <- cache.Close() }()
		close(allowRelease)

		result := <-commitResult
		if !result.committed || result.err != nil {
			t.Fatalf("iteration %d: CommitIf() = %v, %v; want committed", iteration, result.committed, result.err)
		}
		if closeErr := <-closeResult; closeErr != nil {
			t.Fatalf("iteration %d: Close() error = %v", iteration, closeErr)
		}
	}
}

func TestDiskAnalysisCacheOversizedSweepRegistrationSurvivesClose(t *testing.T) {
	cache, err := NewDiskAnalysisCache(testDiskCacheOptions(t.TempDir(), 0, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteWorkspaceIndex(DiskWorkspaceIndexCacheEntry{
		SettingsKey: "oversized",
		Entries: []DiskWorkspaceIndexedDocument{{
			URI:      "file:///site/oversized.asp",
			FileName: "/site/oversized.asp",
			Text:     "oversized payload",
		}},
	}); err != nil {
		_ = cache.Close()
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		_ = cache.Close()
		t.Fatal(err)
	}

	// Force the already-persisted entry above the newly selected threshold.
	cache.maxSize = 1
	cache.nextSweepSize.Store(0)
	cache.mutationMu.Lock()
	cache.dbMu.RLock()
	cache.scheduleSweepIfOversizedLocked()
	cache.dbMu.RUnlock()
	if !cache.sweepScheduled.Load() {
		cache.mutationMu.Unlock()
		_ = cache.Close()
		t.Fatal("oversized cache did not register a sweep")
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- cache.Close() }()
	cache.mutationMu.Unlock()
	if closeErr := <-closeResult; closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
}
