package workspace

import "testing"

func TestDocumentStoreTouchesDocumentsReturnedByURI(t *testing.T) {
	store := NewDocumentStore()
	cached := &CachedDocument{URI: "file:///site/default.asp", Text: "text", Version: 1, Generation: 1, LastAccess: 1, ParseDepth: "full", Virtuals: map[string]any{}}
	store.Cache[cached.URI] = cached

	if got := store.CachedDocumentForURI(cached.URI); got != cached {
		t.Fatalf("cached document lookup = %#v", got)
	}
	if cached.LastAccess <= 1 {
		t.Fatalf("expected last access to be refreshed")
	}
}

func TestDocumentStoreMatchesMixedCaseFileURIAliases(t *testing.T) {
	store := NewDocumentStore()
	alias := "FiLe:///site/default.asp"
	cached := &CachedDocument{URI: alias, Text: "text", Version: 1}
	store.Cache[alias] = cached

	if got := store.CachedDocumentForURI("file:///site/default.asp"); got != cached {
		t.Fatalf("mixed-case file URI lookup = %#v, want cached document", got)
	}
	if got := store.CachedDocumentsForURI("file:///site/default.asp"); len(got) != 1 || got[0] != cached {
		t.Fatalf("mixed-case file URI documents = %#v, want cached document", got)
	}

	store.DeleteCachedDocumentsForURI("FILE:///site/default.asp")
	if len(store.Cache) != 0 {
		t.Fatalf("mixed-case file URI delete left cache entries: %#v", store.Cache)
	}
}

func TestDocumentStoreDemotesEvictableAnalysisState(t *testing.T) {
	store := NewDocumentStore()
	cached := &CachedDocument{
		URI:                  "file:///site/default.asp",
		Text:                 "<% Dim value %>",
		Parsed:               "<% Dim value %>",
		ParseDepth:           "full",
		Virtuals:             map[string]any{"html": "virtual"},
		VirtualsMaterialized: true,
		Analysis:             "analysis",
		Generation:           1,
	}

	demoted := store.Demote(cached, 42, func(_ string, text string) any { return "skeleton:" + text })
	if !demoted {
		t.Fatalf("expected demotion")
	}
	if cached.Analysis != nil || len(cached.Virtuals) != 0 || cached.VirtualsMaterialized {
		t.Fatalf("evictable state was not cleared: %#v", cached)
	}
	if cached.ParseDepth != "skeleton" || cached.Parsed != "skeleton:<% Dim value %>" {
		t.Fatalf("unexpected skeleton parse: %#v", cached)
	}
	if cached.Generation != 2 || cached.DemotedAt != 42 {
		t.Fatalf("generation/demotion time = %d/%d", cached.Generation, cached.DemotedAt)
	}
}

func TestDocumentStoreDoesNotDemoteEmptySkeleton(t *testing.T) {
	store := NewDocumentStore()
	cached := &CachedDocument{URI: "file:///site/default.asp", Text: "plain", ParseDepth: "skeleton", Virtuals: map[string]any{}, Generation: 1}
	if store.Demote(cached, 0, func(_ string, text string) any { return text }) {
		t.Fatalf("empty skeleton should not demote")
	}
	if cached.Generation != 1 {
		t.Fatalf("generation should stay unchanged")
	}
}
