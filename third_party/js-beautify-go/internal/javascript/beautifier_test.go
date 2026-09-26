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
