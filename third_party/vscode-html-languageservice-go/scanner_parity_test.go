package htmlservice

import (
	"reflect"
	"strings"
	"testing"
)

type scannerExpectedToken struct {
	Offset     int
	Type       TokenType
	Content    string
	HasContent bool
}
type scannerStep struct {
	Input  string
	Tokens []scannerExpectedToken
}

func TestScannerBaselineParity(t *testing.T) {
	groups := [][]scannerStep{
		{
			{Input: `<abc`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
			}},
		},
		{
			{Input: `<input`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `input`, HasContent: true},
			}},
		},
		{
			{Input: `< abc`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeWhitespace},
				{Offset: 2, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
			}},
		},
		{
			{Input: `< abc>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeWhitespace},
				{Offset: 2, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 5, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `i <len;`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeContent},
				{Offset: 2, Type: TokenTypeStartTagOpen},
				{Offset: 3, Type: TokenTypeStartTag, Content: `len`, HasContent: true},
				{Offset: 6, Type: TokenTypeUnknown},
			}},
		},
		{
			{Input: `<`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
			}},
		},
		{
			{Input: `</a`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeEndTagOpen},
				{Offset: 2, Type: TokenTypeEndTag, Content: `a`, HasContent: true},
			}},
		},
		{
			{Input: `<abc>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc >`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<foo:bar>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `foo:bar`, HasContent: true},
				{Offset: 8, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `</abc>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeEndTagOpen},
				{Offset: 2, Type: TokenTypeEndTag, Content: `abc`, HasContent: true},
				{Offset: 5, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `</abc  >`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeEndTagOpen},
				{Offset: 2, Type: TokenTypeEndTag, Content: `abc`, HasContent: true},
				{Offset: 5, Type: TokenTypeWhitespace},
				{Offset: 7, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<abc />`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeStartTagSelfClose},
			}},
		},
		{
			{Input: `<script type="text/javascript">var i= 10;</script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeWhitespace},
				{Offset: 8, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeDelimiterAssign},
				{Offset: 13, Type: TokenTypeAttributeValue},
				{Offset: 30, Type: TokenTypeStartTagClose},
				{Offset: 31, Type: TokenTypeScript},
				{Offset: 41, Type: TokenTypeEndTagOpen},
				{Offset: 43, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 49, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script type="text/javascript">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeWhitespace},
				{Offset: 8, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeDelimiterAssign},
				{Offset: 13, Type: TokenTypeAttributeValue},
				{Offset: 30, Type: TokenTypeStartTagClose},
			}},
			{Input: `var i= 10;`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeScript},
			}},
			{Input: `</script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeEndTagOpen},
				{Offset: 2, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 8, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script type="text/javascript">var i= 10;`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeWhitespace},
				{Offset: 8, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeDelimiterAssign},
				{Offset: 13, Type: TokenTypeAttributeValue},
				{Offset: 30, Type: TokenTypeStartTagClose},
				{Offset: 31, Type: TokenTypeScript},
			}},
			{Input: `</script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeEndTagOpen},
				{Offset: 2, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 8, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script type="text/javascript">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeWhitespace},
				{Offset: 8, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeDelimiterAssign},
				{Offset: 13, Type: TokenTypeAttributeValue},
				{Offset: 30, Type: TokenTypeStartTagClose},
			}},
			{Input: `var i= 10;</script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeScript},
				{Offset: 10, Type: TokenTypeEndTagOpen},
				{Offset: 12, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 18, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script type="text/plain">a
<a</script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeWhitespace},
				{Offset: 8, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeDelimiterAssign},
				{Offset: 13, Type: TokenTypeAttributeValue},
				{Offset: 25, Type: TokenTypeStartTagClose},
				{Offset: 26, Type: TokenTypeScript},
				{Offset: 30, Type: TokenTypeEndTagOpen},
				{Offset: 32, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 38, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script>a</script><script>b</script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeStartTagClose},
				{Offset: 8, Type: TokenTypeScript},
				{Offset: 9, Type: TokenTypeEndTagOpen},
				{Offset: 11, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 17, Type: TokenTypeEndTagClose},
				{Offset: 18, Type: TokenTypeStartTagOpen},
				{Offset: 19, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 25, Type: TokenTypeStartTagClose},
				{Offset: 26, Type: TokenTypeScript},
				{Offset: 27, Type: TokenTypeEndTagOpen},
				{Offset: 29, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 35, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script type="text/javascript"></script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeWhitespace},
				{Offset: 8, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeDelimiterAssign},
				{Offset: 13, Type: TokenTypeAttributeValue},
				{Offset: 30, Type: TokenTypeStartTagClose},
				{Offset: 31, Type: TokenTypeEndTagOpen},
				{Offset: 33, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 39, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script>var i= 10;</script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeStartTagClose},
				{Offset: 8, Type: TokenTypeScript},
				{Offset: 18, Type: TokenTypeEndTagOpen},
				{Offset: 20, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 26, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script type="text/javascript" src="main.js"></script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeWhitespace},
				{Offset: 8, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeDelimiterAssign},
				{Offset: 13, Type: TokenTypeAttributeValue},
				{Offset: 30, Type: TokenTypeWhitespace},
				{Offset: 31, Type: TokenTypeAttributeName},
				{Offset: 34, Type: TokenTypeDelimiterAssign},
				{Offset: 35, Type: TokenTypeAttributeValue},
				{Offset: 44, Type: TokenTypeStartTagClose},
				{Offset: 45, Type: TokenTypeEndTagOpen},
				{Offset: 47, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 53, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script><!-- alert("<script></script>"); --></script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeStartTagClose},
				{Offset: 8, Type: TokenTypeScript},
				{Offset: 44, Type: TokenTypeEndTagOpen},
				{Offset: 46, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 52, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script><!-- alert("<script></script>"); </script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeStartTagClose},
				{Offset: 8, Type: TokenTypeScript},
				{Offset: 41, Type: TokenTypeEndTagOpen},
				{Offset: 43, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 49, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script><!-- alert("</script>"); </script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeStartTagClose},
				{Offset: 8, Type: TokenTypeScript},
				{Offset: 20, Type: TokenTypeEndTagOpen},
				{Offset: 22, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 28, Type: TokenTypeEndTagClose},
				{Offset: 29, Type: TokenTypeContent},
				{Offset: 33, Type: TokenTypeEndTagOpen},
				{Offset: 35, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 41, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<script> alert("<script></script>"); </script>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeStartTagClose},
				{Offset: 8, Type: TokenTypeScript},
				{Offset: 24, Type: TokenTypeEndTagOpen},
				{Offset: 26, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 32, Type: TokenTypeEndTagClose},
				{Offset: 33, Type: TokenTypeContent},
				{Offset: 37, Type: TokenTypeEndTagOpen},
				{Offset: 39, Type: TokenTypeEndTag, Content: `script`, HasContent: true},
				{Offset: 45, Type: TokenTypeEndTagClose},
			}},
		},
		{
			{Input: `<abc foo="bar">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
				{Offset: 14, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo='bar'>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
				{Offset: 14, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo="">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
				{Offset: 11, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo=''>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
				{Offset: 11, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo="bar" bar='foo'>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
				{Offset: 14, Type: TokenTypeWhitespace},
				{Offset: 15, Type: TokenTypeAttributeName},
				{Offset: 18, Type: TokenTypeDelimiterAssign},
				{Offset: 19, Type: TokenTypeAttributeValue},
				{Offset: 24, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo=bar bar=help-me>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
				{Offset: 12, Type: TokenTypeWhitespace},
				{Offset: 13, Type: TokenTypeAttributeName},
				{Offset: 16, Type: TokenTypeDelimiterAssign},
				{Offset: 17, Type: TokenTypeAttributeValue},
				{Offset: 24, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo=bar/>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
				{Offset: 12, Type: TokenTypeStartTagSelfClose},
			}},
		},
		{
			{Input: `<abc foo=http://example.com/foo/bar/>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
				{Offset: 35, Type: TokenTypeStartTagSelfClose},
			}},
		},
		{
			{Input: `<abc foo=  "bar">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeWhitespace},
				{Offset: 11, Type: TokenTypeAttributeValue},
				{Offset: 16, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo = "bar">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeWhitespace},
				{Offset: 9, Type: TokenTypeDelimiterAssign},
				{Offset: 10, Type: TokenTypeWhitespace},
				{Offset: 11, Type: TokenTypeAttributeValue},
				{Offset: 16, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo bar>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeWhitespace},
				{Offset: 9, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo!@#="bar">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 11, Type: TokenTypeDelimiterAssign},
				{Offset: 12, Type: TokenTypeAttributeValue},
				{Offset: 17, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc #myinput (click)="bar" [value]="someProperty" *ngIf="someCondition">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 13, Type: TokenTypeWhitespace},
				{Offset: 14, Type: TokenTypeAttributeName},
				{Offset: 21, Type: TokenTypeDelimiterAssign},
				{Offset: 22, Type: TokenTypeAttributeValue},
				{Offset: 27, Type: TokenTypeWhitespace},
				{Offset: 28, Type: TokenTypeAttributeName},
				{Offset: 35, Type: TokenTypeDelimiterAssign},
				{Offset: 36, Type: TokenTypeAttributeValue},
				{Offset: 50, Type: TokenTypeWhitespace},
				{Offset: 51, Type: TokenTypeAttributeName},
				{Offset: 56, Type: TokenTypeDelimiterAssign},
				{Offset: 57, Type: TokenTypeAttributeValue},
				{Offset: 72, Type: TokenTypeStartTagClose},
			}},
		},
		{
			{Input: `<abc foo=">`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `abc`, HasContent: true},
				{Offset: 4, Type: TokenTypeWhitespace},
				{Offset: 5, Type: TokenTypeAttributeName},
				{Offset: 8, Type: TokenTypeDelimiterAssign},
				{Offset: 9, Type: TokenTypeAttributeValue},
			}},
		},
		{
			{Input: `<!--a-->`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartCommentTag},
				{Offset: 4, Type: TokenTypeComment},
				{Offset: 5, Type: TokenTypeEndCommentTag},
			}},
		},
		{
			{Input: `<!--a>foo bar</a -->`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartCommentTag},
				{Offset: 4, Type: TokenTypeComment},
				{Offset: 17, Type: TokenTypeEndCommentTag},
			}},
		},
		{
			{Input: "<!--a>\nfoo \nbar</a -->", Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartCommentTag},
				{Offset: 4, Type: TokenTypeComment},
				{Offset: 19, Type: TokenTypeEndCommentTag},
			}},
		},
		{
			{Input: `<!Doctype a>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartDoctypeTag},
				{Offset: 9, Type: TokenTypeDoctype},
				{Offset: 11, Type: TokenTypeEndDoctypeTag},
			}},
		},
		{
			{Input: `<!doctype a>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartDoctypeTag},
				{Offset: 9, Type: TokenTypeDoctype},
				{Offset: 11, Type: TokenTypeEndDoctypeTag},
			}},
		},
		{
			{Input: `<!DOCTYPE a
"foo" 'bar'>`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartDoctypeTag},
				{Offset: 9, Type: TokenTypeDoctype},
				{Offset: 23, Type: TokenTypeEndDoctypeTag},
			}},
		},
		{
			{Input: `    `, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeContent},
			}},
		},
		{
			{Input: `<!---   `, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartCommentTag},
				{Offset: 4, Type: TokenTypeComment},
			}},
		},
		{
			{Input: `<style>color:red`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `style`, HasContent: true},
				{Offset: 6, Type: TokenTypeStartTagClose},
				{Offset: 7, Type: TokenTypeStyles},
			}},
		},
		{
			{Input: `<script>alert("!!")`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `script`, HasContent: true},
				{Offset: 7, Type: TokenTypeStartTagClose},
				{Offset: 8, Type: TokenTypeScript},
			}},
		},
		{
			{Input: `<input label= />`, Tokens: []scannerExpectedToken{
				{Offset: 0, Type: TokenTypeStartTagOpen},
				{Offset: 1, Type: TokenTypeStartTag, Content: `input`, HasContent: true},
				{Offset: 6, Type: TokenTypeWhitespace},
				{Offset: 7, Type: TokenTypeAttributeName},
				{Offset: 12, Type: TokenTypeDelimiterAssign},
				{Offset: 13, Type: TokenTypeWhitespace},
				{Offset: 14, Type: TokenTypeStartTagSelfClose},
			}},
		},
	}
	for gi, group := range groups {
		state := ScannerStateWithinContent
		for si, step := range group {
			scanner := NewScanner(step.Input, 0, state, false)
			var actual []scannerExpectedToken
			for token := scanner.Scan(); token != TokenTypeEOS; token = scanner.Scan() {
				item := scannerExpectedToken{Offset: scanner.GetTokenOffset(), Type: token}
				if token == TokenTypeStartTag || token == TokenTypeEndTag {
					item.Content = step.Input[scanner.GetTokenByteOffset():scanner.GetTokenByteEnd()]
					item.HasContent = true
				}
				actual = append(actual, item)
			}
			if len(actual) != len(step.Tokens) {
				t.Fatalf("group %d step %d token len got %#v want %#v", gi, si, actual, step.Tokens)
			}
			for i := range actual {
				if actual[i] != step.Tokens[i] {
					t.Fatalf("group %d step %d token %d got %#v want %#v", gi, si, i, actual[i], step.Tokens[i])
				}
			}
			state = scanner.GetScannerState()
		}
	}
}

func TestScannerOffsetsUseUTF16CodeUnits(t *testing.T) {
	scanner := CreateScanner(`😀<div></div>`)
	expected := []scannerExpectedToken{
		{Offset: 0, Type: TokenTypeContent},
		{Offset: 2, Type: TokenTypeStartTagOpen},
		{Offset: 3, Type: TokenTypeStartTag, Content: `div`, HasContent: true},
		{Offset: 6, Type: TokenTypeStartTagClose},
		{Offset: 7, Type: TokenTypeEndTagOpen},
		{Offset: 9, Type: TokenTypeEndTag, Content: `div`, HasContent: true},
		{Offset: 12, Type: TokenTypeEndTagClose},
	}
	var actual []scannerExpectedToken
	for token := scanner.Scan(); token != TokenTypeEOS; token = scanner.Scan() {
		item := scannerExpectedToken{Offset: scanner.GetTokenOffset(), Type: token}
		if token == TokenTypeStartTag || token == TokenTypeEndTag {
			item.Content = scanner.GetTokenText()
			item.HasContent = true
		}
		actual = append(actual, item)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("scanner UTF-16 offsets got %#v want %#v", actual, expected)
	}

	scanner = CreateScanner(`😀<div>`, 2)
	if token := scanner.Scan(); token != TokenTypeStartTagOpen || scanner.GetTokenOffset() != 2 {
		t.Fatalf("initial UTF-16 offset token got %v at %d", token, scanner.GetTokenOffset())
	}
}

func TestScannerUnknownTokenDoesNotSplitUTF8Runes(t *testing.T) {
	scanner := CreateScanner(`<aé>`)
	var actual []scannerExpectedToken
	for token := scanner.Scan(); token != TokenTypeEOS; token = scanner.Scan() {
		item := scannerExpectedToken{Offset: scanner.GetTokenOffset(), Type: token}
		if token == TokenTypeStartTag || token == TokenTypeEndTag || token == TokenTypeUnknown {
			item.Content = scanner.GetTokenText()
			item.HasContent = true
		}
		actual = append(actual, item)
	}
	expected := []scannerExpectedToken{
		{Offset: 0, Type: TokenTypeStartTagOpen},
		{Offset: 1, Type: TokenTypeStartTag, Content: "a", HasContent: true},
		{Offset: 2, Type: TokenTypeUnknown, Content: "é", HasContent: true},
		{Offset: 3, Type: TokenTypeStartTagClose},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("scanner non-ASCII unknown tokens got %#v want %#v", actual, expected)
	}
}

func TestScannerUnknownAstralTokenUsesUTF16CodeUnits(t *testing.T) {
	scanner := CreateScanner(`<a😀>`)
	type token struct {
		typ    TokenType
		offset int
		length int
		end    int
		text   string
		err    string
		state  ScannerState
	}
	var actual []token
	for tok := scanner.Scan(); tok != TokenTypeEOS; tok = scanner.Scan() {
		actual = append(actual, token{
			typ:    tok,
			offset: scanner.GetTokenOffset(),
			length: scanner.GetTokenLength(),
			end:    scanner.GetTokenEnd(),
			text:   scanner.GetTokenText(),
			err:    scanner.GetTokenError(),
			state:  scanner.GetScannerState(),
		})
	}
	expected := []token{
		{typ: TokenTypeStartTagOpen, offset: 0, length: 1, end: 1, text: "<", state: ScannerStateAfterOpeningStartTag},
		{typ: TokenTypeStartTag, offset: 1, length: 1, end: 2, text: "a", state: ScannerStateWithinTag},
		{typ: TokenTypeUnknown, offset: 2, length: 1, end: 3, text: utf16CodeUnitString(0xD83D), err: "Unexpected character in tag.", state: ScannerStateWithinTag},
		{typ: TokenTypeUnknown, offset: 3, length: 1, end: 4, text: utf16CodeUnitString(0xDE00), err: "Unexpected character in tag.", state: ScannerStateWithinTag},
		{typ: TokenTypeStartTagClose, offset: 4, length: 1, end: 5, text: ">", state: ScannerStateWithinContent},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("scanner astral unknown tokens got %#v want %#v", actual, expected)
	}
}

func TestScannerInitialOffsetInsideSurrogatePair(t *testing.T) {
	scanner := CreateScanner(`😀<div>`, 1)
	if tok := scanner.Scan(); tok != TokenTypeContent {
		t.Fatalf("initial surrogate token got %v want content", tok)
	}
	if scanner.GetTokenOffset() != 1 || scanner.GetTokenLength() != 1 || scanner.GetTokenEnd() != 2 {
		t.Fatalf("initial surrogate token range got %d..%d len %d", scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenLength())
	}
	if scanner.GetTokenText() != utf16CodeUnitString(0xDE00) {
		t.Fatalf("initial surrogate text bytes got % x", scanner.GetTokenText())
	}
	if tok := scanner.Scan(); tok != TokenTypeStartTagOpen || scanner.GetTokenOffset() != 2 {
		t.Fatalf("token after initial surrogate got %v at %d want start tag open at 2", tok, scanner.GetTokenOffset())
	}

	scanner = NewScanner(`😀>`, 1, ScannerStateBeforeAttributeValue, false)
	if tok := scanner.Scan(); tok != TokenTypeAttributeValue || scanner.GetTokenOffset() != 1 || scanner.GetTokenEnd() != 2 || scanner.GetScannerState() != ScannerStateWithinTag {
		t.Fatalf("before-attribute surrogate got token=%v range=%d..%d state=%v", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetScannerState())
	}
	scanner = NewScanner(`😀>`, 1, ScannerStateWithinEndTag, false)
	if tok := scanner.Scan(); tok != TokenTypeUnknown || scanner.GetTokenError() != "Closing bracket expected." || scanner.GetScannerState() != ScannerStateWithinContent {
		t.Fatalf("within-end-tag surrogate got token=%v error=%q state=%v", tok, scanner.GetTokenError(), scanner.GetScannerState())
	}
	for _, tc := range []struct {
		input string
		state ScannerState
		typ   TokenType
		err   string
		next  ScannerState
		end   int
		text  string
	}{
		{`😀>`, ScannerStateAfterOpeningStartTag, TokenTypeUnknown, "Start tag name expected.", ScannerStateWithinTag, 2, utf16CodeUnitString(0xDE00)},
		{`😀>`, ScannerStateAfterAttributeName, TokenTypeUnknown, "Unexpected character in tag.", ScannerStateWithinTag, 2, utf16CodeUnitString(0xDE00)},
		{`😀-->`, ScannerStateWithinComment, TokenTypeComment, "", ScannerStateWithinComment, 2, utf16CodeUnitString(0xDE00)},
		{`😀>`, ScannerStateWithinDoctype, TokenTypeDoctype, "", ScannerStateWithinDoctype, 2, utf16CodeUnitString(0xDE00)},
		{`😀</script>`, ScannerStateWithinScriptContent, TokenTypeScript, "", ScannerStateWithinContent, 2, utf16CodeUnitString(0xDE00)},
		{`😀</style>`, ScannerStateWithinStyleContent, TokenTypeStyles, "", ScannerStateWithinContent, 2, utf16CodeUnitString(0xDE00)},
	} {
		scanner = NewScanner(tc.input, 1, tc.state, false)
		if tok := scanner.Scan(); tok != tc.typ || scanner.GetTokenOffset() != 1 || scanner.GetTokenEnd() != tc.end || scanner.GetTokenText() != tc.text || scanner.GetTokenError() != tc.err || scanner.GetScannerState() != tc.next {
			t.Fatalf("initial state %v got token=%v range=%d..%d text=% x error=%q state=%v", tc.state, tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenText(), scanner.GetTokenError(), scanner.GetScannerState())
		}
	}

	scanner = CreateScanner("abc", -1)
	if tok := scanner.Scan(); tok != TokenTypeContent || scanner.GetTokenOffset() != -1 || scanner.GetTokenLength() != 4 || scanner.GetTokenEnd() != 3 || scanner.GetTokenText() != "abc" {
		t.Fatalf("negative initial offset got token=%v range=%d..%d len=%d text=%q", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenLength(), scanner.GetTokenText())
	}
	scanner = CreateScanner("<a", -1)
	if tok := scanner.Scan(); tok != TokenTypeContent || scanner.GetTokenOffset() != -1 || scanner.GetTokenLength() != 1 || scanner.GetTokenEnd() != 0 || scanner.GetTokenText() != "" {
		t.Fatalf("negative initial offset before tag got token=%v range=%d..%d len=%d text=%q", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenLength(), scanner.GetTokenText())
	}
	if tok := scanner.Scan(); tok != TokenTypeStartTagOpen || scanner.GetTokenOffset() != 0 {
		t.Fatalf("negative initial offset second token got token=%v offset=%d", tok, scanner.GetTokenOffset())
	}
	scanner = CreateScanner("abc", 99)
	if tok := scanner.Scan(); tok != TokenTypeEOS || scanner.GetTokenOffset() != 99 || scanner.GetTokenLength() != 0 || scanner.GetTokenEnd() != 99 {
		t.Fatalf("large initial offset got token=%v range=%d..%d len=%d", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenLength())
	}
}

func TestScannerInitialGettersBeforeScanMatchForkSource(t *testing.T) {
	scanner := CreateScanner(`<div>`, 2)
	if scanner.GetTokenType() != TokenTypeUnknown || scanner.GetTokenOffset() != 0 || scanner.GetTokenLength() != 2 || scanner.GetTokenEnd() != 2 || scanner.GetTokenText() != "<d" {
		t.Fatalf("pre-scan getters got type=%v offset=%d length=%d end=%d text=%q", scanner.GetTokenType(), scanner.GetTokenOffset(), scanner.GetTokenLength(), scanner.GetTokenEnd(), scanner.GetTokenText())
	}
	scanner = CreateScanner(`<div>`)
	if scanner.GetTokenType() != TokenTypeUnknown || scanner.GetTokenLength() != 0 || scanner.GetTokenText() != "" {
		t.Fatalf("default pre-scan getters got type=%v length=%d text=%q", scanner.GetTokenType(), scanner.GetTokenLength(), scanner.GetTokenText())
	}
	scanner = CreateScanner(`😀>`, 1)
	if scanner.GetTokenText() != utf16CodeUnitString(0xD83D) || scanner.GetTokenEnd() != 1 {
		t.Fatalf("surrogate pre-scan getters got end=%d text=%q", scanner.GetTokenEnd(), scanner.GetTokenText())
	}
}

func TestScannerInitialOffsetInsideSurrogateRawStatesMatchForkSource(t *testing.T) {
	for _, tc := range []struct {
		input string
		state ScannerState
		token TokenType
		end   int
		text  string
	}{
		{`😀abc</script>`, ScannerStateWithinScriptContent, TokenTypeScript, 5, utf16CodeUnitString(0xDE00) + "abc"},
		{`😀abc</style>`, ScannerStateWithinStyleContent, TokenTypeStyles, 5, utf16CodeUnitString(0xDE00) + "abc"},
		{`😀abc-->`, ScannerStateWithinComment, TokenTypeComment, 5, utf16CodeUnitString(0xDE00) + "abc"},
		{`😀abc>`, ScannerStateWithinDoctype, TokenTypeDoctype, 5, utf16CodeUnitString(0xDE00) + "abc"},
	} {
		scanner := NewScanner(tc.input, 1, tc.state, false)
		if tok := scanner.Scan(); tok != tc.token || scanner.GetTokenOffset() != 1 || scanner.GetTokenEnd() != tc.end || scanner.GetTokenText() != tc.text {
			t.Fatalf("%q state %v got token=%v offset=%d end=%d text=%q", tc.input, tc.state, tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenText())
		}
	}
}

func TestScannerInitialOffsetInsideSurrogateTagStatesMatchForkSource(t *testing.T) {
	for _, tc := range []struct {
		state ScannerState
		token TokenType
		err   string
		next  ScannerState
	}{
		{ScannerStateBeforeAttributeValue, TokenTypeAttributeValue, "", ScannerStateWithinTag},
		{ScannerStateAfterOpeningStartTag, TokenTypeUnknown, "Start tag name expected.", ScannerStateWithinTag},
		{ScannerStateAfterOpeningEndTag, TokenTypeUnknown, "End tag name expected.", ScannerStateWithinEndTag},
	} {
		scanner := NewScanner(`😀abc>`, 1, tc.state, false)
		if tok := scanner.Scan(); tok != tc.token || scanner.GetTokenOffset() != 1 || scanner.GetTokenEnd() != 5 || scanner.GetTokenText() != utf16CodeUnitString(0xDE00)+"abc" || scanner.GetTokenError() != tc.err || scanner.GetScannerState() != tc.next {
			t.Fatalf("state %v got token=%v range=%d..%d text=%q err=%q next=%v", tc.state, tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenText(), scanner.GetTokenError(), scanner.GetScannerState())
		}
	}
}

func TestScannerInitialOffsetInsideSurrogateBeforeAttributeValueStopsAtTagOpen(t *testing.T) {
	scanner := NewScanner(`😀abc</script>`, 1, ScannerStateBeforeAttributeValue, true)
	if tok := scanner.Scan(); tok != TokenTypeAttributeValue || scanner.GetTokenOffset() != 1 || scanner.GetTokenEnd() != 5 || scanner.GetTokenText() != utf16CodeUnitString(0xDE00)+"abc" || scanner.GetScannerState() != ScannerStateWithinTag {
		t.Fatalf("initial attribute value got token=%v range=%d..%d text=%q state=%v", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenText(), scanner.GetScannerState())
	}
	if tok := scanner.Scan(); tok != TokenTypeStartTagClose || scanner.GetTokenOffset() != 5 || scanner.GetTokenEnd() != 5 || scanner.GetTokenError() != "Closing bracket missing." || scanner.GetScannerState() != ScannerStateWithinContent {
		t.Fatalf("pseudo close got token=%v range=%d..%d err=%q state=%v", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenError(), scanner.GetScannerState())
	}
	if tok := scanner.Scan(); tok != TokenTypeEndTagOpen || scanner.GetTokenOffset() != 5 || scanner.GetTokenEnd() != 7 {
		t.Fatalf("end tag open got token=%v range=%d..%d", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd())
	}
	if tok := scanner.Scan(); tok != TokenTypeEndTag || scanner.GetTokenText() != "script" {
		t.Fatalf("end tag got token=%v text=%q", tok, scanner.GetTokenText())
	}
}

func TestScannerInitialOffsetInsideSurrogateBeforeAttributeValueBeforeSelfClose(t *testing.T) {
	scanner := NewScanner(`😀/>`, 1, ScannerStateBeforeAttributeValue, true)
	if tok := scanner.Scan(); tok != TokenTypeAttributeValue || scanner.GetTokenOffset() != 1 || scanner.GetTokenEnd() != 2 || scanner.GetTokenText() != utf16CodeUnitString(0xDE00) || scanner.GetScannerState() != ScannerStateWithinTag {
		t.Fatalf("initial attr before self-close got token=%v range=%d..%d text=%q state=%v", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenText(), scanner.GetScannerState())
	}
	if tok := scanner.Scan(); tok != TokenTypeStartTagSelfClose || scanner.GetTokenOffset() != 2 || scanner.GetTokenEnd() != 4 || scanner.GetTokenText() != "/>" || scanner.GetScannerState() != ScannerStateWithinContent {
		t.Fatalf("self-close got token=%v range=%d..%d text=%q state=%v", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenText(), scanner.GetScannerState())
	}
}

func TestScannerECMAScriptWhitespaceInAttributeRegexes(t *testing.T) {
	scanner := CreateScanner("<div \u00a0x=1>")
	for _, want := range []TokenType{TokenTypeStartTagOpen, TokenTypeStartTag, TokenTypeWhitespace, TokenTypeUnknown} {
		if tok := scanner.Scan(); tok != want {
			t.Fatalf("NBSP attribute scan got %v want %v", tok, want)
		}
	}
	if scanner.GetTokenText() != "\u00a0" || scanner.GetTokenError() != "Unexpected character in tag." {
		t.Fatalf("NBSP unknown got text=%q error=%q", scanner.GetTokenText(), scanner.GetTokenError())
	}

	scanner = CreateScanner("<div a=x\u00a0b>")
	for _, want := range []TokenType{TokenTypeStartTagOpen, TokenTypeStartTag, TokenTypeWhitespace, TokenTypeAttributeName, TokenTypeDelimiterAssign, TokenTypeAttributeValue, TokenTypeUnknown} {
		if tok := scanner.Scan(); tok != want {
			t.Fatalf("NBSP value scan got %v want %v", tok, want)
		}
	}
	if scanner.GetTokenText() != "\u00a0" {
		t.Fatalf("NBSP after value text got %q", scanner.GetTokenText())
	}
}

func TestScannerRawTextCloseUsesASCIICaseOnly(t *testing.T) {
	for _, input := range []string{"<script>a</ſcript>", "<style>a</ſtyle>"} {
		scanner := CreateScanner(input)
		for tok := scanner.Scan(); tok != TokenTypeStartTagClose; tok = scanner.Scan() {
			if tok == TokenTypeEOS {
				t.Fatalf("%s did not reach start tag close", input)
			}
		}
		tok := scanner.Scan()
		if input[1] == 's' && strings.HasPrefix(input, "<script") {
			if tok != TokenTypeScript {
				t.Fatalf("%s raw text got %v want script", input, tok)
			}
		} else if tok != TokenTypeStyles {
			t.Fatalf("%s raw text got %v want styles", input, tok)
		}
		if scanner.GetTokenEnd() != utf16Len(input) {
			t.Fatalf("%s raw text ended at %d want %d", input, scanner.GetTokenEnd(), utf16Len(input))
		}
	}
}

func TestScannerEndTagAstralErrorQueuesContentState(t *testing.T) {
	scanner := CreateScanner("</a😀>")
	for _, want := range []TokenType{TokenTypeEndTagOpen, TokenTypeEndTag} {
		if tok := scanner.Scan(); tok != want {
			t.Fatalf("end tag astral prefix got %v want %v", tok, want)
		}
	}
	if tok := scanner.Scan(); tok != TokenTypeUnknown || scanner.GetTokenOffset() != 3 || scanner.GetTokenEnd() != 4 || scanner.GetTokenError() != "Closing bracket expected." || scanner.GetScannerState() != ScannerStateWithinContent {
		t.Fatalf("end tag astral high got token=%v range=%d..%d error=%q state=%v", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenError(), scanner.GetScannerState())
	}
	if tok := scanner.Scan(); tok != TokenTypeContent || scanner.GetTokenOffset() != 4 || scanner.GetTokenEnd() != 6 || scanner.GetTokenText() != utf16CodeUnitString(0xDE00)+">" {
		t.Fatalf("end tag astral low got token=%v range=%d..%d text=% x", tok, scanner.GetTokenOffset(), scanner.GetTokenEnd(), scanner.GetTokenText())
	}
}
