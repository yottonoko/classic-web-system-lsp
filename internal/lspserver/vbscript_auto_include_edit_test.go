package lspserver

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptAutoIncludeTextEditPreservesPrologueBOMAndLineEndings(t *testing.T) {
	root := t.TempDir()
	existingPath := filepath.Join(root, "existing.inc")
	targetPath := filepath.Join(root, "includes", "shared.inc")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		existingPath: `<% Const ExistingValue = 1 %>`,
		targetPath:   `<% Const SharedValue = 1 %>`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ownerPath := filepath.Join(root, "default.asp")
	source := "\uFEFF  <%@ Language=\"VBScript\" %>\r\n" +
		"<%@ CodePage=65001 %>\r\n" +
		"<!-- #include file=\"existing.inc\" -->\r\n" +
		"<main>\r\n<!-- #include file=\"late.inc\" -->"
	if err := os.WriteFile(ownerPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	server := New(nil, io.Discard, nil)
	owner := core.ParseDocument(filePathURI(ownerPath), source, core.Settings{})
	edit, err := server.vbscriptAutoIncludeTextEdit(owner, filePathURI(targetPath))
	if err != nil {
		t.Fatal(err)
	}
	got := applyAutoIncludeTextEdit(t, source, edit)
	want := "\uFEFF  <%@ Language=\"VBScript\" %>\r\n" +
		"<%@ CodePage=65001 %>\r\n" +
		"<!-- #include file=\"existing.inc\" -->\r\n" +
		"<!-- #include file=\"includes/shared.inc\" -->\r\n" +
		"<main>\r\n<!-- #include file=\"late.inc\" -->"
	if got != want {
		t.Fatalf("edited source = %q, want %q", got, want)
	}
}

func TestVBScriptAutoIncludeTextEditHandlesNoFinalNewline(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "body without prologue",
			source: "<% Response.Write SharedValue %>",
			want:   "<!-- #include file=\"shared.inc\" -->\n<% Response.Write SharedValue %>",
		},
		{
			name:   "page directive without final newline",
			source: "<%@ Language=\"VBScript\" %>",
			want:   "<%@ Language=\"VBScript\" %>\n<!-- #include file=\"shared.inc\" -->",
		},
		{
			name:   "include prologue without final newline",
			source: "<!-- #include file=\"existing.inc\" -->",
			want:   "<!-- #include file=\"existing.inc\" -->\n<!-- #include file=\"shared.inc\" -->",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			targetPath := filepath.Join(root, "shared.inc")
			if err := os.WriteFile(targetPath, []byte(`<% Const SharedValue = 1 %>`), 0o644); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(test.source, "existing.inc") {
				if err := os.WriteFile(filepath.Join(root, "existing.inc"), []byte(`<% Const ExistingValue = 1 %>`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			ownerPath := filepath.Join(root, "default.asp")
			if err := os.WriteFile(ownerPath, []byte(test.source), 0o644); err != nil {
				t.Fatal(err)
			}
			server := New(nil, io.Discard, nil)
			owner := core.ParseDocument(filePathURI(ownerPath), test.source, core.Settings{})
			edit, err := server.vbscriptAutoIncludeTextEdit(owner, filePathURI(targetPath))
			if err != nil {
				t.Fatal(err)
			}
			if got := applyAutoIncludeTextEdit(t, test.source, edit); got != test.want {
				t.Fatalf("edited source = %q, want %q", got, test.want)
			}
		})
	}
}

func TestQuoteAutoIncludePath(t *testing.T) {
	tests := []struct {
		path string
		want string
		ok   bool
	}{
		{path: "includes/shared.inc", want: `"includes/shared.inc"`, ok: true},
		{path: `includes/quo"te.inc`, want: `'includes/quo"te.inc'`, ok: true},
		{path: `includes/single'quote.inc`, want: `"includes/single'quote.inc"`, ok: true},
		{path: `includes/both"'.inc`, ok: false},
		{path: "includes/line\nbreak.inc", ok: false},
		{path: "includes/close-->comment.inc", ok: false},
	}
	for _, test := range tests {
		got, ok := quoteAutoIncludePath(test.path)
		if got != test.want || ok != test.ok {
			t.Errorf("quoteAutoIncludePath(%q) = %q, %v, want %q, %v", test.path, got, ok, test.want, test.ok)
		}
	}
}

func TestVBScriptAutoIncludeTextEditRejectsUnsafeTargets(t *testing.T) {
	tests := []struct {
		name        string
		ownerSource string
		targetName  string
		targetText  string
		targetDir   bool
		targetURI   func(root, targetPath string) string
		extraFiles  map[string]string
	}{
		{
			name:       "non-file URI",
			targetName: "shared.inc",
			targetText: `<% Const SharedValue = 1 %>`,
			targetURI: func(_, _ string) string {
				return "untitled:shared.inc"
			},
		},
		{
			name:       "self include",
			targetName: "default.asp",
			targetText: "<% Response.Write 1 %>",
		},
		{
			name:        "already directly included",
			ownerSource: `<!-- #include file="shared.inc" -->`,
			targetName:  "shared.inc",
			targetText:  `<% Const SharedValue = 1 %>`,
		},
		{
			name:        "already transitively included",
			ownerSource: `<!-- #include file="middle.inc" -->`,
			targetName:  "shared.inc",
			targetText:  `<% Const SharedValue = 1 %>`,
			extraFiles: map[string]string{
				"middle.inc": `<!-- #include file="shared.inc" -->`,
			},
		},
		{
			name:       "would form cycle",
			targetName: "shared.inc",
			targetText: `<!-- #include file="default.asp" -->`,
		},
		{
			name:       "directory target",
			targetName: "includes",
			targetDir:  true,
		},
		{
			name:       "missing target",
			targetName: "missing.inc",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := test.ownerSource
			if source == "" {
				source = "<% Response.Write SharedValue %>"
			}
			ownerPath := filepath.Join(root, "default.asp")
			if err := os.WriteFile(ownerPath, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			targetPath := filepath.Join(root, test.targetName)
			if test.targetDir {
				if err := os.Mkdir(targetPath, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if test.targetText != "" && targetPath != ownerPath {
				if err := os.WriteFile(targetPath, []byte(test.targetText), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range test.extraFiles {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			targetURI := filePathURI(targetPath)
			if test.targetURI != nil {
				targetURI = test.targetURI(root, targetPath)
			}
			server := New(nil, io.Discard, nil)
			owner := core.ParseDocument(filePathURI(ownerPath), source, core.Settings{})
			if edit, err := server.vbscriptAutoIncludeTextEdit(owner, targetURI); err == nil {
				t.Fatalf("vbscriptAutoIncludeTextEdit() = %#v, want rejection", edit)
			}
		})
	}
}

func TestVBScriptAutoIncludeLocalizesUserFacingText(t *testing.T) {
	server := New(nil, io.Discard, nil)
	server.settings.Locale = "ja"
	if got, want := server.autoIncludeCompletionDetail("includes/helpers.inc"), "includes/helpers.inc から自動 include"; got != want {
		t.Fatalf("Japanese completion detail = %q, want %q", got, want)
	}
	if got, want := server.autoIncludeCodeActionTitle("SharedHelper", "includes/helpers.inc"), "SharedHelper のために includes/helpers.inc を include"; got != want {
		t.Fatalf("Japanese code-action title = %q, want %q", got, want)
	}
	server.settings.Locale = "en"
	if got, want := server.autoIncludeCompletionDetail("includes/helpers.inc"), "Auto include from includes/helpers.inc"; got != want {
		t.Fatalf("English completion detail = %q, want %q", got, want)
	}
	if got, want := server.autoIncludeCodeActionTitle("SharedHelper", "includes/helpers.inc"), "Include includes/helpers.inc for SharedHelper"; got != want {
		t.Fatalf("English code-action title = %q, want %q", got, want)
	}
}

func applyAutoIncludeTextEdit(t *testing.T, source string, edit lsp.TextEdit) string {
	t.Helper()
	document := core.NewTextDocument("file:///default.asp", "classic-asp", 0, source)
	start := document.OffsetAt(edit.Range.Start)
	end := document.OffsetAt(edit.Range.End)
	return source[:start] + edit.NewText + source[end:]
}
