package lspserver

import (
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestFileAnalysisSnapshotEstimateTracksUsageGrowth(t *testing.T) {
	base := &fileAnalysisSnapshot{URI: "file:///workspace/accounting.asp"}
	withDeclarations := *base
	withDeclarations.Usage.Declarations = accountingTestDeclarations(64)
	if got, want := estimateFileAnalysisSnapshotBytes(&withDeclarations), estimateFileAnalysisSnapshotBytes(base); got <= want {
		t.Fatalf("usage growth did not increase complete snapshot estimate: without=%d with=%d", want, got)
	}

	withSmallCapacity := *base
	withSmallCapacity.Usage.Declarations = make([]vbUsageDeclaration, 1)
	withLargeCapacity := *base
	withLargeCapacity.Usage.Declarations = make([]vbUsageDeclaration, 1, 128)
	if got, want := estimateFileAnalysisSnapshotBytes(&withLargeCapacity), estimateFileAnalysisSnapshotBytes(&withSmallCapacity); got <= want {
		t.Fatalf("usage slice capacity growth did not increase complete snapshot estimate: small=%d large=%d", want, got)
	}
}

func TestFileAnalysisSnapshotEstimateTracksSummaryGrowth(t *testing.T) {
	base := &fileAnalysisSnapshot{URI: "file:///workspace/accounting.asp"}
	withSummary := *base
	withSummary.Summary = accountingTestSummary(48)
	if got, want := estimateFileAnalysisSnapshotBytes(&withSummary), estimateFileAnalysisSnapshotBytes(base); got <= want {
		t.Fatalf("summary growth did not increase complete snapshot estimate: without=%d with=%d", want, got)
	}

	small := *base
	small.Summary.VBScript.ExternalRefUsages = []vbExternalRefUsage{{Key: "external", Ranges: make([]lsp.Range, 1)}}
	large := *base
	large.Summary.VBScript.ExternalRefUsages = []vbExternalRefUsage{{Key: "external", Ranges: make([]lsp.Range, 1, 128)}}
	if got, want := estimateFileAnalysisSnapshotBytes(&large), estimateFileAnalysisSnapshotBytes(&small); got <= want {
		t.Fatalf("summary nested range capacity growth did not increase complete snapshot estimate: small=%d large=%d", want, got)
	}
}

func TestFileAnalysisSnapshotEstimateTracksEachAnalysisTypeMap(t *testing.T) {
	cases := []struct {
		name string
		grow func(*vbGraphAnalysisTypes)
	}{
		{name: "Types", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.Types = make(map[string]string, 64)
			for index := 0; index < 64; index++ {
				key := accountingTestName(index)
				analysis.Types[key] = accountingTestType(index)
			}
		}},
		{name: "TypeAnnotations", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.TypeAnnotations = make(map[string][]vbTypeAnnotation, 64)
			for index := 0; index < 64; index++ {
				annotations := make([]vbTypeAnnotation, 1, 8)
				annotations[0] = vbTypeAnnotation{
					TypeName: accountingTestType(index), Scope: accountingTestName(index),
					MemberOf: accountingTestName(index + 1), Accessor: "get", Line: index,
				}
				analysis.TypeAnnotations[accountingTestName(index)] = annotations
			}
		}},
		{name: "Returns", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.Returns = make(map[string]string, 64)
			for index := 0; index < 64; index++ {
				analysis.Returns[accountingTestName(index)] = accountingTestType(index)
			}
		}},
		{name: "Params", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.Params = accountingTestNestedStringMap(64)
		}},
		{name: "Members", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.Members = accountingTestNestedStringMap(64)
		}},
		{name: "Signatures", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.Signatures = accountingTestSignatures(64)
		}},
		{name: "ScopedReturns", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.ScopedReturns = make(map[string]string, 64)
			for index := 0; index < 64; index++ {
				analysis.ScopedReturns[accountingTestName(index)] = accountingTestType(index)
			}
		}},
		{name: "ScopedParams", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.ScopedParams = accountingTestNestedStringMap(64)
		}},
		{name: "ScopedSignatures", grow: func(analysis *vbGraphAnalysisTypes) {
			analysis.ScopedSignatures = accountingTestSignatures(64)
		}},
	}

	base := &fileAnalysisSnapshot{URI: "file:///workspace/accounting.asp"}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			large := *base
			test.grow(&large.AnalysisTypes)
			if got, want := estimateFileAnalysisSnapshotBytes(&large), estimateFileAnalysisSnapshotBytes(base); got <= want {
				t.Fatalf("analysis type map growth did not increase complete snapshot estimate: without=%d with=%d", want, got)
			}
		})
	}
}

func TestAnalysisCacheEvictionDeltaIncludesCompleteSnapshotCollections(t *testing.T) {
	parsed := core.ParseDocument("file:///workspace/accounting.asp", strings.Repeat("<% value = value + 1 %>\n", 128), core.Settings{DefaultLanguage: "VBScript"})
	graphDeclarations := accountingTestDeclarations(64)
	snapshot := &fileAnalysisSnapshot{
		URI:               parsed.URI,
		Usage:             vbUsageDeclarations{Declarations: accountingTestDeclarations(64)},
		GraphDeclarations: graphDeclarations,
		Summary:           accountingTestSummary(64),
		AnalysisTypes:     accountingTestAnalysisTypes(64),
		ReferenceShard: &vbscript.ReferenceShard{
			Declarations: map[string]vbscript.Symbol{"value": {Name: "value", Kind: "variable"}},
			Postings: map[string][]vbscript.ReferencePosting{
				"value": make([]vbscript.ReferencePosting, 64),
			},
		},
		VirtualDocuments: map[core.EmbeddedLanguage]core.VirtualDocument{},
	}
	cache := newAnalysisCache()
	cache.rememberSnapshot(parsed, snapshot)

	before, entries := cache.memoryEstimate()
	want := estimateAnalysisDeclarationStorageBytes(nil, graphDeclarations) + estimateAnalysisCacheFileSnapshotBytes(parsed, snapshot) + parsed.EstimateBytes()
	if before != want || entries != 2 {
		t.Fatalf("complete snapshot estimate = %d bytes, %d entries; want %d bytes, 2 entries", before, entries, want)
	}
	if estimateFileAnalysisSnapshotBytes(snapshot) <= estimateFileAnalysisSnapshotBytes(&fileAnalysisSnapshot{URI: parsed.URI}) {
		t.Fatal("complete snapshot collections did not contribute to its estimate")
	}

	freed := cache.evict(0)
	if freed != before {
		t.Fatalf("complete snapshot eviction freed %d bytes; want initial estimate %d", freed, before)
	}
	if bytes, entries := cache.memoryEstimate(); bytes != 0 || entries != 0 {
		t.Fatalf("evicted complete snapshot estimate = %d bytes, %d entries; want 0 bytes, 0 entries", bytes, entries)
	}
}

func accountingTestDeclarations(count int) []vbUsageDeclaration {
	declarations := make([]vbUsageDeclaration, 0, count*2)
	for index := 0; index < count; index++ {
		name := accountingTestName(index)
		declarations = append(declarations, vbUsageDeclaration{
			Name: name, Kind: "variable", Scope: accountingTestName(index + 1),
			Range: lsp.Range{Start: lsp.Position{Line: index}, End: lsp.Position{Line: index, Character: 8}},
			Start: index * 8, End: index*8 + 8, Line: index,
			AssignedValue: accountingTestType(index), TypeName: "String", MemberOf: "Account",
			ProcedureKind: "function",
		})
	}
	return declarations
}

func accountingTestSummary(count int) vbFileAnalysisSummary {
	local := vbLocalSummary{
		Fingerprint:                  accountingTestName(0),
		PublicSymbols:                make([]vbPublicSummarySymbol, 0, count*2),
		Exports:                      make([]vbExportSummary, 0, count*2),
		ExternalRefs:                 make([]vbExternalRef, 0, count*2),
		ExternalRefUsages:            make([]vbExternalRefUsage, 0, count*2),
		ImplicitGlobalCandidateNames: make([]string, 0, count*2),
	}
	for index := 0; index < count; index++ {
		name := accountingTestName(index)
		local.PublicSymbols = append(local.PublicSymbols, vbPublicSummarySymbol{
			Name: name, Kind: "variable", TypeName: accountingTestType(index),
			MemberOf: "Account", Visibility: "public",
		})
		local.Exports = append(local.Exports, vbExportSummary{
			Name: name, Kind: "class", TypeName: accountingTestType(index),
			MemberOf: "Account", Visibility: "public",
			Members: []vbExportSummary{{Name: "Value", Kind: "property", TypeName: "String"}},
		})
		local.ExternalRefs = append(local.ExternalRefs, vbExternalRef{
			Name: name, KindHint: "object", MemberName: "Value",
		})
		local.ExternalRefUsages = append(local.ExternalRefUsages, vbExternalRefUsage{
			Key: name, Count: index + 1, Ranges: make([]lsp.Range, 8, 16),
		})
		local.ImplicitGlobalCandidateNames = append(local.ImplicitGlobalCandidateNames, name)
	}
	return vbFileAnalysisSummary{
		Fingerprint: accountingTestName(100), PublicSignatureHash: accountingTestName(101),
		VBScript: local,
	}
}

func accountingTestAnalysisTypes(count int) vbGraphAnalysisTypes {
	analysis := vbGraphAnalysisTypes{}
	for _, grow := range []func(*vbGraphAnalysisTypes){
		func(value *vbGraphAnalysisTypes) {
			value.Types = make(map[string]string, count)
			for index := 0; index < count; index++ {
				value.Types[accountingTestName(index)] = accountingTestType(index)
			}
		},
		func(value *vbGraphAnalysisTypes) {
			value.TypeAnnotations = make(map[string][]vbTypeAnnotation, count)
			for index := 0; index < count; index++ {
				value.TypeAnnotations[accountingTestName(index)] = []vbTypeAnnotation{{TypeName: accountingTestType(index), Scope: "scope", MemberOf: "Account", Accessor: "get", Line: index}}
			}
		},
		func(value *vbGraphAnalysisTypes) {
			value.Returns = make(map[string]string, count)
			for index := 0; index < count; index++ {
				value.Returns[accountingTestName(index)] = accountingTestType(index)
			}
		},
		func(value *vbGraphAnalysisTypes) { value.Params = accountingTestNestedStringMap(count) },
		func(value *vbGraphAnalysisTypes) { value.Members = accountingTestNestedStringMap(count) },
		func(value *vbGraphAnalysisTypes) { value.Signatures = accountingTestSignatures(count) },
		func(value *vbGraphAnalysisTypes) {
			value.ScopedReturns = make(map[string]string, count)
			for index := 0; index < count; index++ {
				value.ScopedReturns[accountingTestName(index)] = accountingTestType(index)
			}
		},
		func(value *vbGraphAnalysisTypes) { value.ScopedParams = accountingTestNestedStringMap(count) },
		func(value *vbGraphAnalysisTypes) { value.ScopedSignatures = accountingTestSignatures(count) },
	} {
		grow(&analysis)
	}
	return analysis
}

func accountingTestNestedStringMap(count int) map[string]map[string]string {
	values := make(map[string]map[string]string, count)
	for index := 0; index < count; index++ {
		nested := make(map[string]string, 4)
		for member := 0; member < 4; member++ {
			nested[accountingTestName(member)] = accountingTestType(index + member)
		}
		values[accountingTestName(index)] = nested
	}
	return values
}

func accountingTestSignatures(count int) map[string]vbscript.Signature {
	values := make(map[string]vbscript.Signature, count)
	for index := 0; index < count; index++ {
		values[accountingTestName(index)] = vbscript.Signature{
			Name: accountingTestName(index), Kind: "function", Label: accountingTestType(index),
			Parameters: []vbscript.Parameter{{Name: "value", Mode: "ByRef"}},
		}
	}
	return values
}

func accountingTestName(index int) string {
	return "name-" + strconv.Itoa(index) + strings.Repeat("x", 8)
}

func accountingTestType(index int) string {
	return "Type" + strconv.Itoa(index) + strings.Repeat("T", 8)
}
