package lspserver

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

const vbReferenceDocumentFactsAnalysisKey = "lspserver.vb-reference-document-facts.v1"

type vbReferenceDocumentFacts struct {
	DeclarationRanges          map[string][]lsp.Range `json:"declarationRanges"`
	ObjectInitializationRanges map[string][]lsp.Range `json:"objectInitializationRanges"`
}

type vbReferenceDocumentIndex struct {
	declarationRanges          map[string]map[lsp.Range]struct{}
	objectInitializationRanges map[string]map[lsp.Range]struct{}
}

func (index *vbReferenceDocumentIndex) EstimateBytes() int64 {
	if index == nil {
		return 0
	}
	return estimateVBReferenceRangeIndexBytes(index.declarationRanges) +
		estimateVBReferenceRangeIndexBytes(index.objectInitializationRanges) + 64
}

func estimateVBReferenceRangeIndexBytes(index map[string]map[lsp.Range]struct{}) int64 {
	bytes := int64(64 + len(index)*48)
	for name, ranges := range index {
		bytes += int64(len(name))*2 + 64 + int64(len(ranges))*40
	}
	return bytes
}

func vbReferenceObjectInitializationRanges(parsed *core.ParsedDocument, name string) map[lsp.Range]struct{} {
	return vbReferenceDocumentIndexFor(parsed).objectInitializationRanges[strings.ToLower(name)]
}

func vbReferenceDocumentIndexFor(parsed *core.ParsedDocument) *vbReferenceDocumentIndex {
	return vbReferenceDocumentIndexForShard(parsed, nil)
}

func vbReferenceDocumentIndexForShard(parsed *core.ParsedDocument, shard *vbscript.ReferenceShard) *vbReferenceDocumentIndex {
	if parsed == nil {
		return &vbReferenceDocumentIndex{}
	}
	if cached, ok := parsed.LoadRuntimeAnalysis(vbReferenceDocumentFactsAnalysisKey); ok {
		if index, valid := cached.(*vbReferenceDocumentIndex); valid {
			return index
		}
	}
	facts := vbReferenceDocumentFactsForShard(parsed, shard)
	index := &vbReferenceDocumentIndex{
		declarationRanges:          vbReferenceRangeIndex(facts.DeclarationRanges),
		objectInitializationRanges: vbReferenceRangeIndex(facts.ObjectInitializationRanges),
	}
	parsed.StoreRuntimeAnalysis(vbReferenceDocumentFactsAnalysisKey, index)
	return index
}

func vbReferenceDocumentFactsFor(parsed *core.ParsedDocument) vbReferenceDocumentFacts {
	return vbReferenceDocumentFactsForShard(parsed, nil)
}

func vbReferenceDocumentFactsForShard(parsed *core.ParsedDocument, shard *vbscript.ReferenceShard) vbReferenceDocumentFacts {
	if parsed == nil {
		return vbReferenceDocumentFacts{}
	}
	var facts vbReferenceDocumentFacts
	if !parsed.LoadAnalysis(vbReferenceDocumentFactsAnalysisKey, &facts) {
		facts = buildVBReferenceDocumentFactsWithShard(parsed, shard)
		parsed.StoreAnalysis(vbReferenceDocumentFactsAnalysisKey, facts)
	}
	return facts
}

func buildVBReferenceDocumentFacts(parsed *core.ParsedDocument) vbReferenceDocumentFacts {
	return buildVBReferenceDocumentFactsWithShard(parsed, nil)
}

func buildVBReferenceDocumentFactsWithShard(parsed *core.ParsedDocument, shard *vbscript.ReferenceShard) vbReferenceDocumentFacts {
	if shard == nil {
		shard = vbscript.BuildReferenceShard(parsed)
	}
	facts := vbReferenceDocumentFacts{
		DeclarationRanges:          vbReferenceDeclarationFactsWithShard(parsed, shard, true),
		ObjectInitializationRanges: map[string][]lsp.Range{},
	}
	for _, name := range shard.NormalizedNames() {
		for _, posting := range shard.PostingsFor(name) {
			if posting.HasRole(vbscript.ReferenceRoleObjectInitialization) {
				appendUniqueVBReferenceRange(facts.ObjectInitializationRanges, name, posting.Range)
			}
		}
	}
	return facts
}

func vbReferenceDeclarationFacts(parsed *core.ParsedDocument, includeServerObjects bool) map[string][]lsp.Range {
	return vbReferenceDeclarationFactsWithShard(parsed, nil, includeServerObjects)
}

func vbReferenceDeclarationFactsWithShard(parsed *core.ParsedDocument, shard *vbscript.ReferenceShard, includeServerObjects bool) map[string][]lsp.Range {
	ranges := map[string][]lsp.Range{}
	if shard == nil {
		shard = vbscript.BuildReferenceShard(parsed)
	}
	for _, name := range shard.NormalizedNames() {
		for _, declarationRange := range shard.DeclarationRangesFor(name) {
			appendUniqueVBReferenceRange(ranges, name, declarationRange)
		}
	}
	// Parameters and compatibility-only class declarations are not yet represented
	// as declaration postings, so retain them as a narrow overlay.
	for _, declaration := range vbNamingDeclarationsShared(parsed) {
		appendUniqueVBReferenceRange(ranges, declaration.Name, declaration.Range)
	}
	if includeServerObjects {
		for _, declaration := range serverObjectDeclarations(parsed) {
			appendUniqueVBReferenceRange(ranges, declaration.Name, declaration.Range)
		}
	}
	return ranges
}

func appendUniqueVBReferenceRange(ranges map[string][]lsp.Range, name string, value lsp.Range) {
	key := strings.ToLower(name)
	for _, existing := range ranges[key] {
		if existing == value {
			return
		}
	}
	ranges[key] = append(ranges[key], value)
}

func vbReferenceRangeIndex(facts map[string][]lsp.Range) map[string]map[lsp.Range]struct{} {
	index := make(map[string]map[lsp.Range]struct{}, len(facts))
	for name, ranges := range facts {
		values := make(map[lsp.Range]struct{}, len(ranges))
		for _, rangeValue := range ranges {
			values[rangeValue] = struct{}{}
		}
		index[name] = values
	}
	return index
}
