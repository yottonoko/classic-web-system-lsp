package lspserver

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptAssignmentTypeDiagnosticsSplitColonStatements(t *testing.T) {
	source := `<%
' @type first As String
Dim first
first = "有効": first = 1
' @type second As Number
Dim second
second = 1: second = "bad"
%>`
	parsed := core.ParseDocument("file:///site/colon-types.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)

	var mismatches []lsp.Diagnostic
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "typeMismatch" {
			mismatches = append(mismatches, diagnostic)
		}
	}
	if len(mismatches) != 2 {
		t.Fatalf("type mismatch count = %d, want 2: %#v", len(mismatches), diagnostics)
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	wantRanges := []lsp.Range{
		doc.Range(strings.LastIndex(source, "first = 1"), strings.LastIndex(source, "first = 1")+len("first")),
		doc.Range(strings.LastIndex(source, "second = \"bad\""), strings.LastIndex(source, "second = \"bad\"")+len("second")),
	}
	for _, want := range wantRanges {
		found := false
		for _, diagnostic := range mismatches {
			if diagnostic.Range == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing colon-segment mismatch at %#v: %#v", want, mismatches)
		}
	}
}

func TestVBScriptStrictDiagnosticsPreserveTemplateBooleanLiteralIdentity(t *testing.T) {
	source := `<%
' @type trueRoute As ` + "`flag-${True}`" + `
Dim trueRoute
trueRoute = "flag-True"
trueRoute = "flag-False"
' @type falseRoute As ` + "`flag-${False}`" + `
Dim falseRoute
falseRoute = "flag-False"
falseRoute = "flag-True"
' @type normalizedTrue As ` + "`flag-${true}`" + `
Dim normalizedTrue
normalizedTrue = "flag-True"
normalizedTrue = "flag-true"
' @type broadBoolean As ` + "`flag-${Boolean}`" + `
Dim broadBoolean
broadBoolean = "flag-True"
broadBoolean = "flag-False"
broadBoolean = "flag-false"
' @type unionFlag As ` + "`flag-${True | False}`" + `
Dim unionFlag
unionFlag = "flag-True"
unionFlag = "flag-False"
unionFlag = "flag-false"
%>`
	parsed := core.ParseDocument("file:///site/template-boolean-types.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}

	var mismatches []lsp.Diagnostic
	for _, diagnostic := range server.vbscriptTypeDiagnostics(parsed) {
		if diagnostic.Code == "typeMismatch" {
			mismatches = append(mismatches, diagnostic)
		}
	}
	if len(mismatches) != 4 {
		t.Fatalf("template boolean mismatch count = %d, want 4: %#v", len(mismatches), mismatches)
	}
	wantNames := []string{"trueRoute", "falseRoute", "normalizedTrue", "unionFlag"}
	for _, wantName := range wantNames {
		found := false
		for _, diagnostic := range mismatches {
			data, ok := diagnostic.Data.(map[string]any)
			if ok && data["name"] == wantName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing template boolean mismatch for %q: %#v", wantName, mismatches)
		}
	}
}

func TestVBScriptAssignmentTypeDiagnosticsPreserveLiteralCommentAndUTF16Boundaries(t *testing.T) {
	source := `<%
' @type stamp As Date
Dim stamp
stamp = #12:30:00#: label: labeled = "bad": quoted = "😀:x": quoted = 1
' @type labeled As Number
Dim labeled
' @type quoted As String
Dim quoted
' @type guarded As Number
Dim guarded
guarded = 1 ' comment: ignored = "not an assignment"
If ready Then inline = "not checked"
' @type continued As Number
Dim continued
continued = _
  "not checked"
%>`
	parsed := core.ParseDocument("file:///site/colon-boundaries.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)

	var mismatches []lsp.Diagnostic
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "typeMismatch" {
			mismatches = append(mismatches, diagnostic)
		}
	}
	if len(mismatches) != 2 {
		t.Fatalf("boundary mismatch count = %d, want 2: %#v", len(mismatches), diagnostics)
	}

	assignments := vbscriptAssignments(parsed)
	values := map[string][]string{}
	for _, assignment := range assignments {
		values[strings.ToLower(assignment.Name)] = append(values[strings.ToLower(assignment.Name)], assignment.Value)
	}
	if got, want := values["stamp"], []string{"#12:30:00#"}; !sameStringSlice(got, want) {
		t.Fatalf("date assignment values = %#v, want %#v; assignments=%#v", got, want, assignments)
	}
	if got, want := values["quoted"], []string{`"😀:x"`, "1"}; !sameStringSlice(got, want) {
		t.Fatalf("quoted assignment values = %#v, want %#v; assignments=%#v", got, want, assignments)
	}
	if got, want := values["guarded"], []string{"1"}; !sameStringSlice(got, want) {
		t.Fatalf("comment assignment values = %#v, want %#v; assignments=%#v", got, want, assignments)
	}

	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	labeledOffset := strings.Index(source, `label: labeled = "bad"`) + len("label: ")
	quotedOffset := strings.LastIndex(source, "quoted = 1")
	wantRanges := []lsp.Range{
		doc.Range(labeledOffset, labeledOffset+len("labeled")),
		doc.Range(quotedOffset, quotedOffset+len("quoted")),
	}
	for _, want := range wantRanges {
		found := false
		for _, diagnostic := range mismatches {
			if diagnostic.Range == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing boundary mismatch at %#v: %#v", want, mismatches)
		}
	}
	if wantRanges[1].Start.Character == quotedOffset {
		t.Fatalf("quoted mismatch range used byte offset instead of UTF-16 position: %#v", wantRanges[1])
	}
}

func TestVBScriptAssignmentsStopAtRemCommentsAndKeepTokenBoundaries(t *testing.T) {
	source := `<%
' @type leading As Number
Dim leading
   rEm ignored: leading = "bad"
' @type afterRem As Number
Dim afterRem
before = 1: REM ignored: afterRem = "bad"
' @type stringValue As String
Dim stringValue
stringValue = "Rem: ignored = ""bad""": stringValue = 1
' @type dateValue As Date
Dim dateValue
dateValue = #12:30:00#: dateValue = 1
' @type remember As String
Dim remember
Remember = "ok"
' @type remResult As String
Dim remResult
remResult = RemValue
' @type afterIdentifier As Number
Dim afterIdentifier
remResult = RemValue: afterIdentifier = "bad"
%>`
	parsed := core.ParseDocument("file:///site/rem-types.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	assignments := vbscriptAssignments(parsed)
	values := map[string][]string{}
	for _, assignment := range assignments {
		values[strings.ToLower(assignment.Name)] = append(values[strings.ToLower(assignment.Name)], assignment.Value)
	}
	if _, found := values["leading"]; found {
		t.Fatalf("leading Rem comment produced an assignment: %#v", assignments)
	}
	if _, found := values["afterrem"]; found {
		t.Fatalf("colon after Rem comment produced an assignment: %#v", assignments)
	}
	if got, want := values["before"], []string{"1"}; !sameStringSlice(got, want) {
		t.Fatalf("assignment before Rem comment = %#v, want %#v; assignments=%#v", got, want, assignments)
	}
	if got, want := values["stringvalue"], []string{`"Rem: ignored = ""bad"""`, "1"}; !sameStringSlice(got, want) {
		t.Fatalf("string assignment values = %#v, want %#v; assignments=%#v", got, want, assignments)
	}
	if got, want := values["datevalue"], []string{"#12:30:00#", "1"}; !sameStringSlice(got, want) {
		t.Fatalf("date assignment values = %#v, want %#v; assignments=%#v", got, want, assignments)
	}
	if got, want := values["remember"], []string{`"ok"`}; !sameStringSlice(got, want) {
		t.Fatalf("Remember identifier assignment = %#v, want %#v; assignments=%#v", got, want, assignments)
	}
	if got, want := values["remresult"], []string{"RemValue", "RemValue"}; !sameStringSlice(got, want) {
		t.Fatalf("RemValue identifier assignments = %#v, want %#v; assignments=%#v", got, want, assignments)
	}
	if got, want := values["afteridentifier"], []string{`"bad"`}; !sameStringSlice(got, want) {
		t.Fatalf("assignment after RemValue = %#v, want %#v; assignments=%#v", got, want, assignments)
	}

	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	var mismatchNames []string
	for _, diagnostic := range server.vbscriptTypeDiagnostics(parsed) {
		if diagnostic.Code == "typeMismatch" {
			mismatchNames = append(mismatchNames, diagnostic.Data.(map[string]any)["name"].(string))
		}
	}
	if got, want := mismatchNames, []string{"stringValue", "dateValue", "afterIdentifier"}; !sameStringSlice(got, want) {
		t.Fatalf("Rem boundary type mismatches = %#v, want %#v", got, want)
	}
}

func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
