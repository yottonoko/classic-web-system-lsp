package htmlservice

import "testing"

func TestGetMissingBaselineBrowsersMatchesForkSourceListFormatting(t *testing.T) {
	cases := []struct {
		name     string
		browsers []string
		want     string
	}{
		{"nil browsers", nil, ""},
		{"empty browser list", []string{}, "Chrome, Edge, Firefox, or Safari"},
		{"chrome desktop only", []string{"CA1", "E1", "FF1", "FFA1", "S1", "SM1"}, "Chrome on desktop"},
		{"chrome android only", []string{"C1", "E1", "FF1", "FFA1", "S1", "SM1"}, "Chrome on Android"},
		{"chrome both platforms", []string{"E1", "FF1", "FFA1", "S1", "SM1"}, "Chrome"},
		{"edge only", []string{"C1", "CA1", "FF1", "FFA1", "S1", "SM1"}, "Edge"},
		{"firefox desktop only", []string{"C1", "CA1", "E1", "FFA1", "S1", "SM1"}, "Firefox on desktop"},
		{"safari ios only", []string{"C1", "CA1", "E1", "FF1", "FFA1", "S1"}, "Safari on iOS"},
		{"all desktop missing", []string{"CA1", "FFA1", "SM1"}, "Chrome on desktop, Edge, Firefox on desktop, or Safari on macOS"},
	}
	for _, tc := range cases {
		if got := GetMissingBaselineBrowsers(tc.browsers); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestLimitedBaselineDocumentationUsesForkSourceMissingBrowserNames(t *testing.T) {
	doc := GenerateDocumentation(TagData{
		Description: "A limited tag.",
		Status:      &BaselineStatus{Baseline: false},
		Browsers:    []string{"CA1", "E1", "FF1", "FFA1", "S1", "SM1"},
	}, HoverSettings{}, false)
	if doc == nil {
		t.Fatal("expected documentation")
	}
	want := "A limited tag.\n\nLimited availability across major browsers (Not fully implemented in Chrome on desktop)"
	if doc.Value != want {
		t.Fatalf("documentation got %q want %q", doc.Value, want)
	}
}

func TestPathAttributeHelperCaseSensitivityMatchesForkSource(t *testing.T) {
	manager := NewHTMLDataManager(LanguageServiceOptions{})
	if manager.IsPathAttribute("A", "HREF") {
		t.Fatalf("uppercase path attribute should not match fork source helper")
	}
	if !manager.IsPathAttribute("a", "href") {
		t.Fatalf("lowercase href should be a path attribute")
	}
}

func TestDefaultHTMLDataCopiesRemainIndependent(t *testing.T) {
	first := defaultHTMLData()
	second := defaultHTMLData()

	first.Tags[0].Name = "changed"
	first.Tags[0].Attributes[0].Name = "changed"
	if description, ok := first.Tags[0].Description.(map[string]any); ok {
		description["value"] = "changed"
	}

	if second.Tags[0].Name == "changed" {
		t.Fatal("tag mutation leaked into a later default data copy")
	}
	if second.Tags[0].Attributes[0].Name == "changed" {
		t.Fatal("attribute mutation leaked into a later default data copy")
	}
	if description, ok := second.Tags[0].Description.(map[string]any); ok && description["value"] == "changed" {
		t.Fatal("description mutation leaked into a later default data copy")
	}
}
