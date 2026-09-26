package core

import (
	"regexp"
	"testing"
)

func TestInputScanner(t *testing.T) {
	input := NewInputScanner("howdy")
	if input.Next() != "h" || input.Next() != "o" {
		t.Fatal("Next did not advance")
	}
	if input.Peek(2) != "y" {
		t.Fatalf("Peek() = %q", input.Peek(2))
	}
	input.Back()
	if input.Peek() != "o" {
		t.Fatalf("Back() left peek at %q", input.Peek())
	}
	input.Restart()
	if got := input.Read(regexp.MustCompile("how")); got != "how" {
		t.Fatalf("Read() = %q", got)
	}
	if got := input.ReadUntil(regexp.MustCompile("y"), false); got != "d" {
		t.Fatalf("ReadUntil() = %q", got)
	}
}
