package lspserver

import (
	"slices"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptPublicSummarySymbolsIncludeOnlyPublicDeclarations(t *testing.T) {
	source := `<%
Dim PublicValue
Set PublicValue = Server.CreateObject("ADODB.Recordset")
' @type ExplicitRecordset As ADODB.Recordset
Dim ExplicitRecordset
Class PublicClass
  Public publicField
  Private privateField
  Public Function PublicMember()
  End Function
  Private Function PrivateMember()
  End Function
End Class
Function PublicFunction(arg)
  Dim localValue
  localValue = 1
End Function
' @returns PublicFactory ADODB.Recordset
Function PublicFactory()
  Set PublicFactory = Server.CreateObject("ADODB.Recordset")
End Function
Private Sub PrivateProcedure()
End Sub
%>`
	parsed := core.ParseDocument("file:///site/common.inc", source, core.Settings{DefaultLanguage: "VBScript"})
	symbols := collectVBScriptPublicSummarySymbols(parsed)

	for _, expected := range []string{"PublicValue", "ExplicitRecordset", "PublicClass", "publicField", "PublicMember", "PublicFunction", "PublicFactory"} {
		if publicSummarySymbolByName(symbols, expected) == nil {
			t.Fatalf("public summary missing %s: %#v", expected, publicSummaryNames(symbols))
		}
	}
	for _, unexpected := range []string{"arg", "localValue", "privateField", "PrivateMember", "PrivateProcedure"} {
		if publicSummarySymbolByName(symbols, unexpected) != nil {
			t.Fatalf("public summary should not include %s: %#v", unexpected, symbols)
		}
	}
	if symbol := publicSummarySymbolByName(symbols, "PublicValue"); symbol == nil || symbol.TypeName != "ADODB.Recordset" || symbol.ExplicitType {
		t.Fatalf("PublicValue summary mismatch: %#v", symbol)
	}
	if symbol := publicSummarySymbolByName(symbols, "ExplicitRecordset"); symbol == nil || symbol.TypeName != "ADODB.Recordset" || !symbol.ExplicitType {
		t.Fatalf("ExplicitRecordset summary mismatch: %#v", symbol)
	}
	if symbol := publicSummarySymbolByName(symbols, "PublicFactory"); symbol == nil || symbol.TypeName != "ADODB.Recordset" || !symbol.ExplicitType {
		t.Fatalf("PublicFactory summary mismatch: %#v", symbol)
	}
	if symbol := publicSummarySymbolByName(symbols, "PublicFunction"); symbol == nil || symbol.TypeName != "Variant" || symbol.ExplicitType {
		t.Fatalf("PublicFunction summary mismatch: %#v", symbol)
	}
}

func TestVBScriptPublicSummarySymbolsExcludeImplicitAndInferredExports(t *testing.T) {
	source := `<%
Dim ExplicitValue
' @type TypedValue As ADODB.Recordset
Dim TypedValue
InferredValue = Server.CreateObject("ADODB.Recordset")
Function PublicFactory()
  Set PublicFactory = Server.CreateObject("ADODB.Recordset")
End Function
%>`
	parsed := core.ParseDocument("file:///site/common.inc", source, core.Settings{DefaultLanguage: "VBScript"})
	symbols := collectVBScriptPublicSummarySymbols(parsed)

	for _, expected := range []string{"ExplicitValue", "TypedValue", "PublicFactory"} {
		if publicSummarySymbolByName(symbols, expected) == nil {
			t.Fatalf("public summary missing %s: %#v", expected, publicSummaryNames(symbols))
		}
	}
	if publicSummarySymbolByName(symbols, "InferredValue") != nil {
		t.Fatalf("public summary should not include inferred implicit export: %#v", symbols)
	}
	if symbol := publicSummarySymbolByName(symbols, "ExplicitValue"); symbol == nil || symbol.TypeName != "Variant" || symbol.ExplicitType {
		t.Fatalf("ExplicitValue summary mismatch: %#v", symbol)
	}
	if symbol := publicSummarySymbolByName(symbols, "TypedValue"); symbol == nil || symbol.TypeName != "ADODB.Recordset" || !symbol.ExplicitType {
		t.Fatalf("TypedValue summary mismatch: %#v", symbol)
	}
	if symbol := publicSummarySymbolByName(symbols, "PublicFactory"); symbol == nil || symbol.TypeName != "Variant" || symbol.ExplicitType {
		t.Fatalf("PublicFactory summary mismatch: %#v", symbol)
	}
}

func TestVBScriptExportSummariesDoNotRecursivelyExpandSameNameClassMembers(t *testing.T) {
	parsed := core.ParseDocument("file:///site/member-cycle.asp", `<%
Class Foo
  Public Function Foo()
  End Function
End Class
%>`, core.Settings{DefaultLanguage: "VBScript"})

	exports := exportSummariesForVBScriptPublicSymbols(collectVBScriptPublicSummarySymbols(parsed))
	exportedClass := exportSummaryByName(exports, "Foo")
	if exportedClass == nil || exportedClass.Kind != "class" {
		t.Fatalf("Foo class export mismatch: %#v in %#v", exportedClass, exports)
	}
	exportedMember := exportSummaryByName(exportedClass.Members, "Foo")
	if exportedMember == nil || exportedMember.Kind != "method" {
		t.Fatalf("Foo member export mismatch: %#v", exportedClass)
	}
	if len(exportedMember.Members) != 0 {
		t.Fatalf("same-name member export should not recursively expand: %#v", exportedMember)
	}
}

func TestVBScriptSummaryExportsAndUnresolvedExternalReferences(t *testing.T) {
	source := `<%
Option Explicit
Dim localValue
Function LocalTitle()
End Function
Response.Write SharedTitle(localValue)
Response.Write SharedCatalog.Name
''' <see cref="SharedTitle" />
''' <see cref="SharedCatalog.Name" />
%>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	summary := summarizeVBScriptFileAnalysis(parsed)

	exported := exportSummaryByName(summary.VBScript.Exports, "LocalTitle")
	if exported == nil || exported.Kind != "function" {
		t.Fatalf("LocalTitle export mismatch: %#v in %#v", exported, summary.VBScript.Exports)
	}
	for _, unexpected := range []string{"Response", "localValue"} {
		if ref := externalRefByName(summary.VBScript.ExternalRefs, unexpected); ref != nil {
			t.Fatalf("%s should not be summarized as an external ref: %#v", unexpected, summary.VBScript.ExternalRefs)
		}
	}
	if ref := externalRefByName(summary.VBScript.ExternalRefs, "SharedTitle"); ref == nil || ref.KindHint != "function" || ref.MemberName != "" || ref.Range.Start.Line != 5 {
		t.Fatalf("SharedTitle external ref mismatch: %#v", ref)
	}
	if ref := externalRefByName(summary.VBScript.ExternalRefs, "SharedCatalog"); ref == nil || ref.MemberName != "Name" || ref.Range.Start.Line != 6 {
		t.Fatalf("SharedCatalog external ref mismatch: %#v", ref)
	}
	if usage := externalRefUsageByKey(summary.VBScript.ExternalRefUsages, "sharedtitle"); usage == nil || usage.Count != 1 || len(usage.Ranges) != 1 || usage.Ranges[0].Start.Line != 5 {
		t.Fatalf("sharedtitle usage mismatch: %#v in %#v", usage, summary.VBScript.ExternalRefUsages)
	}
	if usage := externalRefUsageByKey(summary.VBScript.ExternalRefUsages, "sharedcatalog.name"); usage == nil || usage.Count != 1 || len(usage.Ranges) != 1 || usage.Ranges[0].Start.Line != 6 {
		t.Fatalf("sharedcatalog.name usage mismatch: %#v in %#v", usage, summary.VBScript.ExternalRefUsages)
	}
	if usage := externalRefUsageByKey(summary.VBScript.ExternalRefUsages, "sharedcatalog"); usage == nil || usage.Count != 1 || len(usage.Ranges) != 1 || usage.Ranges[0].Start.Line != 6 {
		t.Fatalf("sharedcatalog usage mismatch: %#v in %#v", usage, summary.VBScript.ExternalRefUsages)
	}
}

func TestVBScriptPublicSignatureHashIgnoresPrivateBodyOnlyChanges(t *testing.T) {
	before := summarizeVBScriptFileAnalysis(core.ParseDocument("file:///site/shared.inc", `<%
Private Sub Helper()
End Sub

Function SharedValue()
  SharedValue = "ok"
End Function
%>`, core.Settings{DefaultLanguage: "VBScript"}))
	after := summarizeVBScriptFileAnalysis(core.ParseDocument("file:///site/shared.inc", `<%
Private Sub Helper()
  Dim privateValue
  privateValue = "changed"
End Sub

Function SharedValue()
  SharedValue = "ok"
End Function
%>`, core.Settings{DefaultLanguage: "VBScript"}))

	if after.VBScript.Fingerprint == before.VBScript.Fingerprint {
		t.Fatalf("private body edit should change local fingerprint")
	}
	if after.VBScript.Exports[0].Range.Start.Line == before.VBScript.Exports[0].Range.Start.Line {
		t.Fatalf("fixture should shift public export range: before=%#v after=%#v", before.VBScript.Exports[0], after.VBScript.Exports[0])
	}
	if after.PublicSignatureHash != before.PublicSignatureHash {
		t.Fatalf("public signature hash changed for private body edit: before=%s after=%s", before.PublicSignatureHash, after.PublicSignatureHash)
	}
}

func TestVBScriptPublicSignatureHashIncludesProcedureImplicitGlobals(t *testing.T) {
	before := summarizeVBScriptFileAnalysis(core.ParseDocument("file:///site/implicit-candidates.inc", `<%
topLevelValue = 1
Sub Render()
  nestedValue = 2
  Response.Write readOnlyValue
End Sub
%>`, core.Settings{DefaultLanguage: "VBScript"}))
	after := summarizeVBScriptFileAnalysis(core.ParseDocument("file:///site/implicit-candidates.inc", `<%
topLevelValue = 1
Sub Render()
  renamedNestedValue = 2
  Response.Write readOnlyValue
End Sub
%>`, core.Settings{DefaultLanguage: "VBScript"}))

	if got := before.VBScript.ImplicitGlobalCandidateNames; !slices.Equal(got, []string{"nestedvalue", "toplevelvalue"}) {
		t.Fatalf("before implicit candidates = %#v", got)
	}
	if got := after.VBScript.ImplicitGlobalCandidateNames; !slices.Equal(got, []string{"renamednestedvalue", "toplevelvalue"}) {
		t.Fatalf("after implicit candidates = %#v", got)
	}
	if after.PublicSignatureHash == before.PublicSignatureHash {
		t.Fatal("public signature hash did not change for an implicit global rename")
	}
}

func TestVBScriptPublicSignatureHashIgnoresReadOnlyImplicitCandidateTyping(t *testing.T) {
	before := summarizeVBScriptFileAnalysis(core.ParseDocument("file:///site/implicit-typing.inc", `<%
Sub Render()
  nestedValue = 2
End Sub
%>`, core.Settings{DefaultLanguage: "VBScript"}))
	after := summarizeVBScriptFileAnalysis(core.ParseDocument("file:///site/implicit-typing.inc", `<%
Sub Render()
  nestedValue = 2
  Response.Write partialTyp
End Sub
%>`, core.Settings{DefaultLanguage: "VBScript"}))

	if got := after.VBScript.ImplicitGlobalCandidateNames; !slices.Equal(got, []string{"nestedvalue"}) {
		t.Fatalf("implicit candidates = %#v", got)
	}
	if after.PublicSignatureHash != before.PublicSignatureHash {
		t.Fatalf("public signature hash changed for read-only implicit typing: before=%s after=%s", before.PublicSignatureHash, after.PublicSignatureHash)
	}
}

func publicSummarySymbolByName(symbols []vbPublicSummarySymbol, name string) *vbPublicSummarySymbol {
	for i := range symbols {
		if strings.EqualFold(symbols[i].Name, name) {
			return &symbols[i]
		}
	}
	return nil
}

func exportSummaryByName(exports []vbExportSummary, name string) *vbExportSummary {
	for i := range exports {
		if strings.EqualFold(exports[i].Name, name) {
			return &exports[i]
		}
	}
	return nil
}

func externalRefByName(refs []vbExternalRef, name string) *vbExternalRef {
	for i := range refs {
		if strings.EqualFold(refs[i].Name, name) {
			return &refs[i]
		}
	}
	return nil
}

func externalRefUsageByKey(usages []vbExternalRefUsage, key string) *vbExternalRefUsage {
	for i := range usages {
		if usages[i].Key == key {
			return &usages[i]
		}
	}
	return nil
}

func publicSummaryNames(symbols []vbPublicSummarySymbol) []string {
	names := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		names = append(names, symbol.Name)
	}
	return names
}
