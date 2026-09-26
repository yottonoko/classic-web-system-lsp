package lspserver

import (
	"path/filepath"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func (s *Server) vbscriptDefinedInNote(uri string) string {
	source := s.sourceURIDocumentationLink(uri)
	if source == "" {
		return ""
	}
	if s.isJapanese() {
		return source + " で定義されています。"
	}
	return "Defined in " + source + "."
}

func (s *Server) appendVBScriptDefinedInHover(hover *lsp.Hover, uri string) *lsp.Hover {
	if hover == nil {
		return nil
	}
	note := s.vbscriptDefinedInNote(uri)
	if note == "" {
		return hover
	}
	content, ok := hover.Contents.(lsp.MarkupContent)
	if !ok {
		return hover
	}
	if strings.Contains(content.Value, note) {
		return hover
	}
	content.Value = strings.TrimSpace(content.Value) + "\n\n" + note
	hover.Contents = content
	return hover
}

func (s *Server) sourceURIDocumentationLink(uri string) string {
	baseURI, fragment, _ := strings.Cut(uri, "#")
	if baseURI == "" {
		baseURI = uri
	}
	if !strings.HasPrefix(strings.ToLower(baseURI), "file://") {
		return escapeMarkdownLinkText(uri)
	}
	fileName := sourceURIFileName(baseURI)
	if fileName == "" {
		return escapeMarkdownLinkText(uri)
	}
	label := fileName
	if s.rootPath != "" {
		if relative, err := filepath.Rel(filepath.Clean(s.rootPath), filepath.Clean(fileName)); err == nil && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ".." {
			label = relative
		} else if relative == "." {
			label = filepath.Base(fileName)
		}
	}
	label = filepath.ToSlash(label)
	displayFragment := ""
	linkFragment := ""
	if fragment != "" {
		displayFragment = "#" + fragment
		linkFragment = "#" + fragment
	}
	return "[" + escapeMarkdownLinkText(label+displayFragment) + "](" + filePathURI(fileName) + linkFragment + ")"
}

func sourceURIFileName(uri string) string {
	fileName := fileURIPath(uri)
	if fileName == "" {
		return ""
	}
	for _, suffix := range []string{".html.virtual", ".css.virtual", ".javascript.virtual", ".vbscript.virtual", ".jscript.virtual"} {
		if strings.HasSuffix(fileName, suffix) {
			return strings.TrimSuffix(fileName, suffix)
		}
	}
	return fileName
}

func escapeMarkdownLinkText(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, `[`, `\[`)
	text = strings.ReplaceAll(text, `]`, `\]`)
	return text
}
