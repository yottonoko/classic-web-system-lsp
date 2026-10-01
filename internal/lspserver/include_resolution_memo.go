package lspserver

import (
	"context"
	"path/filepath"
	"sync"
)

// includeResolutionMemo shares include-target resolution across one bulk pass
// over many documents. Pages in one directory usually include the same files,
// and each resolution validates trusted roots and stats candidates. The memo
// lives only as long as the pass context, so later requests still observe
// filesystem changes.
type includeResolutionMemo struct {
	mu      sync.Mutex
	entries map[includeResolutionMemoKey]*includeResolutionMemoEntry
}

// includeResolutionMemoKey uses the owner directory because every candidate
// path, including the virtual-mode owner root, derives from it.
type includeResolutionMemoKey struct {
	ownerDirectory string
	includePath    string
	mode           string
}

type includeResolutionMemoEntry struct {
	done    chan struct{}
	details includeTargetDetails
	ok      bool
}

type includeResolutionMemoContextKey struct{}

func withIncludeResolutionMemo(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if includeResolutionMemoFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, includeResolutionMemoContextKey{}, &includeResolutionMemo{entries: map[includeResolutionMemoKey]*includeResolutionMemoEntry{}})
}

func includeResolutionMemoFromContext(ctx context.Context) *includeResolutionMemo {
	if ctx == nil {
		return nil
	}
	memo, _ := ctx.Value(includeResolutionMemoContextKey{}).(*includeResolutionMemo)
	return memo
}

func newIncludeResolutionMemoKey(ownerURI, includePath, mode string) (includeResolutionMemoKey, bool) {
	ownerPath := fileURIPath(ownerURI)
	if ownerPath == "" {
		return includeResolutionMemoKey{}, false
	}
	return includeResolutionMemoKey{ownerDirectory: filepath.Dir(ownerPath), includePath: includePath, mode: mode}, true
}

// resolve returns the memoized result for key, computing it once. A result
// computed while ctx was cancelled is not kept, so it never leaks into a
// later caller.
func (memo *includeResolutionMemo) resolve(ctx context.Context, key includeResolutionMemoKey, compute func() (includeTargetDetails, bool)) (includeTargetDetails, bool) {
	memo.mu.Lock()
	if entry := memo.entries[key]; entry != nil {
		memo.mu.Unlock()
		select {
		case <-entry.done:
			return entry.details, entry.ok
		case <-ctx.Done():
			return includeTargetDetails{}, false
		}
	}
	entry := &includeResolutionMemoEntry{done: make(chan struct{})}
	memo.entries[key] = entry
	memo.mu.Unlock()
	entry.details, entry.ok = compute()
	if ctx.Err() != nil {
		memo.mu.Lock()
		delete(memo.entries, key)
		memo.mu.Unlock()
	}
	close(entry.done)
	return entry.details, entry.ok
}
