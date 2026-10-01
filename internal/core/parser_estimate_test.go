package core

import (
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type parsedEstimateRuntimeValue struct {
	bytes int64
}

func (value parsedEstimateRuntimeValue) EstimateBytes() int64 {
	return value.bytes
}

type parsedEstimateCompleteRuntimeValue struct {
	Payload []byte
	bytes   int64
}

func (value parsedEstimateCompleteRuntimeValue) EstimateBytes() int64 {
	return value.bytes
}

type parsedEstimateProviderRuntimeValue struct {
	shared *int
}

func (value parsedEstimateProviderRuntimeValue) EstimateBytes() int64 {
	return 1
}

func (value parsedEstimateProviderRuntimeValue) RuntimeAnalysisMemoryOwnerSet() []RuntimeAnalysisMemoryOwner {
	return []RuntimeAnalysisMemoryOwner{{Identity: value.shared, Bytes: 128}}
}

type parsedEstimateRuntimeOwnerProvider struct {
	owners []RuntimeAnalysisMemoryOwner
}

func (provider parsedEstimateRuntimeOwnerProvider) RuntimeAnalysisMemoryOwnerSet() []RuntimeAnalysisMemoryOwner {
	return provider.owners
}

type parsedEstimateIdentityWrapper struct {
	any any
}

func TestRuntimeAnalysisProviderMemoryOwnersRejectsDynamicallyUnhashableIdentity(t *testing.T) {
	valid := new(int)
	invalid := parsedEstimateIdentityWrapper{any: []byte("unhashable")}
	if runtimeAnalysisOwnerIdentityIsValid(invalid) {
		t.Fatal("identity wrapper with an unhashable dynamic member was accepted")
	}

	owners := runtimeAnalysisProviderMemoryOwners(parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{
		{Identity: invalid, Bytes: math.MaxInt64},
		{Identity: valid, Bytes: 64},
	}})
	if len(owners) != 1 {
		t.Fatalf("owners = %#v, want only the valid identity", owners)
	}
	if got := owners[valid]; got != 64+24 {
		t.Fatalf("valid owner bytes = %d, want 88", got)
	}
}

func TestRuntimeAnalysisProviderMemoryOwnersValidatesEntries(t *testing.T) {
	first := new(int)
	second := new(int)
	var nilPointer *int
	var nilMap map[string]int
	var nilSlice []byte
	var nilProvider *parsedEstimateRuntimeOwnerProvider

	tests := []struct {
		name     string
		provider RuntimeAnalysisMemoryOwnerProvider
		want     map[any]int64
	}{
		{
			name: "nil provider",
		},
		{
			name:     "typed nil provider",
			provider: nilProvider,
		},
		{
			name: "negative-only owner",
			provider: parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{
				{Identity: first, Bytes: -1},
			}},
		},
		{
			name: "zero-size owner",
			provider: parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{
				{Identity: first, Bytes: 0},
			}},
		},
		{
			name: "negative-first valid-second owner",
			provider: parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{
				{Identity: first, Bytes: -1},
				{Identity: second, Bytes: 64},
			}},
			want: map[any]int64{
				second: 88,
			},
		},
		{
			name: "duplicate valid owners use max and one header",
			provider: parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{
				{Identity: first, Bytes: 128},
				{Identity: first, Bytes: 256},
				{Identity: first, Bytes: 64},
			}},
			want: map[any]int64{
				first: 280,
			},
		},
		{
			name: "max-size duplicate owners saturate one header",
			provider: parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{
				{Identity: first, Bytes: math.MaxInt64},
				{Identity: first, Bytes: math.MaxInt64},
			}},
			want: map[any]int64{
				first: math.MaxInt64,
			},
		},
		{
			name: "incomparable and nil identities ignored",
			provider: parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{
				{Identity: []byte("incomparable"), Bytes: 128},
				{Identity: nil, Bytes: 128},
				{Identity: nilPointer, Bytes: 128},
				{Identity: nilMap, Bytes: 128},
				{Identity: nilSlice, Bytes: 128},
				{Identity: first, Bytes: 32},
				{Identity: second, Bytes: 16},
			}},
			want: map[any]int64{
				first:  56,
				second: 16,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runtimeAnalysisProviderMemoryOwners(test.provider)
			if len(got) != len(test.want) {
				t.Fatalf("provider owners = %#v, want %#v", got, test.want)
			}
			for identity, wantBytes := range test.want {
				gotBytes, ok := got[identity]
				if !ok || gotBytes != wantBytes {
					t.Fatalf("provider owner %p bytes = %d, present=%t; want %d", identity, gotBytes, ok, wantBytes)
				}
			}
		})
	}
}

type parsedEstimateExclusiveRuntimeValue struct {
	Payload []byte
	Extra   int64
}

func (value parsedEstimateExclusiveRuntimeValue) EstimateExclusiveRuntimeBytes() int64 {
	return value.Extra
}

type parsedEstimateNestedCompleteRuntimeValue struct{}

func (*parsedEstimateNestedCompleteRuntimeValue) EstimateBytes() int64 {
	return math.MaxInt64
}

type parsedEstimateNestedCompleteRuntimeContainer struct {
	Child *parsedEstimateNestedCompleteRuntimeValue
}

type parsedEstimateReentrantProvider struct {
	document *ParsedDocument
	once     *sync.Once
	shared   *int
}

func (provider parsedEstimateReentrantProvider) RuntimeAnalysisMemoryOwnerSet() []RuntimeAnalysisMemoryOwner {
	provider.once.Do(func() {
		provider.document.StoreAnalysis("test.reentrant.analysis", "stored")
		provider.document.StoreRuntimeAnalysis("test.reentrant.runtime", parsedEstimateRuntimeValue{bytes: 1})
	})
	return []RuntimeAnalysisMemoryOwner{{Identity: provider.shared, Bytes: 64}}
}

func TestParsedDocumentAccountingCallbacksCanReenterStores(t *testing.T) {
	tests := []struct {
		name string
		call func(*ParsedDocument) any
	}{
		{
			name: "estimate",
			call: func(parsed *ParsedDocument) any { return parsed.EstimateBytes() },
		},
		{
			name: "owners",
			call: func(parsed *ParsedDocument) any { return parsed.RuntimeAnalysisMemoryOwners() },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := ParseDocument("file:///site/runtime-reentrant-"+test.name+".asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
			provider := parsedEstimateReentrantProvider{document: parsed, once: new(sync.Once), shared: new(int)}
			parsed.StoreRuntimeAnalysis("test.reentrant.provider", provider)

			done := make(chan struct{})
			go func() {
				test.call(parsed)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("runtime accounting callback did not complete within one second")
			}
		})
	}
}

func TestParsedDocumentRuntimeAccountingSaturatesEstimatorAndOwnerBytes(t *testing.T) {
	complete := ParseDocument("file:///site/runtime-complete-max.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	complete.StoreRuntimeAnalysis("test.complete-max", parsedEstimateCompleteRuntimeValue{bytes: math.MaxInt64})
	completeOwners := complete.RuntimeAnalysisMemoryOwners()
	if len(completeOwners) != 1 || completeOwners[0].Bytes != math.MaxInt64 {
		t.Fatalf("complete max owners = %#v, want one owner capped at MaxInt64", completeOwners)
	}
	if got := complete.EstimateBytes(); got != math.MaxInt64 || got < 0 {
		t.Fatalf("complete max estimate = %d, want nonnegative MaxInt64", got)
	}

	negative := ParseDocument("file:///site/runtime-complete-negative.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	negative.StoreRuntimeAnalysis("test.complete-negative", parsedEstimateCompleteRuntimeValue{bytes: -1})
	negativeOwners := negative.RuntimeAnalysisMemoryOwners()
	if len(negativeOwners) != 1 || negativeOwners[0].Bytes != 24 {
		t.Fatalf("negative complete owners = %#v, want one 24-byte overhead owner", negativeOwners)
	}
	if got := negative.EstimateBytes(); got < 0 {
		t.Fatalf("negative complete estimate = %d, want nonnegative result", got)
	}

	nested := ParseDocument("file:///site/runtime-nested-complete-max.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	value := &parsedEstimateNestedCompleteRuntimeValue{}
	container := parsedEstimateNestedCompleteRuntimeContainer{Child: value}
	nested.StoreRuntimeAnalysis("test.nested-complete-max", container)
	nestedOwners := ownerBytesByIdentity(nested.RuntimeAnalysisMemoryOwners())
	if got := nestedOwners[backingIdentityForTest(t, value)]; got != math.MaxInt64 {
		t.Fatalf("nested complete owner bytes = %d, want exact MaxInt64 cap", got)
	}
	if got := nested.EstimateBytes(); got != math.MaxInt64 || got < 0 {
		t.Fatalf("nested complete estimate = %d, want nonnegative MaxInt64", got)
	}

	aggregated := ParseDocument("file:///site/runtime-owner-aggregate-max.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	aggregated.StoreRuntimeAnalysis("test.owner-max-one", parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{{Identity: new(int), Bytes: math.MaxInt64}}})
	aggregated.StoreRuntimeAnalysis("test.owner-max-two", parsedEstimateRuntimeOwnerProvider{owners: []RuntimeAnalysisMemoryOwner{{Identity: new(int), Bytes: math.MaxInt64}}})
	if got := aggregated.EstimateBytes(); got != math.MaxInt64 || got < 0 {
		t.Fatalf("aggregated owner estimate = %d, want nonnegative MaxInt64", got)
	}
}

type revisionSpecificRuntimeValue struct{}

func (revisionSpecificRuntimeValue) SkipPreviousRuntimeInheritance() {}

type parsedEstimateNestedRuntimeValue struct {
	Labels    []string
	Lookup    map[string]string
	Source    string
	Exclusive map[string]string
}

func TestParsedDocumentEstimateBytesTracksRetainedSourceAndAnalysis(t *testing.T) {
	small := ParseDocument("file:///site/small.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	large := ParseDocument("file:///site/large.asp", strings.Repeat("<!-- #include file=\"shared.inc\" -->\n<% Dim value : value = 1 %>\n", 128), Settings{DefaultLanguage: "VBScript"})
	large.StoreAnalysis("test.analysis", strings.Repeat("analysis", 256))
	large.StoreRuntimeAnalysis("test.runtime", strings.Repeat("runtime", 256))

	if small.EstimateBytes() <= 0 {
		t.Fatalf("small parsed estimate = %d", small.EstimateBytes())
	}
	if large.EstimateBytes() <= small.EstimateBytes() {
		t.Fatalf("large parsed estimate = %d, want greater than small estimate %d", large.EstimateBytes(), small.EstimateBytes())
	}
}

func TestParsedDocumentEstimateBytesUsesOwnedRuntimeEstimator(t *testing.T) {
	base := ParseDocument("file:///site/runtime-base.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	withRuntime := ParseDocument("file:///site/runtime-owned.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	withRuntime.StoreRuntimeAnalysis("test.owned-runtime", parsedEstimateRuntimeValue{bytes: 32 * 1024})
	if growth := withRuntime.EstimateBytes() - base.EstimateBytes(); growth < 32*1024 {
		t.Fatalf("owned runtime estimate growth = %d bytes, want at least 32768", growth)
	}
}

func TestParsedDocumentCompleteRuntimeEstimatorOverridesGenericBacking(t *testing.T) {
	parsed := ParseDocument("file:///site/runtime-complete.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreRuntimeAnalysis("test.complete-runtime", parsedEstimateCompleteRuntimeValue{
		Payload: make([]byte, 64*1024),
		bytes:   512,
	})

	owners := parsed.RuntimeAnalysisMemoryOwners()
	if len(owners) != 1 {
		t.Fatalf("complete runtime owners = %#v, want one specialized owner", owners)
	}
	if owners[0].Bytes != 512+24 {
		t.Fatalf("complete runtime owner bytes = %d, want complete estimate plus owner overhead 536", owners[0].Bytes)
	}
}

func TestParsedDocumentRuntimeOwnerProviderPrecedesCompleteEstimator(t *testing.T) {
	shared := new(int)
	parsed := ParseDocument("file:///site/runtime-provider.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreRuntimeAnalysis("test.provider-runtime", parsedEstimateProviderRuntimeValue{shared: shared})

	owners := parsed.RuntimeAnalysisMemoryOwners()
	if len(owners) != 1 {
		t.Fatalf("provider runtime owners = %#v, want one shared owner", owners)
	}
	if owners[0].Identity != shared {
		t.Fatalf("provider runtime owner identity = %#v, want %p", owners[0].Identity, shared)
	}
	if owners[0].Bytes != 128+24 {
		t.Fatalf("provider runtime owner bytes = %d, want provider estimate plus one entry overhead 152", owners[0].Bytes)
	}
}

func TestParsedDocumentRuntimeOwnerProviderHandlesNilValue(t *testing.T) {
	var value *parsedEstimateProviderRuntimeValue
	parsed := ParseDocument("file:///site/runtime-provider-nil.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreRuntimeAnalysis("test.provider-runtime", value)

	if owners := parsed.RuntimeAnalysisMemoryOwners(); len(owners) != 0 {
		t.Fatalf("nil provider runtime owners = %#v, want none", owners)
	}
}

func TestParsedDocumentExclusiveRuntimeEstimatorAddsToGenericBacking(t *testing.T) {
	payload := make([]byte, 64*1024)
	value := parsedEstimateExclusiveRuntimeValue{Payload: payload, Extra: 4096}
	parsed := ParseDocument("file:///site/runtime-exclusive.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreRuntimeAnalysis("test.exclusive-runtime", value)

	owners := parsed.RuntimeAnalysisMemoryOwners()
	ownerBytes := ownerBytesByIdentity(owners)
	if len(owners) != 2 {
		t.Fatalf("exclusive runtime owners = %#v, want root and payload owners", owners)
	}
	payloadIdentity := backingIdentityForTest(t, payload)
	payloadBytes, ok := ownerBytes[payloadIdentity]
	if !ok || payloadBytes <= int64(cap(payload)) {
		t.Fatalf("exclusive payload owner = %d, present=%t; want scalable payload backing", payloadBytes, ok)
	}
	var runtimeBytes int64
	for _, owner := range owners {
		runtimeBytes += owner.Bytes
	}
	if got := parsed.EstimateBytes() - parsed.EstimateStructuralBytes(); got != runtimeBytes {
		t.Fatalf("exclusive runtime estimate = %d, want generic backing plus exclusive owner bytes %d", got, runtimeBytes)
	}
	if got := parsed.EstimateBytes(); got < parsed.EstimateStructuralBytes()+4096+int64(cap(payload)) {
		t.Fatalf("exclusive runtime estimate = %d, want generic payload and exclusive bytes", got)
	}
}

func TestParsedDocumentDoesNotInheritRevisionSpecificRuntimeAnalysis(t *testing.T) {
	previous := ParseDocument("file:///site/runtime-revision.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previous.StoreRuntimeAnalysis("test.revision-specific", revisionSpecificRuntimeValue{})
	updated := ParseDocument("file:///site/runtime-revision.asp", "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)
	if _, ok := updated.LoadPreviousRuntimeAnalysis("test.revision-specific"); ok {
		t.Fatal("revision-specific runtime value was inherited")
	}
}

func TestParsedDocumentCountsInheritedRuntimeOwner(t *testing.T) {
	previous := ParseDocument("file:///site/runtime-shared.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	previous.StoreRuntimeAnalysis("test.shared-runtime", parsedEstimateRuntimeValue{bytes: 32 * 1024})
	updated := ParseDocument("file:///site/runtime-shared.asp", "<% Dim value : value = 1 %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)

	previousOwners := previous.RuntimeAnalysisMemoryOwners()
	updatedOwners := updated.RuntimeAnalysisMemoryOwners()
	if len(previousOwners) != 1 || len(updatedOwners) != 1 || previousOwners[0].Identity != updatedOwners[0].Identity {
		t.Fatalf("shared runtime owners = previous %#v updated %#v", previousOwners, updatedOwners)
	}
	if got := updated.EstimateBytes() - updated.EstimateStructuralBytes(); got < 32*1024 {
		t.Fatalf("inherited runtime estimate = %d bytes, want at least 32768", got)
	}
}

func TestParsedDocumentTracksNestedRuntimeBackingAcrossRevisions(t *testing.T) {
	sharedLabels := make([]string, 1024)
	sharedLookup := map[string]string{"shared": strings.Repeat("lookup", 1024)}
	sharedSource := strings.Repeat("source", 1024)
	firstExclusive := map[string]string{"first": "revision"}
	secondExclusive := map[string]string{"second": "revision"}

	previous := ParseDocument("file:///site/runtime-nested.asp", "<% Dim before %>", Settings{DefaultLanguage: "VBScript"})
	previous.StoreRuntimeAnalysis("test.nested-runtime", parsedEstimateNestedRuntimeValue{
		Labels:    sharedLabels,
		Lookup:    sharedLookup,
		Source:    sharedSource,
		Exclusive: firstExclusive,
	})
	updated := ParseDocument("file:///site/runtime-nested.asp", "<% Dim after %>", Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)
	updated.StoreRuntimeAnalysis("test.nested-runtime", parsedEstimateNestedRuntimeValue{
		Labels:    sharedLabels,
		Lookup:    sharedLookup,
		Source:    sharedSource,
		Exclusive: secondExclusive,
	})

	previousOwners := ownerBytesByIdentity(previous.RuntimeAnalysisMemoryOwners())
	updatedOwners := ownerBytesByIdentity(updated.RuntimeAnalysisMemoryOwners())
	for name, shared := range map[string]any{
		"slice":  backingIdentityForTest(t, sharedLabels),
		"map":    backingIdentityForTest(t, sharedLookup),
		"string": backingIdentityForTest(t, sharedSource),
	} {
		previousBytes, previousOK := previousOwners[shared]
		updatedBytes, updatedOK := updatedOwners[shared]
		if !previousOK || !updatedOK {
			t.Fatalf("shared %s backing owners = previous(%t) updated(%t), want both", name, previousOK, updatedOK)
		}
		if previousBytes != updatedBytes {
			t.Fatalf("shared %s backing bytes = previous(%d) updated(%d), want equal", name, previousBytes, updatedBytes)
		}
	}
	if _, found := updatedOwners[backingIdentityForTest(t, firstExclusive)]; found {
		t.Fatal("updated revision retained exclusive predecessor map backing")
	}
	if _, found := previousOwners[backingIdentityForTest(t, secondExclusive)]; found {
		t.Fatal("predecessor revision retained exclusive successor map backing")
	}

	var uniqueRuntimeBytes int64
	for _, bytes := range updatedOwners {
		uniqueRuntimeBytes += bytes
	}
	if got := updated.EstimateBytes() - updated.EstimateStructuralBytes(); got != uniqueRuntimeBytes {
		t.Fatalf("nested runtime estimate = %d, want unique owner bytes %d", got, uniqueRuntimeBytes)
	}
}

func TestParsedDocumentRuntimeOwnerEstimateScalesNestedBacking(t *testing.T) {
	small := ParseDocument("file:///site/runtime-nested-small.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	large := ParseDocument("file:///site/runtime-nested-large.asp", "<% Dim value %>", Settings{DefaultLanguage: "VBScript"})
	small.StoreRuntimeAnalysis("test.nested-runtime", parsedEstimateNestedRuntimeValue{
		Labels: []string{"small"},
		Lookup: map[string]string{"small": "small"},
		Source: "small",
	})
	large.StoreRuntimeAnalysis("test.nested-runtime", parsedEstimateNestedRuntimeValue{
		Labels: make([]string, 2048),
		Lookup: map[string]string{"large": strings.Repeat("large", 2048)},
		Source: strings.Repeat("large", 2048),
	})

	if large.EstimateBytes() <= small.EstimateBytes()+32*1024 {
		t.Fatalf("nested runtime estimate did not scale: small=%d large=%d", small.EstimateBytes(), large.EstimateBytes())
	}
}

func TestParsedDocumentStructuralOwnersDeduplicateRevisionTextBacking(t *testing.T) {
	const source = "<% Dim value %>\n<div>same</div>"
	previous := ParseDocument("file:///site/revision-text.asp", source, Settings{DefaultLanguage: "VBScript"})
	updated := ParseDocument(previous.URI, source, Settings{DefaultLanguage: "VBScript"})
	updated.inheritPreviousRevision(previous)

	owners := updated.StructuralMemoryOwners()
	if len(owners) != 1 {
		t.Fatalf("same revision text owners = %#v, want one shared backing", owners)
	}
	if owners[0].Identity != backingIdentityForTest(t, source) {
		t.Fatalf("same revision text owner identity = %#v, want source backing", owners[0].Identity)
	}
	want := updated.EstimateStructuralBytesWithoutRevisionText() + owners[0].Bytes
	if got := updated.EstimateBytes(); got != want {
		t.Fatalf("same revision text estimate = %d, want %d without double-charged predecessor text", got, want)
	}
}

func TestParsedDocumentStructuralOwnersPreserveEditedPredecessorBacking(t *testing.T) {
	const source = "<% Dim value %>\n<div>old</div>"
	previous := ParseDocument("file:///site/revision-text-edit.asp", source, Settings{DefaultLanguage: "VBScript"})
	document := NewTextDocument(previous.URI, "classic-asp", 1, previous.Text)
	start := strings.Index(source, "old")
	changeRange := document.Range(start, start+len("old"))
	result := UpdateParsedDocument(previous, []IncrementalChange{{Range: &changeRange, Text: "new"}}, Settings{DefaultLanguage: "VBScript"})
	if !result.Incremental || result.Parsed == nil {
		t.Fatalf("text edit did not retain incremental predecessor: incremental=%t reason=%s", result.Incremental, result.Reason)
	}
	updated := result.Parsed
	previousOwners := ownerBytesByIdentity(previous.StructuralMemoryOwners())
	updatedOwners := ownerBytesByIdentity(updated.StructuralMemoryOwners())
	previousTextIdentity := backingIdentityForTest(t, previous.Text)
	previousBytes, previousOK := previousOwners[previousTextIdentity]
	updatedBytes, updatedOK := updatedOwners[previousTextIdentity]
	if !previousOK || !updatedOK {
		t.Fatalf("predecessor text owner presence = previous(%t) updated(%t), want both", previousOK, updatedOK)
	}
	if previousBytes != updatedBytes {
		t.Fatalf("predecessor text owner bytes = previous(%d) updated(%d), want one charged backing", previousBytes, updatedBytes)
	}
	if len(updatedOwners) != 2 {
		t.Fatalf("edited revision text owners = %d, want current and predecessor backings", len(updatedOwners))
	}
	var unique int64
	for _, bytes := range updatedOwners {
		unique += bytes
	}
	want := updated.EstimateStructuralBytesWithoutRevisionText() + unique
	if got := updated.EstimateBytes(); got != want {
		t.Fatalf("edited revision text estimate = %d, want %d with each backing charged once", got, want)
	}
}

func ownerBytesByIdentity(owners []RuntimeAnalysisMemoryOwner) map[any]int64 {
	result := make(map[any]int64, len(owners))
	for _, owner := range owners {
		result[owner.Identity] = owner.Bytes
	}
	return result
}

func backingIdentityForTest[T any](t *testing.T, value T) any {
	t.Helper()
	identity, ok := runtimeValueBackingIdentity(reflect.ValueOf(value))
	if !ok {
		t.Fatalf("value %#v has no runtime backing identity", value)
	}
	return exportedMemoryOwnerIdentity(identity)
}

func TestParsedDocumentCloneStructuralOwnsExportedStateAndOmitsRuntimeState(t *testing.T) {
	parsed := ParseDocument("file:///site/clone.asp", "<!-- #include file=\"shared.inc\" -->\n<%", Settings{DefaultLanguage: "VBScript"})
	parsed.StoreAnalysis("test.analysis", []byte("persisted"))
	parsed.StoreRuntimeAnalysis("test.runtime", []byte("runtime"))
	parsed.ChangeImpact = IncrementalImpact{Languages: []EmbeddedLanguage{LanguageHTML}, Replacement: "replacement"}
	previous := ParseDocument(parsed.URI, "<% previous %>", Settings{DefaultLanguage: "VBScript"})
	parsed.inheritPreviousRevision(previous)

	clone := parsed.CloneStructural()
	if clone == nil || clone == parsed {
		t.Fatalf("clone = %#v, want distinct parsed document", clone)
	}
	if clone.Text != parsed.Text || clone.URI != parsed.URI || clone.DefaultLanguage != parsed.DefaultLanguage {
		t.Fatalf("clone structural identity changed: clone=%#v parsed=%#v", clone, parsed)
	}
	if len(clone.Regions) != len(parsed.Regions) || len(clone.Includes) != len(parsed.Includes) || len(clone.Errors) != len(parsed.Errors) {
		t.Fatalf("clone structural lengths differ: clone regions=%d includes=%d errors=%d, parsed regions=%d includes=%d errors=%d", len(clone.Regions), len(clone.Includes), len(clone.Errors), len(parsed.Regions), len(parsed.Includes), len(parsed.Errors))
	}
	clone.Regions[0].Start++
	clone.Includes[0].Path = "mutated.inc"
	if len(clone.Errors) > 0 {
		clone.Errors[0].Message = "mutated"
	}
	if clone.Regions[0].Start == parsed.Regions[0].Start || clone.Includes[0].Path == parsed.Includes[0].Path || len(clone.Errors) > 0 && clone.Errors[0].Message == parsed.Errors[0].Message {
		t.Fatal("clone structural slices share mutable backing")
	}
	clone.Analysis["test.analysis"][0] = 'x'
	var original []byte
	if !parsed.LoadAnalysis("test.analysis", &original) || string(original) != "persisted" {
		t.Fatalf("original analysis changed through clone: %q", original)
	}
	if _, ok := clone.LoadRuntimeAnalysis("test.runtime"); ok {
		t.Fatal("clone retained runtime analysis backing")
	}
	if _, ok := clone.PreviousRevisionText(); ok {
		t.Fatal("clone retained incremental predecessor state")
	}
	if len(clone.ChangeImpact.Languages) != 0 || clone.ChangeImpact.Replacement != "" {
		t.Fatalf("clone retained runtime change impact: %#v", clone.ChangeImpact)
	}
}
