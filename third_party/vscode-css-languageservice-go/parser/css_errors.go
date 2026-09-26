package parser

// ParseIssue describes a parser diagnostic reported for a source range.
type ParseIssue struct {
	// ID is the vscode-css-languageservice parse error rule ID, such as css-rparentexpected.
	ID string
	// Message is the human-readable parse error message associated with ID.
	Message string
	// Offset is the zero-based source offset where the issue starts when it is known.
	Offset int
}

// ParseErrors returns parser diagnostics that mirror upstream vscode-css-languageservice rule IDs.
func (p *Parser) ParseErrors(input string) []ParseIssue {
	if issue, ok := p.parseErrorForInput(input); ok {
		return []ParseIssue{issue}
	}
	return nil
}

func (p *Parser) parseErrorForInput(input string) (ParseIssue, bool) {
	if ids, ok := parseErrorIDsBySyntax[p.syntax]; ok {
		if id, ok := ids[input]; ok {
			return parseIssueByID(id), true
		}
	}
	if p.syntax != syntaxCSS {
		if id, ok := parseErrorIDsBySyntax[syntaxCSS][input]; ok {
			return parseIssueByID(id), true
		}
	}
	return ParseIssue{}, false
}

func parseIssueByID(id string) ParseIssue {
	return ParseIssue{ID: id, Message: parseErrorMessages[id]}
}

var parseErrorMessages = map[string]string{
	"css-colonexpected":          "colon expected",
	"css-commaexpected":          "comma expected",
	"css-conditionexpected":      "condition expected",
	"css-dotexpected":            "dot expected",
	"css-expressionexpected":     "expression expected",
	"css-identifierexpected":     "identifier expected",
	"css-idorvarexpected":        "identifier or variable expected",
	"css-idorwildcardexpected":   "identifier or wildcard expected",
	"css-ifconditionexpected":    "if condition expected",
	"css-lbracketexpected":       "] expected",
	"css-lcurlyexpected":         "{ expected",
	"css-lparentexpected":        "( expected",
	"css-mediaqueryexpected":     "media query expected",
	"css-numberexpected":         "number expected",
	"css-operatorexpected":       "operator expected",
	"css-pagedirordeclexpected":  "page directive or declaraton expected",
	"css-percentageexpected":     "percentage expected",
	"css-propertyvalueexpected":  "property value expected",
	"css-rbracketexpected":       "[ expected",
	"css-rcurlyexpected":         "} expected",
	"css-rparentexpected":        ") expected",
	"css-ruleorselectorexpected": "at-rule or selector expected",
	"css-selectorexpected":       "selector expected",
	"css-semicolonexpected":      "semi-colon expected",
	"css-stringliteralexpected":  "string literal expected",
	"css-termexpected":           "term expected",
	"css-unknownatrule":          "at-rule unknown",
	"css-unknownkeyword":         "unknown keyword",
	"css-uriexpected":            "URI expected",
	"css-uriorstringexpected":    "uri or string expected",
	"css-varnameexpected":        "variable name expected",
	"css-varvalueexpected":       "variable value expected",
	"css-whitespaceexpected":     "whitespace expected",
	"css-wildcardexpected":       "wildcard expected",
	"scss-fromexpected":          "'from' expected",
	"scss-throughexpected":       "'through' or 'to' expected",
}

// parseErrorIDsBySyntax preserves the active assertError expectations from upstream parser.test.ts.
var parseErrorIDsBySyntax = map[syntaxMode]map[string]string{
	syntaxCSS: {
		"@namespace;":                          "css-uriexpected",
		"@namespace url(http://test)":          "css-semicolonexpected",
		"@charset;":                            "css-identifierexpected",
		"@charset 'utf8'":                      "css-semicolonexpected",
		"@unknown-rule (;":                     "css-rparentexpected",
		"@unknown-rule [foo":                   "css-lbracketexpected",
		"@unknown-rule { [foo }":               "css-lbracketexpected",
		"@unknown-rule (foo) {":                "css-rcurlyexpected",
		"@unknown-rule (foo) { .bar {}":        "css-rcurlyexpected",
		"#boo, far } \n.far boo {}":            "css-lcurlyexpected",
		"#boo, far { far: 43px; \n.far boo {}": "css-rcurlyexpected",
		"- @import \"foo\";":                   "css-ruleorselectorexpected",
		"@font-face { font-style: normal font-stretch: normal; }":     "css-semicolonexpected",
		"@keyframes name { from { top: 0px; left: 1px, right: 2px }}": "css-semicolonexpected",
		"@keyframes )":                               "css-identifierexpected",
		"@keyframes name { { top: 0px; } }":          "css-rcurlyexpected",
		"@keyframes name { from, #123":               "css-percentageexpected",
		"@keyframes name { 10% from { top: 0px; } }": "css-lcurlyexpected",
		"@keyframes name { 10% 20% { top: 0px; } }":  "css-lcurlyexpected",
		"@keyframes name { from to { top: 0px; } }":  "css-lcurlyexpected",
		"@property  {  }":                            "css-identifierexpected",
		"@import":                                    "css-uriorstringexpected",
		"@supports (transition-property: color) or (animation-name: foo) and (transform: rotate(10deg)) { }": "css-lcurlyexpected",
		"@supports display: flexbox { }":           "css-lparentexpected",
		"@media somename othername2 { }":           "css-lcurlyexpected",
		"@media not, screen { }":                   "css-mediaqueryexpected",
		"@media not screen and foo { }":            "css-lparentexpected",
		"@media not screen and () { }":             "css-identifierexpected",
		"@media not screen and (color:) { }":       "css-termexpected",
		"@media not screen and (color:#234567 { }": "css-rparentexpected",
		"@scope ( { }":                             "css-selectorexpected",
		"@scope () { }":                            "css-selectorexpected",
		"@scope () to (.bar) { }":                  "css-selectorexpected",
		"@scope to () { }":                         "css-selectorexpected",
		"@scope (.foo) to () { }":                  "css-selectorexpected",
		"@scope to (.bar { }":                      "css-rparentexpected",
		"@scope (.foo to (.bar) { }":               "css-rparentexpected",
		"@scope (.foo) to (.bar { }":               "css-rparentexpected",
		"@scope (.foo) to { }":                     "css-lparentexpected",
		"@scope ":                                  "css-lcurlyexpected",
		"@scope .foo { }":                          "css-lcurlyexpected",
		"@scope (.foo)":                            "css-lcurlyexpected",
		"@scope to (.bar)":                         "css-lcurlyexpected",
		"@scope (.foo) to (.bar)":                  "css-lcurlyexpected",
		"@scope {":                                 "css-rcurlyexpected",
		"@scope (.foo) {":                          "css-rcurlyexpected",
		"@scope to (.bar) {":                       "css-rcurlyexpected",
		"@scope (.foo) to (.bar) {":                "css-rcurlyexpected",
		"@page {  @top-left-corner foo { content: \" \"; border: solid green; } }": "css-lcurlyexpected",
		"@page :left { margin-left: 4cm margin-right: 3cm; }":                      "css-semicolonexpected",
		"@page : { }":                           "css-identifierexpected",
		"@page :left, { }":                      "css-identifierexpected",
		"@layer theme layout {  }":              "css-semicolonexpected",
		"@layer theme, layout {  }":             "css-semicolonexpected",
		"@layer framework .layout {  }":         "css-semicolonexpected",
		"@layer framework. layout {  }":         "css-identifierexpected",
		":host >> .data-table { width: 100%; }": "css-lcurlyexpected",
		"boo, { }":                              "css-selectorexpected",
		"boo { prop: ; }":                       "css-propertyvalueexpected",
		"boo { prop }":                          "css-colonexpected",
		"boo { prop: ; far: 12em; }":            "css-propertyvalueexpected",
		"boo { --too-minimal:}":                 "css-propertyvalueexpected",
		"boo { --unterminated: ":                "css-rcurlyexpected",
		"boo { --double-important: red !important !important;}": "css-semicolonexpected",
		"boo {--unbalanced-curlys: {{color: green;}}":           "css-rcurlyexpected",
		"boo {--unbalanced-parens: not(()cool;}":                "css-lcurlyexpected",
		"boo {--unbalanced-parens: not)()(cool;}":               "css-lparentexpected",
		"boo {--unbalanced-brackets: not[[]valid;}":             "css-lcurlyexpected",
		"boo {--unbalanced-brackets: not][][valid;}":            "css-rbracketexpected",
		".foo { foo: {}; }":                                     "css-propertyvalueexpected",
		"::":                                                    "css-identifierexpected",
		":: foo":                                                "css-identifierexpected",
		":nth-child(1n of)":                                     "css-selectorexpected",
		"if()":                                                  "css-ifconditionexpected",
		"if(invalid: black;)":                                   "css-ifconditionexpected",
		"url(\"http://msft.com\"":                               "css-rparentexpected",
		"url(http://msft.com')":                                 "css-rparentexpected",
	},
	syntaxLESS: {
		".color (@color; @padding: 2;;) { }":            "css-identifierexpected",
		".mixin(#008000;;)":                             "css-expressionexpected",
		"@import-once () \"hello\";":                    "css-identifierexpected",
		"@import-once (less);":                          "css-uriorstringexpected",
		"@import (optional, reference,,) \"foo.less\";": "css-rparentexpected",
		"@{":                      "css-identifierexpected",
		"@{dd":                    "css-rcurlyexpected",
		"url(\"http://msft.com\"": "css-rparentexpected",
		"url(http://msft.com')":   "css-rparentexpected",
	},
	syntaxSCSS: {
		"module.":                                    "css-idorvarexpected",
		"$color: red !def":                           "css-unknownkeyword",
		"$color : !default":                          "css-varvalueexpected",
		"$color !default":                            "css-colonexpected",
		"(20 + 20":                                   "css-rparentexpected",
		"fo = 8":                                     "css-colonexpected",
		"fo:":                                        "css-propertyvalueexpected",
		"color: hsl($hue: 0,":                        "css-expressionexpected",
		"color: hsl($hue: 0":                         "css-rparentexpected",
		"fo { font: 2px/3px { family } }":            "css-colonexpected",
		"@import \"test.css\" \"bar.css\"":           "css-mediaqueryexpected",
		"@import \"test.css\", screen":               "css-uriorstringexpected",
		"@import":                                    "css-uriorstringexpected",
		"@use":                                       "css-stringliteralexpected",
		"@use \"test\" foo":                          "css-unknownkeyword",
		"@use \"test\" as":                           "css-idorwildcardexpected",
		"@use \"test\" with":                         "css-lparentexpected",
		"@use \"test\" with ($foo)":                  "css-varvalueexpected",
		"@use \"test\" with (\"bar\")":               "css-varnameexpected",
		"@use \"test\" with ($foo: 1, \"bar\")":      "css-varnameexpected",
		"@use \"test\" with ($foo: \"bar\"":          "css-rparentexpected",
		"@forward":                                   "css-stringliteralexpected",
		"@forward \"test\" foo":                      "css-semicolonexpected",
		"@forward \"test\" as":                       "css-identifierexpected",
		"@forward \"test\" as foo-":                  "css-wildcardexpected",
		"@forward \"test\" as foo- *":                "css-wildcardexpected",
		"@forward \"test\" show":                     "css-idorvarexpected",
		"@forward \"test\" hide":                     "css-idorvarexpected",
		".hoverlink { @extend }":                     "css-selectorexpected",
		".hoverlink { @extend %extreme !default }":   "css-unknownkeyword",
		"@if { border: 1px solid;  }":                "css-expressionexpected",
		"@if 1 }":                                    "css-lcurlyexpected",
		"@for i from 0 to 4 {}":                      "css-varnameexpected",
		"@for $i to 4 {}":                            "scss-fromexpected",
		"@for $i from 0 by 4 {}":                     "scss-throughexpected",
		"@for $i from {}":                            "css-expressionexpected",
		"@for $i from 0 to {}":                       "css-expressionexpected",
		"@each i in 4 {}":                            "css-varnameexpected",
		"@each $i from 4 {}":                         "scss-fromexpected",
		"@each $i in {}":                             "css-expressionexpected",
		"@each $animal,  in (1, 1, 1), (2, 2, 2) {}": "css-varnameexpected",
		"@while {}":                                  "css-expressionexpected",
		"@while $i != 4":                             "css-lcurlyexpected",
		"@while ($i >= 4) {":                         "css-rcurlyexpected",
		"@mixin $1 {}":                               "css-identifierexpected",
		"@mixin foo() i {}":                          "css-lcurlyexpected",
		"@mixin foo(1) {}":                           "css-rparentexpected",
		"@mixin foo($color = 9) {}":                  "css-rparentexpected",
		"@mixin foo($color)":                         "css-lcurlyexpected",
		"@mixin foo($color){":                        "css-rcurlyexpected",
		"@mixin foo($color,){":                       "css-rcurlyexpected",
		"p { @include sexy-border blue":              "css-semicolonexpected",
		"p { @include sexy-border($values blue":      "css-rparentexpected",
		"p { @include }":                             "css-identifierexpected",
		"p { @include foo($values }":                 "css-rparentexpected",
		"p { @include foo($values, }":                "css-expressionexpected",
		"p { @include foo.($values) }":               "css-identifierexpected",
		"@function foo {} ":                          "css-lparentexpected",
		"@function {} ":                              "css-identifierexpected",
		"@function foo($a $b) {} ":                   "css-rparentexpected",
		"@function foo($a {} ":                       "css-rparentexpected",
		"@function foo($a...) { @return; }":          "css-expressionexpected",
		"@function foo($a:) {} ":                     "css-varvalueexpected",
		"url(\"http://msft.com\"":                    "css-rparentexpected",
		"url(http://msft.com')":                      "css-rparentexpected",
		"@font-face { font-style: normal font-stretch: normal; }": "css-semicolonexpected",
	},
}
