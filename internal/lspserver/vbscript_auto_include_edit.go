package lspserver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) vbscriptAutoIncludeTextEdit(owner *core.ParsedDocument, targetURI string) (lsp.TextEdit, error) {
	if owner == nil || isStandaloneVBScriptDocument(owner) {
		return lsp.TextEdit{}, autoIncludeEditError("the owner is not a Classic ASP document")
	}
	ownerPath := fileURIPath(owner.URI)
	targetPath := fileURIPath(targetURI)
	if ownerPath == "" || targetPath == "" {
		return lsp.TextEdit{}, autoIncludeEditError("the owner and target must use file URIs")
	}
	ownerPath = filepath.Clean(ownerPath)
	targetPath = filepath.Clean(targetPath)
	ownerIdentity := workspacepkg.FileIdentityKeyFromFileName(ownerPath)
	targetIdentity := workspacepkg.FileIdentityKeyFromFileName(targetPath)
	if ownerIdentity != workspacepkg.FileIdentityKeyFromURI(owner.URI) ||
		targetIdentity != workspacepkg.FileIdentityKeyFromURI(targetURI) {
		return lsp.TextEdit{}, autoIncludeEditError("a file URI did not round-trip to the same file identity")
	}
	if ownerIdentity == targetIdentity {
		return lsp.TextEdit{}, autoIncludeEditError("a document cannot include itself")
	}
	targetInfo, ok := s.fsStat(targetPath)
	if !ok || targetInfo == nil || !targetInfo.File {
		return lsp.TextEdit{}, autoIncludeEditError("the target is not a resolvable file")
	}

	includePath := s.includePathForRenamedTarget(owner.URI, "file", targetPath)
	if includePath == "" || filepath.IsAbs(filepath.FromSlash(includePath)) {
		return lsp.TextEdit{}, autoIncludeEditError("the target has no owner-relative include path")
	}
	quotedPath, ok := quoteAutoIncludePath(includePath)
	if !ok {
		return lsp.TextEdit{}, autoIncludeEditError("the include path cannot be quoted safely")
	}
	resolved, ok := s.includeTargetDetailsForMode(owner.URI, includePath, "file")
	if !ok || !resolved.Exists ||
		workspacepkg.FileIdentityKeyFromFileName(resolved.Path) != targetIdentity {
		return lsp.TextEdit{}, autoIncludeEditError("the generated include path does not resolve back to the target")
	}
	resolvedInfo, ok := s.fsStat(resolved.Path)
	if !ok || resolvedInfo == nil || !resolvedInfo.File {
		return lsp.TextEdit{}, autoIncludeEditError("the generated include path does not resolve to a file")
	}

	if s.includeIdentityReachable(owner, targetIdentity) {
		return lsp.TextEdit{}, autoIncludeEditError("the target is already included directly or transitively")
	}
	target := s.parsedIncludeFile(targetPath)
	if target == nil {
		return lsp.TextEdit{}, autoIncludeEditError("the target could not be read")
	}
	if s.includeIdentityReachable(target, ownerIdentity) {
		return lsp.TextEdit{}, autoIncludeEditError("the new include would create a cycle")
	}

	offset, prefix, suffix := autoIncludeInsertion(owner)
	document := core.NewTextDocument(owner.URI, "classic-asp", 0, owner.Text)
	position := document.PositionAt(offset)
	return lsp.TextEdit{
		Range:   lsp.Range{Start: position, End: position},
		NewText: prefix + "<!-- #include file=" + quotedPath + " -->" + suffix,
	}, nil
}

func (s *Server) includeIdentityReachable(start *core.ParsedDocument, targetIdentity string) bool {
	if start == nil || targetIdentity == "" {
		return false
	}
	queue := []*core.ParsedDocument{start}
	visited := map[string]struct{}{}
	if startPath := fileURIPath(start.URI); startPath != "" {
		visited[workspacepkg.FileIdentityKeyFromFileName(startPath)] = struct{}{}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, include := range current.Includes {
			details, ok := s.includeTargetDetailsForMode(current.URI, include.Path, include.Mode)
			if !ok || !details.Exists || details.Path == "" {
				continue
			}
			identity := workspacepkg.FileIdentityKeyFromFileName(details.Path)
			if identity == targetIdentity {
				return true
			}
			if _, seen := visited[identity]; seen {
				continue
			}
			visited[identity] = struct{}{}
			if included := s.parsedIncludeFile(details.Path); included != nil {
				queue = append(queue, included)
			}
		}
	}
	return false
}

func quoteAutoIncludePath(path string) (string, bool) {
	if path == "" || strings.ContainsAny(path, "\x00\r\n") || strings.Contains(path, "-->") {
		return "", false
	}
	if strings.Contains(path, `"`) {
		if strings.Contains(path, "'") {
			return "", false
		}
		return "'" + path + "'", true
	}
	return `"` + path + `"`, true
}

func autoIncludeInsertion(owner *core.ParsedDocument) (offset int, prefix, suffix string) {
	text := owner.Text
	cursor := utf8BOMEnd(text)
	insertion := cursor
	hasPrologue := false

	for {
		start := skipAutoIncludeWhitespace(text, cursor)
		region := autoIncludeRegionStartingAt(owner.Regions, start)
		if region == nil || region.Kind != core.RegionASPDirective {
			break
		}
		cursor = region.End
		insertion = cursor
		hasPrologue = true
	}

	document := core.NewTextDocument(owner.URI, "classic-asp", 0, text)
	for _, include := range owner.Includes {
		pathStart := document.OffsetAt(include.Range.Start)
		pathEnd := document.OffsetAt(include.Range.End)
		if pathStart < cursor || pathEnd < pathStart || pathEnd > len(text) {
			continue
		}
		commentStart := strings.LastIndex(text[cursor:pathStart], "<!--")
		if commentStart < 0 {
			break
		}
		commentStart += cursor
		if skipAutoIncludeWhitespace(text, cursor) != commentStart {
			break
		}
		relativeEnd := strings.Index(text[pathEnd:], "-->")
		if relativeEnd < 0 {
			break
		}
		cursor = pathEnd + relativeEnd + len("-->")
		insertion = cursor
		hasPrologue = true
	}

	lineEnding := autoIncludeLineEnding(text)
	if !hasPrologue {
		if insertion < len(text) {
			suffix = lineEnding
		}
		return insertion, "", suffix
	}
	prefix = lineEnding
	if autoIncludeNeedsTrailingLineEnding(text[insertion:]) {
		suffix = lineEnding
	}
	return insertion, prefix, suffix
}

func autoIncludeRegionStartingAt(regions []core.Region, offset int) *core.Region {
	for index := range regions {
		region := &regions[index]
		if region.End <= offset {
			continue
		}
		if region.Start == offset {
			return region
		}
		return nil
	}
	return nil
}

func utf8BOMEnd(text string) int {
	if strings.HasPrefix(text, "\uFEFF") {
		return len("\uFEFF")
	}
	return 0
}

func skipAutoIncludeWhitespace(text string, offset int) int {
	for offset < len(text) {
		switch text[offset] {
		case ' ', '\t', '\r', '\n', '\f':
			offset++
		default:
			return offset
		}
	}
	return offset
}

func autoIncludeLineEnding(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 && index > 0 && text[index-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func autoIncludeNeedsTrailingLineEnding(text string) bool {
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '\r', '\n':
			return false
		case ' ', '\t', '\f':
			continue
		default:
			return true
		}
	}
	return false
}

func autoIncludeEditError(reason string) error {
	return fmt.Errorf("cannot create Classic ASP auto-include edit: %s", reason)
}
