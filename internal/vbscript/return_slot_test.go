package vbscript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestIsReturnValueSlotUsesOnlyActiveFunctionOrPropertyGet(t *testing.T) {
	source := `<%
Function a()
  a = 1
  a = a - 9
  a = a(1)
  a 1
  Call a
End Function
Sub b()
  b = b - 1
End Sub
Function c()
  a = a - 1
End Function
Class Item
  Public Property Get Value()
    Value = Value & "x"
  End Property
  Public Property Let Other(value)
    Other = Other & value
  End Property
End Class
a = a - 1
%>`
	parsed := core.ParseDocument("file:///site/return-slot.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	cases := []struct {
		name    string
		marker  string
		advance int
		want    bool
	}{
		{name: "function read", marker: "a - 9", want: true},
		{name: "recursive call", marker: "a(1)", want: false},
		{name: "statement call", marker: "a 1", want: false},
		{name: "call keyword", marker: "Call a", advance: len("Call "), want: false},
		{name: "sub name", marker: "b - 1", want: false},
		{name: "unrelated function", marker: "a - 1\nEnd Function", want: false},
		{name: "property get read", marker: "Value &", want: true},
		{name: "property let name", marker: "Other &", want: false},
		{name: "outside procedure", marker: "a - 1\n%>", want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			offset := strings.Index(source, testCase.marker)
			if offset < 0 {
				t.Fatalf("marker %q missing", testCase.marker)
			}
			offset += testCase.advance
			if got := IsReturnValueSlot(parsed, document.PositionAt(offset)); got != testCase.want {
				t.Fatalf("IsReturnValueSlot(%q) = %t, want %t", testCase.marker, got, testCase.want)
			}
		})
	}
}
