package css

import "testing"

func TestBeautifyBasicCSS(t *testing.T) {
	got, err := Beautify(".tabs{color:red;}", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := ".tabs {\n    color: red;\n}"
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

func TestBeautifyKeepsOpeningQuoteOfUnterminatedStrings(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`a { b: "x`, "a {\n  b: \"x"},
		{"a { b: 'x\ny: z }", "a {\n  b: 'x\n y: z\n}"},
		{`"000`, `"000`},
		{`a{b:"c"}`, "a {\n  b: \"c\"\n}"},
	}
	for _, test := range tests {
		got, err := Beautify(test.input, map[string]any{"indent_size": 2})
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("Beautify(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
