package embedded

import (
	"context"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestServicePanicsAreReportedAndReturnZeroValues(t *testing.T) {
	var operations []string
	SetPanicReporter(func(operation string, recovered any, stack []byte) {
		if recovered == nil || len(stack) == 0 {
			t.Errorf("%s: recovered = %v, stack length = %d", operation, recovered, len(stack))
		}
		operations = append(operations, operation)
	})
	defer SetPanicReporter(nil)

	// A nil parsed document makes every service dereference nil.
	if hover := NewHTML().Hover(nil, lsp.Position{}); hover != nil {
		t.Fatalf("HTML hover = %#v, want nil", hover)
	}
	if list := NewCSS().Complete(context.Background(), nil, lsp.Position{}); len(list.Items) != 0 {
		t.Fatalf("CSS completions = %#v, want none", list.Items)
	}
	if diagnostics := NewCSS().Diagnostics(nil); diagnostics != nil {
		t.Fatalf("CSS diagnostics = %#v, want nil", diagnostics)
	}
	if got := strings.Join(operations, ","); got != "html.Hover,css.Complete,css.Diagnostics" {
		t.Fatalf("reported operations = %q", got)
	}
}

func TestFormatterPanicsBecomeErrors(t *testing.T) {
	SetPanicReporter(nil)
	format := func() (text string, err error) {
		defer recoverFormat("FormatHTML", &text, &err)
		text = "partial"
		panic("boom")
	}
	text, err := format()
	if text != "" || err == nil || !strings.Contains(err.Error(), "FormatHTML failed: boom") {
		t.Fatalf("format() = %q, %v", text, err)
	}
}
