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
