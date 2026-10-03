package javascript

import "testing"

func TestBeautifyBasicJavaScript(t *testing.T) {
	got, err := Beautify("if(a){b();}else{c();}", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "if (a) {\n    b();\n} else {\n    c();\n}"
	if got != want {
		t.Fatalf("Beautify() = %q, want %q", got, want)
	}
}

// Expected outputs come from js-beautify 1.15.4.
func TestBeautifyReadsSmartyTemplatesOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		input      string
		templating []string
		want       string
	}{
		{
			input:      "var a = {$x};\n{* c *}\nvar b={literal}{a}{/literal};",
			templating: []string{"smarty"},
			want:       "var a = {$x};\n{* c *}\nvar b = {literal}{a}{/literal};",
		},
		{input: "var a = {$x};", templating: []string{"auto"}, want: "var a = {\n    $x\n};"},
		{input: "var a = {$x", templating: []string{"smarty"}, want: "var a = {$x"},
	}
	for _, test := range tests {
		got, err := Beautify(test.input, map[string]any{"templating": test.templating})
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("Beautify(%q, %v) = %q, want %q", test.input, test.templating, got, test.want)
		}
	}
}
