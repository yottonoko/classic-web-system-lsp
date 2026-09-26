package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

type spec struct {
	Language string
	Package  string
	TestName string
	Call     string
	Visible  string
	Imports  []string
}

func main() {
	specs := []spec{
		{
			Language: "css",
			Package:  "css",
			TestName: "TestLegacyCSSCases",
			Call:     "Beautify(tc.Input, tc.Options)",
			Visible:  "visible",
			Imports:  []string{"github.com/yottonoko/js-beautify-go/internal/legacycases"},
		},
		{
			Language: "javascript",
			Package:  "javascript",
			TestName: "TestLegacyJavaScriptCases",
			Call:     "Beautify(tc.Input, tc.Options)",
			Visible:  "visible",
			Imports:  []string{"github.com/yottonoko/js-beautify-go/internal/legacycases"},
		},
		{
			Language: "html",
			Package:  "html",
			TestName: "TestLegacyHTMLCases",
			Call:     "Beautify(tc.Input, tc.Options, javascript.Beautify, css.Beautify)",
			Visible:  "htmlVisible",
			Imports: []string{
				"github.com/yottonoko/js-beautify-go/internal/css",
				"github.com/yottonoko/js-beautify-go/internal/javascript",
				"github.com/yottonoko/js-beautify-go/internal/legacycases",
			},
		},
	}
	for _, item := range specs {
		if err := writeSpec(item); err != nil {
			fmt.Fprintf(os.Stderr, "legacytestgen: %v\n", err)
			os.Exit(1)
		}
	}
}

func writeSpec(item spec) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "package %s\n\n", item.Package)
	fmt.Fprintln(&buf, "import (")
	fmt.Fprintln(&buf, "\t\"os\"")
	fmt.Fprintln(&buf, "\t\"strings\"")
	fmt.Fprintln(&buf, "\t\"testing\"")
	fmt.Fprintln(&buf)
	for _, imp := range item.Imports {
		fmt.Fprintf(&buf, "\t%q\n", imp)
	}
	fmt.Fprintln(&buf, ")")
	fmt.Fprintln(&buf)
	fmt.Fprintf(&buf, "func %s(t *testing.T) {\n", item.TestName)
	fmt.Fprintln(&buf, "\tif os.Getenv(\"JS_BEAUTIFY_GO_LEGACY\") == \"\" {")
	fmt.Fprintf(&buf, "\t\tt.Skip(\"set JS_BEAUTIFY_GO_LEGACY=1 to run the imported js-beautify %s parity corpus\")\n", item.Language)
	fmt.Fprintln(&buf, "\t}")
	fmt.Fprintf(&buf, "\tcases, err := legacycases.Load(%q)\n", item.Language)
	fmt.Fprintln(&buf, "\tif err != nil {")
	fmt.Fprintln(&buf, "\t\tt.Fatal(err)")
	fmt.Fprintln(&buf, "\t}")
	fmt.Fprintln(&buf, "\tfailures := 0")
	fmt.Fprintln(&buf, "\tfor _, tc := range cases {")
	fmt.Fprintf(&buf, "\t\tgot, err := %s\n", item.Call)
	fmt.Fprintln(&buf, "\t\tif err != nil {")
	buf.WriteString("\t\t\tt.Fatalf(\"%s: %v\", tc.Name, err)\n")
	fmt.Fprintln(&buf, "\t\t}")
	fmt.Fprintln(&buf, "\t\tif got != tc.Expected {")
	fmt.Fprintln(&buf, "\t\t\tfailures++")
	fmt.Fprintln(&buf, "\t\t\tif failures <= 10 {")
	fmt.Fprintf(&buf, "\t\t\t\tt.Errorf(\"%%s:\\ninput:\\n%%s\\nwant:\\n%%s\\ngot:\\n%%s\", tc.Name, %s(tc.Input), %s(tc.Expected), %s(got))\n", item.Visible, item.Visible, item.Visible)
	fmt.Fprintln(&buf, "\t\t\t}")
	fmt.Fprintln(&buf, "\t\t}")
	fmt.Fprintln(&buf, "\t}")
	fmt.Fprintln(&buf, "\tif failures > 0 {")
	fmt.Fprintf(&buf, "\t\tt.Fatalf(\"%%d/%%d legacy %s cases failed\", failures, len(cases))\n", item.Language)
	fmt.Fprintln(&buf, "\t}")
	fmt.Fprintln(&buf, "}")
	fmt.Fprintln(&buf)
	fmt.Fprintf(&buf, "func %s(value string) string {\n", item.Visible)
	fmt.Fprintln(&buf, "\treturn strings.ReplaceAll(strings.ReplaceAll(value, \"\\t\", `\\t`), \"\\n\", `\\n`+\"\\n\")")
	fmt.Fprintln(&buf, "}")
	path := filepath.Join("internal", item.Language, "legacy_generated_test.go")
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
