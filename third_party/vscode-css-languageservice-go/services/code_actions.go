package services

import (
	"math"
	"sort"
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

type rankedProperty struct {
	name  string
	score float64
}

func CodeActions(document *lsp.TextDocument, r lsp.Range, context lsp.CodeActionContext, manager *languagefacts.DataManager) []lsp.CodeAction {
	if !codeActionContextAllows(context, lsp.CodeActionKindQuickFix) {
		return nil
	}
	if manager == nil {
		manager = languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
	}
	var result []lsp.CodeAction
	for _, diagnostic := range context.Diagnostics {
		code, _ := diagnostic.Code.(string)
		if code != RuleUnknownProperty.ID {
			continue
		}
		if !propertyRangeMatchesDeclaration(document, diagnostic.Range) {
			continue
		}
		propertyName := document.GetText(&diagnostic.Range)
		for _, candidate := range rankedPropertyCandidates(propertyName, manager) {
			edit := lsp.Replace(diagnostic.Range, candidate.name)
			result = append(result, lsp.CodeAction{
				Title:       "Rename to '" + candidate.name + "'",
				Kind:        lsp.CodeActionKindQuickFix,
				Diagnostics: []lsp.Diagnostic{diagnostic},
				Edit: &lsp.WorkspaceEdit{DocumentChanges: []lsp.TextDocumentEdit{{
					TextDocument: lsp.VersionedTextDocumentIdentifier{URI: document.URI, Version: document.Version},
					Edits:        []lsp.TextEdit{edit},
				}}},
			})
		}
	}
	return result
}

func codeActionContextAllows(context lsp.CodeActionContext, kind lsp.CodeActionKind) bool {
	if len(context.Only) == 0 {
		return true
	}
	for _, requested := range context.Only {
		if requested == kind || strings.HasPrefix(string(kind), string(requested)+".") || strings.HasPrefix(string(requested), string(kind)+".") {
			return true
		}
	}
	return false
}

func propertyRangeMatchesDeclaration(document *lsp.TextDocument, r lsp.Range) bool {
	start := byteOffsetAtPosition(document, r.Start)
	end := byteOffsetAtPosition(document, r.End)
	text := document.Text()
	for _, block := range parseCSSBlocks(text) {
		if start < block.bodyStart || end > block.bodyEnd {
			continue
		}
		for _, declaration := range parseDeclarations(text, block.bodyStart, block.bodyEnd) {
			if start == declaration.nameOffset && end == declaration.nameOffset+len(declaration.name) {
				return true
			}
			if start >= declaration.nameOffset && end <= declaration.nameOffset+len(declaration.name) {
				return true
			}
		}
	}
	return false
}

func rankedPropertyCandidates(propertyName string, manager *languagefacts.DataManager) []rankedProperty {
	var candidates []rankedProperty
	limit := float64(len(propertyName)) / 2
	for _, property := range manager.GetProperties() {
		score := Difference(propertyName, property.Name, 4)
		if score >= limit {
			candidates = append(candidates, rankedProperty{name: property.Name, score: score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}
	return candidates
}

func Difference(first, second string, maxLenDelta int) float64 {
	lengthDifference := int(math.Abs(float64(len(first) - len(second))))
	if lengthDifference > maxLenDelta {
		return 0
	}
	lcs := make([][]int, len(first)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(second)+1)
	}
	for i := 1; i <= len(first); i++ {
		for j := 1; j <= len(second); j++ {
			if first[i-1] == second[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] > lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}
	return float64(lcs[len(first)][len(second)]) - math.Sqrt(float64(lengthDifference))
}
