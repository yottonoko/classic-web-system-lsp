package parser

import (
	"strings"
	"testing"
)

type printingVisitor struct {
	tree []string
}

func (v *printingVisitor) VisitNode(node *Node) bool {
	v.tree = append(v.tree, strings.ToLower(NodeTypeName(node.Type())))
	return true
}

func assertNodes(t *testing.T, fn func(string) *Node, input, expected string) {
	t.Helper()
	node := fn(input)
	visitor := &printingVisitor{}
	node.AcceptVisitor(visitor)
	actual := append([]string(nil), visitor.tree...)
	actualStr := strings.Join(actual, ",")
	segments := strings.Split(expected, ",")
	for len(segments) > 0 {
		expectedSegment := segments[0]
		segments = segments[1:]
		if expectedSegment == "..." && (len(segments) == 0 || segments[0] == "...") {
			if len(segments) == 0 {
				actual = nil
			}
			continue
		}
		if len(actual) == 0 {
			t.Fatalf("%s not found in actual: %q", expectedSegment, actualStr)
		}
		actualSegment := actual[0]
		actual = actual[1:]
		if expectedSegment == "..." {
			if len(segments) == 0 {
				actual = nil
				continue
			}
			nextExpectedSegment := segments[0]
			for len(actual) > 0 && nextExpectedSegment != actual[0] {
				actual = actual[1:]
			}
			continue
		}
		if actualSegment != expectedSegment {
			t.Fatalf("%s not found in actual: %q", expectedSegment, actualStr)
		}
	}
	if len(actual) != 0 {
		t.Fatalf("%s not found in expected: %q", strings.Join(actual, ","), expected)
	}
}

func TestNode(t *testing.T) {
	node := NewNode(-1, -1)
	if node.Offset != -1 || node.Length != -1 || node.Parent != nil || len(node.GetChildren()) != 0 {
		t.Fatalf("unexpected empty node: %+v", node)
	}
	count := 0
	node.Accept(func(n *Node) bool {
		if n != node {
			t.Fatalf("visited wrong node")
		}
		count++
		return true
	})
	if count != 1 {
		t.Fatalf("visited %d nodes, want 1", count)
	}
	child := NewNode(-1, -1)
	node.AdoptChild(child)
	count = 0
	expects := []*Node{node, child}
	node.Accept(func(n *Node) bool {
		if n != expects[count] {
			t.Fatalf("visited wrong node at %d", count)
		}
		count++
		return true
	})
	if count != 2 {
		t.Fatalf("visited %d nodes, want 2", count)
	}
}

func TestAdopting(t *testing.T) {
	child := NewNode(-1, -1)
	p1 := NewNode(-1, -1)
	p2 := NewNode(-1, -1)
	if child.Parent != nil || len(p1.GetChildren()) != 0 || len(p2.GetChildren()) != 0 {
		t.Fatal("unexpected initial parent/children")
	}
	child = p1.AdoptChild(child)
	if child.Parent != p1 || len(p1.GetChildren()) != 1 || len(p2.GetChildren()) != 0 {
		t.Fatal("first adoption failed")
	}
	child = p2.AdoptChild(child)
	if child.Parent != p2 || len(p1.GetChildren()) != 0 || len(p2.GetChildren()) != 1 {
		t.Fatal("second adoption failed")
	}
}

func TestNodeTrees(t *testing.T) {
	ruleset := func(input string) *Node {
		parser := NewParser()
		return parser.InternalParse(input, parser.parseRuleset)
	}
	stylesheet := func(input string) *Node {
		parser := NewParser()
		return parser.InternalParse(input, parser.parseStylesheet)
	}
	t.Run("RuleSet", func(t *testing.T) {
		assertNodes(t, ruleset, "selector{prop:value}", "ruleset,...,selector,simpleselector,elementnameselector,identifier,declarations,declaration,property,...")
		assertNodes(t, ruleset, "selector { prop: value }", "ruleset,...,selector,...,declaration,property,...,expression,...")
		assertNodes(t, ruleset, "selector { prop; }", "ruleset,...,selector,...")
	})

	keyframe := func(input string) *Node {
		parser := NewParser()
		return parser.InternalParse(input, parser.parseKeyframe)
	}
	t.Run("Keyframe", func(t *testing.T) {
		assertNodes(t, keyframe, "@keyframes name { from { top: 0px} to { top: 100px } }", "keyframe,identifier,...,keyframeselector,...,declaration,...,keyframeselector,...,declaration,...")
	})

	startingStyle := func(input string) *Node {
		parser := NewParser()
		return parser.InternalParse(input, parser.parseStartingStyleAtRule)
	}
	t.Run("Starting-style", func(t *testing.T) {
		assertNodes(t, startingStyle, "@starting-style { p { opacity: 0; } }", "startingstyleatrule,declarations,ruleset,...,selector,...,elementnameselector,...,...,...,...,...,...,...,...,...")
	})

	fontFace := func(input string) *Node {
		parser := NewParser()
		return parser.InternalParse(input, parser.parseFontFace)
	}
	t.Run("UnicodeRange", func(t *testing.T) {
		assertNodes(t, fontFace, "@font-face { unicode-range: U+0020-01ff, U+1?? }", "fontface,declarations,declaration,property,identifier,expression,binaryexpression,term,unicoderange,...")
	})

	t.Run("Stylesheet", func(t *testing.T) {
		assertNodes(t, stylesheet, "selector { .foo {} }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,ruleset,...,selector,...,classselector,...")
		assertNodes(t, stylesheet, "selector { :hover {} }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,ruleset,...,selector,...,pseudoselector,...")
		assertNodes(t, stylesheet, "selector { :hover {}; }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,ruleset,...,selector,...,pseudoselector,...")
		assertNodes(t, stylesheet, "selector { [value] {} }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,ruleset,...,selector,...,attributeselector,...")
		assertNodes(t, stylesheet, "selector { & div {} }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,ruleset,...,selector,...,selectorcombinator,...,elementnameselector,...")
		assertNodes(t, stylesheet, "selector { .foo { color: blue; } }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,ruleset,...,selector,...,classselector,...,declaration,property,identifier,...")
		assertNodes(t, stylesheet, "selector { @media screen { color: blue; } }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,media,...,mediaquery,...,declaration,property,identifier,...")
		assertNodes(t, stylesheet, "selector { @supports (width: 20rx) { color: blue; } }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,supports,...,declarations,declaration,property,...")
		assertNodes(t, stylesheet, "selector { @layer foo { color: blue; } }", "stylesheet,ruleset,...,selector,...,elementnameselector,...,layer,...,declarations,declaration,property,...")
	})
}
