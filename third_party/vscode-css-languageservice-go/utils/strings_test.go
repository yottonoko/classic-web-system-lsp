package utils

import (
	"regexp"
	"testing"
)

func TestTrim(t *testing.T) {
	t.Run("trim", func(t *testing.T) {
		tests := []struct {
			input string
			re    string
			want  string
		}{
			{"test+-", `[ ]+$`, "test+-"},
			{"t est+-", `[ ]+$`, "t est+-"},
			{"test+- ", `[ ]+$`, "test+-"},
			{"test+- ", `[ \+]+$`, "test+-"},
			{"test+- ", `[ \+\-]+$`, "test"},
			{"test++- ", `[ \+\-]+$`, "test"},
			{"x  ", `[ ]$`, "x "},
			{"abc123", `[0-9]`, "abc12"},
		}
		for _, tt := range tests {
			if got := Trim(tt.input, regexp.MustCompile(tt.re)); got != tt.want {
				t.Fatalf("Trim(%q, %q) = %q, want %q", tt.input, tt.re, got, tt.want)
			}
		}
	})
}
