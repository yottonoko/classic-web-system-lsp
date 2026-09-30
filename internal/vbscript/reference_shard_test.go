package vbscript

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestReferenceShardClassifiesReferenceRolesAndScopes(t *testing.T) {
	const source = `<%
Function MakeCustomer()
  Set MakeCustomer = New Customer
  MakeCustomer = MakeCustomer()
End Function
Class Customer
  Public Name
  Public Default Property Get DisplayName()
    DisplayName = Name
  End Property
End Class
''' <see cref="Customer.Name" />
%>`
	parsed := core.ParseDocument("file:///tmp/reference-shard.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)

	makeCustomer := shard.PostingsFor("MAKECUSTOMER")
	if len(makeCustomer) != 4 {
		t.Fatalf("MakeCustomer postings = %#v, want declaration, two return writes, and call", makeCustomer)
	}
	assertPostingRole(t, makeCustomer, ReferenceRoleDeclaration, 1)
	assertPostingRole(t, makeCustomer, ReferenceRoleFunctionReturn|ReferenceRoleWrite, 2)
	assertPostingRole(t, makeCustomer, ReferenceRoleObjectInitialization, 2)
	assertPostingRole(t, makeCustomer, ReferenceRoleFunctionReturn|ReferenceRoleWrite, 3)
	assertPostingRole(t, makeCustomer, ReferenceRoleCall|ReferenceRoleRead, 3)
	for _, posting := range makeCustomer[1:] {
		if posting.Scope != "MakeCustomer" || posting.ScopeKind != "function" {
			t.Fatalf("MakeCustomer scope = %q/%q, want function MakeCustomer", posting.ScopeKind, posting.Scope)
		}
	}

	customer := shard.PostingsFor("Customer")
	assertPostingRole(t, customer, ReferenceRoleRead, 2)
	assertPostingRole(t, customer, ReferenceRoleCref|ReferenceRoleRead, 11)
	name := shard.PostingsFor("Name")
	assertPostingRole(t, name, ReferenceRoleDeclaration, 6)
	assertPostingRole(t, name, ReferenceRoleCref|ReferenceRoleRead, 11)
	if posting := postingAtLine(t, name, 6); posting.ClassOwner != "Customer" {
		t.Fatalf("class member owner = %q, want Customer", posting.ClassOwner)
	}
	if posting := postingAtLine(t, name, 11); posting.Owner != "customer" {
		t.Fatalf("dotted cref owner = %q, want customer", posting.Owner)
	}
	displayName := shard.PostingsFor("DisplayName")
	if posting := postingAtLine(t, displayName, 8); posting.Scope != "DisplayName" || posting.ScopeKind != "property-get" {
		t.Fatalf("property posting scope = %#v", posting)
	}
	if posting := postingAtLine(t, displayName, 8); posting.ClassOwner != "Customer" {
		t.Fatalf("property posting class owner = %q, want Customer", posting.ClassOwner)
	}
}

func TestReferenceShardPromotionPreservesRuntimeOwnerIdentity(t *testing.T) {
	const source = "<% Dim value : value = 1 %>\n<div>a</div>"
	first := core.ParseDocument("file:///tmp/reference-owner.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	firstShard := BuildReferenceShard(first)
	document := core.NewTextDocument(first.URI, "classic-asp", 1, source)
	start := len(source) - len("a</div>")
	changeRange := document.Range(start, start+1)
	result := core.UpdateParsedDocument(first, []core.IncrementalChange{{Range: &changeRange, Text: "b"}}, core.Settings{DefaultLanguage: "VBScript"})
	if !result.Incremental || result.Parsed == nil {
		t.Fatalf("test update was not incremental: %#v", result)
	}
	second := result.Parsed
	secondShard := BuildReferenceShard(second)
	if secondShard != firstShard {
		t.Fatal("unchanged VBScript reference shard was not reused")
	}
	firstOwners := first.RuntimeAnalysisMemoryOwners()
	secondOwners := second.RuntimeAnalysisMemoryOwners()
	if len(secondOwners) != 1 {
		t.Fatalf("reference shard owners = first %#v second %#v, want one promoted owner", firstOwners, secondOwners)
	}
	// The first document also owns its cached source text document.
	shared := false
	for _, owner := range firstOwners {
		shared = shared || owner.Identity == secondOwners[0].Identity
	}
	if !shared {
		t.Fatal("reference shard promotion replaced the shared runtime owner identity")
	}
}

func TestReferenceShardClassifiesInlineAndMemberWritesAndObjectFactoryCalls(t *testing.T) {
	const source = `<%
If enabled Then Set customer.Name = CreateObject ("Customer.Name")
%>`
	parsed := core.ParseDocument("file:///tmp/reference-shard-inline.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	name := postingAtLine(t, shard.PostingsFor("Name"), 1)
	if !name.HasRole(ReferenceRoleWrite) || name.Owner != "customer" {
		t.Fatalf("member write = %#v", name)
	}
	assertPostingRole(t, shard.PostingsFor("CreateObject"), ReferenceRoleCall|ReferenceRoleRead, 1)
}

func TestReferenceShardFlatPathPreservesFallbackDeclarationsAndMemberOwners(t *testing.T) {
	const source = `<%
Dim first(upperBound), second
first = second
result = customer.Name
result = customer.Property
result = customer.Profile.Title
message = "Sub Function Class Property With"
' Function fake
%><%= customer.Name %>`
	parsed := core.ParseDocument("file:///tmp/reference-shard-flat.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	if shard.documentCST != nil {
		t.Fatal("flat reference shard eagerly retained a document CST")
	}
	for _, name := range []string{"first", "second"} {
		declaration, ok := shard.Declarations[name]
		if !ok || declaration.Kind != "variable" {
			t.Fatalf("flat %s declaration = %#v, want variable", name, declaration)
		}
	}
	if posting := postingAtLine(t, shard.PostingsFor("first"), 1); !posting.HasRole(ReferenceRoleDeclaration) {
		t.Fatalf("first declaration posting = %#v", posting)
	}
	if posting := postingAtLine(t, shard.PostingsFor("first"), 2); !posting.HasRole(ReferenceRoleWrite) {
		t.Fatalf("first assignment posting = %#v", posting)
	}
	namePostings := shard.PostingsFor("name")
	if len(namePostings) != 2 {
		t.Fatalf("flat dotted Name postings = %#v, want two", namePostings)
	}
	for _, posting := range namePostings {
		if posting.Owner != "customer" {
			t.Fatalf("flat dotted Name owner = %q, want customer", posting.Owner)
		}
	}
	if propertyPostings := shard.PostingsFor("property"); len(propertyPostings) != 1 || propertyPostings[0].Owner != "customer" {
		t.Fatalf("flat dotted Property postings = %#v, want one customer-owned posting", propertyPostings)
	}
	if titlePostings := shard.PostingsFor("title"); len(titlePostings) != 1 || titlePostings[0].Owner != "customer.profile" {
		t.Fatalf("flat chained member owner = %#v, want customer.profile", titlePostings)
	}
	root := ParseDocumentCST(parsed)
	if root == nil || shard.documentCST != root {
		t.Fatalf("flat CST attachment = returned(%p) retained(%p), want one shared root", root, shard.documentCST)
	}
	if ParseDocumentCST(parsed) != root {
		t.Fatal("flat ParseDocumentCST rebuilt the lazily attached root")
	}
}

func TestReferenceShardFlatPathPreservesContinuedDimDeclarations(t *testing.T) {
	const source = `<%
Dim first(upperBound) _
  , second, third
first = second
%>`
	parsed := core.ParseDocument("file:///tmp/reference-shard-flat-continuation.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	for _, name := range []string{"first", "second", "third"} {
		if declaration, ok := shard.Declarations[name]; !ok || declaration.Kind != "variable" {
			t.Fatalf("continued Dim %s declaration = %#v, want variable", name, declaration)
		}
	}
}

func TestReferenceShardStructuredPathPreservesScopesWithAndASPExpressions(t *testing.T) {
	const source = `<%
Class Widget
  Public Value
  Public Property Get DisplayName()
    With widget
      .Value = Value
    End With
  End Property
End Class
Sub Render()
  Widget.Value
End Sub
Function Build(value)
  Build = value
End Function
%><%= Widget.Value %>`
	parsed := core.ParseDocument("file:///tmp/reference-shard-structured.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	if shard.documentCST == nil {
		t.Fatal("structured reference shard did not retain its document CST")
	}
	if got := ParseDocumentCST(parsed); got != shard.documentCST {
		t.Fatal("structured ParseDocumentCST did not reuse the retained CST")
	}
	if len(shard.Scopes) != 4 {
		t.Fatalf("structured scopes = %#v, want class, property, sub, and function", shard.Scopes)
	}
	for _, expected := range []struct {
		name string
		kind string
	}{
		{name: "Widget", kind: "class"},
		{name: "DisplayName", kind: "property-get"},
		{name: "Render", kind: "sub"},
		{name: "Build", kind: "function"},
	} {
		found := false
		for _, scope := range shard.Scopes {
			if scope.Name == expected.name && scope.Kind == expected.kind {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing structured scope %s/%s in %#v", expected.name, expected.kind, shard.Scopes)
		}
	}
	valuePostings := shard.PostingsFor("value")
	var withMember, expressionMember bool
	for _, posting := range valuePostings {
		if posting.Owner == "widget" && posting.ScopeKind == "property-get" {
			withMember = true
		}
		if posting.Owner == "widget" && posting.Scope == "" {
			expressionMember = true
		}
	}
	if !withMember || !expressionMember {
		t.Fatalf("structured With/expression owners missing: %#v", valuePostings)
	}
}

func TestReferenceShardStructuredPathPreservesASPRegionAndCommentBoundaries(t *testing.T) {
	for _, test := range []struct {
		name      string
		source    string
		posting   string
		wantScope string
	}{
		{
			name:      "unterminated literal",
			source:    `<% Function F(): x = "unfinished %><% End Function %><% y = 1 %>`,
			posting:   "y",
			wantScope: "",
		},
		{
			name:      "apostrophe comment continuation",
			source:    "<% x = 1 ' comment _\nSub Render()\n  value = 1\nEnd Sub %>",
			posting:   "value",
			wantScope: "Render",
		},
		{
			name:      "Rem comment continuation",
			source:    "<% x = 1\nRem comment _\nSub Render()\n  value = 1\nEnd Sub %>",
			posting:   "value",
			wantScope: "Render",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed := core.ParseDocument("file:///tmp/reference-shard-structured-boundaries.asp", test.source, core.Settings{DefaultLanguage: "VBScript"})
			shard := BuildReferenceShard(parsed)
			postings := shard.PostingsFor(test.posting)
			if len(postings) != 1 {
				t.Fatalf("%s postings = %#v, want one", test.posting, postings)
			}
			if postings[0].Scope != test.wantScope {
				t.Fatalf("%s scope = %q, want %q: %#v", test.posting, postings[0].Scope, test.wantScope, postings[0])
			}
		})
	}
}

func TestReferenceShardProvidesStablePerNameMetadata(t *testing.T) {
	const source = `<%
Function Build(value)
  Build = value
  result = Build(value)
End Function
customer.Build(value)
%>`
	parsed := core.ParseDocument("file:///tmp/reference-shard-metadata.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)

	if names := shard.NormalizedNames(); !sort.StringsAreSorted(names) {
		t.Fatalf("normalized names are not sorted: %#v", names)
	}
	summary, ok := shard.SummaryFor("BUILD")
	if !ok || summary.Postings != 4 || summary.Roles.Declarations != 1 || summary.Roles.Calls != 2 || !summary.Declared {
		t.Fatalf("Build summary = %#v", summary)
	}
	if got := shard.DeclarationRangesFor("build"); len(got) != 1 || got[0].Start.Line != 1 {
		t.Fatalf("Build declaration ranges = %#v", got)
	}
	groups := shard.CallGroupsFor("build")
	if len(groups) != 2 || groups[0].Scope != "" || groups[0].Owner != "customer" || groups[1].Scope != "build" || groups[1].Owner != "" {
		t.Fatalf("Build call groups = %#v", groups)
	}
	postings := shard.PostingsFor("build")
	for _, group := range groups {
		for _, index := range group.PostingIndexes {
			if index < 0 || index >= len(postings) || !postings[index].HasRole(ReferenceRoleCall) {
				t.Fatalf("call group index %d does not address a call in %#v", index, postings)
			}
		}
	}
}

func TestReferenceShardEstimateBytesDoesNotMaterializeMetadata(t *testing.T) {
	shard := referenceShardForEstimate(1, 1, 1)
	before := shard.EstimateBytes()
	if shard.metadataReady || shard.metadata.names != nil || shard.metadata.summaries != nil {
		t.Fatal("estimating an unmaterialized shard initialized metadata")
	}
	if got := shard.EstimateBytes(); got != before {
		t.Fatalf("repeated unmaterialized estimate changed from %d to %d", before, got)
	}

	shard.NormalizedNames()
	after := shard.EstimateBytes()
	if after <= before {
		t.Fatalf("materialized metadata did not increase estimate: before=%d after=%d", before, after)
	}
}

func TestReferenceShardEstimateBytesCachesDocumentCSTEstimate(t *testing.T) {
	parsed := core.ParseDocument("file:///tmp/reference-shard-cst-estimate.asp", "<% Function Build(value) : Build = value : End Function %>", core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	first := shard.EstimateBytes()
	if shard.documentCST == nil || shard.documentCSTEstimateFor != shard.documentCST || shard.documentCSTEstimate <= 0 {
		t.Fatalf("document CST estimate cache = root(%p) cached(%p) bytes(%d), want populated", shard.documentCST, shard.documentCSTEstimateFor, shard.documentCSTEstimate)
	}

	var repeated int64
	allocs := testing.AllocsPerRun(100, func() {
		repeated = shard.EstimateBytes()
	})
	if repeated != first {
		t.Fatalf("repeated shard estimate = %d, want %d", repeated, first)
	}
	if allocs != 0 {
		t.Fatalf("repeated shard estimate allocations = %f, want zero", allocs)
	}
}

func TestReferenceShardEstimateBytesIncludesLateAttachedCST(t *testing.T) {
	const source = "<% Dim value : value = value + 1 %>"
	parsed := core.ParseDocument("file:///tmp/reference-shard-late-cst.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	built := BuildReferenceShard(parsed)
	restoredDocument := core.ParseDocument(parsed.URI, source, core.Settings{DefaultLanguage: "VBScript"})
	restoredShard := &ReferenceShard{
		Version:      built.Version,
		Declarations: built.Declarations,
		Postings:     built.Postings,
		Scopes:       built.Scopes,
	}
	SeedReferenceShard(restoredDocument, restoredShard)
	restored := BuildReferenceShard(restoredDocument)
	before := restored.EstimateBytes()
	if restored.documentCST != nil || restored.documentCSTEstimateFor != nil {
		t.Fatal("restored shard unexpectedly retained a document CST before attachment")
	}

	root := ParseDocumentCST(restoredDocument)
	if root == nil {
		t.Fatal("late document CST attachment returned nil")
	}
	after := restored.EstimateBytes()
	if got, want := after-before, root.EstimateBytes(); got != want {
		t.Fatalf("late CST attachment increased estimate by %d, want %d", got, want)
	}
	if restored.documentCSTEstimateFor != root {
		t.Fatalf("cached CST estimate root = %p, want %p", restored.documentCSTEstimateFor, root)
	}
}

func TestReferenceShardReleaseDocumentCSTAllowsReattachment(t *testing.T) {
	parsed := core.ParseDocument("file:///tmp/reference-shard-release-cst.asp", "<% If ready Then value = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	first := ParseDocumentCST(parsed)
	if first == nil || shard.documentCST != first {
		t.Fatal("reference shard did not retain its initial document CST")
	}
	_ = shard.EstimateBytes()

	shard.ReleaseDocumentCST()
	if shard.documentCST != nil || shard.documentCSTEstimateFor != nil || shard.documentCSTEstimate != 0 {
		t.Fatalf("released document CST state = root(%p) cached(%p) bytes(%d), want empty", shard.documentCST, shard.documentCSTEstimateFor, shard.documentCSTEstimate)
	}

	second := ParseDocumentCST(parsed)
	if second == nil || second == first || shard.documentCST != second {
		t.Fatalf("reattached document CST = returned(%p) retained(%p) initial(%p)", second, shard.documentCST, first)
	}
}

func TestReferenceShardMemoryOwnerGenerationTracksCSTAndMetadata(t *testing.T) {
	parsed := core.ParseDocument("file:///tmp/reference-shard-owner-generation.asp", "<% Function Build(value) : Build = value : End Function %>", core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	withCST := parsed.RuntimeAnalysisMemoryOwners()
	withCSTBytes := runtimeOwnerBytesTotal(withCST)

	shard.ReleaseDocumentCST()
	withoutCST := parsed.RuntimeAnalysisMemoryOwners()
	withoutCSTBytes := runtimeOwnerBytesTotal(withoutCST)
	if withoutCSTBytes >= withCSTBytes {
		t.Fatalf("runtime owners after CST release = %d bytes, want less than %d", withoutCSTBytes, withCSTBytes)
	}
	if len(withCST) > 0 && len(withoutCST) > 0 && &withCST[0] == &withoutCST[0] {
		t.Fatal("parsed runtime owner cache was reused after CST release")
	}

	if _, ok := shard.SummaryFor("value"); !ok {
		t.Fatal("materializing reference metadata did not find value")
	}
	withMetadata := parsed.RuntimeAnalysisMemoryOwners()
	withMetadataBytes := runtimeOwnerBytesTotal(withMetadata)
	if withMetadataBytes <= withoutCSTBytes {
		t.Fatalf("runtime owners after metadata materialization = %d bytes, want more than %d", withMetadataBytes, withoutCSTBytes)
	}
}

func TestReferenceShardReleaseDocumentCSTSynchronizesConcurrentEstimates(t *testing.T) {
	parsed := core.ParseDocument("file:///tmp/reference-shard-release-concurrent.asp", "<% Dim value : value = value + 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(2)
		go func() {
			defer group.Done()
			<-start
			_ = shard.EstimateBytes()
		}()
		go func() {
			defer group.Done()
			<-start
			shard.ReleaseDocumentCST()
		}()
	}
	close(start)
	group.Wait()

	shard.ReleaseDocumentCST()
	if shard.documentCST != nil || shard.documentCSTEstimateFor != nil {
		t.Fatal("concurrent release retained a document CST")
	}
	if root := ParseDocumentCST(parsed); root == nil || shard.documentCST != root {
		t.Fatal("document CST did not reattach after concurrent release")
	}
}

func runtimeOwnerBytesTotal(owners []core.RuntimeAnalysisMemoryOwner) int64 {
	var total int64
	for _, owner := range owners {
		total += owner.Bytes
	}
	return total
}

func TestReferenceShardEstimateBytesTracksMaterializedMetadataGrowth(t *testing.T) {
	smallNames := referenceShardForEstimate(1, 1, 8)
	largeNames := referenceShardForEstimate(32, 1, 8)
	smallNames.NormalizedNames()
	largeNames.NormalizedNames()
	if got, want := largeNames.EstimateBytes(), smallNames.EstimateBytes(); got <= want {
		t.Fatalf("name and summary growth did not increase estimate: small=%d large=%d", want, got)
	}

	smallCollections := referenceShardForEstimate(1, 1, 1)
	largeCollections := referenceShardForEstimate(1, 8, 32)
	smallCollections.NormalizedNames()
	largeCollections.NormalizedNames()
	largeSummary, ok := largeCollections.SummaryFor("name-000")
	if !ok || len(largeSummary.DeclarationRanges) != 8 || len(largeSummary.GlobalResolutions) != 40 || len(largeSummary.CallGroups) != 1 || len(largeSummary.CallGroups[0].PostingIndexes) != 32 {
		t.Fatalf("materialized summary collections = %#v", largeSummary)
	}
	if got, want := largeCollections.EstimateBytes(), smallCollections.EstimateBytes(); got <= want {
		t.Fatalf("range, resolution, and posting-index growth did not increase estimate: small=%d large=%d", want, got)
	}
}

func TestReferenceShardEstimateBytesConcurrentWithMetadataMaterialization(t *testing.T) {
	shard := referenceShardForEstimate(16, 4, 16)
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		group.Add(3)
		go func() {
			defer group.Done()
			_ = shard.EstimateBytes()
		}()
		go func() {
			defer group.Done()
			_ = shard.NormalizedNames()
		}()
		go func() {
			defer group.Done()
			_, _ = shard.SummaryFor("name-000")
		}()
	}
	group.Wait()
	if !shard.metadataReady {
		t.Fatal("concurrent metadata access did not materialize metadata")
	}
}

func referenceShardForEstimate(nameCount, declarationCount, callCount int) *ReferenceShard {
	shard := &ReferenceShard{
		Version:      referenceShardVersion,
		Declarations: make(map[string]Symbol, nameCount),
		Postings:     make(map[string][]ReferencePosting, nameCount),
	}
	for nameIndex := 0; nameIndex < nameCount; nameIndex++ {
		name := fmt.Sprintf("name-%03d", nameIndex)
		shard.Declarations[name] = Symbol{Name: name, Kind: "variable", Range: lsp.Range{Start: lsp.Position{Line: nameIndex}, End: lsp.Position{Line: nameIndex, Character: 4}}}
		postings := make([]ReferencePosting, 0, declarationCount+callCount)
		for declarationIndex := 0; declarationIndex < declarationCount; declarationIndex++ {
			postings = append(postings, ReferencePosting{
				Name:  name,
				Range: lsp.Range{Start: lsp.Position{Line: declarationIndex}, End: lsp.Position{Line: declarationIndex, Character: 4}},
				Roles: ReferenceRoleDeclaration,
			})
		}
		for callIndex := 0; callIndex < callCount; callIndex++ {
			postings = append(postings, ReferencePosting{
				Name:      name,
				Range:     lsp.Range{Start: lsp.Position{Line: declarationCount + callIndex}, End: lsp.Position{Line: declarationCount + callIndex, Character: 4}},
				Roles:     ReferenceRoleRead | ReferenceRoleCall,
				Scope:     "Run",
				ScopeKind: "Sub",
				Owner:     "service",
			})
		}
		shard.Postings[name] = postings
	}
	return shard
}

func TestGlobalReferenceResolutionsSeparateClassAndProcedureShadows(t *testing.T) {
	postings := []ReferencePosting{
		{Name: "Utility", Roles: ReferenceRoleDeclaration},
		{Name: "Utility", Roles: ReferenceRoleDeclaration, ClassOwner: "LocalContainer", Scope: "Run", ScopeKind: "sub"},
		{Name: "Utility", Roles: ReferenceRoleRead | ReferenceRoleCall, ClassOwner: "localcontainer", Scope: "run", ScopeKind: "sub"},
		{Name: "Utility", Roles: ReferenceRoleRead | ReferenceRoleCall, ClassOwner: "LocalContainer", Scope: "Configure", ScopeKind: "sub"},
		{Name: "Utility", Roles: ReferenceRoleRead | ReferenceRoleCall, ClassOwner: "UnshadowedContainer", Scope: "Run", ScopeKind: "sub"},
		{Name: "Utility", Roles: ReferenceRoleDeclaration, ClassOwner: "MemberContainer", Scope: "MemberContainer", ScopeKind: "class"},
		{Name: "Utility", Roles: ReferenceRoleRead | ReferenceRoleCall, ClassOwner: "membercontainer", Scope: "Run", ScopeKind: "sub"},
		{Name: "Utility", Roles: ReferenceRoleRead | ReferenceRoleCall, Owner: "service", ClassOwner: "UnshadowedContainer", Scope: "Run", ScopeKind: "sub"},
		{Name: "Utility", Roles: ReferenceRoleDeclaration, ClassOwner: "Kontainer", Scope: "Run", ScopeKind: "sub"},
		{Name: "Utility", Roles: ReferenceRoleRead | ReferenceRoleCall, ClassOwner: "Kontainer", Scope: "run", ScopeKind: "sub"},
	}
	want := []bool{true, false, false, true, true, false, false, false, false, false}
	got := GlobalReferenceResolutions(postings)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global resolutions = %#v, want %#v; fold keys = %q/%q", got, want, referenceFoldKey(postings[8].ClassOwner), referenceFoldKey(postings[9].ClassOwner))
	}
	for index, posting := range postings {
		if legacy := PostingResolvesToGlobal(posting, postings); got[index] != legacy {
			t.Fatalf("global resolution %d = %v, legacy = %v", index, got[index], legacy)
		}
	}
}

func TestReferenceCountFingerprintIncludesShadowResolution(t *testing.T) {
	shadowed := []ReferencePosting{
		{Name: "Utility", Roles: ReferenceRoleDeclaration, ClassOwner: "Container", Scope: "Run", ScopeKind: "sub"},
		{Name: "Utility", Roles: ReferenceRoleRead | ReferenceRoleCall, ClassOwner: "Container", Scope: "Run", ScopeKind: "sub"},
	}
	unshadowed := append([]ReferencePosting(nil), shadowed...)
	unshadowed[1].Scope = "Configure"
	shadowedSummary := summarizeReferenceName("utility", shadowed, Symbol{}, false)
	unshadowedSummary := summarizeReferenceName("utility", unshadowed, Symbol{}, false)
	if shadowedSummary.CountFingerprint == unshadowedSummary.CountFingerprint {
		t.Fatal("shadow resolution did not change the count fingerprint")
	}
}

func TestReferenceShardSeparatesCountAndLocationFingerprints(t *testing.T) {
	before := BuildReferenceShard(core.ParseDocument("file:///tmp/before.asp", "<% Dim value : value = value + 1 %>", core.Settings{DefaultLanguage: "VBScript"}))
	shifted := BuildReferenceShard(core.ParseDocument("file:///tmp/shifted.asp", "\n<% Dim value : value = value + 1 %>", core.Settings{DefaultLanguage: "VBScript"}))
	changed := BuildReferenceShard(core.ParseDocument("file:///tmp/changed.asp", "<% Dim value : value = 1 %>", core.Settings{DefaultLanguage: "VBScript"}))
	beforeSummary, _ := before.SummaryFor("value")
	shiftedSummary, _ := shifted.SummaryFor("value")
	changedSummary, _ := changed.SummaryFor("value")
	if beforeSummary.CountFingerprint != shiftedSummary.CountFingerprint {
		t.Fatal("range-only edit changed count fingerprint")
	}
	if beforeSummary.LocationFingerprint == shiftedSummary.LocationFingerprint {
		t.Fatal("range-only edit did not change location fingerprint")
	}
	if beforeSummary.CountFingerprint == changedSummary.CountFingerprint {
		t.Fatal("role/count edit did not change count fingerprint")
	}
}

func TestReferenceShardRestoresMissingMetadata(t *testing.T) {
	const source = `<% Dim value : value = value + 1 %>`
	parsed := core.ParseDocument("file:///tmp/reference-shard-legacy.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	legacy := &ReferenceShard{Version: shard.Version, Declarations: shard.Declarations, Postings: shard.Postings, Scopes: shard.Scopes}
	restored := core.ParseDocument(parsed.URI, "", core.Settings{DefaultLanguage: "VBScript"})
	SeedReferenceShard(restored, legacy)
	got := BuildReferenceShard(restored)
	if len(got.NormalizedNames()) == 0 {
		t.Fatalf("metadata was not restored: %#v", got)
	}
	if gotSummary, ok := got.SummaryFor("value"); !ok || gotSummary.Postings != 3 {
		t.Fatalf("restored summary = %#v, %v", gotSummary, ok)
	}
	if resolutions := got.GlobalResolutionsFor("value"); len(resolutions) != 3 || !resolutions[0] || !resolutions[1] || !resolutions[2] {
		t.Fatalf("restored global resolutions = %#v", resolutions)
	}
}

func TestReferenceShardPersistsAndRestores(t *testing.T) {
	const source = `<% Function Build(): Build = 1: End Function: value = Build() %>`
	parsed := core.ParseDocument("file:///tmp/persisted-reference-shard.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	want := BuildReferenceShard(parsed)
	payload, err := json.Marshal(parsed.Analysis)
	if err != nil {
		t.Fatal(err)
	}
	restored := core.ParseDocument(parsed.URI, source, core.Settings{DefaultLanguage: "VBScript"})
	if err := json.Unmarshal(payload, &restored.Analysis); err != nil {
		t.Fatal(err)
	}
	got := BuildReferenceShard(restored)
	if got.Version != want.Version || len(got.PostingsFor("build")) != len(want.PostingsFor("build")) {
		t.Fatalf("restored shard = %#v, want %#v", got, want)
	}
	if again := BuildReferenceShard(restored); again != got {
		t.Fatal("runtime reference shard cache did not reuse the decoded shard")
	}
}

func TestBuildReferenceShardSingleflightsSameParsedDocument(t *testing.T) {
	const source = `<% Dim value : value = value + 1 %>`
	parsed := core.ParseDocument("file:///tmp/reference-shard-singleflight.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	const workers = 16
	start := make(chan struct{})
	release := make(chan struct{})
	buildStarted := make(chan struct{})
	var startedOnce sync.Once
	var buildCount atomic.Int32
	builder := func(document *core.ParsedDocument) *ReferenceShard {
		buildCount.Add(1)
		startedOnce.Do(func() { close(buildStarted) })
		<-release
		return buildReferenceShard(document)
	}

	results := make(chan *ReferenceShard, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for range workers {
		go func() {
			ready.Done()
			<-start
			results <- buildReferenceShardSingleflight(parsed, builder)
		}()
	}
	ready.Wait()
	close(start)
	<-buildStarted
	close(release)

	var first *ReferenceShard
	for range workers {
		shard := <-results
		if first == nil {
			first = shard
		} else if shard != first {
			t.Fatal("concurrent callers received different reference shards")
		}
	}
	if got := buildCount.Load(); got != 1 {
		t.Fatalf("reference shard build count = %d, want 1", got)
	}
}

func TestSeedReferenceShardRestoresWithoutSource(t *testing.T) {
	const source = `<% Dim value : value = value + 1 %>`
	parsed := core.ParseDocument("file:///tmp/reference-shard-seed.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	restored := core.ParseDocument(parsed.URI, "", core.Settings{DefaultLanguage: "VBScript"})
	SeedReferenceShard(restored, shard)

	if got := BuildReferenceShard(restored); got != shard {
		t.Fatal("seeded reference shard was not reused")
	}
	if got := len(BuildReferenceShard(restored).PostingsFor("value")); got != 3 {
		t.Fatalf("seeded value postings = %d, want 3", got)
	}
}

func TestCallRangesUsesSharedShardAndPreservesArrayDeclarationBehavior(t *testing.T) {
	const source = `<%
Dim values(5)
Function LoadValue()
End Function
values(0) = LoadValue()
%>`
	parsed := core.ParseDocument("file:///tmp/reference-call-ranges.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	if got := CallRanges(parsed, "values"); len(got) != 2 {
		t.Fatalf("values call ranges = %#v, want array declaration and access", got)
	}
	if got := CallRanges(parsed, "LoadValue"); len(got) != 1 || got[0].Start.Line != 4 {
		t.Fatalf("LoadValue call ranges = %#v, want only invocation", got)
	}
}

func BenchmarkBuildReferenceShard(b *testing.B) {
	var source string
	for index := 0; index < 500; index++ {
		source += fmt.Sprintf("Function Build%d(value)\n  Build%d = value + %d\nEnd Function\nresult = Build%d(%d)\n", index, index, index, index, index)
	}
	source = "<%\n" + source + "%>"
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		parsed := core.ParseDocument("file:///tmp/reference-shard-benchmark.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		_ = BuildReferenceShard(parsed)
	}
}

func BenchmarkCallRangesWarmShard(b *testing.B) {
	const source = `<% Function Build(value): Build = value: End Function: result = Build(1) %>`
	parsed := core.ParseDocument("file:///tmp/reference-call-benchmark.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	_ = BuildReferenceShard(parsed)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		_ = CallRanges(parsed, "Build")
	}
}

func BenchmarkSymbolIndexFromReferenceShard(b *testing.B) {
	const source = `<% Dim value : value = value + 1 %>`
	parsed := core.ParseDocument("file:///tmp/reference-symbol-index-benchmark.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	shard := BuildReferenceShard(parsed)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		index := symbolIndexFromReferenceShard(shard)
		if reflect.ValueOf(index.Occurrences).Pointer() != reflect.ValueOf(shard.Postings).Pointer() {
			b.Fatal("symbol index copied the posting map")
		}
	}
}

func BenchmarkGlobalReferenceResolutionsClassScopeHeavy(b *testing.B) {
	const classes = 128
	const postingsPerProcedure = 32
	postings := make([]ReferencePosting, 0, classes*(postingsPerProcedure+1))
	for classIndex := range classes {
		classOwner := fmt.Sprintf("Container%d", classIndex)
		scope := fmt.Sprintf("Run%d", classIndex)
		postings = append(postings, ReferencePosting{
			Name: "Utility", Roles: ReferenceRoleDeclaration,
			ClassOwner: classOwner, Scope: scope, ScopeKind: "sub",
		})
		for range postingsPerProcedure {
			postings = append(postings, ReferencePosting{
				Name: "Utility", Roles: ReferenceRoleRead | ReferenceRoleCall,
				ClassOwner: classOwner, Scope: scope, ScopeKind: "sub",
			})
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		resolved := GlobalReferenceResolutions(postings)
		if len(resolved) != len(postings) || resolved[len(resolved)-1] {
			b.Fatalf("global resolutions = %#v", resolved)
		}
	}
}

func assertPostingRole(t *testing.T, postings []ReferencePosting, roles ReferenceRole, line int) {
	t.Helper()
	for _, posting := range postings {
		if posting.Range.Start.Line == line && posting.HasRole(roles) {
			return
		}
	}
	t.Fatalf("no posting at line %d has roles %b in %#v", line, roles, postings)
}

func postingAtLine(t *testing.T, postings []ReferencePosting, line int) ReferencePosting {
	t.Helper()
	for _, posting := range postings {
		if posting.Range.Start.Line == line {
			return posting
		}
	}
	t.Fatalf("no posting at line %d in %#v", line, postings)
	return ReferencePosting{}
}
