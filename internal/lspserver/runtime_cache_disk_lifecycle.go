package lspserver

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) closeDiskAnalysisCache() {
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	cache := s.diskAnalysisCache
	s.diskAnalysisCache = nil
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	if cache == nil {
		return
	}
	javascriptProjectIdentityMemory.Delete(cache)
	if err := cache.Flush(); err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.flush.failed: " + err.Error())
	}
	if err := cache.Close(); err != nil {
		s.logServerWarning("[asp-lsp] analysisDatabase.close.failed: " + err.Error())
	}
}

func (s *Server) sweepDiskCacheIfDue(cache *workspacepkg.DiskAnalysisCache, ttl time.Duration, maxSize int64) {
	if cache == nil || !cache.Enabled() {
		return
	}
	key := diskCacheSweepKey{directory: cache.Directory(), ttl: ttl, maxSize: maxSize}
	scheduleDiskCacheSweepWithLauncher(key, time.Now(), cache.Sweep, s.runAsyncDiskCacheWrite)
}

func scheduleDiskCacheSweep(key diskCacheSweepKey, now time.Time, sweep func() error) {
	scheduleDiskCacheSweepWithLauncher(key, now, sweep, func(task func()) bool {
		go task()
		return true
	})
}

func scheduleDiskCacheSweepWithLauncher(key diskCacheSweepKey, now time.Time, sweep func() error, launch func(func()) bool) {
	diskCacheSweeps.Lock()
	if _, running := diskCacheSweeps.inFlight[key]; running {
		diskCacheSweeps.Unlock()
		return
	}
	if last := diskCacheSweeps.last[key]; !last.IsZero() && now.Sub(last) < diskCacheSweepInterval {
		diskCacheSweeps.Unlock()
		return
	}
	diskCacheSweeps.inFlight[key] = struct{}{}
	diskCacheSweeps.Unlock()

	task := func() {
		err := sweep()
		finishedAt := time.Now()
		diskCacheSweeps.Lock()
		delete(diskCacheSweeps.inFlight, key)
		if err == nil {
			diskCacheSweeps.last[key] = finishedAt
		}
		diskCacheSweeps.Unlock()
	}
	if launch == nil || !launch(task) {
		diskCacheSweeps.Lock()
		delete(diskCacheSweeps.inFlight, key)
		diskCacheSweeps.Unlock()
	}
}

func (s *Server) scheduleLegacyDiskCacheCleanup(cache *workspacepkg.DiskAnalysisCache, configuredDirectory string) {
	if cache == nil || !cache.Enabled() {
		return
	}
	legacyRoot := diskCacheRootDirectory(configuredDirectory)
	s.runAsyncDiskCacheWrite(func() {
		if err := s.cleanupLegacyDiskCache(cache, legacyRoot); err != nil && !os.IsNotExist(err) {
			s.logServerWarning("[asp-lsp] legacyDiskCache.cleanup.failed: " + err.Error())
		}
	})
}

func (s *Server) cleanupLegacyDiskCache(cache *workspacepkg.DiskAnalysisCache, legacyRoot string) error {
	return s.cleanupLegacyDiskCacheEntries(legacyRoot, func() bool {
		return s.legacyDiskCacheCleanupCancelled(cache)
	})
}

func (s *Server) cleanupLegacyDiskCacheEntries(legacyRoot string, cancelled func() bool) error {
	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		return err
	}
	deleted := 0
	var removalErr error
	isCancelled := func() bool {
		return cancelled != nil && cancelled()
	}
	remove := func(fileName string) bool {
		if isCancelled() {
			return false
		}
		if err := os.Remove(fileName); err != nil && !os.IsNotExist(err) {
			removalErr = errors.Join(removalErr, err)
			return !isCancelled()
		}
		deleted++
		if deleted%legacyDiskCacheCleanupBatchSize == 0 {
			timer := time.NewTimer(legacyDiskCacheCleanupPause)
			<-timer.C
		}
		return !isCancelled()
	}
	for _, entry := range entries {
		if isCancelled() {
			return nil
		}
		if !entry.IsDir() {
			if strings.EqualFold(filepath.Ext(entry.Name()), ".cbor") && !remove(filepath.Join(legacyRoot, entry.Name())) {
				return nil
			}
			continue
		}
		if !isLegacyDiskCacheShard(entry.Name()) {
			continue
		}
		shardPath := filepath.Join(legacyRoot, entry.Name())
		shardEntries, readErr := os.ReadDir(shardPath)
		if readErr != nil {
			continue
		}
		for _, shardEntry := range shardEntries {
			if shardEntry.IsDir() || !strings.EqualFold(filepath.Ext(shardEntry.Name()), ".cbor") {
				continue
			}
			if !remove(filepath.Join(shardPath, shardEntry.Name())) {
				return nil
			}
		}
		_ = os.Remove(shardPath)
	}
	if deleted > 0 {
		s.logDebugSummary("[asp-lsp] legacyDiskCache.cleanup: " + strconv.Itoa(deleted))
	}
	return removalErr
}

func diskCacheRootDirectory(configuredDirectory string) string {
	root := strings.TrimSpace(configuredDirectory)
	if root == "" {
		root = filepath.Join(os.TempDir(), "asp-lsp-analysis-cache")
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return root
}

func (s *Server) legacyDiskCacheCleanupCancelled(cache *workspacepkg.DiskAnalysisCache) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diskCacheWritesClosed || s.diskAnalysisCache != cache
}

func isLegacyDiskCacheShard(name string) bool {
	if len(name) != 2 {
		return false
	}
	for _, character := range name {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}
