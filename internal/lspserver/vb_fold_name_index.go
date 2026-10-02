package lspserver

import (
	"iter"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

const (
	graphVBDeclarationNamesRuntimeKey = "lspserver.graph-vb-declaration-names.runtime.v1"
	vbProcedureScopeNamesRuntimeKey   = "lspserver.vb-procedure-scope-names.runtime.v1"
	vbInlayDeclarationNamesRuntimeKey = "lspserver.vb-inlay-declaration-names.runtime.v1"
	vbParameterNamesRuntimeKey        = "lspserver.vb-parameter-names.runtime.v1"
	vbNamingDeclarationNamesKey       = "lspserver.vb-naming-declaration-names.runtime.v1"
	vbAssignmentNamesRuntimeKey       = "lspserver.vb-assignment-names.runtime.v1"
)

// vbFoldNameIndex lists the positions in a cached per-document slice whose
// names may equal a queried name under strings.EqualFold. Names with
// non-ASCII bytes are kept apart because EqualFold folds U+017F and U+212A
// onto ASCII letters. Callers still compare names with EqualFold.
type vbFoldNameIndex struct {
	count  int
	byName map[string][]int
	other  []int
}

func vbUsageDeclarationName(declaration vbUsageDeclaration) string { return declaration.Name }
func vbProcedureScopeNameOf(procedure vbProcedureScope) string     { return procedure.Name }
func vbAssignmentName(assignment vbAssignment) string              { return assignment.Name }

// vbFoldNameIndexFor returns the index for items, which must be the same
// deterministic per-document slice every time key is used.
func vbFoldNameIndexFor[T any](parsed *core.ParsedDocument, key string, items []T, name func(T) string) *vbFoldNameIndex {
	if value, ok := parsed.LoadRuntimeAnalysis(key); ok {
		if index, ok := value.(*vbFoldNameIndex); ok && index.count == len(items) {
			return index
		}
	}
	index := &vbFoldNameIndex{count: len(items), byName: map[string][]int{}}
	for position, item := range items {
		itemName := name(item)
		if vbASCIIText(itemName) {
			lowered := strings.ToLower(itemName)
			index.byName[lowered] = append(index.byName[lowered], position)
		} else {
			index.other = append(index.other, position)
		}
	}
	parsed.StoreRuntimeAnalysis(key, index)
	return index
}

// candidates yields positions in ascending order.
func (index *vbFoldNameIndex) candidates(name string) iter.Seq[int] {
	return func(yield func(int) bool) {
		if !vbASCIIText(name) {
			for position := range index.count {
				if !yield(position) {
					return
				}
			}
			return
		}
		matches, other := index.byName[strings.ToLower(name)], index.other
		for len(matches) > 0 || len(other) > 0 {
			var position int
			if len(other) == 0 || len(matches) > 0 && matches[0] < other[0] {
				position, matches = matches[0], matches[1:]
			} else {
				position, other = other[0], other[1:]
			}
			if !yield(position) {
				return
			}
		}
	}
}

func vbASCIIText(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] >= 0x80 {
			return false
		}
	}
	return true
}
