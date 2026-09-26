package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestVirtualDocumentEstimateBytesTracksRuntimeLineIndex(t *testing.T) {
	small := ParseDocument("file:///site/virtual-small.asp", "<div>one</div>\n", Settings{})
	large := ParseDocument("file:///site/virtual-large.asp", strings.Repeat("<div>line</div>\n", 1024), Settings{})
	smallVirtual := BuildVirtualDocument(small, LanguageHTML)
	largeVirtual := BuildVirtualDocument(large, LanguageHTML)

	if smallVirtual.runtimeDoc == nil || largeVirtual.runtimeDoc == nil {
		t.Fatal("virtual document runtime index was not initialized")
	}
	smallWant := int64(cap(smallVirtual.runtimeDoc.lineStarts))*8 + int64(cap(smallVirtual.runtimeDoc.lineASCII))
	largeWant := int64(cap(largeVirtual.runtimeDoc.lineStarts))*8 + int64(cap(largeVirtual.runtimeDoc.lineASCII))
	if got := smallVirtual.EstimateExclusiveRuntimeBytes(); got != smallWant {
		t.Fatalf("small virtual estimate = %d, want runtime line index estimate %d", got, smallWant)
	}
	if got := largeVirtual.EstimateExclusiveRuntimeBytes(); got != largeWant {
		t.Fatalf("large virtual estimate = %d, want runtime line index estimate %d", got, largeWant)
	}
	if largeVirtual.EstimateExclusiveRuntimeBytes() <= smallVirtual.EstimateExclusiveRuntimeBytes() {
		t.Fatalf("line-heavy virtual estimate did not grow: small=%d large=%d", smallVirtual.EstimateExclusiveRuntimeBytes(), largeVirtual.EstimateExclusiveRuntimeBytes())
	}

	largeOwners := large.RuntimeAnalysisMemoryOwners()
	ownerBytes := make(map[any]int64, len(largeOwners))
	for _, owner := range largeOwners {
		ownerBytes[owner.Identity] = owner.Bytes
	}
	components := []struct {
		name  string
		value any
		bytes int64
	}{
		{name: "URI", value: largeVirtual.URI, bytes: int64(len(largeVirtual.URI))*2 + 16},
		{name: "language", value: largeVirtual.LanguageID, bytes: int64(len(largeVirtual.LanguageID))*2 + 16},
		{name: "text", value: largeVirtual.Text, bytes: int64(len(largeVirtual.Text))*2 + 16},
		{name: "segments", value: largeVirtual.Segments, bytes: int64(cap(largeVirtual.Segments))*int64(reflect.TypeOf(SourceMapSegment{}).Size()) + 16},
	}
	var genericBytes int64
	seen := make(map[any]struct{}, len(components))
	for _, component := range components {
		identity, ok := runtimeValueBackingIdentity(reflect.ValueOf(component.value))
		if !ok {
			t.Fatalf("virtual %s has no backing identity", component.name)
		}
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		got, ok := ownerBytes[identity]
		if !ok {
			t.Fatalf("runtime owner list omitted virtual %s backing", component.name)
		}
		if got < component.bytes {
			t.Fatalf("virtual %s owner estimate = %d, want at least %d", component.name, got, component.bytes)
		}
		genericBytes += component.bytes
	}
	var totalBytes int64
	for _, bytes := range ownerBytes {
		totalBytes += bytes
	}
	if totalBytes < largeVirtual.EstimateExclusiveRuntimeBytes()+genericBytes {
		t.Fatalf("runtime owner total = %d, want at least line index plus generic backing bytes %d", totalBytes, largeVirtual.EstimateExclusiveRuntimeBytes()+genericBytes)
	}
}

func TestVirtualDocumentEstimateBytesPreservesRemappedRuntimeDocument(t *testing.T) {
	const source = "<header>old</header><script>const value = 1;</script>"
	previous := ParseDocument("file:///site/virtual-remap.asp", source, Settings{})
	previousVirtual := BuildVirtualDocument(previous, LanguageJavaScript)
	if previousVirtual.runtimeDoc == nil || len(previousVirtual.Segments) == 0 {
		t.Fatal("JavaScript virtual document was not initialized")
	}

	start := strings.Index(source, "old")
	document := NewTextDocument(previous.URI, "classic-asp", 1, source)
	rangeValue := document.Range(start, start+len("old"))
	updated := UpdateParsedDocument(previous, []IncrementalChange{{Range: &rangeValue, Text: "old\nnew"}}, Settings{})
	if !updated.Incremental || updated.Parsed == nil {
		t.Fatalf("HTML edit did not use incremental update: incremental=%t reason=%s", updated.Incremental, updated.Reason)
	}
	currentVirtual := BuildVirtualDocument(updated.Parsed, LanguageJavaScript)
	if currentVirtual.Text != previousVirtual.Text {
		t.Fatalf("remapped JavaScript virtual text changed: previous=%q current=%q", previousVirtual.Text, currentVirtual.Text)
	}
	if currentVirtual.runtimeDoc != previousVirtual.runtimeDoc {
		t.Fatal("unaffected remapped virtual document rebuilt its runtime document")
	}
	if currentVirtual.EstimateExclusiveRuntimeBytes() != previousVirtual.EstimateExclusiveRuntimeBytes() {
		t.Fatalf("remapped runtime index estimate changed: previous=%d current=%d", previousVirtual.EstimateExclusiveRuntimeBytes(), currentVirtual.EstimateExclusiveRuntimeBytes())
	}
	delta := len("old\nnew") - len("old")
	for index, segment := range currentVirtual.Segments {
		if segment.SourceStart != previousVirtual.Segments[index].SourceStart+delta || segment.SourceEnd != previousVirtual.Segments[index].SourceEnd+delta {
			t.Fatalf("remapped segment %d = %#v, want source shift by %d from %#v", index, segment, delta, previousVirtual.Segments[index])
		}
	}
}
