package vbscript

import (
	"sort"
	"unsafe"
)

// CSTStatementRole describes how a logical statement participates in a
// structured VBScript block.
type CSTStatementRole string

const (
	// CSTStatementRoleHeader starts a structured block.
	CSTStatementRoleHeader CSTStatementRole = "header"
	// CSTStatementRoleBranch starts a branch within its enclosing block.
	CSTStatementRoleBranch CSTStatementRole = "branch"
	// CSTStatementRoleTerminator closes a structured block.
	CSTStatementRoleTerminator CSTStatementRole = "terminator"
	// CSTStatementRoleExecutable is an executable leaf statement.
	CSTStatementRoleExecutable CSTStatementRole = "executable"
)

// CSTStatementKind identifies a logical VBScript statement without requiring
// consumers to reparse its source text.
type CSTStatementKind string

const (
	CSTStatementIf          CSTStatementKind = "if"
	CSTStatementElseIf      CSTStatementKind = "elseif"
	CSTStatementElse        CSTStatementKind = "else"
	CSTStatementEndIf       CSTStatementKind = "end-if"
	CSTStatementSelect      CSTStatementKind = "select"
	CSTStatementCase        CSTStatementKind = "case"
	CSTStatementCaseElse    CSTStatementKind = "case-else"
	CSTStatementEndSelect   CSTStatementKind = "end-select"
	CSTStatementFor         CSTStatementKind = "for"
	CSTStatementForEach     CSTStatementKind = "for-each"
	CSTStatementNext        CSTStatementKind = "next"
	CSTStatementDo          CSTStatementKind = "do"
	CSTStatementLoop        CSTStatementKind = "loop"
	CSTStatementWhile       CSTStatementKind = "while"
	CSTStatementWend        CSTStatementKind = "wend"
	CSTStatementSub         CSTStatementKind = "sub"
	CSTStatementFunction    CSTStatementKind = "function"
	CSTStatementPropertyGet CSTStatementKind = "property-get"
	CSTStatementPropertyLet CSTStatementKind = "property-let"
	CSTStatementPropertySet CSTStatementKind = "property-set"
	CSTStatementEndSub      CSTStatementKind = "end-sub"
	CSTStatementEndFunction CSTStatementKind = "end-function"
	CSTStatementEndProperty CSTStatementKind = "end-property"
	CSTStatementClass       CSTStatementKind = "class"
	CSTStatementEndClass    CSTStatementKind = "end-class"
	CSTStatementExecutable  CSTStatementKind = "executable"
)

// CSTStatement is lossless metadata for one logical VBScript statement. Start
// and End are byte offsets in the input passed to ParseCST. Tokens include
// significant newlines within explicit line continuations.
type CSTStatement struct {
	Kind   CSTStatementKind
	Role   CSTStatementRole
	Start  int
	End    int
	Tokens []Token
	Parts  CSTStatementParts
}

// CSTStatementParts exposes the syntax-bearing portions used by control-flow
// consumers. Every slice retains original byte offsets.
type CSTStatementParts struct {
	Condition     []Token
	Selector      []Token
	CaseValues    []Token
	LoopCondition []Token
	InlineThen    []Token
	InlineElse    []Token
	Callee        []Token
	Until         bool
}

// CSTNode is a lightweight VBScript concrete syntax node with byte offsets.
type CSTNode struct {
	Kind      string
	Start     int
	End       int
	NameToken *Token
	Tokens    []Token
	// Statement is present exactly once for each logical statement represented
	// by the tree. Declaration alias nodes may omit it to avoid duplicate syntax.
	Statement *CSTStatement
	Children  []*CSTNode
}

// EstimateBytes reports the storage retained by a parsed CST. The estimate is
// intentionally based only on the already-built immutable tree.
func (node *CSTNode) EstimateBytes() int64 {
	estimator := cstEstimator{
		seenNodes:       map[*CSTNode]struct{}{},
		seenStatements:  map[*CSTStatement]struct{}{},
		seenTokenSlices: map[cstBackingIdentity]struct{}{},
		seenNameTokens:  map[*Token]struct{}{},
		seenChildSlices: map[cstBackingIdentity]struct{}{},
	}
	estimator.visitNode(node)
	estimator.finish()
	return estimator.bytes
}

const cstEstimateMaxInt64 = int64(1<<63 - 1)

// cstBackingIdentity identifies a slice backing by its first element and
// capacity. The element kind is kept separate by the estimator's maps.
type cstBackingIdentity struct {
	pointer  uintptr
	capacity int
}

type cstBackingRange struct {
	start uintptr
	end   uintptr
}

type cstEstimator struct {
	bytes int64

	seenNodes       map[*CSTNode]struct{}
	seenStatements  map[*CSTStatement]struct{}
	seenTokenSlices map[cstBackingIdentity]struct{}
	seenNameTokens  map[*Token]struct{}

	tokenRanges       []cstBackingRange
	tokenStringRanges []cstBackingRange
	nameTokens        []*Token

	seenChildSlices map[cstBackingIdentity]struct{}
	childRanges     []cstBackingRange
}

func (estimator *cstEstimator) visitNode(node *CSTNode) {
	if node == nil {
		return
	}
	if _, exists := estimator.seenNodes[node]; exists {
		return
	}
	estimator.seenNodes[node] = struct{}{}

	estimator.add(int64(unsafe.Sizeof(CSTNode{})))
	estimator.addScaled(len(node.Kind), 2)
	estimator.visitTokens(node.Tokens)
	if node.NameToken != nil {
		estimator.visitNameToken(node.NameToken)
	}
	if node.Statement != nil {
		estimator.visitStatement(node.Statement)
	}
	estimator.visitChildren(node.Children)
	for _, child := range node.Children {
		estimator.visitNode(child)
	}
}

func (estimator *cstEstimator) visitStatement(statement *CSTStatement) {
	if statement == nil {
		return
	}
	if _, exists := estimator.seenStatements[statement]; exists {
		return
	}
	estimator.seenStatements[statement] = struct{}{}

	estimator.add(int64(unsafe.Sizeof(CSTStatement{})))
	estimator.addScaled(len(statement.Kind), 2)
	estimator.addScaled(len(statement.Role), 2)
	estimator.visitTokens(statement.Tokens)
	estimator.visitTokens(statement.Parts.Condition)
	estimator.visitTokens(statement.Parts.Selector)
	estimator.visitTokens(statement.Parts.CaseValues)
	estimator.visitTokens(statement.Parts.LoopCondition)
	estimator.visitTokens(statement.Parts.InlineThen)
	estimator.visitTokens(statement.Parts.InlineElse)
	estimator.visitTokens(statement.Parts.Callee)
}

func (estimator *cstEstimator) visitTokens(tokens []Token) {
	if capacity := cap(tokens); capacity > 0 {
		pointer := uintptr(unsafe.Pointer(unsafe.SliceData(tokens)))
		if pointer != 0 {
			identity := cstBackingIdentity{pointer: pointer, capacity: capacity}
			if _, exists := estimator.seenTokenSlices[identity]; !exists {
				estimator.seenTokenSlices[identity] = struct{}{}
				estimator.addBackingRange(&estimator.tokenRanges, pointer, capacity, int64(unsafe.Sizeof(Token{})))
			}
		}
	}
	for _, token := range tokens {
		estimator.visitTokenStrings(token)
	}
}

func (estimator *cstEstimator) visitChildren(children []*CSTNode) {
	if capacity := cap(children); capacity == 0 {
		return
	} else {
		pointer := uintptr(unsafe.Pointer(unsafe.SliceData(children)))
		if pointer == 0 {
			return
		}
		identity := cstBackingIdentity{pointer: pointer, capacity: capacity}
		if _, exists := estimator.seenChildSlices[identity]; exists {
			return
		}
		if estimator.seenChildSlices == nil {
			estimator.seenChildSlices = map[cstBackingIdentity]struct{}{}
		}
		estimator.seenChildSlices[identity] = struct{}{}
		estimator.addBackingRange(&estimator.childRanges, pointer, capacity, int64(unsafe.Sizeof((*CSTNode)(nil))))
	}
}

func (estimator *cstEstimator) visitNameToken(token *Token) {
	if _, exists := estimator.seenNameTokens[token]; exists {
		return
	}
	estimator.seenNameTokens[token] = struct{}{}
	estimator.nameTokens = append(estimator.nameTokens, token)
	estimator.visitTokenStrings(*token)
}

func (estimator *cstEstimator) visitTokenStrings(token Token) {
	estimator.visitString(token.Kind)
	estimator.visitString(token.Text)
}

func (estimator *cstEstimator) visitString(value string) {
	if len(value) == 0 {
		return
	}
	pointer := uintptr(unsafe.Pointer(unsafe.StringData(value)))
	if pointer == 0 {
		return
	}
	appendCSTBackingRange(&estimator.tokenStringRanges, pointer, uintptr(len(value)))
}

func (estimator *cstEstimator) addBackingRange(ranges *[]cstBackingRange, pointer uintptr, capacity int, elementSize int64) {
	if pointer == 0 || capacity <= 0 || elementSize <= 0 {
		return
	}
	byteSize := uintptr(elementSize)
	maxUintptr := ^uintptr(0)
	capacityValue := uint64(capacity)
	if capacityValue > uint64(maxUintptr)/uint64(byteSize) {
		capacityValue = uint64(maxUintptr) / uint64(byteSize)
	}
	byteCount := uintptr(capacityValue) * byteSize
	if byteCount == 0 {
		return
	}
	appendCSTBackingRange(ranges, pointer, byteCount)
}

func appendCSTBackingRange(ranges *[]cstBackingRange, pointer, byteCount uintptr) {
	if pointer == 0 || byteCount == 0 {
		return
	}
	maxUintptr := ^uintptr(0)
	end := pointer
	if byteCount > maxUintptr-pointer {
		end = maxUintptr
	} else {
		end += byteCount
	}
	if end <= pointer {
		return
	}
	*ranges = append(*ranges, cstBackingRange{start: pointer, end: end})
}

func (estimator *cstEstimator) finish() {
	estimator.finishBackingRanges(&estimator.tokenRanges, int64(unsafe.Sizeof(Token{})), int64(unsafe.Sizeof(Token{})))
	estimator.finishBackingRanges(&estimator.tokenStringRanges, 1, 2)
	estimator.finishBackingRanges(&estimator.childRanges, int64(unsafe.Sizeof((*CSTNode)(nil))), int64(unsafe.Sizeof((*CSTNode)(nil))))
	for _, token := range estimator.nameTokens {
		pointer := uintptr(unsafe.Pointer(token))
		if pointer == 0 || estimator.tokenPointerInBacking(pointer) {
			continue
		}
		estimator.add(int64(unsafe.Sizeof(Token{})))
	}
}

// finishBackingRanges normalizes ranges collected during traversal and charges their union.
func (estimator *cstEstimator) finishBackingRanges(ranges *[]cstBackingRange, unitSize, bytesPerUnit int64) {
	if len(*ranges) == 0 || unitSize <= 0 || bytesPerUnit <= 0 {
		return
	}
	sort.Slice(*ranges, func(i, j int) bool { return (*ranges)[i].start < (*ranges)[j].start })
	merged := (*ranges)[:0]
	for _, current := range *ranges {
		if len(merged) == 0 || current.start > merged[len(merged)-1].end {
			merged = append(merged, current)
			continue
		}
		if current.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = current.end
		}
	}
	*ranges = merged
	byteSize := uintptr(unitSize)
	for _, current := range merged {
		units := (current.end - current.start) / byteSize
		if units > 0 {
			estimator.add(cstEstimateProductUintptr(units, bytesPerUnit))
		}
	}
}

func (estimator *cstEstimator) tokenPointerInBacking(pointer uintptr) bool {
	tokenSize := uintptr(unsafe.Sizeof(Token{}))
	for _, backing := range estimator.tokenRanges {
		if pointer < backing.start || pointer >= backing.end {
			continue
		}
		return tokenSize > 0 && (pointer-backing.start)%tokenSize == 0
	}
	return false
}

func (estimator *cstEstimator) add(value int64) {
	if value <= 0 || estimator.bytes >= cstEstimateMaxInt64 {
		return
	}
	if value > cstEstimateMaxInt64-estimator.bytes {
		estimator.bytes = cstEstimateMaxInt64
		return
	}
	estimator.bytes += value
}

func (estimator *cstEstimator) addScaled(value int, scale int64) {
	estimator.add(cstEstimateProduct(value, scale))
}

func cstEstimateProduct(value int, scale int64) int64 {
	if value <= 0 || scale <= 0 {
		return 0
	}
	intValue := int64(value)
	if intValue > cstEstimateMaxInt64/scale {
		return cstEstimateMaxInt64
	}
	return intValue * scale
}

func cstEstimateProductUintptr(value uintptr, scale int64) int64 {
	if value == 0 || scale <= 0 {
		return 0
	}
	if uint64(value) > uint64(cstEstimateMaxInt64/scale) {
		return cstEstimateMaxInt64
	}
	return int64(value) * scale
}

type NodeLookup struct {
	Nodes          []*CSTNode
	MaxEndByIndex  []int
	PreOrderByNode map[*CSTNode]int
}

func BuildNodeLookup(nodes []*CSTNode) NodeLookup {
	preOrder := PreOrderIndexForNodes(nodes)
	sorted := append([]*CSTNode(nil), nodes...)
	if !nodesSortedByStart(sorted, preOrder) {
		sort.SliceStable(sorted, func(i, j int) bool {
			return compareNodeStartAndPreOrder(sorted[i], sorted[j], preOrder) < 0
		})
	}
	maxEndByIndex := make([]int, len(sorted))
	maxEnd := -1
	for i, node := range sorted {
		if node.End > maxEnd {
			maxEnd = node.End
		}
		maxEndByIndex[i] = maxEnd
	}
	return NodeLookup{Nodes: sorted, MaxEndByIndex: maxEndByIndex, PreOrderByNode: preOrder}
}

func PreOrderIndexForNodes(nodes []*CSTNode) map[*CSTNode]int {
	index := map[*CSTNode]int{}
	for i, node := range nodes {
		index[node] = i
	}
	return index
}

func SmallestContainingNode(lookup NodeLookup, offset int) *CSTNode {
	index := lastNodeStartingAtOrBefore(lookup.Nodes, offset)
	var best *CSTNode
	bestSpan := int(^uint(0) >> 1)
	bestPreOrderIndex := int(^uint(0) >> 1)
	for index >= 0 && lookup.MaxEndByIndex[index] >= offset {
		node := lookup.Nodes[index]
		if offset >= node.Start && offset <= node.End {
			span := node.End - node.Start
			preOrderIndex := lookup.PreOrderByNode[node]
			if span < bestSpan || span == bestSpan && preOrderIndex < bestPreOrderIndex {
				best = node
				bestSpan = span
				bestPreOrderIndex = preOrderIndex
			}
			if index == 0 || lookup.Nodes[index-1].Start < node.Start {
				break
			}
		}
		index--
	}
	return best
}

func nodesSortedByStart(nodes []*CSTNode, preOrder map[*CSTNode]int) bool {
	for i := 1; i < len(nodes); i++ {
		if compareNodeStartAndPreOrder(nodes[i-1], nodes[i], preOrder) > 0 {
			return false
		}
	}
	return true
}

func compareNodeStartAndPreOrder(left, right *CSTNode, preOrder map[*CSTNode]int) int {
	if left.Start != right.Start {
		return left.Start - right.Start
	}
	return preOrder[left] - preOrder[right]
}

func lastNodeStartingAtOrBefore(nodes []*CSTNode, offset int) int {
	low := 0
	high := len(nodes) - 1
	found := -1
	for low <= high {
		middle := (low + high) / 2
		if nodes[middle].Start <= offset {
			found = middle
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	return found
}
