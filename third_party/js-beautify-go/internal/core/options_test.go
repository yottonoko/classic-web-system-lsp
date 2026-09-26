package core

import (
	"reflect"
	"testing"
)

func TestMergeOpts(t *testing.T) {
	got := MergeOpts(map[string]any{"a": 1, "b": map[string]any{"a": 2, "c": 3}}, "b")
	want := map[string]any{"a": 2, "c": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MergeOpts() = %#v, want %#v", got, want)
	}
}

func TestNormalizeOpts(t *testing.T) {
	got := NormalizeOpts(map[string]any{"a-b": 1})
	if got["a_b"] != 1 {
		t.Fatalf("NormalizeOpts did not normalize dash key: %#v", got)
	}
}

func TestOptionGetters(t *testing.T) {
	opts, err := NewBaseOptions(map[string]any{
		"a": "c,d",
		"b": false,
		"c": `\r\n`,
		"d": "10px",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := opts.GetArray("a", []string{"x"}); !reflect.DeepEqual(got, []string{"c", "d"}) {
		t.Fatalf("GetArray() = %#v", got)
	}
	if opts.GetBoolean("b", true) {
		t.Fatal("GetBoolean returned true for false option")
	}
	if got := opts.GetCharacters("c", ""); got != "\r\n" {
		t.Fatalf("GetCharacters() = %q", got)
	}
	if got := opts.GetNumber("d", 1); got != 10 {
		t.Fatalf("GetNumber() = %d", got)
	}
}

func TestSelectionErrorsMatchJSBeautify(t *testing.T) {
	opts, err := NewBaseOptions(map[string]any{
		"wrap_attributes": "",
		"multi_choice":    "auto,force",
		"list_option":     []string{},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = opts.GetSelection("wrap_attributes", []string{"auto", "force"}, []string{"auto"})
	want := "Invalid Option Value: The option 'wrap_attributes' can contain only the following values:\nauto,force\nYou passed in: ''"
	if err == nil || err.Error() != want {
		t.Fatalf("GetSelection error = %q, want %q", err, want)
	}
	_, err = opts.GetSelection("multi_choice", []string{"auto", "force"}, []string{"auto"})
	want = "Invalid Option Value: The option 'multi_choice' can only be one of the following values:\nauto,force\nYou passed in: 'auto,force'"
	if err == nil || err.Error() != want {
		t.Fatalf("GetSelection multi error = %q, want %q", err, want)
	}
	_, err = opts.GetSelectionList("list_option", []string{"auto", "none"}, []string{"none"})
	want = "Invalid Option Value: The option 'list_option' can contain only the following values:\nauto,none\nYou passed in: ''"
	if err == nil || err.Error() != want {
		t.Fatalf("GetSelectionList error = %q, want %q", err, want)
	}
}
