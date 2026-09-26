package parser

import (
	"strings"
	"testing"
)

func TestLESSNodeTrees(t *testing.T) {
	ruleset := func(input string) *Node {
		parser := NewLESSParser()
		return parser.InternalParse(input, parser.parseRuleset)
	}

	t.Run("RuleSet", func(t *testing.T) {
		assertNodes(t, ruleset, "selector { prop: value }", "ruleset,...,selector,...,declaration,property,...,expression,...")
		assertNodes(t, ruleset, "selector { prop; }", "ruleset,...,selector,...,property,...")
		assertNodes(t, ruleset, "selector { prop {} }", "ruleset,...,ruleset,...")
	})
}

func TestLESSParserPortedNamedCases(t *testing.T) {
	newLESS := func() *Parser { return NewLESSParser() }
	runParserPortCases(t, []parserPortCase{
		{
			name:   "Variable",
			input:  "@color",
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseVariable)
			},
			want: []NodeType{NodeTypeVariableName},
		},
		{
			name:   "Media",
			input:  "@media @phone {}",
			parser: newLESS,
			want:   []NodeType{NodeTypeMedia},
		},
		{
			name:   "VariableDeclaration",
			input:  "@color: #F5F5F5",
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, func() *Node {
					return p.parseVariableDeclaration(0, len([]rune(input)))
				})
			},
			want: []NodeType{NodeTypeVariableDeclaration, NodeTypeVariableName, NodeTypeHexColorValue},
		},
		{
			name:   "MixinDeclaration",
			input:  ".color(@color: 25.5px) { }",
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseLESSMixinDeclaration)
			},
			want: []NodeType{NodeTypeMixinDeclaration},
		},
		{
			name:   "MixinReference",
			input:  ".box-shadow(0 0 5px, 30%)",
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseLESSMixinReference)
			},
			want: []NodeType{NodeTypeMixinReference},
		},
		{name: "DetachedRuleSet", input: ".media-switch({ flex-direction: row; });", parser: newLESS, want: []NodeType{NodeTypeMixinReference}},
		{
			name:   "MixinParameter",
			input:  "@const: value",
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseLESSMixinParameter)
			},
			want: []NodeType{NodeTypeFunctionParameter, NodeTypeVariableName},
		},
		{
			name:   "Expr",
			input:  "(@const + 20)",
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseExpression)
			},
			want: []NodeType{NodeTypeVariableName, NodeTypeOperator, NodeTypeNumericValue},
		},
		{
			name:   "LessOperator",
			input:  ">=",
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseOperator)
			},
			want: []NodeType{NodeTypeOperator},
		},
		{
			name:   "Declaration",
			input:  "dummy: @color",
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, func() *Node {
					return p.parseDeclaration(0, len([]rune(input)))
				})
			},
			want: []NodeType{NodeTypeDeclaration, NodeTypeVariableName},
		},
		{name: "@iport", input: `@import (optional, reference) "foo.less";`, parser: newLESS, want: []NodeType{NodeTypeImport}},
		{name: "Ruleset", input: "selector { .mixin; }", parser: newLESS, want: []NodeType{NodeTypeRuleset, NodeTypeMixinReference}},
		{
			name:   "term",
			input:  `~"ms:alwaysHasItsOwnSyntax.For.Stuff()"`,
			parser: newLESS,
			parse: func(p *Parser, input string) *Node {
				return p.InternalParse(input, p.parseTerm)
			},
			want: []NodeType{NodeTypeEscapedValue},
		},
		{name: "Nested Ruleset", input: ".class1 { @const: 1; > .class2 { display: none; } }", parser: newLESS, want: []NodeType{NodeTypeVariableDeclaration, NodeTypeRuleset}},
		{name: "Interpolation", input: ".@{name} { } .px2rem(@name, @px) { @{name}: @px / @basesize; }", parser: newLESS, want: []NodeType{NodeTypeSelectorInterpolation, NodeTypeInterpolation}},
		{name: "Selector Combinator", input: ".root { &:hover {} }", parser: newLESS, want: []NodeType{NodeTypeSelectorCombinatorParent}},
		{name: "Merge", input: ".mixin() { transform+_: scale(2); } .myclass { box-shadow+: inset 0 0 10px #555; }", parser: newLESS, want: []NodeType{NodeTypeMixinDeclaration, NodeTypeDeclaration}},
	})
}

func TestLESSParserVariables(t *testing.T) {
	valid := []string{
		"@color",
		"$color",
		"$$color",
		"@$color",
		"$@color",
		"@co42lor",
		"@-co42lor",
		"@@foo",
		"@@@foo",
		"@12ooo",
		"@foo[]",
		"@foo[bar]",
		"@foo[@bar]",
		"@foo[$bar]",
		"@foo[@@bar]",
		"@foo[100]",
		"@foo[1prop]",
		"@foo[--prop]",
	}
	for _, input := range valid {
		t.Run(input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(input, parser.parseVariable)
			if node == nil {
				t.Fatalf("parseVariable(%q) returned nil", input)
			}
			if node.Type() != NodeTypeVariableName {
				t.Fatalf("node type = %s, want VariableName", NodeTypeName(node.Type()))
			}
			if got := node.GetText(); got != input {
				t.Fatalf("node text = %q, want %q", got, input)
			}
		})
	}

	invalid := []string{"@ @foo", "@-@foo"}
	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			parser := NewLESSParser()
			if node := parser.InternalParse(input, parser.parseVariable); node != nil {
				t.Fatalf("parseVariable(%q) = %s, want nil", input, node.GetText())
			}
		})
	}
}

func TestLESSParserVariableDeclarations(t *testing.T) {
	inputs := []string{
		"@color: #F5F5F5",
		"@color: 0",
		"@color: 255",
		"@color: 25.5",
		"@color: 25px",
		"@color: 25.5px",
		"@primary-font: \"wf_SegoeUI\",\"Segoe UI\",\"Segoe\",\"Segoe WP\"",
		"@greeting: `\"hello\".toUpperCase() + \"!\";`",
		"@greeting: { display: none; }",
		"@b: @a !important",
		"@rules: .mixin()",
		"@rules: .mixin()[]",
		"@rules: .mixin(@value)[@lookup][prop]",
		"@rules: .mixin[@lookup][prop]",
		"@expr: .mixin(@value)[] .mixin(@value2)[]",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(input, func() *Node {
				return parser.parseVariableDeclaration(0, len([]rune(input)))
			})
			if node == nil {
				t.Fatalf("parseVariableDeclaration(%q) returned nil", input)
			}
			if node.Type() != NodeTypeVariableDeclaration {
				t.Fatalf("node type = %s, want VariableDeclaration", NodeTypeName(node.Type()))
			}
			if !containsNodeType(collectNodeTypes(node), NodeTypeVariableName) {
				t.Fatalf("node types = %v, missing VariableName", parserTestNodeTypeNames(collectNodeTypes(node)))
			}
		})
	}
}

func TestLESSParserMediaAndNestedVariables(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "media variable",
			input: "@media @phone {}",
			want:  []NodeType{NodeTypeMedia},
		},
		{
			name:  "media mixin reference",
			input: "@media(max-width: 767px) { .mixinRef() }",
			want:  []NodeType{NodeTypeMedia},
		},
		{
			name:  "nested media",
			input: ".something { @media (max-width: 760px) { > div { display: block; } } }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeMedia},
		},
		{
			name:  "ruleset variable declaration and reference",
			input: ".class1 { @const: 1; three: @const; }",
			want:  []NodeType{NodeTypeVariableDeclaration, NodeTypeVariableName},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(tt.input)
			types := collectNodeTypes(root)
			for _, want := range tt.want {
				if !containsNodeType(types, want) {
					t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(want))
				}
			}
		})
	}
}

func TestLESSParserMixinDeclarations(t *testing.T) {
	inputs := []string{
		".color (@color: 25.5px) { }",
		".color(@color: 25.5px) { }",
		".color(@color) { }",
		".color(@color; @border) { }",
		".color() { }",
		".color( ) { }",
		".mixin (@a) when (@a > 10), (@a < -10) { }",
		".mixin (@a) when (isnumber(@a)) and (@a > 0) { }",
		".mixin (@b) when not (@rules[@b] >= 0) { }",
		".mixin (@b) when not (@b > 0) { }",
		".mixin (@a, @rest...) { }",
		".mixin (@a) when (lightness(@a) >= 50%) { }",
		".class(@color-list, @i: 1) when (@i <= @list-length) and (@list-length > 1) { }",
		"#color() { }",
		"#truth (@a) when (@a = true) { }",
		".color (@color; @padding: 2;) { }",
		".font-face(@source, @target) { @font-face { font-family: @source; src: local('@{target}');} }",
		".mixin-definition(@a: {}; @b: {default: works;};) { @a(); @b(); }",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(input, parser.parseLESSMixinDeclaration)
			if node == nil {
				t.Fatalf("parseLESSMixinDeclaration(%q) returned nil", input)
			}
			if node.Type() != NodeTypeMixinDeclaration {
				t.Fatalf("node type = %s, want MixinDeclaration", NodeTypeName(node.Type()))
			}
		})
	}
}

func TestLESSParserMixinReferences(t *testing.T) {
	inputs := []string{
		".box-shadow(0 0 5px, 30%)",
		".box-shadow",
		".mixin(10) !important",
		".mixin(@a: 2, @b: 1)",
		"#mixin(@a: 2, @b: 1)",
		"#bundle > .button",
		"#bundle.button",
		"#bundle #inner #button(1)",
		".mixin(#008000;)",
		".mixin ()",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(input, parser.parseLESSMixinReference)
			if node == nil {
				t.Fatalf("parseLESSMixinReference(%q) returned nil", input)
			}
			if node.Type() != NodeTypeMixinReference {
				t.Fatalf("node type = %s, want MixinReference", NodeTypeName(node.Type()))
			}
		})
	}
}

func TestLESSParserMixinParametersAndFunctions(t *testing.T) {
	parameters := []struct {
		input string
		want  []NodeType
	}{
		{input: "@_", want: []NodeType{NodeTypeFunctionParameter, NodeTypeVariableName}},
		{input: "@const: value", want: []NodeType{NodeTypeFunctionParameter, NodeTypeVariableName}},
		{input: "@const", want: []NodeType{NodeTypeFunctionParameter, NodeTypeVariableName}},
		{input: "@rest...", want: []NodeType{NodeTypeFunctionParameter, NodeTypeVariableName}},
		{input: "...", want: []NodeType{NodeTypeFunctionParameter}},
		{input: "value", want: []NodeType{NodeTypeFunctionParameter}},
		{input: "\"string\"", want: []NodeType{NodeTypeFunctionParameter, NodeTypeStringLiteral}},
		{input: "50%", want: []NodeType{NodeTypeFunctionParameter, NodeTypeNumericValue}},
	}
	for _, tt := range parameters {
		t.Run("parameter "+tt.input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(tt.input, parser.parseLESSMixinParameter)
			if node == nil {
				t.Fatalf("parseLESSMixinParameter(%q) returned nil", tt.input)
			}
			assertContainsNodeTypes(t, node, tt.want...)
		})
	}

	terms := []string{
		"%()",
		"func(a, b; bar)",
		"func({a: b();}, bar)",
		"func(.(@val) {})",
	}
	for _, input := range terms {
		t.Run("function "+input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(input, parser.parseTerm)
			if node == nil {
				t.Fatalf("parseTerm(%q) returned nil", input)
			}
			assertContainsNodeTypes(t, node, NodeTypeFunction)
		})
	}

	stylesheetFunctions := []string{
		"each(@list, .(@v) { prop: @v });",
		"each(@list, #(@v) { prop: @v });",
	}
	for _, input := range stylesheetFunctions {
		t.Run("stylesheet "+input, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(input)
			assertContainsNodeTypes(t, root, NodeTypeFunction)
		})
	}
}

func TestLESSParserImportsAndPlugin(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "import with layer",
			input: `@import url("override.css") layer;`,
			want:  []NodeType{NodeTypeImport},
		},
		{
			name:  "import once",
			input: `@import-once "lib";`,
			want:  []NodeType{NodeTypeImport},
		},
		{
			name:  "import options",
			input: `@import (optional, reference) "foo.less";`,
			want:  []NodeType{NodeTypeImport},
		},
		{
			name:  "css import option",
			input: `@import (css) "lib";`,
			want:  []NodeType{NodeTypeImport},
		},
		{
			name:  "trailing comma import option",
			input: `@import (optional, reference,) "foo.less";`,
			want:  []NodeType{NodeTypeImport},
		},
		{
			name:  "plugin",
			input: `@plugin "my-plugin";`,
			want:  []NodeType{NodeTypePlugin},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(tt.input)
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

func TestLESSParserExtendsAndSelectorCombinators(t *testing.T) {
	extendInputs := []string{
		"nav { &:extend(.inline); }",
		"nav { &:extend(.test all); }",
		".big-bucket:extend(.bucket all) { }",
		".some-class:extend(tr .bucket) {}",
		".c:extend(.a, .b) {}",
		".d { &:extend(.a, .b); }",
	}
	for _, input := range extendInputs {
		t.Run("extend "+input, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(input)
			assertContainsNodeTypes(t, root, NodeTypeExtendsReference)
		})
	}

	selectorInputs := []string{
		".root { &:hover {} }",
		".root { &.float {} }",
		".root { &-foo {} }",
		".root { &-1 {} }",
		".root { &1 {} }",
		".root { &-foo-1 {} }",
		".root { &-foo-1-2 {} }",
		".root { &--& {} }",
		".root { &-10-thing {} }",
	}
	for _, input := range selectorInputs {
		t.Run("selector "+input, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(input)
			assertContainsNodeTypes(t, root, NodeTypeSelectorCombinatorParent)
		})
	}
}

func TestLESSParserRulesetKeyframesAndAdditionalGuards(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "keyframes in ruleset",
			input: "selector { property: value; @keyframes foo {} @-moz-keyframes foo {} }",
			want:  []NodeType{NodeTypeDeclaration, NodeTypeKeyframe, NodeTypeKeyframe},
		},
		{
			name:  "comma nested selectors",
			input: "selector { nested, a, b {} }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeRuleset},
		},
		{
			name:  "multi selector guard",
			input: ".something .other when (@my-option = true) { color: white; }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeDeclaration},
		},
		{
			name:  "parent selector guard",
			input: "& when (@my-option = true) { button { color: white; } }",
			want:  []NodeType{NodeTypeSelectorCombinatorParent, NodeTypeRuleset},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(tt.input)
			types := collectNodeTypes(root)
			for _, want := range tt.want {
				index := indexNodeType(types, want)
				if index == -1 {
					t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(want))
				}
				types = types[index+1:]
			}
		})
	}
}

func TestLESSParserRulesetMixinReferences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "bare mixin reference",
			input: "selector { .mixin; }",
			want:  []NodeType{NodeTypeMixinReference},
		},
		{
			name:  "call mixin references",
			input: "selector { .mixin(1px); .mixin(blue, 1px, 'farboo'); }",
			want:  []NodeType{NodeTypeMixinReference, NodeTypeMixinReference},
		},
		{
			name:  "semicolon separated arguments",
			input: "selector { .mixin(blue; 1px;'farboo'); }",
			want:  []NodeType{NodeTypeMixinReference},
		},
		{
			name:  "variable mixin reference",
			input: ".foo { @greeting(); }",
			want:  []NodeType{NodeTypeMixinReference},
		},
		{
			name:  "nested import",
			input: `selector { @import "bar"; }`,
			want:  []NodeType{NodeTypeImport},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(tt.input)
			types := collectNodeTypes(root)
			for _, want := range tt.want {
				index := indexNodeType(types, want)
				if index == -1 {
					t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(want))
				}
				types = types[index+1:]
			}
		})
	}
}

func TestLESSParserStylesheetMixinAndDetachedRulesets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "stylesheet mixin declarations",
			input: ".mixin (@a, @rest...) {} .mixin (@a) when (lightness(@a) >= 50%) { background-color: black; }",
			want:  []NodeType{NodeTypeMixinDeclaration, NodeTypeMixinDeclaration},
		},
		{
			name:  "ruleset containing mixin reference",
			input: ".some-mixin { font-weight:bold; } h1 { .some-mixin; font-size:40px; }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeRuleset, NodeTypeMixinReference},
		},
		{
			name:  "top-level mixin reference",
			input: ".generate-columns(1);",
			want:  []NodeType{NodeTypeMixinReference},
		},
		{
			name:  "detached ruleset variables without semicolons",
			input: "@dr1: {f:b}\n@dr2: {f:b}",
			want:  []NodeType{NodeTypeVariableDeclaration, NodeTypeVariableDeclaration},
		},
		{
			name:  "detached ruleset mixin call",
			input: ".media-switch({ flex-direction: row; });",
			want:  []NodeType{NodeTypeMixinReference},
		},
		{
			name:  "detached nested ruleset mixin call",
			input: ".foo(10px; { .bar { .baz { color: red; }}});",
			want:  []NodeType{NodeTypeMixinReference},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(tt.input)
			types := collectNodeTypes(root)
			for _, want := range tt.want {
				index := indexNodeType(types, want)
				if index == -1 {
					t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(want))
				}
				types = types[index+1:]
			}
		})
	}
}

func TestLESSParserExpressions(t *testing.T) {
	inputs := []string{
		"(@const + 20)",
		"(@const - 20)",
		"(@const * 20)",
		"(@const / 20)",
		"(20 + @const)",
		"(20 - @const)",
		"(20 * @const)",
		"(20 / @const)",
		"(20 / 20 + @const)",
		"(20 + 20 + @const)",
		"(20 + 20 + 20 + @const)",
		"(20 + 20 + 20 + 20 + @const)",
		"(20 + 20 + @const + 20 + 20 + @const)",
		"(20 + 20)",
		"(@var1 + @var2)",
		"((@const + 5) * 2)",
		"((@const + (5 + 2)) * 2)",
		"(@const + ((5 + 2) * 2))",
		"@color",
		"@color, @color",
		"@color, 42%",
		"@color, 42%, @color",
		"@color - (@color + 10%)",
		"(@base + @filler)",
		"(100% / 2 + @filler)",
		"100% / 2 + @filler",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(input, parser.parseExpression)
			if node == nil {
				t.Fatalf("parseExpression(%q) returned nil", input)
			}
			if node.Type() != NodeTypeExpression {
				t.Fatalf("node type = %s, want Expression", NodeTypeName(node.Type()))
			}
			types := collectNodeTypes(node)
			if strings.Contains(input, "@") && !containsNodeType(types, NodeTypeVariableName) {
				t.Fatalf("node types = %v, missing VariableName", parserTestNodeTypeNames(types))
			}
			if hasNumericLiteral(input) && !containsNodeType(types, NodeTypeNumericValue) {
				t.Fatalf("node types = %v, missing NumericValue", parserTestNodeTypeNames(types))
			}
			if strings.ContainsAny(input, "+-*/") && !containsNodeType(types, NodeTypeOperator) {
				t.Fatalf("node types = %v, missing Operator", parserTestNodeTypeNames(types))
			}
		})
	}
}

func TestLESSParserOperators(t *testing.T) {
	for _, input := range []string{">=", ">", "<", "=<"} {
		t.Run(input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(input, parser.parseOperator)
			if node == nil {
				t.Fatalf("parseOperator(%q) returned nil", input)
			}
			if node.Type() != NodeTypeOperator {
				t.Fatalf("node type = %s, want Operator", NodeTypeName(node.Type()))
			}
			if got := node.GetText(); got != input {
				t.Fatalf("node text = %q, want %q", got, input)
			}
		})
	}
}

func TestLESSParserDeclarations(t *testing.T) {
	tests := []struct {
		input string
		want  []NodeType
	}{
		{input: "border: thin solid 1px", want: []NodeType{NodeTypeNumericValue}},
		{input: "dummy: @color", want: []NodeType{NodeTypeVariableName}},
		{input: "dummy: blue", want: nil},
		{input: "dummy: (20 / @const)", want: []NodeType{NodeTypeNumericValue, NodeTypeOperator, NodeTypeVariableName}},
		{input: "dummy: (20 / 20 + @const)", want: []NodeType{NodeTypeNumericValue, NodeTypeOperator, NodeTypeVariableName}},
		{input: "dummy: func(@red)", want: []NodeType{NodeTypeFunction}},
		{input: "dummy: desaturate(@red, 10%)", want: []NodeType{NodeTypeFunction, NodeTypeNumericValue}},
		{input: "dummy: desaturate(16, 10%)", want: []NodeType{NodeTypeFunction, NodeTypeNumericValue}},
		{input: "100: 100", want: []NodeType{NodeTypeNumericValue}},
		{input: "1prop: 100", want: []NodeType{NodeTypeNumericValue}},
		{input: "1@{var}prop: 100", want: []NodeType{NodeTypeInterpolation, NodeTypeNumericValue}},
		{input: "--100: 100", want: []NodeType{NodeTypeNumericValue}},
		{input: "--1prop: 100", want: []NodeType{NodeTypeNumericValue}},
		{input: "color: @base-color + #111", want: []NodeType{NodeTypeVariableName, NodeTypeOperator, NodeTypeHexColorValue}},
		{input: "color: 100% / 2 + @ref", want: []NodeType{NodeTypeNumericValue, NodeTypeOperator, NodeTypeVariableName}},
		{input: "border: (@width * 2) solid black", want: []NodeType{NodeTypeVariableName, NodeTypeOperator, NodeTypeNumericValue}},
		{input: "property: @class", want: []NodeType{NodeTypeVariableName}},
		{input: "prop-erty: fnc(@t, 10%)", want: []NodeType{NodeTypeFunction, NodeTypeNumericValue}},
		{input: "background: url(//yourdomain/yourpath.png)", want: []NodeType{NodeTypeURILiteral}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(tt.input, func() *Node {
				return parser.parseDeclaration(0, len([]rune(tt.input)))
			})
			if node == nil {
				t.Fatalf("parseDeclaration(%q) returned nil", tt.input)
			}
			if node.Type() != NodeTypeDeclaration {
				t.Fatalf("node type = %s, want Declaration", NodeTypeName(node.Type()))
			}
			types := collectNodeTypes(node)
			for _, want := range tt.want {
				if !containsNodeType(types, want) {
					t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(want))
				}
			}
		})
	}
}

func TestLESSParserTermsAndURLs(t *testing.T) {
	termTests := []struct {
		input string
		want  NodeType
	}{
		{input: "%('repetitions: %S file: %S', 1 + 2, \"directory/file.less\")", want: NodeTypeFunction},
		{input: "~\"ms:alwaysHasItsOwnSyntax.For.Stuff()\"", want: NodeTypeEscapedValue},
		{input: "~`colorPaconstte(\"@{blue}\", 1)`", want: NodeTypeEscapedValue},
	}
	for _, tt := range termTests {
		t.Run(tt.input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(tt.input, parser.parseTerm)
			if node == nil {
				t.Fatalf("parseTerm(%q) returned nil", tt.input)
			}
			if !containsNodeType(collectNodeTypes(node), tt.want) {
				t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(collectNodeTypes(node)), NodeTypeName(tt.want))
			}
		})
	}

	urls := []string{
		"url(//yourdomain/yourpath.png)",
		"url('http://msft.com')",
		"url(\"http://msft.com\")",
		"url( \"http://msft.com\")",
		"url(\t\"http://msft.com\")",
		"url(\n\"http://msft.com\")",
		"url(\"http://msft.com\"\n)",
		"url(\"\")",
		"uRL(\"\")",
		"URL(\"\")",
		"url(http://msft.com)",
		"url()",
	}
	for _, input := range urls {
		t.Run(input, func(t *testing.T) {
			parser := NewLESSParser()
			node := parser.InternalParse(input, parser.parseURILiteral)
			if node == nil {
				t.Fatalf("parseURILiteral(%q) returned nil", input)
			}
			if node.Type() != NodeTypeURILiteral {
				t.Fatalf("node type = %s, want URILiteral", NodeTypeName(node.Type()))
			}
		})
	}
}

func TestLESSParserNestedInterpolationGuardsMergeAndContainer(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []NodeType
	}{
		{
			name:  "nested variables and ruleset",
			input: ".class1 { @const: 1; .class { @const: 2; three: @const; const: 3; } one: @const; }",
			want:  []NodeType{NodeTypeVariableDeclaration, NodeTypeRuleset, NodeTypeVariableName},
		},
		{
			name:  "child selector nested ruleset",
			input: ".class1 { @const: 1; > .class2 { display: none; } }",
			want:  []NodeType{NodeTypeVariableDeclaration, NodeTypeRuleset},
		},
		{
			name:  "nested supports ruleset",
			input: ".foo { @supports(display: grid) { .bar { display: none; }}}",
			want:  []NodeType{NodeTypeSupports, NodeTypeRuleset},
		},
		{
			name:  "nested supports declarations",
			input: ".foo { @supports(display: grid) { display: none; }}",
			want:  []NodeType{NodeTypeSupports, NodeTypeDeclaration},
		},
		{
			name:  "document nested ruleset",
			input: ".parent { color:green; @document url-prefix() { .child { color:red; }}}",
			want:  []NodeType{NodeTypeDocument, NodeTypeRuleset},
		},
		{
			name:  "selector interpolation",
			input: ".@{name} { } .${name} { } .my-element:not(.prefix-@{sub-element}) { } .-@{color} { } .--@{color} { }",
			want:  []NodeType{NodeTypeSelectorInterpolation},
		},
		{
			name:  "property interpolation",
			input: ".px2rem(@name, @px) { @{name}: @px / @basesize; }",
			want:  []NodeType{NodeTypeInterpolation, NodeTypeVariableName, NodeTypeOperator},
		},
		{
			name:  "css guards",
			input: ".selector when not ( @testCondition = 2) and not ( @testCondition = 3 ) { } button when (@my-option = true) { color: white; }",
			want:  []NodeType{NodeTypeRuleset, NodeTypeRuleset},
		},
		{
			name:  "merge properties",
			input: ".mixin() { transform+_: scale(2); } .myclass { box-shadow+: inset 0 0 10px #555; }",
			want:  []NodeType{NodeTypeMixinDeclaration, NodeTypeDeclaration, NodeTypeRuleset, NodeTypeDeclaration},
		},
		{
			name:  "container nested ruleset",
			input: ".item-icon { @container (max-height: 100px) { .item-icon { display: none; } } }",
			want:  []NodeType{NodeTypeContainer, NodeTypeRuleset},
		},
		{
			name:  "container nested declaration",
			input: ":root { @container (max-height: 100px) { display: none;} }",
			want:  []NodeType{NodeTypeContainer, NodeTypeDeclaration},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewLESSParser().ParseStylesheet(tt.input)
			types := collectNodeTypes(root)
			for _, want := range tt.want {
				index := indexNodeType(types, want)
				if index == -1 {
					t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(want))
				}
				types = types[index+1:]
			}
		})
	}
}

func indexNodeType(types []NodeType, want NodeType) int {
	for i, typ := range types {
		if typ == want {
			return i
		}
	}
	return -1
}

func assertContainsNodeTypes(t *testing.T, root *Node, want ...NodeType) {
	t.Helper()
	types := collectNodeTypes(root)
	for _, typ := range want {
		if !containsNodeType(types, typ) {
			t.Fatalf("node types = %v, missing %s", parserTestNodeTypeNames(types), NodeTypeName(typ))
		}
	}
}

func hasNumericLiteral(input string) bool {
	for i, r := range input {
		if r < '0' || r > '9' {
			continue
		}
		if i == 0 {
			return true
		}
		prev := rune(input[i-1])
		if prev != '@' && prev != '$' && prev != '_' && prev != '-' && (prev < 'a' || prev > 'z') && (prev < 'A' || prev > 'Z') {
			return true
		}
	}
	return false
}
