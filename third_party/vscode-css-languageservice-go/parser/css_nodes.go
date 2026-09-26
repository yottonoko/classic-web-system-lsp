package parser

// NodeType identifies the kind of parser node.
type NodeType int

const (
	NodeTypeUndefined NodeType = iota
	NodeTypeIdentifier
	NodeTypeStylesheet
	NodeTypeRuleset
	NodeTypeSelector
	NodeTypeSimpleSelector
	NodeTypeSelectorInterpolation
	NodeTypeSelectorCombinator
	NodeTypeSelectorCombinatorParent
	NodeTypeSelectorCombinatorSibling
	NodeTypeSelectorCombinatorAllSiblings
	NodeTypeSelectorCombinatorShadowPiercingDescendant
	NodeTypePage
	NodeTypePageBoxMarginBox
	NodeTypeClassSelector
	NodeTypeIdentifierSelector
	NodeTypeElementNameSelector
	NodeTypePseudoSelector
	NodeTypeAttributeSelector
	NodeTypeDeclaration
	NodeTypeDeclarations
	NodeTypeProperty
	NodeTypeExpression
	NodeTypeBinaryExpression
	NodeTypeTerm
	NodeTypeOperator
	NodeTypeValue
	NodeTypeStringLiteral
	NodeTypeURILiteral
	NodeTypeEscapedValue
	NodeTypeFunction
	NodeTypeNumericValue
	NodeTypeHexColorValue
	NodeTypeRatioValue
	NodeTypeMixinDeclaration
	NodeTypeMixinReference
	NodeTypeVariableName
	NodeTypeVariableDeclaration
	NodeTypePrio
	NodeTypeInterpolation
	NodeTypeNestedProperties
	NodeTypeExtendsReference
	NodeTypeSelectorPlaceholder
	NodeTypeDebug
	NodeTypeIf
	NodeTypeElse
	NodeTypeFor
	NodeTypeEach
	NodeTypeWhile
	NodeTypeMixinContentReference
	NodeTypeMixinContentDeclaration
	NodeTypeMedia
	NodeTypeScope
	NodeTypeKeyframe
	NodeTypeFontFace
	NodeTypeImport
	NodeTypeNamespace
	NodeTypeInvocation
	NodeTypeFunctionDeclaration
	NodeTypeReturnStatement
	NodeTypeMediaQuery
	NodeTypeMediaCondition
	NodeTypeMediaFeature
	NodeTypeFunctionParameter
	NodeTypeFunctionArgument
	NodeTypeKeyframeSelector
	NodeTypeViewPort
	NodeTypeDocument
	NodeTypeAtApplyRule
	NodeTypeCustomPropertyDeclaration
	NodeTypeCustomPropertySet
	NodeTypeListEntry
	NodeTypeSupports
	NodeTypeSupportsCondition
	NodeTypeNamespacePrefix
	NodeTypeGridLine
	NodeTypePlugin
	NodeTypeUnknownAtRule
	NodeTypeUse
	NodeTypeModuleConfiguration
	NodeTypeForward
	NodeTypeForwardVisibility
	NodeTypeModule
	NodeTypeUnicodeRange
	NodeTypeLayer
	NodeTypeLayerNameList
	NodeTypeLayerName
	NodeTypePropertyAtRule
	NodeTypeContainer
	NodeTypeModuleConfig
	NodeTypeSelectorList
	NodeTypeStartingStyleAtRule
)

// ReferenceType identifies the kind of symbol reference.
type ReferenceType int

const (
	ReferenceTypeMixin ReferenceType = iota
	ReferenceTypeRule
	ReferenceTypeVariable
	ReferenceTypeFunction
	ReferenceTypeKeyframe
	ReferenceTypeUnknown
	ReferenceTypeModule
	ReferenceTypeForward
	ReferenceTypeForwardVisibility
	ReferenceTypeProperty
)

// TextProvider resolves source text for a parser node range.
type TextProvider func(offset, length int) string

// VisitorFunc visits a parser node and controls child traversal.
type VisitorFunc func(*Node) bool

// Visitor visits parser nodes during tree traversal.
type Visitor interface {
	VisitNode(*Node) bool
}

// Node represents a CSS, LESS, or SCSS parser tree node.
type Node struct {
	Parent       *Node
	Offset       int
	Length       int
	Options      map[string]any
	TextProvider TextProvider
	children     []*Node
	nodeType     NodeType
}

func NewNode(offset, length int, nodeType ...NodeType) *Node {
	typ := NodeTypeUndefined
	if len(nodeType) > 0 {
		typ = nodeType[0]
	}
	return &Node{Offset: offset, Length: length, nodeType: typ}
}

func (n *Node) End() int {
	return n.Offset + n.Length
}

func (n *Node) Type() NodeType {
	if n == nil {
		return NodeTypeUndefined
	}
	return n.nodeType
}

func (n *Node) SetType(nodeType NodeType) {
	n.nodeType = nodeType
}

func (n *Node) getTextProvider() TextProvider {
	for node := n; node != nil; node = node.Parent {
		if node.TextProvider != nil {
			return node.TextProvider
		}
	}
	return func(offset, length int) string { return "unknown" }
}

func (n *Node) GetText() string {
	return n.getTextProvider()(n.Offset, n.Length)
}

func (n *Node) Matches(str string) bool {
	return n.Length == len([]rune(str)) && n.GetText() == str
}

func (n *Node) StartsWith(str string) bool {
	return n.Length >= len([]rune(str)) && n.getTextProvider()(n.Offset, len([]rune(str))) == str
}

func (n *Node) EndsWith(str string) bool {
	size := len([]rune(str))
	return n.Length >= size && n.getTextProvider()(n.End()-size, size) == str
}

func (n *Node) Accept(visitor VisitorFunc) {
	if n == nil {
		return
	}
	if visitor(n) {
		for _, child := range n.children {
			child.Accept(visitor)
		}
	}
}

func (n *Node) AcceptVisitor(visitor Visitor) {
	n.Accept(visitor.VisitNode)
}

func (n *Node) AdoptChild(child *Node, index ...int) *Node {
	if child == nil {
		return nil
	}
	if child.Parent != nil {
		child.Parent.removeChild(child)
	}
	child.Parent = n
	insert := -1
	if len(index) > 0 {
		insert = index[0]
	}
	if insert != -1 {
		n.children = append(n.children, nil)
		copy(n.children[insert+1:], n.children[insert:])
		n.children[insert] = child
	} else {
		n.children = append(n.children, child)
	}
	return child
}

func (n *Node) AttachTo(parent *Node, index ...int) *Node {
	if parent != nil {
		parent.AdoptChild(n, index...)
	}
	return n
}

func (n *Node) AddChild(child *Node) bool {
	if child == nil {
		return false
	}
	n.AdoptChild(child)
	n.updateOffsetAndLength(child)
	return true
}

func (n *Node) AddChildren(children []*Node) {
	for _, child := range children {
		n.AddChild(child)
	}
}

func (n *Node) HasChildren() bool {
	return len(n.children) > 0
}

func (n *Node) GetChildren() []*Node {
	return append([]*Node(nil), n.children...)
}

func (n *Node) GetChild(index int) *Node {
	if index >= 0 && index < len(n.children) {
		return n.children[index]
	}
	return nil
}

func (n *Node) FindFirstChildBeforeOffset(offset int) *Node {
	for i := len(n.children) - 1; i >= 0; i-- {
		if n.children[i].Offset <= offset {
			return n.children[i]
		}
	}
	return nil
}

func (n *Node) FindChildAtOffset(offset int, goDeep bool) *Node {
	current := n.FindFirstChildBeforeOffset(offset)
	if current != nil && current.End() >= offset {
		if goDeep {
			if child := current.FindChildAtOffset(offset, true); child != nil {
				return child
			}
		}
		return current
	}
	return nil
}

func (n *Node) Encloses(candidate *Node) bool {
	return n.Offset <= candidate.Offset && n.End() >= candidate.End()
}

func (n *Node) GetParent() *Node {
	result := n.Parent
	for result != nil && result.Type() == NodeTypeUndefined && result.Offset == -1 && result.Length == -1 {
		result = result.Parent
	}
	return result
}

func (n *Node) FindParent(nodeType NodeType) *Node {
	for result := n; result != nil; result = result.Parent {
		if result.Type() == nodeType {
			return result
		}
	}
	return nil
}

func (n *Node) FindAParent(types ...NodeType) *Node {
	for result := n; result != nil; result = result.Parent {
		for _, typ := range types {
			if result.Type() == typ {
				return result
			}
		}
	}
	return nil
}

func (n *Node) SetData(key string, value any) {
	if n.Options == nil {
		n.Options = map[string]any{}
	}
	n.Options[key] = value
}

func (n *Node) GetData(key string) any {
	if n.Options == nil {
		return nil
	}
	return n.Options[key]
}

func (n *Node) updateOffsetAndLength(child *Node) {
	if child.Offset < n.Offset || n.Offset == -1 {
		n.Offset = child.Offset
	}
	if child.End() > n.End() || n.Length == -1 {
		n.Length = child.End() - n.Offset
	}
}

func (n *Node) removeChild(child *Node) {
	for i, existing := range n.children {
		if existing == child {
			n.children = append(n.children[:i], n.children[i+1:]...)
			return
		}
	}
}

func GetNodeAtOffset(node *Node, offset int) *Node {
	var candidate *Node
	if node == nil || offset < node.Offset || offset > node.End() {
		return nil
	}
	node.Accept(func(current *Node) bool {
		if current.Offset == -1 && current.Length == -1 {
			return true
		}
		if current.Offset <= offset && current.End() >= offset {
			if candidate == nil || current.Length <= candidate.Length {
				candidate = current
			}
			return true
		}
		return false
	})
	return candidate
}

func GetNodePath(node *Node, offset int) []*Node {
	candidate := GetNodeAtOffset(node, offset)
	var path []*Node
	for candidate != nil {
		path = append([]*Node{candidate}, path...)
		candidate = candidate.Parent
	}
	return path
}

func NodeTypeName(nodeType NodeType) string {
	if int(nodeType) >= 0 && int(nodeType) < len(nodeTypeNames) {
		return nodeTypeNames[nodeType]
	}
	return "Undefined"
}

var nodeTypeNames = []string{
	"Undefined",
	"Identifier",
	"Stylesheet",
	"Ruleset",
	"Selector",
	"SimpleSelector",
	"SelectorInterpolation",
	"SelectorCombinator",
	"SelectorCombinatorParent",
	"SelectorCombinatorSibling",
	"SelectorCombinatorAllSiblings",
	"SelectorCombinatorShadowPiercingDescendant",
	"Page",
	"PageBoxMarginBox",
	"ClassSelector",
	"IdentifierSelector",
	"ElementNameSelector",
	"PseudoSelector",
	"AttributeSelector",
	"Declaration",
	"Declarations",
	"Property",
	"Expression",
	"BinaryExpression",
	"Term",
	"Operator",
	"Value",
	"StringLiteral",
	"URILiteral",
	"EscapedValue",
	"Function",
	"NumericValue",
	"HexColorValue",
	"RatioValue",
	"MixinDeclaration",
	"MixinReference",
	"VariableName",
	"VariableDeclaration",
	"Prio",
	"Interpolation",
	"NestedProperties",
	"ExtendsReference",
	"SelectorPlaceholder",
	"Debug",
	"If",
	"Else",
	"For",
	"Each",
	"While",
	"MixinContentReference",
	"MixinContentDeclaration",
	"Media",
	"Scope",
	"Keyframe",
	"FontFace",
	"Import",
	"Namespace",
	"Invocation",
	"FunctionDeclaration",
	"ReturnStatement",
	"MediaQuery",
	"MediaCondition",
	"MediaFeature",
	"FunctionParameter",
	"FunctionArgument",
	"KeyframeSelector",
	"ViewPort",
	"Document",
	"AtApplyRule",
	"CustomPropertyDeclaration",
	"CustomPropertySet",
	"ListEntry",
	"Supports",
	"SupportsCondition",
	"NamespacePrefix",
	"GridLine",
	"Plugin",
	"UnknownAtRule",
	"Use",
	"ModuleConfiguration",
	"Forward",
	"ForwardVisibility",
	"Module",
	"UnicodeRange",
	"Layer",
	"LayerNameList",
	"LayerName",
	"PropertyAtRule",
	"Container",
	"ModuleConfig",
	"SelectorList",
	"StartingStyleAtRule",
}
