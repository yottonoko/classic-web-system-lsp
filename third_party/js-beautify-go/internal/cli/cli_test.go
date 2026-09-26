package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunStdinStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", "-"}, "js-beautify", strings.NewReader("if(a){b();}"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "if (a) {\n    b();\n}" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunInfersTypeFromScriptName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"-"}, "css-beautify", strings.NewReader("a{color:red;}"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("css script exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "color: red") {
		t.Fatalf("css stdout = %q", got)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"-"}, "html-beautify", strings.NewReader("<div><p>x</p></div>"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("html script exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "\n    <p>x</p>") {
		t.Fatalf("html stdout = %q", got)
	}
}

func TestRunOutfileAndReplace(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "input.css")
	out := filepath.Join(dir, "out.css")
	if err := os.WriteFile(in, []byte(".tabs{color:red}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "css", "-o", out, in}, "css-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run exit = %d stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "color: red") {
		t.Fatalf("outfile content = %q", data)
	}
	code = Run([]string{"--type", "css", "-r", in}, "css-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("replace exit = %d stderr=%s", code, stderr.String())
	}
	data, _ = os.ReadFile(in)
	if !strings.Contains(string(data), "color: red") {
		t.Fatalf("replace content = %q", data)
	}
}

func TestRunMultipleFilesKeepsPerFileOptions(t *testing.T) {
	dir := t.TempDir()
	twoDir := filepath.Join(dir, "two")
	fourDir := filepath.Join(dir, "four")
	if err := os.MkdirAll(twoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fourDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(twoDir, ".editorconfig"), []byte("root = true\n[*]\nindent_style = space\nindent_size = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fourDir, ".editorconfig"), []byte("root = true\n[*]\nindent_style = space\nindent_size = 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	twoFile := filepath.Join(twoDir, "input.js")
	fourFile := filepath.Join(fourDir, "input.js")
	for _, file := range []string{twoFile, fourFile} {
		if err := os.WriteFile(file, []byte("if(a){b();}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", "--editorconfig", twoFile, fourFile}, "js-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("multi-file exit = %d stderr=%s", code, stderr.String())
	}
	twoData, err := os.ReadFile(twoFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(twoData); !strings.Contains(got, "\n  b();") || strings.Contains(got, "\n    b();") {
		t.Fatalf("two-space file content = %q", got)
	}
	fourData, err := os.ReadFile(fourFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(fourData); !strings.Contains(got, "\n    b();") {
		t.Fatalf("four-space file content = %q", got)
	}
}

func TestRunMultipleFilesReplaceFormatsAll(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "a.js"),
		filepath.Join(dir, "b.js"),
		filepath.Join(dir, "c.js"),
	}
	for _, file := range files {
		if err := os.WriteFile(file, []byte("if(a){b();}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", "--replace", files[0], files[1], files[2]}, "js-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("multi-replace exit = %d stderr=%s", code, stderr.String())
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); got != "if (a) {\n    b();\n}" {
			t.Fatalf("%s content = %q", file, got)
		}
	}
}

func TestRunMultipleFilesStatusOutputOrder(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "a.js"),
		filepath.Join(dir, "b.js"),
		filepath.Join(dir, "c.js"),
	}
	for _, file := range files {
		if err := os.WriteFile(file, []byte("if(a){b();}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", files[2], files[0], files[1]}, "js-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("multi-file exit = %d stderr=%s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	want := []string{
		"beautified " + rel(files[2]),
		"beautified " + rel(files[0]),
		"beautified " + rel(files[1]),
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stdout lines = %#v want %#v", lines, want)
	}
}

func TestRunMultipleFilesReturnsFirstInputOrderError(t *testing.T) {
	dir := t.TempDir()
	okFile := filepath.Join(dir, "ok.js")
	firstErr := filepath.Join(dir, "first-error")
	secondErr := filepath.Join(dir, "second-error")
	if err := os.WriteFile(okFile, []byte("if(a){b();}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(firstErr, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(secondErr, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", okFile, firstErr, secondErr}, "js-beautify", strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("multi-file error exit = %d stdout=%s", code, stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "first-error") || strings.Contains(got, "second-error") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunStdinWithFileStaysSequential(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "input.js")
	if err := os.WriteFile(file, []byte("if(c){d();}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", "-", file}, "js-beautify", strings.NewReader("if(a){b();}"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mixed stdin exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.HasPrefix(got, "if (a) {\n    b();\n}") || !strings.Contains(got, "beautified "+rel(file)) {
		t.Fatalf("stdout = %q", got)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "if (c) {\n    d();\n}" {
		t.Fatalf("file content = %q", got)
	}
}

func TestRunConfigAndEditorConfig(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)

	if err := os.WriteFile(filepath.Join(dir, ".jsbeautifyrc"), []byte(`{"indent_size":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".editorconfig"), []byte("root = true\n[*]\nindent_style = space\nindent_size = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input.js"), []byte("if(a){b();}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", "input.js"}, "js-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\n  b();") {
		t.Fatalf("config stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"--type", "js", "--editorconfig", "input.js"}, "js-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("editorconfig exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\n   b();") {
		t.Fatalf("editorconfig stdout = %q", stdout.String())
	}
}

func TestRunGlobReplacesMatches(t *testing.T) {
	dir := t.TempDir()
	files := []string{filepath.Join(dir, "a.js"), filepath.Join(dir, "b.js")}
	for _, file := range files {
		if err := os.WriteFile(file, []byte("if(a){b();}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", filepath.Join(dir, "*.js")}, "js-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("glob exit = %d stderr=%s", code, stderr.String())
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); !strings.Contains(got, "\n    b();") {
			t.Fatalf("%s content = %q", file, got)
		}
	}
}

func TestRunEOL(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", "--eol", `\r\n`, "-"}, "js-beautify", strings.NewReader("if(a){b();}"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("eol exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "\r\n    b();\r\n") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunInvalidOption(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", "--operator-position", "sideways", "-"}, "js-beautify", strings.NewReader("a+b"), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("invalid option exit = %d stdout=%s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "Invalid Option Value") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunUnchangedReplaceDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "input.js")
	formatted := "if (a) {\n    b();\n}"
	if err := os.WriteFile(file, []byte(formatted), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(file, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--type", "js", "--replace", file}, "js-beautify", strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("replace exit = %d stderr=%s", code, stderr.String())
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(oldTime) {
		t.Fatalf("modtime changed: got %s want %s", info.ModTime(), oldTime)
	}
	if !strings.Contains(stdout.String(), "unchanged") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func BenchmarkRunMultipleCSSFilesReplace(b *testing.B) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "test", "resources", "github.css"))
	if err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	files := make([]string, 32)
	args := []string{"--type", "css", "--quiet"}
	for i := range files {
		files[i] = filepath.Join(dir, fmt.Sprintf("github-%02d.css", i))
		args = append(args, files[i])
	}

	b.ReportAllocs()
	b.ResetTimer()
	for iter := 0; iter < b.N; iter++ {
		b.StopTimer()
		for i, file := range files {
			data := append([]byte{}, fixture...)
			data = fmt.Appendf(data, "\n/* benchmark iteration %d file %d */\n", iter, i)
			if err := os.WriteFile(file, data, 0o644); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
		code := Run(args, "css-beautify", strings.NewReader(""), io.Discard, io.Discard)
		if code != 0 {
			b.Fatalf("Run exit = %d", code)
		}
	}
}
