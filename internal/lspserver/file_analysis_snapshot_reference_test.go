package lspserver

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestFileAnalysisSnapshotPersistsAndSeedsReferenceFacts(t *testing.T) {
	const source = `<%
Dim customer
Set customer = New Customer
Response.Write customer
%>`
	server := New(nil, io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///tmp/reference-facts-snapshot.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	snapshot := server.buildFileAnalysisSnapshot(parsed)
	if snapshot.SchemaVersion != fileAnalysisSnapshotSchemaVersion {
		t.Fatalf("snapshot schema version = %d, want %d", snapshot.SchemaVersion, fileAnalysisSnapshotSchemaVersion)
	}
	if snapshot.ReferenceShard == nil || len(snapshot.ReferenceShard.PostingsFor("customer")) != 4 {
		t.Fatalf("snapshot reference shard = %#v", snapshot.ReferenceShard)
	}
	if len(snapshot.ReferenceFacts.DeclarationRanges["customer"]) != 1 {
		t.Fatalf("snapshot declaration ranges = %#v", snapshot.ReferenceFacts.DeclarationRanges)
	}
	if len(snapshot.ReferenceFacts.ObjectInitializationRanges["customer"]) != 1 {
		t.Fatalf("snapshot object initialization ranges = %#v", snapshot.ReferenceFacts.ObjectInitializationRanges)
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var encodedFields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &encodedFields); err != nil {
		t.Fatal(err)
	}
	if _, duplicated := encodedFields["Symbols"]; duplicated {
		t.Fatal("snapshot persisted the symbol index already represented by the reference shard")
	}
	var restoredSnapshot fileAnalysisSnapshot
	if err := json.Unmarshal(payload, &restoredSnapshot); err != nil {
		t.Fatal(err)
	}
	restored := core.ParseDocument(parsed.URI, "", core.Settings{DefaultLanguage: "VBScript"})
	seedParsedAnalysis(restored, &restoredSnapshot)

	if got := vbscript.BuildReferenceShard(restored); len(got.PostingsFor("customer")) != 4 {
		t.Fatalf("restored reference shard = %#v", got)
	}
	index := vbReferenceDocumentIndexFor(restored)
	if len(index.declarationRanges["customer"]) != 1 || len(index.objectInitializationRanges["customer"]) != 1 {
		t.Fatalf("restored reference facts = %#v", index)
	}
}

func TestFileAnalysisSnapshotRoundTripsScopedVBTypeAnalysis(t *testing.T) {
	const source = `<%
Class First
  Public StringMember
  ' @returns Render String
  Public Function Render(value)
    ' @type local As String
    Dim local
    local = "value"
    Render = local
  End Function
End Class
' @type first As First
Dim first
Set first = New First
first.Render("value")
%>`
	server := New(nil, io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///tmp/scoped-type-snapshot.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	snapshot := server.buildFileAnalysisSnapshot(parsed)
	if snapshot.SchemaVersion != fileAnalysisSnapshotSchemaVersion {
		t.Fatalf("snapshot schema version = %d, want %d", snapshot.SchemaVersion, fileAnalysisSnapshotSchemaVersion)
	}
	if got := snapshot.AnalysisTypes.ScopedReturns[graphSignatureKey("First", "Render")]; got != "String" {
		t.Fatalf("snapshot scoped return = %q, want String: %#v", got, snapshot.AnalysisTypes.ScopedReturns)
	}
	annotations := snapshot.AnalysisTypes.TypeAnnotations["local"]
	if len(annotations) != 1 || !strings.EqualFold(annotations[0].Scope, "first.render") {
		t.Fatalf("snapshot local annotation scope = %#v, want first.render", annotations)
	}
	var localUsage, parameterUsage *vbUsageDeclaration
	for index := range snapshot.Usage.Declarations {
		declaration := &snapshot.Usage.Declarations[index]
		if strings.EqualFold(declaration.Scope, "first.render") && strings.EqualFold(declaration.Name, "local") {
			localUsage = declaration
		}
		if strings.EqualFold(declaration.Scope, "first.render") && strings.EqualFold(declaration.Name, "value") {
			parameterUsage = declaration
		}
	}
	if localUsage == nil || parameterUsage == nil {
		t.Fatalf("snapshot scoped usage missing: local=%#v parameter=%#v declarations=%#v", localUsage, parameterUsage, snapshot.Usage.Declarations)
	}
	var localAssignment *vbAssignment
	for index := range snapshot.Assignments {
		assignment := &snapshot.Assignments[index]
		if strings.EqualFold(assignment.Name, "local") {
			localAssignment = assignment
			break
		}
	}
	if localAssignment == nil || !strings.EqualFold(localAssignment.Scope, "first.render") {
		t.Fatalf("snapshot local assignment scope = %#v, want first.render", localAssignment)
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var restoredSnapshot fileAnalysisSnapshot
	if err := json.Unmarshal(payload, &restoredSnapshot); err != nil {
		t.Fatal(err)
	}
	restored := core.ParseDocument(parsed.URI, source, core.Settings{DefaultLanguage: "VBScript"})
	seedParsedAnalysis(restored, &restoredSnapshot)
	restoredAnalysis := graphAnalysisTypes(restored)
	if got := restoredAnalysis.ScopedReturns[graphSignatureKey("First", "Render")]; got != "String" {
		t.Fatalf("restored scoped return = %q, want String: %#v", got, restoredAnalysis.ScopedReturns)
	}
	restoredAnnotations := restoredAnalysis.TypeAnnotations["local"]
	if len(restoredAnnotations) != 1 || !strings.EqualFold(restoredAnnotations[0].Scope, "first.render") {
		t.Fatalf("restored local annotation scope = %#v, want first.render", restoredAnnotations)
	}
	restoredUsage := collectVBUsageDeclarations(restored)
	for _, declaration := range restoredUsage.Declarations {
		if strings.EqualFold(declaration.Name, "local") && declaration.Local && !strings.EqualFold(declaration.Scope, "first.render") {
			t.Fatalf("restored local usage scope = %q, want first.render", declaration.Scope)
		}
	}
	restoredAssignments := vbscriptAssignments(restored)
	foundRestoredAssignment := false
	for _, assignment := range restoredAssignments {
		if strings.EqualFold(assignment.Name, "local") {
			foundRestoredAssignment = true
			if !strings.EqualFold(assignment.Scope, "first.render") {
				t.Fatalf("restored local assignment scope = %q, want first.render", assignment.Scope)
			}
		}
	}
	if !foundRestoredAssignment {
		t.Fatalf("restored local assignment missing: %#v", restoredAssignments)
	}
}

func TestFileAnalysisSnapshotRejectsStaleSchema(t *testing.T) {
	server := newRuntimeCacheTestServer(t)
	const source = `<%
Function Shared(firstName)
End Function
Sub Shared(secondName)
End Sub
%>`
	doc := core.NewTextDocument("file:///workspace/stale-snapshot.asp", "classic-asp", 1, source)
	parsed := core.ParseDocument(doc.URI, source, core.Settings{DefaultLanguage: "VBScript"})
	snapshot := server.buildFileAnalysisSnapshot(parsed)
	if len(snapshot.SignatureList) < 2 {
		t.Fatalf("snapshot signatures = %#v, want duplicate declarations", snapshot.SignatureList)
	}
	snapshot.Signatures["shared"] = snapshot.SignatureList[1]
	snapshot.SchemaVersion = fileAnalysisSnapshotSchemaVersion - 1
	server.writeDiskParsedDocument(doc, "VBScript", parsed, snapshot)
	server.waitForAsyncDiskCacheWrites()
	if restored, ok := server.readDiskFileAnalysisSnapshot(doc, parsed, "VBScript"); ok || restored != nil {
		t.Fatalf("stale schema snapshot was accepted: restored=%#v ok=%t", restored, ok)
	}
}

func TestFileAnalysisSnapshotRoundTripsFirstDuplicateSignature(t *testing.T) {
	const source = `<%
Function Shared(firstName)
End Function
Sub Shared(secondName)
End Sub
%>`
	server := New(nil, io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///tmp/signature-snapshot.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	snapshot := server.buildFileAnalysisSnapshot(parsed)
	if got := snapshot.Signatures["shared"]; got.Label != "Shared(ByRef firstName)" {
		t.Fatalf("current snapshot signature = %#v, want first duplicate", got)
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var restoredSnapshot fileAnalysisSnapshot
	if err := json.Unmarshal(payload, &restoredSnapshot); err != nil {
		t.Fatal(err)
	}
	restored := core.ParseDocument(parsed.URI, source, core.Settings{DefaultLanguage: "VBScript"})
	seedParsedAnalysis(restored, &restoredSnapshot)
	if got := vbscript.BuildSignatures(restored)["shared"]; got.Label != "Shared(ByRef firstName)" {
		t.Fatalf("restored snapshot signature = %#v, want first duplicate", got)
	}
	var current map[string]vbscript.Signature
	if !restored.LoadAnalysis("vbscript.signatures-by-name.v2", &current) {
		t.Fatal("restored snapshot did not seed the current signature analysis key")
	}
	var legacy map[string]vbscript.Signature
	if restored.LoadAnalysis("vbscript.signatures-by-name.v1", &legacy) {
		t.Fatalf("restored snapshot seeded the legacy signature analysis key: %#v", legacy)
	}
}

func TestSeedParsedAnalysisSeedsRuntimeOnlyFactsWithoutJSONCopies(t *testing.T) {
	const source = `<%
Public Function SharedName(value)
  SharedName = value
End Function
Response.Write SharedName("x")
%>`
	server := New(nil, io.Discard, io.Discard)
	parsed := core.ParseDocument("file:///tmp/seed-runtime-only.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	snapshot := server.buildFileAnalysisSnapshot(parsed)
	if len(snapshot.Summary.VBScript.PublicSymbols) == 0 {
		t.Fatalf("snapshot summary has no public symbols: %#v", snapshot.Summary)
	}
	// The restored document has no source, so any fact rebuilt instead of
	// seeded would be empty.
	restored := core.ParseDocument(parsed.URI, "", core.Settings{DefaultLanguage: "VBScript"})
	seedParsedAnalysis(restored, snapshot)

	stored := restored.AnalysisSnapshot()
	for _, key := range []string{"vbscript.reference-shard.v3", "vbscript.symbol-index.v2", vbFileAnalysisSummaryAnalysisKey} {
		if _, ok := stored[key]; ok {
			t.Fatalf("seeded analysis kept a JSON copy of %s", key)
		}
	}
	if got := summarizeVBScriptFileAnalysis(restored); len(got.VBScript.PublicSymbols) != len(snapshot.Summary.VBScript.PublicSymbols) {
		t.Fatalf("seeded summary = %#v, want %#v", got.VBScript.PublicSymbols, snapshot.Summary.VBScript.PublicSymbols)
	}
	if got := vbscript.BuildReferenceShard(restored); len(got.PostingsFor("sharedname")) == 0 {
		t.Fatalf("seeded reference shard = %#v", got)
	}
}
