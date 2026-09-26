package beautify

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var benchmarkSink string

func TestPublicAPIOptionsCompatibility(t *testing.T) {
	resetPublicCacheForTest()

	jsNil, err := JS("if(a){b();}", nil)
	if err != nil {
		t.Fatal(err)
	}
	jsEmpty, err := JS("if(a){b();}", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if jsNil != jsEmpty {
		t.Fatalf("nil and empty options differ:\n%q\n%q", jsNil, jsEmpty)
	}

	jsDash, err := JS("if(a){b();}", Options{"indent-size": 2})
	if err != nil {
		t.Fatal(err)
	}
	jsUnderscore, err := JS("if(a){b();}", Options{"indent_size": 2})
	if err != nil {
		t.Fatal(err)
	}
	if jsDash != jsUnderscore || !strings.Contains(jsDash, "\n  b();") {
		t.Fatalf("dash/underscore options differ or indent was not applied:\ndash=%q\nunderscore=%q", jsDash, jsUnderscore)
	}

	htmlOut, err := HTML(`<div><p>x</p></div><script>if(a){b();}</script><style>a{color:red;}</style>`, Options{
		"indent_size": 4,
		"html":        map[string]any{"indent-size": 2},
		"js":          map[string]any{"indent-size": 3},
		"css":         map[string]any{"indent-size": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(htmlOut, "\n  <p>x</p>") {
		t.Fatalf("html child options were not applied:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, "\n     b();") {
		t.Fatalf("js child options were not applied:\n%s", htmlOut)
	}
	if !strings.Contains(htmlOut, "\n       color: red;") {
		t.Fatalf("css child options were not applied:\n%s", htmlOut)
	}
}

func TestPublicCacheCorrectness(t *testing.T) {
	resetPublicCacheForTest()

	source := "if(a){b();}"
	options := Options{"indent_size": 4}
	got, err := JS(source, options)
	if err != nil {
		t.Fatal(err)
	}
	if size := publicCacheSizeForTest(); size != 1 {
		t.Fatalf("cache size after first call = %d, want 1", size)
	}
	again, err := JS(source, options)
	if err != nil {
		t.Fatal(err)
	}
	if got != again {
		t.Fatalf("cache hit changed output:\n%q\n%q", got, again)
	}
	if size := publicCacheSizeForTest(); size != 1 {
		t.Fatalf("cache size after same input = %d, want 1", size)
	}

	otherSource, err := JS("if(a){c();}", options)
	if err != nil {
		t.Fatal(err)
	}
	if got == otherSource {
		t.Fatalf("different source reused cached output: %q", otherSource)
	}
	if size := publicCacheSizeForTest(); size != 2 {
		t.Fatalf("cache size after different source = %d, want 2", size)
	}

	options["indent_size"] = 2
	otherOptions, err := JS(source, options)
	if err != nil {
		t.Fatal(err)
	}
	if got == otherOptions || !strings.Contains(otherOptions, "\n  b();") {
		t.Fatalf("mutated options reused cached output:\nold=%q\nnew=%q", got, otherOptions)
	}
	if size := publicCacheSizeForTest(); size != 3 {
		t.Fatalf("cache size after different options = %d, want 3", size)
	}
}

func TestPublicCacheKeyStableForNestedTypedMaps(t *testing.T) {
	source := "if(a){b();}"
	a := publicCacheKeyFor("js", source, Options{
		"indent-size": 4,
		"js": map[string]string{
			"indent-size":      "2",
			"wrap-line-length": "80",
		},
	})
	b := publicCacheKeyFor("js", source, Options{
		"js": map[string]string{
			"wrap-line-length": "80",
			"indent-size":      "2",
		},
		"indent-size": 4,
	})
	if a != b {
		t.Fatalf("nested typed map cache keys differ:\n%#v\n%#v", a, b)
	}
}

func TestHTMLWithBeautifiersUsesEmbeddedDelegates(t *testing.T) {
	got, err := HTMLWithBeautifiers(`<script>if(a){b();}</script><style>a{color:red;}</style>`, Options{
		"indent_size": 2,
		"indent_char": " ",
	}, func(source string, opts Options) (string, error) {
		return source, nil
	}, CSS)
	if err != nil {
		t.Fatal(err)
	}
	want := "<script>\n  if(a){b();}\n</script>\n<style>\n  a {\n    color: red;\n  }\n</style>"
	if got != want {
		t.Fatalf("HTMLWithBeautifiers output:\n%q\nwant:\n%q", got, want)
	}
}

func TestHTMLWithBeautifiersDoesNotUsePublicHTMLCache(t *testing.T) {
	resetPublicCacheForTest()

	source := `<script>if(a){b();}</script><style>a{color:red;}</style>`
	options := Options{"indent_size": 2}
	standard, err := HTML(source, options)
	if err != nil {
		t.Fatal(err)
	}
	custom, err := HTMLWithBeautifiers(source, options, func(source string, opts Options) (string, error) {
		return "customJS();", nil
	}, func(source string, opts Options) (string, error) {
		return "/* custom css */", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom == standard || !strings.Contains(custom, "customJS();") || !strings.Contains(custom, "custom css") {
		t.Fatalf("HTMLWithBeautifiers reused public HTML cache:\nstandard=%q\ncustom=%q", standard, custom)
	}
	standardAgain, err := HTML(source, options)
	if err != nil {
		t.Fatal(err)
	}
	if standardAgain != standard {
		t.Fatalf("custom delegates polluted public HTML cache:\nstandard=%q\nagain=%q", standard, standardAgain)
	}
}

func TestDefaultOptionsMutationIsolation(t *testing.T) {
	js := DefaultJSOptions()
	js["indent_size"] = 2
	if got := DefaultJSOptions()["indent_size"]; got != 4 {
		t.Fatalf("DefaultJSOptions leaked map mutation: %v", got)
	}

	cssOptions := DefaultCSSOptions()
	cssOptions["templating"].([]string)[0] = "none"
	if got := DefaultCSSOptions()["templating"].([]string)[0]; got != "auto" {
		t.Fatalf("DefaultCSSOptions leaked slice mutation: %v", got)
	}

	htmlOptions := DefaultHTMLOptions()
	htmlOptions["content_unformatted"].([]string)[0] = "xmp"
	if got := DefaultHTMLOptions()["content_unformatted"].([]string)[0]; got != "pre" {
		t.Fatalf("DefaultHTMLOptions leaked slice mutation: %v", got)
	}
}

func BenchmarkPerfComparePublicJS(b *testing.B) {
	source := readBenchmarkFile(b, "underscore-min.js")
	options := Options{"wrap_line_length": 80}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, err := JS(source, options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = output
	}
}

func BenchmarkPerfComparePublicCSS(b *testing.B) {
	source := readBenchmarkFile(b, "github.css")
	options := Options{"wrap_line_length": 80}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, err := CSS(source, options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = output
	}
}

func BenchmarkPerfComparePublicHTML(b *testing.B) {
	source := readBenchmarkFile(b, "github.html")
	options := Options{"wrap_line_length": 80}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, err := HTML(source, options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = output
	}
}

func BenchmarkPerfComparePublicJSCacheHit(b *testing.B) {
	resetPublicCacheForTest()
	source := readBenchmarkFile(b, "underscore-min.js")
	options := Options{"wrap_line_length": 80}
	benchmarkPrime(b, func() (string, error) { return JS(source, options) })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, err := JS(source, options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = output
	}
}

func BenchmarkPerfComparePublicCSSCacheHit(b *testing.B) {
	resetPublicCacheForTest()
	source := readBenchmarkFile(b, "github.css")
	options := Options{"wrap_line_length": 80}
	benchmarkPrime(b, func() (string, error) { return CSS(source, options) })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, err := CSS(source, options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = output
	}
}

func BenchmarkPerfComparePublicHTMLCacheHit(b *testing.B) {
	resetPublicCacheForTest()
	source := readBenchmarkFile(b, "github.html")
	options := Options{"wrap_line_length": 80}
	benchmarkPrime(b, func() (string, error) { return HTML(source, options) })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, err := HTML(source, options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = output
	}
}

func BenchmarkPerfComparePublicJSCold(b *testing.B) {
	benchmarkCold(b, "underscore-min.js", "\n/* js cold ", " */", JS)
}

func BenchmarkPerfComparePublicCSSCold(b *testing.B) {
	benchmarkCold(b, "github.css", "\n/* css cold ", " */", CSS)
}

func BenchmarkPerfComparePublicHTMLCold(b *testing.B) {
	benchmarkCold(b, "github.html", "\n<!-- html cold ", " -->", HTML)
}

func readBenchmarkFile(b *testing.B, name string) string {
	b.Helper()
	data, err := os.ReadFile(filepath.Join("test", "resources", name))
	if err != nil {
		b.Fatal(err)
	}
	return string(data)
}

func benchmarkPrime(b *testing.B, fn func() (string, error)) {
	b.Helper()
	output, err := fn()
	if err != nil {
		b.Fatal(err)
	}
	benchmarkSink = output
}

func benchmarkCold(b *testing.B, name string, marker string, suffix string, fn func(string, Options) (string, error)) {
	b.Helper()
	resetPublicCacheForTest()
	base := readBenchmarkFile(b, name)
	sources := make([]string, publicCacheLimit*2)
	for i := range sources {
		sources[i] = base + marker + strconv.Itoa(i) + suffix
	}
	options := Options{"wrap_line_length": 80}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, err := fn(sources[i%len(sources)], options)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSink = output
	}
}

func resetPublicCacheForTest() {
	publicCache.Lock()
	defer publicCache.Unlock()
	publicCache.values = map[publicCacheKey]string{}
	publicCache.order = nil
}

func publicCacheSizeForTest() int {
	publicCache.Lock()
	defer publicCache.Unlock()
	return len(publicCache.values)
}
