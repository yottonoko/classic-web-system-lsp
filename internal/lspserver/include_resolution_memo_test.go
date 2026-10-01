package lspserver

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestIncludeResolutionMemoSharesResultsPerOwnerDirectory(t *testing.T) {
	ctx := withIncludeResolutionMemo(context.Background())
	if withIncludeResolutionMemo(ctx) != ctx {
		t.Fatal("nested memo context replaced the existing memo")
	}
	memo := includeResolutionMemoFromContext(ctx)
	first, ok := newIncludeResolutionMemoKey("file:///site/pages/a.asp", "common.inc", "file")
	if !ok {
		t.Fatal("file URI owner produced no memo key")
	}
	second, _ := newIncludeResolutionMemoKey("file:///site/pages/b.asp", "common.inc", "file")
	other, _ := newIncludeResolutionMemoKey("file:///site/admin/a.asp", "common.inc", "file")
	if first != second || first == other {
		t.Fatalf("memo keys first=%#v second=%#v other=%#v", first, second, other)
	}
	if _, ok := newIncludeResolutionMemoKey("untitled:Untitled-1", "common.inc", "file"); ok {
		t.Fatal("non-file owner produced a memo key")
	}

	var computed atomic.Int32
	release := make(chan struct{})
	compute := func() (includeTargetDetails, bool) {
		computed.Add(1)
		<-release
		return includeTargetDetails{Path: "/site/pages/common.inc"}, true
	}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			details, ok := memo.resolve(ctx, first, compute)
			if !ok || details.Path != "/site/pages/common.inc" {
				t.Errorf("memo result = %#v, %t", details, ok)
			}
		}()
	}
	close(release)
	group.Wait()
	if got := computed.Load(); got != 1 {
		t.Fatalf("concurrent resolutions computed %d times, want 1", got)
	}
}

func TestIncludeResolutionMemoDropsResultsComputedAfterCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx := withIncludeResolutionMemo(parent)
	memo := includeResolutionMemoFromContext(ctx)
	key, _ := newIncludeResolutionMemoKey("file:///site/a.asp", "common.inc", "virtual")
	memo.resolve(ctx, key, func() (includeTargetDetails, bool) {
		cancel()
		return includeTargetDetails{}, false
	})
	computed := false
	details, ok := memo.resolve(context.Background(), key, func() (includeTargetDetails, bool) {
		computed = true
		return includeTargetDetails{Path: "/site/common.inc"}, true
	})
	if !computed || !ok || details.Path != "/site/common.inc" {
		t.Fatalf("result after cancelled resolution = %#v, %t, recomputed=%t", details, ok, computed)
	}
}
