package css

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkCSSBeautify(b *testing.B) {
	data := readBenchmarkFile(b, "github.css")
	options := map[string]any{"wrap_line_length": 80}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Beautify(data, options); err != nil {
			b.Fatal(err)
		}
	}
}

func readBenchmarkFile(b *testing.B, name string) string {
	b.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "resources", name))
	if err != nil {
		b.Fatal(err)
	}
	return string(data)
}
