package workspace

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkspaceIncludeGraphTracksReverseCandidates(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	parent := filepath.Clean("/site/default.asp")
	shared := filepath.Clean("/site/shared.inc")
	other := filepath.Clean("/site/other.inc")

	graph.Reset("settings")
	graph.Upsert(parent, SourceMetadata{FileName: parent, MtimeMS: 1, Size: 10}, []string{shared}, "shared-refs")
	if got := graph.CandidatesForTargets([]string{shared}); !reflect.DeepEqual(got, []string{parent}) {
		t.Fatalf("shared candidates = %#v", got)
	}
	if got := graph.CandidatesForTargets([]string{other}); len(got) != 0 {
		t.Fatalf("other candidates = %#v", got)
	}

	graph.Upsert(parent, SourceMetadata{FileName: parent, MtimeMS: 2, Size: 12}, []string{other}, "other-refs")
	if got := graph.CandidatesForTargets([]string{shared}); len(got) != 0 {
		t.Fatalf("shared candidates after swap = %#v", got)
	}
	if got := graph.CandidatesForTargets([]string{other}); !reflect.DeepEqual(got, []string{parent}) {
		t.Fatalf("other candidates after swap = %#v", got)
	}
	entry, ok := graph.Get(parent)
	if !ok || entry.RefsFingerprint != "other-refs" {
		t.Fatalf("entry fingerprint = %#v, %v", entry, ok)
	}

	graph.Delete(parent)
	if got := graph.CandidatesForTargets([]string{other}); len(got) != 0 {
		t.Fatalf("other candidates after delete = %#v", got)
	}
}

func TestWorkspaceIncludeGraphSnapshotAndTransitiveDependents(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := filepath.Clean("/site/default.asp")
	first := filepath.Clean("/site/includes/first.inc")
	shared := filepath.Clean("/site/includes/shared.inc")
	graph.Reset("settings")
	graph.Upsert(root, SourceMetadata{FileName: root, MtimeMS: 1, Size: 10}, []string{first}, "root-refs")
	graph.Upsert(first, SourceMetadata{FileName: first, MtimeMS: 1, Size: 10}, []string{shared}, "first-refs")

	snapshot, ok := graph.Snapshot("")
	if !ok || snapshot.SettingsKey != "settings" || len(snapshot.Entries) != 2 {
		t.Fatalf("snapshot = %#v, %v", snapshot, ok)
	}
	restored := NewWorkspaceIncludeGraph()
	restored.Restore(snapshot)
	if got := restored.CandidatesForTargets([]string{shared}); !reflect.DeepEqual(got, []string{first}) {
		t.Fatalf("restored candidates = %#v", got)
	}
	if got := graph.DependentFileNamesForTargets([]string{shared}, true); !reflect.DeepEqual(got, []string{first, root}) {
		t.Fatalf("transitive dependents = %#v", got)
	}
	if !graph.DependsOnAnyTarget(root, []string{shared}, true) {
		t.Fatalf("root should transitively depend on shared")
	}
	if graph.DependsOnAnyTarget(root, []string{shared}, false) {
		t.Fatalf("root should not directly depend on shared")
	}
}

func TestWorkspaceIncludeGraphUpdatesTransitiveDependentsAfterEdgeSwap(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := filepath.Clean("/site/default.asp")
	first := filepath.Clean("/site/includes/first.inc")
	shared := filepath.Clean("/site/includes/shared.inc")
	other := filepath.Clean("/site/includes/other.inc")
	graph.Reset("settings")
	graph.Upsert(root, SourceMetadata{FileName: root, MtimeMS: 1, Size: 10}, []string{first}, "root-refs")
	graph.Upsert(first, SourceMetadata{FileName: first, MtimeMS: 1, Size: 10}, []string{shared}, "first-refs")
	if got := graph.DependentFileNamesForTargets([]string{shared}, true); !reflect.DeepEqual(got, []string{first, root}) {
		t.Fatalf("shared transitive dependents before swap = %#v", got)
	}

	graph.Upsert(first, SourceMetadata{FileName: first, MtimeMS: 2, Size: 11}, []string{other}, "other-refs")
	if got := graph.DependentFileNamesForTargets([]string{shared}, true); len(got) != 0 {
		t.Fatalf("shared transitive dependents after swap = %#v", got)
	}
	if got := graph.DependentFileNamesForTargets([]string{other}, true); !reflect.DeepEqual(got, []string{first, root}) {
		t.Fatalf("other transitive dependents after swap = %#v", got)
	}
}

func TestWorkspaceIncludeGraphHandlesTransitiveCycles(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	a := filepath.Clean("/site/a.inc")
	b := filepath.Clean("/site/b.inc")
	c := filepath.Clean("/site/c.inc")
	graph.Reset("settings")
	graph.Upsert(a, SourceMetadata{FileName: a, MtimeMS: 1, Size: 10}, []string{b}, "a-refs")
	graph.Upsert(b, SourceMetadata{FileName: b, MtimeMS: 1, Size: 10}, []string{c}, "b-refs")
	graph.Upsert(c, SourceMetadata{FileName: c, MtimeMS: 1, Size: 10}, []string{a}, "c-refs")

	if got := stringSet(graph.DependentFileNamesForTargets([]string{c}, true)); !reflect.DeepEqual(got, stringSet([]string{a, b, c})) {
		t.Fatalf("cycle transitive dependents = %#v", got)
	}
	if !graph.DependsOnAnyTarget(a, []string{c}, true) {
		t.Fatalf("a should transitively depend on c")
	}
}

func TestWorkspaceIncludeGraphEphemeralEdgesStayOutOfSnapshots(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := filepath.Clean("/site/default.asp")
	open := filepath.Clean("/site/open.asp")
	shared := filepath.Clean("/site/shared.inc")
	graph.Reset("settings")
	graph.Upsert(root, SourceMetadata{FileName: root, MtimeMS: 1, Size: 10}, []string{shared}, "root-refs")
	graph.UpsertEphemeral(open, []string{shared}, "open-refs")

	if got := graph.CandidatesForTargets([]string{shared}); !reflect.DeepEqual(got, []string{root, open}) {
		t.Fatalf("candidates with ephemeral = %#v", got)
	}
	snapshot, ok := graph.Snapshot("")
	if !ok || len(snapshot.Entries) != 1 || snapshot.Entries[0].FileName != root {
		t.Fatalf("snapshot with ephemeral = %#v, %v", snapshot, ok)
	}
	graph.ClearEphemeral()
	if got := graph.CandidatesForTargets([]string{shared}); !reflect.DeepEqual(got, []string{root}) {
		t.Fatalf("candidates after clear = %#v", got)
	}
}

func TestWorkspaceIncludeGraphClosuresAreDeterministicAndReusableForMembership(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := filepath.Clean("/site/default.asp")
	first := filepath.Clean("/site/includes/first.inc")
	second := filepath.Clean("/site/includes/second.inc")
	shared := filepath.Clean("/site/includes/shared.inc")
	page := filepath.Clean("/site/page.asp")
	graph.Reset("settings")
	graph.Upsert(root, SourceMetadata{FileName: root}, []string{first, second}, "root")
	graph.Upsert(first, SourceMetadata{FileName: first}, []string{shared}, "first")
	graph.Upsert(second, SourceMetadata{FileName: second}, []string{shared}, "second")
	graph.Upsert(page, SourceMetadata{FileName: page}, []string{root}, "page")

	forward := graph.ForwardClosure([]string{root, root})
	if want := []string{root, first, second, shared}; !reflect.DeepEqual(forward.FileNames, want) {
		t.Fatalf("forward closure = %#v, want %#v", forward.FileNames, want)
	}
	if !reflect.DeepEqual(graph.ReferenceScope([]string{root}).FileNames, forward.FileNames) {
		t.Fatalf("reference scope = %#v, want %#v", graph.ReferenceScope([]string{root}).FileNames, forward.FileNames)
	}
	if !forward.Contains(shared) || forward.Contains(page) {
		t.Fatalf("forward membership shared=%v page=%v", forward.Contains(shared), forward.Contains(page))
	}
	membership := forward.IdentityMembership()
	delete(membership, FileIdentityKeyFromFileName(shared))
	if !forward.Contains(shared) {
		t.Fatal("caller mutation changed closure membership")
	}

	reverse := graph.ReverseClosure([]string{shared})
	if want := []string{shared, first, second, root, page}; !reflect.DeepEqual(reverse.FileNames, want) {
		t.Fatalf("reverse closure = %#v, want %#v", reverse.FileNames, want)
	}
	if len(reverse.IdentityKeys) != len(reverse.FileNames) {
		t.Fatalf("identity keys = %#v for files %#v", reverse.IdentityKeys, reverse.FileNames)
	}
	if want := []string{first, second, root, page}; !reflect.DeepEqual(graph.DependentClosure([]string{shared}).FileNames, want) {
		t.Fatalf("dependent closure = %#v, want %#v", graph.DependentClosure([]string{shared}).FileNames, want)
	}
	if !reflect.DeepEqual(graph.AffectedScope([]string{shared}).FileNames, reverse.FileNames) {
		t.Fatalf("affected scope = %#v, want %#v", graph.AffectedScope([]string{shared}).FileNames, reverse.FileNames)
	}
}

func TestWorkspaceIncludeGraphClosureTracksTopologyReplacementWithoutStaleOwners(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := filepath.Clean("/site/default.asp")
	first := filepath.Clean("/site/first.inc")
	oldShared := filepath.Clean("/site/old.inc")
	newShared := filepath.Clean("/site/new.inc")
	graph.Upsert(root, SourceMetadata{FileName: root}, []string{first}, "root")
	graph.Upsert(first, SourceMetadata{FileName: first}, []string{oldShared}, "old")

	graph.Upsert(first, SourceMetadata{FileName: first}, []string{newShared}, "new")
	if got := graph.AffectedScope([]string{oldShared}).FileNames; !reflect.DeepEqual(got, []string{oldShared}) {
		t.Fatalf("old affected scope after replacement = %#v", got)
	}
	if want := []string{newShared, first, root}; !reflect.DeepEqual(graph.AffectedScope([]string{newShared}).FileNames, want) {
		t.Fatalf("new affected scope = %#v, want %#v", graph.AffectedScope([]string{newShared}).FileNames, want)
	}
}

func TestWorkspaceIncludeGraphApplyEdgeDeltaSkipsContentOnlyTopologyInvalidation(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := filepath.Clean("/site/default.asp")
	first := filepath.Clean("/site/first.inc")
	second := filepath.Clean("/site/second.inc")
	graph.UpsertWithReferences(root, SourceMetadata{FileName: root, MtimeMS: 1, Size: 10}, []string{first, second}, []IncludeReference{{Path: "first.inc", Mode: "file"}}, "old")

	affected, changed := graph.ApplyEdgeDelta(
		root,
		SourceMetadata{FileName: root, MtimeMS: 2, Size: 20, ContentHash: "new"},
		[]string{first, second, first},
		[]IncludeReference{{Path: "first.inc", Mode: "virtual"}},
		"new",
	)
	if changed || len(affected.FileNames) != 0 || len(affected.IdentityKeys) != 0 {
		t.Fatalf("content-only delta = %#v, changed=%v", affected, changed)
	}
	entry, ok := graph.Get(root)
	if !ok {
		t.Fatal("updated entry is missing")
	}
	if entry.Source.ContentHash != "new" || entry.RefsFingerprint != "new" {
		t.Fatalf("updated metadata = %#v", entry)
	}
	if want := []string{first, second}; !reflect.DeepEqual(entry.TargetFileNames, want) {
		t.Fatalf("target order = %#v, want %#v", entry.TargetFileNames, want)
	}
	if want := []IncludeReference{{Path: "first.inc", Mode: "virtual"}}; !reflect.DeepEqual(entry.References, want) {
		t.Fatalf("references = %#v, want %#v", entry.References, want)
	}
}

func TestWorkspaceIncludeGraphApplyEdgeDeltaReturnsAffectedOriginsForOrderedEdgeChange(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := filepath.Clean("/site/default.asp")
	page := filepath.Clean("/site/page.asp")
	middle := filepath.Clean("/site/middle.inc")
	first := filepath.Clean("/site/first.inc")
	second := filepath.Clean("/site/second.inc")
	graph.Upsert(root, SourceMetadata{FileName: root}, []string{middle}, "root")
	graph.Upsert(page, SourceMetadata{FileName: page}, []string{middle}, "page")
	graph.Upsert(middle, SourceMetadata{FileName: middle}, []string{first, second}, "middle")

	affected, changed := graph.ApplyEdgeDelta(
		middle,
		SourceMetadata{FileName: middle, ContentHash: "reordered"},
		[]string{second, first},
		[]IncludeReference{{Path: "second.inc", Mode: "file"}, {Path: "first.inc", Mode: "file"}},
		"reordered",
	)
	if !changed {
		t.Fatal("ordered edge change was reported as unchanged")
	}
	if want := []string{middle, root, page}; !reflect.DeepEqual(affected.FileNames, want) {
		t.Fatalf("affected origins = %#v, want %#v", affected.FileNames, want)
	}
	if !affected.Contains(root) || !affected.Contains(page) || !affected.Contains(middle) {
		t.Fatalf("affected membership = %#v", affected.IdentityMembership())
	}
	if want := []string{second, first}; !reflect.DeepEqual(graph.TargetFileNamesForOwner(middle), want) {
		t.Fatalf("updated target order = %#v, want %#v", graph.TargetFileNamesForOwner(middle), want)
	}
}

func TestWorkspaceIncludeGraphApplyEdgeDeltaHandlesDiamondAndCycle(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := filepath.Clean("/site/default.asp")
	left := filepath.Clean("/site/left.inc")
	right := filepath.Clean("/site/right.inc")
	middle := filepath.Clean("/site/middle.inc")
	leaf := filepath.Clean("/site/leaf.inc")
	graph.Upsert(root, SourceMetadata{FileName: root}, []string{left, right}, "root")
	graph.Upsert(left, SourceMetadata{FileName: left}, []string{middle}, "left")
	graph.Upsert(right, SourceMetadata{FileName: right}, []string{middle}, "right")
	graph.Upsert(middle, SourceMetadata{FileName: middle}, []string{leaf}, "middle")
	graph.Upsert(leaf, SourceMetadata{FileName: leaf}, nil, "leaf")

	affected, changed := graph.ApplyEdgeDelta(
		middle,
		SourceMetadata{FileName: middle},
		[]string{leaf, root},
		[]IncludeReference{{Path: "leaf.inc", Mode: "file"}, {Path: "/default.asp", Mode: "virtual"}},
		"cycle",
	)
	if !changed {
		t.Fatal("cycle edge addition was reported as unchanged")
	}
	if want := []string{middle, left, right, root}; !reflect.DeepEqual(affected.FileNames, want) {
		t.Fatalf("diamond/cycle affected origins = %#v, want %#v", affected.FileNames, want)
	}
	if got := graph.ForwardClosure([]string{root}).FileNames; !reflect.DeepEqual(got, []string{root, left, right, middle, leaf}) {
		t.Fatalf("cycle forward closure = %#v", got)
	}
}

func TestWorkspaceIncludeGraphApplyEdgeDeltaTreatsFirstDirectEdgesAsTopologyChange(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	owner := filepath.Clean("/site/new.inc")
	target := filepath.Clean("/site/target.inc")

	affected, changed := graph.ApplyEdgeDelta(owner, SourceMetadata{FileName: owner}, []string{target}, nil, "new")
	if !changed || !reflect.DeepEqual(affected.FileNames, []string{owner}) {
		t.Fatalf("first edge delta = %#v, changed=%v", affected, changed)
	}

	empty := filepath.Clean("/site/empty.inc")
	affected, changed = graph.ApplyEdgeDelta(empty, SourceMetadata{FileName: empty}, nil, nil, "empty")
	if changed || len(affected.FileNames) != 0 {
		t.Fatalf("first empty entry delta = %#v, changed=%v", affected, changed)
	}
}

func TestWorkspaceIncludeGraphApplyDeleteDeltaReturnsFormerDependents(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	root := "/workspace/root.asp"
	owner := "/workspace/owner.inc"
	target := "/workspace/target.inc"
	graph.Upsert(root, SourceMetadata{FileName: root}, []string{owner}, "root")
	graph.Upsert(owner, SourceMetadata{FileName: owner}, []string{target}, "owner")
	graph.Upsert(target, SourceMetadata{FileName: target}, nil, "target")

	affected, changed := graph.ApplyDeleteDelta(owner)
	if !changed {
		t.Fatal("delete delta was not reported")
	}
	if got, want := affected.FileNames, []string{owner, root}; !reflect.DeepEqual(got, want) {
		t.Fatalf("affected files = %#v, want %#v", got, want)
	}
	if _, ok := graph.Get(owner); ok {
		t.Fatal("deleted owner remained in graph")
	}
	if got := graph.DependentFileNamesForTargets([]string{target}, true); len(got) != 0 {
		t.Fatalf("deleted owner remained in reverse graph: %#v", got)
	}
}

func TestWorkspaceIncludeGraphApplyDeleteDeltaHandlesTargetOnlyEntry(t *testing.T) {
	graph := NewWorkspaceIncludeGraph()
	owner := "/workspace/default.asp"
	target := "/workspace/shared.inc"
	graph.Upsert(owner, SourceMetadata{FileName: owner}, []string{target}, "owner")

	affected, changed := graph.ApplyDeleteDelta(target)
	if !changed {
		t.Fatal("target-only delete delta was not reported")
	}
	if got, want := affected.FileNames, []string{target, owner}; !reflect.DeepEqual(got, want) {
		t.Fatalf("affected files = %#v, want %#v", got, want)
	}
	if got := graph.TargetFileNamesForOwner(owner); len(got) != 0 {
		t.Fatalf("deleted target remained on owner: %#v", got)
	}
	if got := graph.DependentFileNamesForTargets([]string{target}, true); len(got) != 0 {
		t.Fatalf("deleted target retained reverse owners: %#v", got)
	}
}

func stringSet(values []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
