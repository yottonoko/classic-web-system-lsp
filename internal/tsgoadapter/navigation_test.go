package tsgoadapter

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAnalyzeJavaScriptNavigationWrapsTypeScriptGoAdapter(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `const target = "next.asp";
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "next.asp" {
		t.Fatalf("navigation values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationAdapterPreservesLargeSwitchFallback(t *testing.T) {
	const cases = 2500
	var source strings.Builder
	source.WriteString("switch (unknownFlag) {\n")
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = %q;\n  break;\n", index, fmt.Sprintf("adapter-%04d.asp", index))
	}
	source.WriteString("}\n")

	result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != cases {
		t.Fatalf("large switch adapter sinks = %d, want %d", len(result.Sinks), cases)
	}
	for _, sink := range result.Sinks {
		if len(sink.Expression.Values) == 0 {
			t.Fatalf("large switch adapter sink has no values: %#v", sink)
		}
	}
}
