package services

import (
	"strings"
	"testing"
)

func TestCSSNavigationScopePortedNamedCases(t *testing.T) {
	t.Run("scope creation", func(t *testing.T) {
		global := newGlobalSymbolScope()
		child1 := newSymbolScope(10, 5)
		child2 := newSymbolScope(15, 5)
		global.addChild(child1)
		global.addChild(child2)
		if len(global.children) != 2 || child1.parent != global || child2.parent != global {
			t.Fatalf("children = %#v", global.children)
		}
		if global.findScope(-1) != nil ||
			global.findScope(0) != global ||
			global.findScope(10) != child1 ||
			global.findScope(14) != child1 ||
			global.findScope(15) != child2 ||
			global.findScope(19) != child2 ||
			global.findScope(19).parent != global {
			t.Fatalf("unexpected scope lookup")
		}
	})
	t.Run("scope building", func(t *testing.T) {
		assertScopeBuilding(t, "css", ".class {}", scopeRange{offset: 7, length: 2})
		assertScopeBuilding(t, "css", ".class {} .class {}", scopeRange{offset: 7, length: 2}, scopeRange{offset: 17, length: 2})
	})
	t.Run("symbols in scopes", func(t *testing.T) {
		assertSymbolsInScope(t, "css", "@keyframes animation {};", 0, expectedScopeSymbol{name: "animation", kind: scopeSymbolKeyframe})
		assertSymbolsInScope(t, "css", " .class1 {} .class2 {}", 0,
			expectedScopeSymbol{name: ".class1", kind: scopeSymbolRule},
			expectedScopeSymbol{name: ".class2", kind: scopeSymbolRule},
		)
	})
	t.Run("scopes and symbols", func(t *testing.T) {
		assertScopesAndSymbols(t, "css", ".class {}", ".class,[]")
		assertScopesAndSymbols(t, "css", "@keyframes animation {}; .class {}", "animation,.class,[],[]")
		assertScopesAndSymbols(t, "css", "@page :pseudo-class { margin:2in; }", "[]")
		assertScopesAndSymbols(t, "css", "@media print { body { font-size: 10pt } }", "[body,[]]")
		assertScopesAndSymbols(t, "css", "@scope (.foo) to (.bar) { body { font-size: 10pt } }", "[body,[]]")
		assertScopesAndSymbols(t, "css", "@-moz-keyframes identifier { 0% { top: 0; } 50% { top: 30px; left: 20px; }}", "identifier,[[],[]]")
		assertScopesAndSymbols(t, "css", "@font-face { font-family: \"Bitstream Vera Serif Bold\"; }", "[]")
	})
	t.Run("test variables in root scope", func(t *testing.T) {
		assertSymbolsInScope(t, "css", ":root{ --var1: abc; --var2: def; }", 0,
			expectedScopeSymbol{name: "--var1", kind: scopeSymbolVariable},
			expectedScopeSymbol{name: "--var2", kind: scopeSymbolVariable},
		)
	})
	t.Run("test variables in local scope", func(t *testing.T) {
		assertSymbolsInScope(t, "css", ".a{ --var1: abc; --var2: def; }", 2,
			expectedScopeSymbol{name: "--var1", kind: scopeSymbolVariable},
			expectedScopeSymbol{name: "--var2", kind: scopeSymbolVariable},
		)
	})
	t.Run("test variables in local scope get root variables too", func(t *testing.T) {
		assertSymbolsInScope(t, "css", ".a{ --var1: abc; } :root{ --var2: abc;}", 2,
			expectedScopeSymbol{name: "--var1", kind: scopeSymbolVariable},
			expectedScopeSymbol{name: "--var2", kind: scopeSymbolVariable},
		)
	})
	t.Run("test variables in local scope get root variables and other local variables too", func(t *testing.T) {
		assertSymbolsInScope(t, "css", ".a{ --var1: abc; } .b{ --var2: abc; } :root{ --var3: abc;}", 2,
			expectedScopeSymbol{name: "--var1", kind: scopeSymbolVariable},
			expectedScopeSymbol{name: "--var2", kind: scopeSymbolVariable},
			expectedScopeSymbol{name: "--var3", kind: scopeSymbolVariable},
		)
	})
}

func TestLESSNavigationScopePortedNamedCases(t *testing.T) {
	t.Run("scope building", func(t *testing.T) {
		assertScopeBuilding(t, "less", "@let: blue")
		assertScopeBuilding(t, "less", ".class { .nested {} }", scopeRange{offset: 7, length: 14}, scopeRange{offset: 17, length: 2})
	})
	t.Run("symbols in scopes", func(t *testing.T) {
		assertSymbolsInScope(t, "less", "@let: iable;", 0, expectedScopeSymbol{name: "@let", kind: scopeSymbolVariable})
		assertSymbolsInScope(t, "less", "@let: iable;", 11, expectedScopeSymbol{name: "@let", kind: scopeSymbolVariable})
		assertSymbolsInScope(t, "less", "@let: iable; .class { @color: blue; }", 11,
			expectedScopeSymbol{name: "@let", kind: scopeSymbolVariable},
			expectedScopeSymbol{name: ".class", kind: scopeSymbolRule},
		)
		assertSymbolsInScope(t, "less", "@let: iable; .class { @color: blue; }", 21, expectedScopeSymbol{name: "@color", kind: scopeSymbolVariable})
		assertSymbolsInScope(t, "less", "@let: iable; .class { @color: blue; }", 36, expectedScopeSymbol{name: "@color", kind: scopeSymbolVariable})
		assertSymbolsInScope(t, "less", "@namespace \"x\"; .mixin() {}", 0, expectedScopeSymbol{name: ".mixin", kind: scopeSymbolMixin})
		assertSymbolsInScope(t, "less", ".mixin() { .nested() {} }", 10, expectedScopeSymbol{name: ".nested", kind: scopeSymbolMixin})
		assertSymbolsInScope(t, "less", ".mixin() { .nested() {} }", 11)
		assertSymbolsInScope(t, "less", "@keyframes animation {};", 0, expectedScopeSymbol{name: "animation", kind: scopeSymbolKeyframe})
		assertSymbolsInScope(t, "less", ".a(@gutter: @gutter-width) { &:extend(.b); }", 1)
	})
	t.Run("scopes and symbols", func(t *testing.T) {
		assertScopesAndSymbols(t, "less", "@var1: 1; @var2: 2; .foo { @var3: 3; }", "@var1,@var2,.foo,[@var3]")
		assertScopesAndSymbols(t, "less", ".mixin1 { @var0: 1} .mixin2(@var1) { @var3: 3 }", ".mixin1,.mixin2,[@var0],[@var1,@var3]")
		assertScopesAndSymbols(t, "less", "a b { @var0: 1; c { d { } } }", "[@var0,c,[d,[]]]")
	})
}

func TestSCSSNavigationScopePortedNamedCases(t *testing.T) {
	t.Run("symbols in scopes", func(t *testing.T) {
		assertSymbolsInScope(t, "scss", "$var: iable;", 0, expectedScopeSymbol{name: "$var", kind: scopeSymbolVariable})
		assertSymbolsInScope(t, "scss", "$var: iable;", 11, expectedScopeSymbol{name: "$var", kind: scopeSymbolVariable})
		assertSymbolsInScope(t, "scss", "$var: iable; .class { $color: blue; }", 11,
			expectedScopeSymbol{name: "$var", kind: scopeSymbolVariable},
			expectedScopeSymbol{name: ".class", kind: scopeSymbolRule},
		)
		assertSymbolsInScope(t, "scss", "$var: iable; .class { $color: blue; }", 22, expectedScopeSymbol{name: "$color", kind: scopeSymbolVariable})
		assertSymbolsInScope(t, "scss", "$var: iable; .class { $color: blue; }", 36, expectedScopeSymbol{name: "$color", kind: scopeSymbolVariable})
		assertSymbolsInScope(t, "scss", "@namespace \"x\"; @mixin mix() {}", 0, expectedScopeSymbol{name: "mix", kind: scopeSymbolMixin})
		assertSymbolsInScope(t, "scss", "@mixin mix { @mixin nested() {} }", 12, expectedScopeSymbol{name: "nested", kind: scopeSymbolMixin})
		assertSymbolsInScope(t, "scss", "@mixin mix () { @mixin nested() {} }", 13)
	})
	t.Run("scopes and symbols", func(t *testing.T) {
		assertScopesAndSymbols(t, "scss", "$var1: 1; $var2: 2; .foo { $var3: 3; }", "$var1,$var2,.foo,[$var3]")
		assertScopesAndSymbols(t, "scss", "@mixin mixin1 { $var0: 1} @mixin mixin2($var1) { $var3: 3 }", "mixin1,mixin2,[$var0],[$var1,$var3]")
		assertScopesAndSymbols(t, "scss", "a b { $var0: 1; c { d { } } }", "[$var0,c,[d,[]]]")
		assertScopesAndSymbols(t, "scss", "@function a($p1: 1, $p2: 2) { $v1: 3; @return $v1; }", "a,[$p1,$p2,$v1]")
		assertScopesAndSymbols(t, "scss", "$var1: 3; @if $var1 == 2 { $var2: 1; } @else { $var2: 2; $var3: 2;} ", "$var1,[$var2],[$var2,$var3]")
		assertScopesAndSymbols(t, "scss", "@if $var1 == 2 { $var2: 1; } @else if $var1 == 2 { $var3: 2; } @else { $var3: 2; } ", "[$var2],[$var3],[$var3]")
		assertScopesAndSymbols(t, "scss", "$var1: 3; @while $var1 < 2 { #rule { a: b; } }", "$var1,[#rule,[]]")
		assertScopesAndSymbols(t, "scss", "$i:0; @each $name in f1, f2, f3  { $i:$i+1; }", "$i,[$name,$i]")
		assertScopesAndSymbols(t, "scss", "$i:0; @for $x from $i to 5  { }", "$i,[$x]")
		assertScopesAndSymbols(t, "scss", "@each $i, $j, $k in f1, f2, f3  { }", "[$i,$j,$k]")
	})
}

type scopeRange struct {
	offset int
	length int
}

type expectedScopeSymbol struct {
	name string
	kind scopeSymbolKind
}

func assertScopeBuilding(t *testing.T, languageID, input string, expected ...scopeRange) {
	t.Helper()
	global := buildSymbolScope(input, languageID)
	var actual []scopeRange
	collectScopeRanges(global, &actual)
	if len(actual) != len(expected) {
		t.Fatalf("%s\nactual scopes: %#v\nwant: %#v", input, actual, expected)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("%s\nactual scopes: %#v\nwant: %#v", input, actual, expected)
		}
	}
}

func collectScopeRanges(scope *symbolScope, result *[]scopeRange) {
	for _, child := range scope.children {
		*result = append(*result, scopeRange{offset: child.offset, length: child.length})
		collectScopeRanges(child, result)
	}
}

func assertSymbolsInScope(t *testing.T, languageID, input string, offset int, expected ...expectedScopeSymbol) {
	t.Helper()
	global := buildSymbolScope(input, languageID)
	scope := global.findScope(offset)
	if scope == nil {
		t.Fatalf("%s\nno scope at %d", input, offset)
	}
	for _, symbol := range expected {
		if _, ok := scope.getSymbol(symbol.name, symbol.kind); ok {
			continue
		}
		if _, ok := global.getSymbol(symbol.name, symbol.kind); ok {
			continue
		}
		t.Fatalf("%s\nsymbol %s not found in %s", input, symbol.name, scopeDebugString(scope))
	}
}

func assertScopesAndSymbols(t *testing.T, languageID, input, expected string) {
	t.Helper()
	actual := scopeDebugString(buildSymbolScope(input, languageID))
	if actual != expected {
		t.Fatalf("%s\nactual: %s\nwant:   %s", input, actual, expected)
	}
}

func scopeDebugString(scope *symbolScope) string {
	var parts []string
	for _, symbol := range sortedScopeSymbols(scope.symbols) {
		parts = append(parts, symbol.name)
	}
	for _, child := range scope.children {
		parts = append(parts, "["+scopeDebugString(child)+"]")
	}
	return strings.Join(parts, ",")
}
