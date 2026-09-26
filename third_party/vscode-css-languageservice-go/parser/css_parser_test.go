package parser

import "testing"

func TestParseStylesheetModernAtRules(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "scope",
			input: "@scope (.foo) to (.bar) { .foo { color: red; } }",
			want:  []NodeType{NodeTypeScope},
		},
		{
			name:  "container",
			input: "@container card (inline-size > 30em) { #inner { background-color: skyblue; } }",
			want:  []NodeType{NodeTypeContainer},
		},
		{
			name:  "property",
			input: "@property --my-color { syntax: '<color>'; inherits: false; initial-value: #c0ffee; } .after { color: red; }",
			want:  []NodeType{NodeTypePropertyAtRule, NodeTypeRuleset},
		},
		{
			name:  "viewport",
			input: "@-ms-viewport { width: 320px; height: 768px; }",
			want:  []NodeType{NodeTypeViewPort},
		},
		{
			name:  "document",
			input: "@-moz-document url(http://test) { body { color: purple; } }",
			want:  []NodeType{NodeTypeDocument},
		},
		{
			name:  "import statement",
			input: `@import url("./700.css") only screen and (max-width: 700px); body { color: red; }`,
			want:  []NodeType{NodeTypeImport, NodeTypeRuleset},
		},
		{
			name:  "namespace statement",
			input: `@namespace pref url(http://test); body { color: red; }`,
			want:  []NodeType{NodeTypeNamespace, NodeTypeRuleset},
		},
		{
			name:  "unknown block recovery",
			input: "@unknown-rule (foo) {} .foo {}",
			want:  []NodeType{NodeTypeUnknownAtRule, NodeTypeRuleset},
		},
		{
			name:  "unknown statement recovery",
			input: "@unknown-rule 'foo'; .foo {}",
			want:  []NodeType{NodeTypeUnknownAtRule, NodeTypeRuleset},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewParser().ParseStylesheet(tt.input)
			children := root.GetChildren()
			if len(children) != len(tt.want) {
				t.Fatalf("children = %v, want %v", nodeTypes(children), tt.want)
			}
			for i, want := range tt.want {
				if got := children[i].Type(); got != want {
					t.Fatalf("child %d type = %s, want %s", i, NodeTypeName(got), NodeTypeName(want))
				}
			}
		})
	}
}

func TestParseStylesheetValueNodes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "url",
			input: `a { background: url("foo.png"); }`,
			want:  []NodeType{NodeTypeURILiteral},
		},
		{
			name:  "hex color",
			input: `a { color: #fff; }`,
			want:  []NodeType{NodeTypeHexColorValue},
		},
		{
			name:  "function and numbers",
			input: `a { width: calc(50% + 20px); }`,
			want:  []NodeType{NodeTypeFunction, NodeTypeNumericValue},
		},
		{
			name:  "string",
			input: `a { content: "hello"; }`,
			want:  []NodeType{NodeTypeStringLiteral},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewParser().ParseStylesheet(tt.input)
			types := collectNodeTypes(root)
			for _, want := range tt.want {
				if !containsNodeType(types, want) {
					t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(want))
				}
			}
		})
	}
}

func TestParseStylesheetPreprocessorNodes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "scss variable followed by ruleset",
			input: `$var: 1; .foo { color: $var; }`,
			want:  []NodeType{NodeTypeVariableDeclaration, NodeTypeRuleset},
		},
		{
			name:  "less variable followed by ruleset",
			input: `@var: 1; .foo { color: @var; }`,
			want:  []NodeType{NodeTypeVariableDeclaration, NodeTypeRuleset},
		},
		{
			name:  "scss mixin",
			input: `@mixin mix($a: 1) { color: $a; }`,
			want:  []NodeType{NodeTypeMixinDeclaration},
		},
		{
			name:  "scss function",
			input: `@function size($a) { @return $a * 2; }`,
			want:  []NodeType{NodeTypeFunctionDeclaration},
		},
		{
			name:  "less mixin",
			input: `.mixin(@a: 1) { color: @a; }`,
			want:  []NodeType{NodeTypeMixinDeclaration},
		},
		{
			name:  "scss use and forward",
			input: `@use "test"; @forward "lib"; .foo { color: red; }`,
			want:  []NodeType{NodeTypeUse, NodeTypeForward, NodeTypeRuleset},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewParser().ParseStylesheet(tt.input)
			children := root.GetChildren()
			if len(children) != len(tt.want) {
				t.Fatalf("children = %v, want %v", nodeTypes(children), tt.want)
			}
			for i, want := range tt.want {
				if got := children[i].Type(); got != want {
					t.Fatalf("child %d type = %s, want %s", i, NodeTypeName(got), NodeTypeName(want))
				}
			}
		})
	}
}

func TestParseSCSSModuleMembers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "module variable",
			input: `a { color: module.$color; }`,
			want:  []NodeType{NodeTypeModule, NodeTypeVariableName},
		},
		{
			name:  "module function",
			input: `a { color: module.func($red); }`,
			want:  []NodeType{NodeTypeModule, NodeTypeFunction},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewParser().ParseStylesheet(tt.input)
			types := collectNodeTypes(root)
			for _, want := range tt.want {
				if !containsNodeType(types, want) {
					t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(want))
				}
			}
		})
	}
}

func TestCSSParserPortedNamedCases(t *testing.T) {
	runParserPortCases(t, []parserPortCase{
		{
			name:  "stylesheet - graceful handling of unknown rules",
			input: "@unknown-rule (foo) { .bar {} } foo { @unknown-rule; }",
			want:  []NodeType{NodeTypeUnknownAtRule, NodeTypeRuleset},
		},
		{
			name:  "stylesheet - unknown rules node ends properly. Microsoft/vscode#53159",
			input: "@unknown-rule (foo) {} .foo {}",
			want:  []NodeType{NodeTypeUnknownAtRule, NodeTypeRuleset},
		},
		{
			name:  "stylesheet /panic/",
			input: "#boo, far } \n.far boo {}",
		},
		{
			name:  "@font-face",
			input: "@font-face { unicode-range: U+0021-007F }",
			want:  []NodeType{NodeTypeFontFace, NodeTypeUnicodeRange},
		},
		{
			name:  "@keyframe selector",
			input: "@keyframes name { from, 20% { width: 10px; } }",
			want:  []NodeType{NodeTypeKeyframe, NodeTypeKeyframeSelector, NodeTypeDeclaration},
		},
		{
			name:  "@container query length units",
			input: "@container (min-width: 700px) { .card h2 { font-size: max(1.5em, 1.23em + 2cqi); } }",
			want:  []NodeType{NodeTypeContainer, NodeTypeRuleset, NodeTypeFunction},
		},
		{
			name:  "@import",
			input: `@import url("./700.css") only screen and (max-width: 700px);`,
			want:  []NodeType{NodeTypeImport},
		},
		{
			name:  "@supports",
			input: "@supports (display: flexbox) { body { display: flexbox } }",
			want:  []NodeType{NodeTypeSupports, NodeTypeSupportsCondition, NodeTypeRuleset},
		},
		{
			name:  "@media",
			input: "@media screen and (color), projection and (color) { body { color: red; } }",
			want:  []NodeType{NodeTypeMedia, NodeTypeMediaQuery, NodeTypeRuleset},
		},
		{
			name:  "media_list",
			input: "@media somename, othername { }",
			want:  []NodeType{NodeTypeMedia, NodeTypeMediaQuery},
		},
		{
			name:  "medium",
			input: "@media -asda34s { }",
			want:  []NodeType{NodeTypeMedia, NodeTypeMediaQuery},
		},
		{
			name:  "@page",
			input: `@page :left { margin-left: 4cm; margin-right: 3cm; }`,
			want:  []NodeType{NodeTypePage, NodeTypeDeclaration},
		},
		{
			name:  "@layer",
			input: "@layer utilities { .padding-sm { padding: .5rem; } }",
			want:  []NodeType{NodeTypeLayer, NodeTypeRuleset},
		},
		{
			name:  "operator",
			input: "+",
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseOperator)
			},
			want: []NodeType{NodeTypeOperator},
		},
		{
			name:  "combinator",
			input: "a { & > b { color: red; } }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeSelectorCombinatorParent},
		},
		{
			name:  "unary_operator",
			input: "a { margin: -45px; }",
			want:  []NodeType{NodeTypeNumericValue},
		},
		{
			name:  "ruleset",
			input: "boo { prop: value; }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeDeclaration},
		},
		{
			name:  "ruleset /Panic/",
			input: "boo { --unbalanced-curlys: {{color: green;}}",
		},
		{
			name:  "nested ruleset",
			input: ".foo { color: red; input { color: blue; } }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeRuleset, NodeTypeDeclaration},
		},
		{
			name:  "nested ruleset 2",
			input: ".foo { & .bar & .baz & .qux { color: blue; } }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeSelectorCombinatorParent, NodeTypeDeclaration},
		},
		{
			name:  "simple selector",
			input: "name.far { color: red; }",
			want:  []NodeType{NodeTypeSimpleSelector, NodeTypeElementNameSelector},
		},
		{
			name:  "element name",
			input: "foo|h1 { color: red; }",
			want:  []NodeType{NodeTypeElementNameSelector},
		},
		{
			name:  "attrib",
			input: "[name ~= name3] { color: red; }",
			want:  []NodeType{NodeTypeAttributeSelector},
		},
		{
			name:  "pseudo",
			input: ":nth-child(2n+1 of .foo) { color: red; }",
			want:  []NodeType{NodeTypePseudoSelector},
		},
		{
			name:  "declaration",
			input: "a { grid-template-columns: repeat(4, 10px [col-start] 250px [col-end]) 10px; }",
			want:  []NodeType{NodeTypeDeclaration, NodeTypeFunction},
		},
		{
			name:  "term",
			input: `a { background: url("this is a url"); }`,
			want:  []NodeType{NodeTypeURILiteral},
		},
		{
			name:  "test token prio",
			input: "a { color: red !important; }",
			want:  []NodeType{NodeTypeDeclaration, NodeTypePrio},
		},
		{
			name:  "hexcolor",
			input: "a { color: #FFFFFFFF; }",
			want:  []NodeType{NodeTypeHexColorValue},
		},
		{
			name:  "test class",
			input: ".faa42 { color: red; }",
			want:  []NodeType{NodeTypeClassSelector},
		},
		{
			name:  "prio",
			input: "a { color: red !important; }",
			want:  []NodeType{NodeTypeDeclaration, NodeTypePrio},
		},
		{
			name:  "expr",
			input: "a { margin: 5px / 6px; }",
			want:  []NodeType{NodeTypeExpression, NodeTypeNumericValue, NodeTypeOperator},
		},
	})
}

func TestSCSSParserPortedNamedCases(t *testing.T) {
	newSCSS := func() *Parser { return NewSCSSParser() }
	runParserPortCases(t, []parserPortCase{
		{
			name:   "Variable",
			input:  "$color",
			parser: newSCSS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseVariable)
			},
			want: []NodeType{NodeTypeVariableName},
		},
		{
			name:   "VariableDeclaration",
			input:  "$color: #F5F5F5",
			parser: newSCSS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, func() *Node {
					return p.parseVariableDeclaration(0, len([]rune(input)))
				})
			},
			want: []NodeType{NodeTypeVariableDeclaration, NodeTypeVariableName, NodeTypeHexColorValue},
		},
		{
			name:   "Expr",
			input:  "($const + 20)",
			parser: newSCSS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseExpression)
			},
			want: []NodeType{NodeTypeVariableName, NodeTypeOperator, NodeTypeNumericValue},
		},
		{
			name:   "SCSSOperator",
			input:  ">=",
			parser: newSCSS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseOperator)
			},
			want: []NodeType{NodeTypeOperator},
		},
		{
			name:   "Interpolation",
			input:  "--#{module.$propname}: some-value",
			parser: newSCSS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, func() *Node {
					return p.parseDeclaration(0, len([]rune(input)))
				})
			},
			want: []NodeType{NodeTypeInterpolation},
		},
		{
			name:   "Declaration",
			input:  "dummy: module.$color",
			parser: newSCSS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, func() *Node {
					return p.parseDeclaration(0, len([]rune(input)))
				})
			},
			want: []NodeType{NodeTypeDeclaration, NodeTypeModule, NodeTypeVariableName},
		},
		{name: "@import", input: `@import "test.css", "bar.css";`, parser: newSCSS, want: []NodeType{NodeTypeImport}},
		{name: "@layer", input: "@layer #{$layer} { }", parser: newSCSS, want: []NodeType{NodeTypeLayer}},
		{name: "@use", input: `@use "test" as foo;`, parser: newSCSS, want: []NodeType{NodeTypeUse}},
		{name: "@forward", input: `@forward "test" hide this;`, parser: newSCSS, want: []NodeType{NodeTypeForward}},
		{name: "@media", input: "@media #{$media} and ($feature: $value) { color: red; }", parser: newSCSS, want: []NodeType{NodeTypeMedia}},
		{name: "@debug", input: "@debug test;", parser: newSCSS, want: []NodeType{NodeTypeDebug}},
		{name: "@if", input: "@if $foo == 1 { color: red; }", parser: newSCSS, want: []NodeType{NodeTypeIf, NodeTypeDeclaration}},
		{name: "@for", input: "@for $i from 1 to 5 { width: 2em; }", parser: newSCSS, want: []NodeType{NodeTypeFor, NodeTypeDeclaration}},
		{name: "@each", input: "@each $i in 1, 2, 3 { width: 2em; }", parser: newSCSS, want: []NodeType{NodeTypeEach, NodeTypeDeclaration}},
		{name: "@while", input: "@while $i < 0 { width: 2em; }", parser: newSCSS, want: []NodeType{NodeTypeWhile, NodeTypeDeclaration}},
		{name: "@mixin", input: "@mixin large-text { color: #ff0000; }", parser: newSCSS, want: []NodeType{NodeTypeMixinDeclaration, NodeTypeDeclaration}},
		{name: "@content", input: "@content;", parser: newSCSS, want: []NodeType{NodeTypeMixinContentReference}},
		{name: "@include", input: "p { @include sexy-border(blue); }", parser: newSCSS, want: []NodeType{NodeTypeMixinReference}},
		{name: "@at-root", input: "@at-root #main2 .some-class { padding-left: 8px; }", parser: newSCSS, want: []NodeType{NodeTypeSelectorPlaceholder, NodeTypeDeclaration}},
		{name: "Ruleset", input: ".selector { prop: erty $const 1px; }", parser: newSCSS, want: []NodeType{NodeTypeRuleset, NodeTypeVariableName}},
		{name: "Nested Ruleset", input: ".class1 { $const: 1; .class { three: $const; } }", parser: newSCSS, want: []NodeType{NodeTypeRuleset, NodeTypeVariableDeclaration, NodeTypeRuleset}},
		{name: "Parent Selector", input: "a { &:hover { color: red; } }", parser: newSCSS, want: []NodeType{NodeTypeSelectorCombinatorParent}},
		{name: "Selector Placeholder", input: "%hover { color: red; }", parser: newSCSS, want: []NodeType{NodeTypeSelectorPlaceholder}},
		{
			name:   "Map",
			input:  "($key1 + 3: 1px)",
			parser: newSCSS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseExpression)
			},
			want: []NodeType{NodeTypeVariableName, NodeTypeOperator, NodeTypeNumericValue},
		},
		{name: "@font-face", input: "@font-face { unicode-range: U+0021-007F, u+1f49C, U+4??; }", parser: newSCSS, want: []NodeType{NodeTypeFontFace, NodeTypeUnicodeRange}},
		{
			name:   "if function",
			input:  "if(true, black, white)",
			parser: newSCSS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseExpression)
			},
			want: []NodeType{NodeTypeFunction},
		},
	})
}

func TestCSSUnknownAtRuleNodeEndsAtHead(t *testing.T) {
	root := NewParser().ParseStylesheet("@unknown-rule (foo) {} .foo {}")
	unknownAtRule := root.GetChild(0)
	if unknownAtRule == nil {
		t.Fatal("missing unknown at-rule child")
	}
	if unknownAtRule.Type() != NodeTypeUnknownAtRule {
		t.Fatalf("child type = %s, want %s", NodeTypeName(unknownAtRule.Type()), NodeTypeName(NodeTypeUnknownAtRule))
	}
	if unknownAtRule.Offset != 0 || unknownAtRule.Length != 13 {
		t.Fatalf("unknown at-rule range = (%d, %d), want (0, 13)", unknownAtRule.Offset, unknownAtRule.Length)
	}
}

type parserPortCase struct {
	name   string
	input  string
	parser func() *Parser
	parse  func(*Parser, string) *Node
	want   []NodeType
}

func runParserPortCases(t *testing.T, tests []parserPortCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newParser := tt.parser
			if newParser == nil {
				newParser = NewParser
			}
			parse := tt.parse
			if parse == nil {
				parse = func(p *Parser, input string) *Node {
					return p.ParseStylesheet(input)
				}
			}
			node := parse(newParser(), tt.input)
			if node == nil {
				t.Fatalf("parse(%q) returned nil", tt.input)
			}
			assertContainsNodeTypes(t, node, tt.want...)
		})
	}
}

func nodeTypes(nodes []*Node) []NodeType {
	types := make([]NodeType, len(nodes))
	for i, node := range nodes {
		types[i] = node.Type()
	}
	return types
}

func collectNodeTypes(root *Node) []NodeType {
	var types []NodeType
	root.Accept(func(node *Node) bool {
		types = append(types, node.Type())
		return true
	})
	return types
}

func containsNodeType(types []NodeType, want NodeType) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}

func parserTestNodeTypeNames(types []NodeType) []string {
	names := make([]string, len(types))
	for i, typ := range types {
		names[i] = NodeTypeName(typ)
	}
	return names
}
