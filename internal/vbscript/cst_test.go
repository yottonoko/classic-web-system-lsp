package vbscript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestCSTBuildsErrorTolerantDeclarationsAndPreservesTriviaTokens(t *testing.T) {
	cst := ParseCST(`Class Broken
  Public Name
  Sub Save(value)
' trailing comment`)

	if !hasToken(cst.Tokens, "comment", "' trailing comment") {
		t.Fatalf("missing trailing comment token: %#v", cst.Tokens)
	}
	classNode := childByKindAndName(cst.Children, "Class", "Broken")
	if classNode == nil {
		t.Fatalf("missing Broken class node: %#v", cst.Children)
	}
	if procedure := childByKindAndName(classNode.Children, "Procedure", "Save"); procedure == nil {
		t.Fatalf("missing nested Save procedure node: %#v", classNode.Children)
	}
}

func TestCSTTokenizesLeadingDecimalPointNumericLiterals(t *testing.T) {
	tokens := Tokenize(`positive = .5 : negative = -.25 : explicit = +.75`)
	want := map[string]bool{".5": false, "-.25": false, ".75": false}
	for _, token := range tokens {
		if token.Kind == "number" {
			if _, ok := want[token.Text]; ok {
				want[token.Text] = true
			}
		}
	}
	for literal, found := range want {
		if !found {
			t.Fatalf("numeric literal %q missing from %#v", literal, tokens)
		}
	}
}

func TestCSTBuildsStatementNodesForBlocksCallsAndAssignments(t *testing.T) {
	cst := ParseCST(`If ready Then
  Call Save(name)
End If
Select Case kind
End Select
Do While ready
Loop
While ready
Wend
For index = 1 To 3
Next
For Each item In items
Next
value = _
  other`)
	kinds := map[string]bool{}
	for _, node := range flattenCSTNodes(cst) {
		kinds[node.Kind] = true
	}
	for _, expected := range []string{"If", "Call", "Select", "DoLoop", "While", "For", "ForEach", "Assignment"} {
		if !kinds[expected] {
			t.Fatalf("missing %s node in %#v", expected, kinds)
		}
	}
}

func TestCSTBuildsOneVariableDeclarationNodePerDimName(t *testing.T) {
	cst := ParseCST(`Dim first(upperBound), second, third`)
	var names []string
	for _, node := range flattenCSTNodes(cst) {
		if node.Kind == "VariableDeclaration" && node.NameToken != nil {
			names = append(names, node.NameToken.Text)
		}
	}
	if len(names) != 3 || names[0] != "first" || names[1] != "second" || names[2] != "third" {
		t.Fatalf("variable declaration names = %#v, want first, second, third", names)
	}
}

func TestCSTExposesStructuredStatementMetadata(t *testing.T) {
	source := `Class Widget
  Public Property Get Name()
    If ready Then
      value = 1
    ElseIf fallback Then
      value = 2
    Else
      value = 3
    End If
  End Property
End Class
Select Case kind
Case 1
Case Else
End Select
For index = 1 To 3
Next
Do Until finished
Loop While pending
While ready
Wend
Sub Save()
End Sub
Function Load()
End Function`
	cst := ParseCST(source)
	want := map[CSTStatementKind]CSTStatementRole{
		CSTStatementClass:       CSTStatementRoleHeader,
		CSTStatementPropertyGet: CSTStatementRoleHeader,
		CSTStatementIf:          CSTStatementRoleHeader,
		CSTStatementElseIf:      CSTStatementRoleBranch,
		CSTStatementElse:        CSTStatementRoleBranch,
		CSTStatementEndIf:       CSTStatementRoleTerminator,
		CSTStatementEndProperty: CSTStatementRoleTerminator,
		CSTStatementEndClass:    CSTStatementRoleTerminator,
		CSTStatementSelect:      CSTStatementRoleHeader,
		CSTStatementCase:        CSTStatementRoleBranch,
		CSTStatementCaseElse:    CSTStatementRoleBranch,
		CSTStatementEndSelect:   CSTStatementRoleTerminator,
		CSTStatementFor:         CSTStatementRoleHeader,
		CSTStatementNext:        CSTStatementRoleTerminator,
		CSTStatementDo:          CSTStatementRoleHeader,
		CSTStatementLoop:        CSTStatementRoleTerminator,
		CSTStatementWhile:       CSTStatementRoleHeader,
		CSTStatementWend:        CSTStatementRoleTerminator,
		CSTStatementSub:         CSTStatementRoleHeader,
		CSTStatementEndSub:      CSTStatementRoleTerminator,
		CSTStatementFunction:    CSTStatementRoleHeader,
		CSTStatementEndFunction: CSTStatementRoleTerminator,
	}
	found := map[CSTStatementKind]CSTStatementRole{}
	for _, node := range flattenCSTNodes(cst) {
		if node.Statement == nil {
			continue
		}
		found[node.Statement.Kind] = node.Statement.Role
		if node.Statement.Start < 0 || node.Statement.End <= node.Statement.Start || node.Statement.End > len(source) {
			t.Fatalf("invalid statement range: %#v", node.Statement)
		}
	}
	for kind, role := range want {
		if found[kind] != role {
			t.Errorf("statement %q role = %q, want %q", kind, found[kind], role)
		}
	}
}

func TestCSTLogicalStatementRangesHandleContinuationsColonsAndDateLiterals(t *testing.T) {
	source := "value = _\r\n  other: stamp = #12:30:00#: Call Save(value)"
	cst := ParseCST(source)
	var statements []*CSTStatement
	for _, node := range flattenCSTNodes(cst) {
		if node.Statement != nil {
			statements = append(statements, node.Statement)
		}
	}
	if len(statements) != 3 {
		t.Fatalf("statement count = %d, want 3: %#v", len(statements), statements)
	}
	wantText := []string{"value = _\r\n  other", "stamp = #12:30:00#", "Call Save(value)"}
	for index, statement := range statements {
		got := source[statement.Start:statement.End]
		if strings.ReplaceAll(got, "  ", "") != strings.ReplaceAll(wantText[index], "  ", "") {
			t.Errorf("statement %d text = %q, want %q", index, got, wantText[index])
		}
	}
	if len(statements[0].Tokens) < 3 || !hasToken(statements[0].Tokens, "newline", "\r\n") {
		t.Fatalf("continuation tokens = %#v", statements[0].Tokens)
	}
}

func TestCSTKeepsColonSeparatedInlineIfBranchesStructured(t *testing.T) {
	cst := ParseCST("If ready Then first = 1: second = 2 Else third = 3: fourth = 4")
	ifNode := firstCSTNodeByKind(cst, "If")
	if ifNode == nil {
		t.Fatal("missing inline If")
	}
	var thenLeaves int
	for _, child := range ifNode.Children {
		if child.Kind == "Else" {
			if len(child.Children) != 2 {
				t.Fatalf("else leaf count = %d, want 2", len(child.Children))
			}
			continue
		}
		if child.Statement != nil && child.Statement.Role == CSTStatementRoleExecutable {
			thenLeaves++
		}
	}
	if thenLeaves != 2 {
		t.Fatalf("then leaf count = %d, want 2", thenLeaves)
	}
}

func TestCSTKeepsColonSeparatedInlineIfWithoutElseStructured(t *testing.T) {
	cst := ParseCST("If ready Then first = 1: second = 2")
	ifNode := firstCSTNodeByKind(cst, "If")
	if ifNode == nil {
		t.Fatal("missing inline If")
	}
	if len(ifNode.Children) != 2 {
		t.Fatalf("then leaf count = %d, want 2", len(ifNode.Children))
	}
}

func TestCSTDoesNotContinueUnderscoreFollowedByComment(t *testing.T) {
	cst := ParseCST("value = _ ' continuation is invalid here\nnextValue = 2")
	var statements []*CSTStatement
	for _, node := range flattenCSTNodes(cst) {
		if node.Statement != nil {
			statements = append(statements, node.Statement)
		}
	}
	if len(statements) != 2 {
		t.Fatalf("statement count = %d, want 2: %#v", len(statements), statements)
	}
	if got := cstTokenText(statements[1].Tokens); got != "nextValue=2" {
		t.Fatalf("second statement = %q", got)
	}
}

func TestCSTExposesTypedControlFlowParts(t *testing.T) {
	cst := ParseCST("If ready Then Save(value) Else Retry()\nSelect Case kind\nCase 1, 2\nEnd Select\nDo Until finished\nLoop While pending")
	statements := map[CSTStatementKind]*CSTStatement{}
	for _, node := range flattenCSTNodes(cst) {
		if node.Statement != nil {
			statements[node.Statement.Kind] = node.Statement
		}
	}
	if got := cstTokenText(statements[CSTStatementIf].Parts.Condition); got != "ready" {
		t.Errorf("if condition = %q", got)
	}
	if got := cstTokenText(statements[CSTStatementIf].Parts.InlineThen); got != "Save(value)" {
		t.Errorf("inline then = %q", got)
	}
	if got := cstTokenText(statements[CSTStatementIf].Parts.InlineElse); got != "Retry()" {
		t.Errorf("inline else = %q", got)
	}
	if got := cstTokenText(statements[CSTStatementSelect].Parts.Selector); got != "kind" {
		t.Errorf("selector = %q", got)
	}
	if got := cstTokenText(statements[CSTStatementCase].Parts.CaseValues); got != "1,2" {
		t.Errorf("case values = %q", got)
	}
	if statement := statements[CSTStatementDo]; !statement.Parts.Until || cstTokenText(statement.Parts.LoopCondition) != "finished" {
		t.Errorf("do parts = %#v", statement.Parts)
	}
	if statement := statements[CSTStatementLoop]; statement.Parts.Until || cstTokenText(statement.Parts.LoopCondition) != "pending" {
		t.Errorf("loop parts = %#v", statement.Parts)
	}
}

func TestParseDocumentCSTPreservesBlocksAcrossASPIslands(t *testing.T) {
	source := `<% If ready Then %><p>not vbscript</p><% Call Save(value)
End If %>`
	parsed := core.ParseDocument("file:///flow.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	cst := ParseDocumentCST(parsed)
	ifNode := firstCSTNodeByKind(cst, "If")
	if ifNode == nil {
		t.Fatal("missing cross-island If node")
	}
	call := firstCSTNodeByKind(ifNode, "Call")
	terminator := firstCSTNodeByKind(ifNode, "Terminator")
	if call == nil || terminator == nil || terminator.Statement == nil || terminator.Statement.Kind != CSTStatementEndIf {
		t.Fatalf("cross-island children = %#v", ifNode.Children)
	}
	if call.Start != strings.Index(source, "Call Save") || call.End != strings.Index(source, "Call Save")+len("Call Save(value)") {
		t.Fatalf("call range = %d:%d", call.Start, call.End)
	}
	for _, node := range flattenCSTNodes(cst) {
		if node.Statement != nil && strings.Contains(cstTokenText(node.Statement.Tokens), "notvbscript") {
			t.Fatalf("HTML became VBScript statement: %#v", node.Statement)
		}
	}
}

func TestParseDocumentCSTReusesReferenceShardTreePerParsedRevision(t *testing.T) {
	parsed := core.ParseDocument("file:///cached-flow.asp", "<% If ready Then : Save : End If %>", core.Settings{DefaultLanguage: "VBScript"})
	BuildReferenceShard(parsed)
	first := ParseDocumentCST(parsed)
	second := ParseDocumentCST(parsed)
	if second != first {
		t.Fatal("ParseDocumentCST rebuilt the CST retained by the reference shard")
	}

	clone := parsed.CloneStructural()
	cloned := ParseDocumentCST(clone)
	if cloned == first {
		t.Fatal("structural clone inherited runtime CST ownership")
	}
}

func cstTokenText(tokens []Token) string {
	var text strings.Builder
	for _, token := range tokens {
		text.WriteString(token.Text)
	}
	return text.String()
}

func firstCSTNodeByKind(root *CSTNode, kind string) *CSTNode {
	for _, node := range flattenCSTNodes(root) {
		if node.Kind == kind {
			return node
		}
	}
	return nil
}

func hasToken(tokens []Token, kind string, text string) bool {
	for _, token := range tokens {
		if token.Kind == kind && token.Text == text {
			return true
		}
	}
	return false
}

func childByKindAndName(nodes []*CSTNode, kind string, name string) *CSTNode {
	for _, node := range nodes {
		if node.Kind != kind || node.NameToken == nil || node.NameToken.Text != name {
			continue
		}
		return node
	}
	return nil
}

func flattenCSTNodes(root *CSTNode) []*CSTNode {
	nodes := []*CSTNode{root}
	for _, child := range root.Children {
		nodes = append(nodes, flattenCSTNodes(child)...)
	}
	return nodes
}
