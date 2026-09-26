package htmlservice

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const baselineAssertionSourceCommit = "82b56fd"
const baselineAssertionManifestTotal = 362
const baselineAssertionManifestUnique = 357

var baselineAssertionManifestPerFile = map[string]int{
	"completion.test.ts":      269,
	"customProviders.test.ts": 11,
	"pathCompletions.test.ts": 82,
}

type baselineAssertionEntry struct {
	File         string
	Helper       string
	Input        string
	Kind         string
	Label        string
	ResultText   string
	FilterText   string
	NotAvailable bool
	Count        *int
}

var baselineAssertionManifest = []baselineAssertionEntry{
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<|", Kind: "item", Label: "!DOCTYPE", ResultText: "<!DOCTYPE html>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<|", Kind: "item", Label: "iframe", ResultText: "<iframe", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<|", Kind: "item", Label: "h1", ResultText: "<h1", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<|", Kind: "item", Label: "div", ResultText: "<div", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "\n<|", Kind: "item", Label: "!DOCTYPE", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "\n<|", Kind: "item", Label: "iframe", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "\n<|", Kind: "item", Label: "h1", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "\n<|", Kind: "item", Label: "div", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "< |", Kind: "item", Label: "iframe", ResultText: "<iframe", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "< |", Kind: "item", Label: "h1", ResultText: "<h1", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "< |", Kind: "item", Label: "div", ResultText: "<div", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<h|", Kind: "item", Label: "html", ResultText: "<html", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<h|", Kind: "item", Label: "h1", ResultText: "<h1", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<h|", Kind: "item", Label: "header", ResultText: "<header", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input|", Kind: "item", Label: "input", ResultText: "<input", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<inp|ut", Kind: "item", Label: "input", ResultText: "<input", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<|inp", Kind: "item", Label: "input", ResultText: "<input", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input |", Kind: "item", Label: "type", ResultText: "<input type=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input |", Kind: "item", Label: "style", ResultText: "<input style=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input |", Kind: "item", Label: "onmousemove", ResultText: "<input onmousemove=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input t|", Kind: "item", Label: "type", ResultText: "<input type=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input t|", Kind: "item", Label: "tabindex", ResultText: "<input tabindex=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input t|ype", Kind: "item", Label: "type", ResultText: "<input type=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input t|ype", Kind: "item", Label: "tabindex", ResultText: "<input tabindex=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input t|ype=\"text\"", Kind: "item", Label: "type", ResultText: "<input type=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input t|ype=\"text\"", Kind: "item", Label: "tabindex", ResultText: "<input tabindex=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" |", Kind: "item", Label: "style", ResultText: "<input type=\"text\" style=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" |", Kind: "item", Label: "type", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" |", Kind: "item", Label: "size", ResultText: "<input type=\"text\" size=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input | type=\"text\"", Kind: "item", Label: "style", ResultText: "<input style=\"$1\" type=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input | type=\"text\"", Kind: "item", Label: "type", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input | type=\"text\"", Kind: "item", Label: "size", ResultText: "<input size=\"$1\" type=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" type=\"number\" |", Kind: "item", Label: "style", ResultText: "<input type=\"text\" type=\"number\" style=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" type=\"number\" |", Kind: "item", Label: "type", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" type=\"number\" |", Kind: "item", Label: "size", ResultText: "<input type=\"text\" type=\"number\" size=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" s|", Kind: "item", Label: "style", ResultText: "<input type=\"text\" style=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" s|", Kind: "item", Label: "src", ResultText: "<input type=\"text\" src=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"text\" s|", Kind: "item", Label: "size", ResultText: "<input type=\"text\" size=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input di| type=\"text\"", Kind: "item", Label: "disabled", ResultText: "<input disabled type=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input di| type=\"text\"", Kind: "item", Label: "dir", ResultText: "<input dir=\"$1\" type=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input disabled | type=\"text\"", Kind: "item", Label: "dir", ResultText: "<input disabled dir=\"$1\" type=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input disabled | type=\"text\"", Kind: "item", Label: "style", ResultText: "<input disabled style=\"$1\" type=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=|", Kind: "item", Label: "text", ResultText: "<input type=\"text\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=|", Kind: "item", Label: "checkbox", ResultText: "<input type=\"checkbox\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"c|", Kind: "item", Label: "color", ResultText: "<input type=\"color", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"c|", Kind: "item", Label: "checkbox", ResultText: "<input type=\"checkbox", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"|", Kind: "item", Label: "color", ResultText: "<input type=\"color", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type=\"|", Kind: "item", Label: "checkbox", ResultText: "<input type=\"checkbox", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type= |", Kind: "item", Label: "color", ResultText: "<input type= \"color\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input type= |", Kind: "item", Label: "checkbox", ResultText: "<input type= \"checkbox\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input src=\"c\" type=\"color|\" ", Kind: "item", Label: "color", ResultText: "<input src=\"c\" type=\"color\" ", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<iframe sandbox=\"allow-forms |", Kind: "item", Label: "allow-modals", ResultText: "<iframe sandbox=\"allow-forms allow-modals", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<iframe sandbox=\"allow-forms allow-modals|", Kind: "item", Label: "allow-modals", ResultText: "<iframe sandbox=\"allow-forms allow-modals", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<iframe sandbox=\"allow-forms all|\"", Kind: "item", Label: "allow-modals", ResultText: "<iframe sandbox=\"allow-forms allow-modals\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<iframe sandbox=\"allow-forms a|llow-modals \"", Kind: "item", Label: "allow-modals", ResultText: "<iframe sandbox=\"allow-forms allow-modals \"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input src=\"c\" type=color| ", Kind: "item", Label: "color", ResultText: "<input src=\"c\" type=\"color\" ", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<th><input type=\"che|</th>", Kind: "item", Label: "checkbox", ResultText: "<th><input type=\"checkbox</th>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<th><input type=\"che|</th><td></td>", Kind: "item", Label: "checkbox", ResultText: "<th><input type=\"checkbox</th><td></td>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div dir=|></div>", Kind: "item", Label: "ltr", ResultText: "<div dir=\"ltr\"></div>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div dir=|></div>", Kind: "item", Label: "rtl", ResultText: "<div dir=\"rtl\"></div>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<ul><|>", Kind: "item", Label: "/ul", ResultText: "<ul></ul>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<ul><|>", Kind: "item", Label: "li", ResultText: "<ul><li>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<ul><li><|", Kind: "item", Label: "/li", ResultText: "<ul><li></li>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<ul><li><|", Kind: "item", Label: "a", ResultText: "<ul><li><a", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<goo></|>", Kind: "item", Label: "/goo", ResultText: "<goo></goo>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<foo></f|", Kind: "item", Label: "/foo", ResultText: "<foo></foo>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<foo></f|o", Kind: "item", Label: "/foo", ResultText: "<foo></foo>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<foo></|fo", Kind: "item", Label: "/foo", ResultText: "<foo></foo>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<foo></ |>", Kind: "item", Label: "/foo", ResultText: "<foo></foo>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span></ s|", Kind: "item", Label: "/span", ResultText: "<span></span>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<li><br></ |>", Kind: "item", Label: "/li", ResultText: "<li><br></li>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<li/|>", Kind: "count", Count: assertionIntPtr(0)},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "  <div/|   ", Kind: "count", Count: assertionIntPtr(0)},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<foo><br/></ f|>", Kind: "item", Label: "/foo", ResultText: "<foo><br/></foo>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<li><div/></|", Kind: "item", Label: "/li", ResultText: "<li><div/></li>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<li><br/|>", Kind: "count", Count: assertionIntPtr(0)},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<li><br>a/|", Kind: "count", Count: assertionIntPtr(0)},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<foo><bar></bar></|   ", Kind: "item", Label: "/foo", ResultText: "<foo><bar></bar></foo>   ", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>\n  <form>\n    <div>\n      <label></label>\n      <|\n    </div>\n  </form></div>", Kind: "item", Label: "span", ResultText: "<div>\n  <form>\n    <div>\n      <label></label>\n      <span\n    </div>\n  </form></div>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>\n  <form>\n    <div>\n      <label></label>\n      <|\n    </div>\n  </form></div>", Kind: "item", Label: "/div", ResultText: "<div>\n  <form>\n    <div>\n      <label></label>\n    </div>\n    </div>\n  </form></div>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body><div><div></div></div></|  >", Kind: "item", Label: "/body", ResultText: "<body><div><div></div></div></body  >", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body>\n  <div>\n    </|", Kind: "item", Label: "/div", ResultText: "<body>\n  <div>\n  </div>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div><a hre|</div>", Kind: "item", Label: "href", ResultText: "<div><a href=\"$1\"</div>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<a><b>foo</b><|f>", Kind: "item", Label: "/a", ResultText: "<a><b>foo</b></a>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<a><b>foo</b><|f>", Kind: "item", Label: "/f", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<a><b>foo</b><| bar.", Kind: "item", Label: "/a", ResultText: "<a><b>foo</b></a> bar.", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<a><b>foo</b><| bar.", Kind: "item", Label: "/bar", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div><h1><br><span></span><img></| </h1></div>", Kind: "item", Label: "/h1", ResultText: "<div><h1><br><span></span><img></h1> </h1></div>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>|", Kind: "item", Label: "</div>", ResultText: "<div>$0</div>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>|", Kind: "item", Label: "</div>", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div d|", Kind: "item", Label: "data-", ResultText: "<div data-$1=\"$2\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div no-data-test=\"no-data\" d|", Kind: "item", Label: "no-data-test", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div data-custom=\"test\"><div d|", Kind: "item", Label: "data-", ResultText: "<div data-custom=\"test\"><div data-$1=\"$2\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div data-custom=\"test\"><div d|", Kind: "item", Label: "data-custom", ResultText: "<div data-custom=\"test\"><div data-custom=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div d|", Kind: "item", Label: "data-", ResultText: "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div data-$1=\"$2\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div d|", Kind: "item", Label: "data-custom", ResultText: "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div data-custom=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div d|", Kind: "item", Label: "data-custom-two", ResultText: "<div data-custom=\"test\"><div data-custom-two=\"2\"></div></div>\n <div data-custom-two=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body data-ng-app=\"\"><div id=\"first\" data-ng-include=\" 'firstdoc.html' \"></div><div id=\"second\" inc|></div></body>", Kind: "item", Label: "data-ng-include", ResultText: "<body data-ng-app=\"\"><div id=\"first\" data-ng-include=\" 'firstdoc.html' \"></div><div id=\"second\" data-ng-include=\"$1\"></div></body>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<d|", Kind: "item", Label: "div", ResultText: "<div", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<LI></|", Kind: "item", Label: "/LI", ResultText: "<LI></LI>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<LI></|", Kind: "item", Label: "/li", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<lI></|", Kind: "item", Label: "/lI", ResultText: "<lI></lI>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<iNpUt |", Kind: "item", Label: "type", ResultText: "<iNpUt type=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<INPUT TYPE=|", Kind: "item", Label: "color", ResultText: "<INPUT TYPE=\"color\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<dIv>|", Kind: "item", Label: "</dIv>", ResultText: "<dIv>$0</dIv>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<script id=\"entry-template\" type=\"text/x-handlebars-template\"> <| </script>", Kind: "item", Label: "div", ResultText: "<script id=\"entry-template\" type=\"text/x-handlebars-template\"> <div </script>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<script id=\"html-template\" type=\"text/html\"> <| </script>", Kind: "item", Label: "div", ResultText: "<script id=\"html-template\" type=\"text/html\"> <div </script>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-activedescendant", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-atomic", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-autocomplete", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-busy", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-checked", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-colcount", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-colindex", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-colspan", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-controls", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-current", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-describedby", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-disabled", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-dropeffect", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-errormessage", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-expanded", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-flowto", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-grabbed", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-haspopup", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-hidden", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-invalid", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-label", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-labelledby", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-level", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-live", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-modal", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-multiline", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-multiselectable", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-orientation", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-owns", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-placeholder", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-posinset", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-pressed", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-readonly", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-relevant", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-required", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-roledescription", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-rowcount", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-rowindex", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-rowspan", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-selected", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-setsize", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-sort", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-valuemax", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-valuemin", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-valuenow", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div  |> </div >", Kind: "item", Label: "aria-valuetext", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-activedescendant", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-atomic", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-autocomplete", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-busy", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-checked", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-colcount", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-colindex", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-colspan", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-controls", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-current", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-describedby", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-disabled", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-dropeffect", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-errormessage", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-expanded", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-flowto", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-grabbed", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-haspopup", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-hidden", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-invalid", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-label", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-labelledby", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-level", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-live", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-modal", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-multiline", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-multiselectable", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-orientation", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-owns", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-placeholder", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-posinset", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-pressed", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-readonly", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-relevant", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-required", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-roledescription", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-rowcount", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-rowindex", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-rowspan", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-selected", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-setsize", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-sort", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-valuemax", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-valuemin", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-valuenow", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<span  |> </span >", Kind: "item", Label: "aria-valuetext", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-activedescendant", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-atomic", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-autocomplete", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-busy", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-checked", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-colcount", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-colindex", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-colspan", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-controls", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-current", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-describedby", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-disabled", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-dropeffect", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-errormessage", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-expanded", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-flowto", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-grabbed", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-haspopup", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-hidden", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-invalid", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-label", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-labelledby", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-level", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-live", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-modal", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-multiline", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-multiselectable", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-orientation", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-owns", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-placeholder", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-posinset", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-pressed", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-readonly", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-relevant", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-required", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-roledescription", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-rowcount", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-rowindex", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-rowspan", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-selected", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-setsize", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-sort", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-valuemax", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-valuemin", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-valuenow", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<input  |> </input >", Kind: "item", Label: "aria-valuetext", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<|", Kind: "item", Label: "div", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div clas|", Kind: "item", Label: "class", ResultText: "<div class=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div clas|", Kind: "item", Label: "class", ResultText: "<div class='$1'", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div clas|", Kind: "item", Label: "class", ResultText: "<div class=$1", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>&|", Kind: "item", Label: "&hookrightarrow;", ResultText: "<div>&hookrightarrow;", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>&|", Kind: "item", Label: "&plus;", ResultText: "<div>&plus;", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>Hello&|", Kind: "item", Label: "&ZeroWidthSpace;", ResultText: "<div>Hello&ZeroWidthSpace;", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>Hello&gt|", Kind: "item", Label: "&gtrdot;", ResultText: "<div>Hello&gtrdot;", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div class=\"&g|\"", Kind: "item", Label: "&grave;", ResultText: "<div class=\"&grave;\"", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div class=&d|", Kind: "item", Label: "&duarr;", ResultText: "<div class=&duarr;", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div &d|", Kind: "item", Label: "&duarr;", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div&d|", Kind: "item", Label: "&duarr;", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div> <| </div>", Kind: "item", Label: "/div", ResultText: "", FilterText: "/div", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>\n  <|\n</div>", Kind: "item", Label: "/div", ResultText: "", FilterText: "  </div", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "</|", Kind: "item", Label: "/a", ResultText: "</a>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div></div></|", Kind: "item", Label: "/a", ResultText: "<div></div></a>", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body>\n<|", Kind: "item", Label: "/body", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body>\n<|", Kind: "item", Label: "div", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body>\n<|", Kind: "item", Label: "/body", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body>\n<|", Kind: "item", Label: "div", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body>\n<|", Kind: "item", Label: "/body", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<body>\n<|", Kind: "item", Label: "div", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "<div>\n  <|\n</div>", Kind: "item", Label: "/div", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "completion.test.ts", Helper: "testCompletionFor", Input: "</|", Kind: "item", Label: "/a", ResultText: "", FilterText: "", NotAvailable: true},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./|\">", Kind: "item", Label: "about/", ResultText: "<script src=\"./about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./|\">", Kind: "item", Label: "index.html", ResultText: "<script src=\"./index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./|\">", Kind: "item", Label: "src/", ResultText: "<script src=\"./src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src='./|'>", Kind: "item", Label: "about/", ResultText: "<script src='./about/'>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src='./|'>", Kind: "item", Label: "index.html", ResultText: "<script src='./index.html'>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src='./|'>", Kind: "item", Label: "src/", ResultText: "<script src='./src/'>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"../|\">", Kind: "item", Label: "about/", ResultText: "<script src=\"../about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"../|\">", Kind: "item", Label: "index.html", ResultText: "<script src=\"../index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"../|\">", Kind: "item", Label: "src/", ResultText: "<script src=\"../src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"../src/|\">", Kind: "item", Label: "feature.js", ResultText: "<script src=\"../src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"../src/|\">", Kind: "item", Label: "test.js", ResultText: "<script src=\"../src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"/|\">", Kind: "item", Label: "about/", ResultText: "<script src=\"/about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"/|\">", Kind: "item", Label: "index.html", ResultText: "<script src=\"/index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"/|\">", Kind: "item", Label: "src/", ResultText: "<script src=\"/src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"/src/|\">", Kind: "item", Label: "feature.js", ResultText: "<script src=\"/src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"/src/|\">", Kind: "item", Label: "test.js", ResultText: "<script src=\"/src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"|\">", Kind: "item", Label: "about/", ResultText: "<script src=\"about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"|\">", Kind: "item", Label: "index.html", ResultText: "<script src=\"index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"|\">", Kind: "item", Label: "src/", ResultText: "<script src=\"src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"|\">", Kind: "item", Label: "about.css", ResultText: "<script src=\"about.css\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"|\">", Kind: "item", Label: "about.html", ResultText: "<script src=\"about.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"|\">", Kind: "item", Label: "media/", ResultText: "<script src=\"media/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"/src/f|\">", Kind: "item", Label: "feature.js", ResultText: "<script src=\"/src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"/src/f|\">", Kind: "item", Label: "test.js", ResultText: "<script src=\"/src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"../src/f|\">", Kind: "item", Label: "feature.js", ResultText: "<script src=\"../src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"../src/f|\">", Kind: "item", Label: "test.js", ResultText: "<script src=\"../src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|\">", Kind: "item", Label: "about/", ResultText: "<script src=\"about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|\">", Kind: "item", Label: "index.html", ResultText: "<script src=\"index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|\">", Kind: "item", Label: "src/", ResultText: "<script src=\"src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"src/|\">", Kind: "item", Label: "feature.js", ResultText: "<script src=\"src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"src/|\">", Kind: "item", Label: "test.js", ResultText: "<script src=\"src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"src/f|\">", Kind: "item", Label: "feature.js", ResultText: "<script src=\"src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"src/f|\">", Kind: "item", Label: "test.js", ResultText: "<script src=\"src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|\">", Kind: "item", Label: "about.css", ResultText: "<script src=\"about.css\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|\">", Kind: "item", Label: "about.html", ResultText: "<script src=\"about.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|\">", Kind: "item", Label: "media/", ResultText: "<script src=\"media/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"media/|\">", Kind: "item", Label: "icon.pic", ResultText: "<script src=\"media/icon.pic\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"media/f|\">", Kind: "item", Label: "icon.pic", ResultText: "<script src=\"media/icon.pic\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"src/f|eature.js\">", Kind: "item", Label: "feature.js", ResultText: "<script src=\"src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"src/f|eature.js\">", Kind: "item", Label: "test.js", ResultText: "<script src=\"src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|rc/feature.js\">", Kind: "item", Label: "about/", ResultText: "<script src=\"about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|rc/feature.js\">", Kind: "item", Label: "index.html", ResultText: "<script src=\"index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"s|rc/feature.js\">", Kind: "item", Label: "src/", ResultText: "<script src=\"src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"media/f|eature.js\">", Kind: "item", Label: "icon.pic", ResultText: "<script src=\"media/icon.pic\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"m|edia/feature.js\">", Kind: "item", Label: "about.css", ResultText: "<script src=\"about.css\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"m|edia/feature.js\">", Kind: "item", Label: "about.html", ResultText: "<script src=\"about.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"m|edia/feature.js\">", Kind: "item", Label: "media/", ResultText: "<script src=\"media/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./| about/about.html>", Kind: "item", Label: "about/", ResultText: "<script src=\"./about/ about/about.html>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./| about/about.html>", Kind: "item", Label: "index.html", ResultText: "<script src=\"./index.html about/about.html>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./| about/about.html>", Kind: "item", Label: "src/", ResultText: "<script src=\"./src/ about/about.html>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./a|bout /about.html>", Kind: "item", Label: "about/", ResultText: "<script src=\"./about/ /about.html>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./a|bout /about.html>", Kind: "item", Label: "index.html", ResultText: "<script src=\"./index.html /about.html>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./a|bout /about.html>", Kind: "item", Label: "src/", ResultText: "<script src=\"./src/ /about.html>", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./|\"", Kind: "count", Count: assertionIntPtr(4)},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<my-custom-element src=\"../|\">", Kind: "item", Label: "about/", ResultText: "<my-custom-element src=\"../about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<my-custom-element src=\"../|\">", Kind: "item", Label: "index.html", ResultText: "<my-custom-element src=\"../index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<my-custom-element src=\"../|\">", Kind: "item", Label: "src/", ResultText: "<my-custom-element src=\"../src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<my-custom-element href=\"../src/|\">", Kind: "item", Label: "feature.js", ResultText: "<my-custom-element href=\"../src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<my-custom-element href=\"../src/|\">", Kind: "item", Label: "test.js", ResultText: "<my-custom-element href=\"../src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"stylesheet\" href=\"|\">", Kind: "item", Label: "about.css", ResultText: "<link rel=\"stylesheet\" href=\"about.css\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"stylesheet\" href=\"|\">", Kind: "item", Label: "about.html", ResultText: "<link rel=\"stylesheet\" href=\"about.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"stylesheet\" href=\"|\">", Kind: "item", Label: "media/", ResultText: "<link rel=\"stylesheet\" href=\"media/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"stylesheet\" href=\"./|\">", Kind: "item", Label: "about/", ResultText: "<link rel=\"stylesheet\" href=\"./about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"stylesheet\" href=\"./|\">", Kind: "item", Label: "styles.css", ResultText: "<link rel=\"stylesheet\" href=\"./styles.css\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"stylesheet\" href=\"./|\">", Kind: "item", Label: "index.html", ResultText: "<link rel=\"stylesheet\" href=\"./index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"stylesheet\" href=\"./|\">", Kind: "item", Label: "src/", ResultText: "<link rel=\"stylesheet\" href=\"./src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel='stylesheet' href=\"./|\">", Kind: "item", Label: "about/", ResultText: "<link rel='stylesheet' href=\"./about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel='stylesheet' href=\"./|\">", Kind: "item", Label: "styles.css", ResultText: "<link rel='stylesheet' href=\"./styles.css\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel='stylesheet' href=\"./|\">", Kind: "item", Label: "index.html", ResultText: "<link rel='stylesheet' href=\"./index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel='stylesheet' href=\"./|\">", Kind: "item", Label: "src/", ResultText: "<link rel='stylesheet' href=\"./src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./|\">", Kind: "item", Label: "about/", ResultText: "<script src=\"./about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./|\">", Kind: "item", Label: "index.html", ResultText: "<script src=\"./index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./|\">", Kind: "item", Label: "src/", ResultText: "<script src=\"./src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./src/|\">", Kind: "item", Label: "feature.js", ResultText: "<script src=\"./src/feature.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<script src=\"./src/|\">", Kind: "item", Label: "test.js", ResultText: "<script src=\"./src/test.js\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link href=\"./|\">", Kind: "item", Label: "about/", ResultText: "<link href=\"./about/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link href=\"./|\">", Kind: "item", Label: "index.html", ResultText: "<link href=\"./index.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link href=\"./|\">", Kind: "item", Label: "src/", ResultText: "<link href=\"./src/\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link href=\"./|\">", Kind: "item", Label: "styles.css", ResultText: "<link href=\"./styles.css\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"icon\" href=\"|\">", Kind: "item", Label: "about.css", ResultText: "<link rel=\"icon\" href=\"about.css\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"icon\" href=\"|\">", Kind: "item", Label: "about.html", ResultText: "<link rel=\"icon\" href=\"about.html\">", FilterText: "", NotAvailable: false},
	{File: "pathCompletions.test.ts", Helper: "testCompletion2For", Input: "<link rel=\"icon\" href=\"|\">", Kind: "item", Label: "media/", ResultText: "<link rel=\"icon\" href=\"media/\">", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<|", Kind: "item", Label: "foo", ResultText: "<foo", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<|", Kind: "item", Label: "Bar", ResultText: "<Bar", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<foo |", Kind: "item", Label: "bar", ResultText: "<foo bar=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<foo |", Kind: "item", Label: "fooAttr", ResultText: "<foo fooAttr=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<foo |", Kind: "item", Label: "xattr", ResultText: "<foo xattr=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<foo bar=|", Kind: "item", Label: "baz", ResultText: "<foo bar=\"baz\"", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<foo xattr=|", Kind: "item", Label: "xval", ResultText: "<foo xattr=\"xval\"", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<Bar |", Kind: "item", Label: "Xoo", ResultText: "", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<other |", Kind: "item", Label: "fooAttr", ResultText: "<other fooAttr=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<other |", Kind: "item", Label: "xattr", ResultText: "<other xattr=\"$1\"", FilterText: "", NotAvailable: false},
	{File: "customProviders.test.ts", Helper: "testCompletionFor", Input: "<other xattr=|", Kind: "item", Label: "xval", ResultText: "<other xattr=\"xval\"", FilterText: "", NotAvailable: false},
}

func TestBaselineAssertionManifest(t *testing.T) {
	if got := len(baselineAssertionManifest); got != baselineAssertionManifestTotal {
		t.Fatalf("baseline assertion manifest total: got %d want %d", got, baselineAssertionManifestTotal)
	}
	unique := map[baselineAssertionEntry]bool{}
	perFile := map[string]int{}
	for _, entry := range baselineAssertionManifest {
		unique[entry] = true
		perFile[entry.File]++
	}
	if got := len(unique); got != baselineAssertionManifestUnique {
		t.Fatalf("baseline assertion manifest unique count: got %d want %d", got, baselineAssertionManifestUnique)
	}
	for file, want := range baselineAssertionManifestPerFile {
		if got := perFile[file]; got != want {
			t.Fatalf("baseline assertion count for %s: got %d want %d", file, got, want)
		}
	}

	covered, err := goAssertionStringLiterals()
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, entry := range baselineAssertionManifest {
		switch entry.Kind {
		case "count":
			if !covered[entry.Input] {
				missing = append(missing, fmt.Sprintf("%s %s count input %q", entry.File, entry.Helper, entry.Input))
			}
		case "item":
			if entry.Label != "" && !covered[entry.Label] {
				missing = append(missing, fmt.Sprintf("%s %s label %q for %q", entry.File, entry.Helper, entry.Label, entry.Input))
			}
			if entry.ResultText != "" && !covered[entry.ResultText] {
				missing = append(missing, fmt.Sprintf("%s %s resultText %q for %q", entry.File, entry.Helper, entry.ResultText, entry.Input))
			}
			if entry.FilterText != "" && !covered[entry.FilterText] {
				missing = append(missing, fmt.Sprintf("%s %s filterText %q for %q", entry.File, entry.Helper, entry.FilterText, entry.Input))
			}
		}
	}
	if len(missing) > 0 {
		limit := minInt(len(missing), 20)
		t.Fatalf("baseline assertion units missing from Go tests: %d/%d\n%s", len(missing), len(baselineAssertionManifest), strings.Join(missing[:limit], "\n"))
	}
}

func goAssertionStringLiterals() (map[string]bool, error) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range files {
		switch name {
		case "baseline_coverage_manifest_test.go", "baseline_assertion_manifest_test.go":
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			return nil, err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err == nil {
				covered[value] = true
			}
			return true
		})
	}
	return covered, nil
}

func assertionIntPtr(v int) *int { return &v }
