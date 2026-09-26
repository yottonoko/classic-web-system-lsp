package lspserver

import (
	"encoding/json"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func (r *javaScriptRequest) virtualPosition(position lsp.Position) lsp.Position {
	value, _ := r.active.virtual.ToVirtualPosition(position, r.active.source)
	return value
}

func javaScriptVirtualPath(uri string, language core.EmbeddedLanguage) string {
	path := fileURIPath(uri)
	if path == "" {
		path = filepath.Join("/", "__asp_lsp", strings.NewReplacer(":", "_", "/", "_").Replace(uri))
	}
	suffix := ".__asp_client.js"
	if language == core.LanguageJScript {
		suffix = ".__asp_server.js"
	}
	return javaScriptProjectPath(path + suffix)
}

func javaScriptProjectPath(path string) string {
	slashPath := filepath.ToSlash(path)
	if strings.HasPrefix(slashPath, "//") {
		cleaned := pathpkg.Clean("/" + strings.TrimLeft(slashPath, "/"))
		return "//" + strings.TrimPrefix(cleaned, "/")
	}
	if len(slashPath) >= 3 && slashPath[0] == '/' && slashPath[2] == ':' {
		slashPath = slashPath[1:]
	}
	if len(slashPath) >= 2 && slashPath[1] == ':' {
		slashPath = pathpkg.Clean(slashPath)
		return strings.ToLower(slashPath[:1]) + slashPath[1:]
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.ToSlash(path)
	if len(path) >= 2 && path[1] == ':' {
		path = strings.ToLower(path[:1]) + path[1:]
	}
	return path
}

type javaScriptFileURI struct{ path string }

func (u *javaScriptFileURI) String() string {
	uri := absoluteFilePathURI(u.path)
	path := filepath.ToSlash(u.path)
	if len(path) >= 2 && path[1] == ':' {
		prefix := "file:///" + path[:1] + ":"
		if strings.HasPrefix(uri, prefix) {
			return "file:///" + path[:1] + "%3A" + uri[len(prefix):]
		}
	}
	return uri
}

func remapJavaScriptServiceValue(value any, active *javaScriptVirtualFile, files map[string]*javaScriptVirtualFile) any {
	return remapJavaScriptServiceValueForFile(value, active, files)
}

func isJavaScriptDiagnosticMethod(method string) bool {
	switch method {
	case "textDocument/diagnostic", "textDocument/syntacticDiagnostic", "textDocument/semanticDiagnostic", "textDocument/suggestionDiagnostic":
		return true
	default:
		return false
	}
}

func remapJavaScriptDiagnosticResponse(raw []byte, target any, mapping *javaScriptVirtualFile) bool {
	var report javaScriptDiagnosticReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return false
	}
	filtered := report.Items[:0]
	for _, diagnostic := range report.Items {
		rangeInSource, ok := mapping.virtual.SourceRangeForVirtualRange(mapping.source, diagnostic.Range)
		if !ok {
			continue
		}
		diagnostic.Range = rangeInSource
		filtered = append(filtered, diagnostic)
	}
	report.Items = filtered
	if typed, ok := target.(*javaScriptDiagnosticReport); ok {
		*typed = report
		return true
	}
	remapped, err := json.Marshal(report)
	return err == nil && json.Unmarshal(remapped, target) == nil
}

func remapJavaScriptServiceValueForFile(value any, current *javaScriptVirtualFile, files map[string]*javaScriptVirtualFile) any {
	switch typed := value.(type) {
	case []any:
		for index := range typed {
			typed[index] = remapJavaScriptServiceValueForFile(typed[index], current, files)
		}
		return typed
	case map[string]any:
		if textDocument, ok := typed["textDocument"].(map[string]any); ok {
			if uri, ok := textDocument["uri"].(string); ok {
				current = javaScriptVirtualFileForURI(files, uri)
			}
		}
		if fromRanges, ok := typed["fromRanges"].([]any); ok {
			rangeMapping := current
			if from, ok := typed["from"].(map[string]any); ok {
				if uri, ok := from["uri"].(string); ok {
					rangeMapping = javaScriptVirtualFileForURI(files, uri)
				}
			}
			typed["fromRanges"] = remapJavaScriptRangeList(fromRanges, rangeMapping)
		}
		if uri, ok := typed["uri"].(string); ok {
			if mapped := javaScriptVirtualFileForURI(files, uri); mapped != nil {
				current = mapped
				typed["uri"] = mapped.owner
			} else {
				current = nil
			}
		}
		if changes, ok := typed["changes"].(map[string]any); ok {
			mappedChanges := map[string]any{}
			uris := make([]string, 0, len(changes))
			for uri := range changes {
				uris = append(uris, uri)
			}
			slices.Sort(uris)
			for _, uri := range uris {
				edits := changes[uri]
				mapping := javaScriptVirtualFileForURI(files, uri)
				targetURI := uri
				if mapping != nil {
					targetURI = mapping.owner
				}
				remappedEdits := remapJavaScriptServiceValueForFile(edits, mapping, files)
				if previous, exists := mappedChanges[targetURI]; exists {
					previousItems, previousOK := previous.([]any)
					remappedItems, remappedOK := remappedEdits.([]any)
					if previousOK && remappedOK {
						mappedChanges[targetURI] = append(previousItems, remappedItems...)
						continue
					}
				}
				mappedChanges[targetURI] = remappedEdits
			}
			typed["changes"] = mappedChanges
		}
		if current != nil {
			remapJavaScriptFoldingRange(typed, current)
		}
		for key, child := range typed {
			if key == "changes" || key == "fromRanges" {
				continue
			}
			if key == "position" {
				if current != nil {
					typed[key] = remapJavaScriptPosition(child, current)
				}
				continue
			}
			if key == "range" || key == "selectionRange" || key == "targetRange" || key == "targetSelectionRange" || key == "originSelectionRange" {
				if current != nil {
					typed[key] = remapJavaScriptRange(child, current)
				}
				continue
			}
			typed[key] = remapJavaScriptServiceValueForFile(child, current, files)
		}
		return typed
	default:
		return value
	}
}

func javaScriptVirtualFileForURI(files map[string]*javaScriptVirtualFile, uri string) *javaScriptVirtualFile {
	if mapping := files[uri]; mapping != nil {
		return mapping
	}
	path := fileURIPath(uri)
	if path == "" {
		return nil
	}
	return files[absoluteFilePathURI(path)]
}

func remapJavaScriptPosition(value any, mapping *javaScriptVirtualFile) any {
	position, ok := javaScriptPositionFromJSON(value)
	if !ok {
		return value
	}
	source, ok := sourcePositionForVirtualBoundary(mapping, position, false)
	if !ok {
		return value
	}
	return source
}

func remapJavaScriptRangeList(values []any, mapping *javaScriptVirtualFile) []any {
	if mapping == nil {
		return values
	}
	for index := range values {
		values[index] = remapJavaScriptRange(values[index], mapping)
	}
	return values
}

func remapJavaScriptFoldingRange(value map[string]any, mapping *javaScriptVirtualFile) {
	startLine, startOK := value["startLine"].(float64)
	endLine, endOK := value["endLine"].(float64)
	if !startOK || !endOK {
		return
	}
	startCharacter, hasStartCharacter := value["startCharacter"].(float64)
	endCharacter, hasEndCharacter := value["endCharacter"].(float64)
	start, startOK := sourcePositionForVirtualBoundary(mapping, lsp.Position{Line: int(startLine), Character: int(startCharacter)}, false)
	end, endOK := sourcePositionForVirtualBoundary(mapping, lsp.Position{Line: int(endLine), Character: int(endCharacter)}, true)
	if !startOK || !endOK {
		return
	}
	value["startLine"] = float64(start.Line)
	value["endLine"] = float64(end.Line)
	if hasStartCharacter {
		value["startCharacter"] = float64(start.Character)
	}
	if hasEndCharacter {
		value["endCharacter"] = float64(end.Character)
	}
}

func remapJavaScriptRange(value any, mapping *javaScriptVirtualFile) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	start, startOK := javaScriptPositionFromJSON(object["start"])
	end, endOK := javaScriptPositionFromJSON(object["end"])
	if !startOK || !endOK {
		return value
	}
	sourceStart, startOK := sourcePositionForVirtualBoundary(mapping, start, false)
	sourceEnd, endOK := sourcePositionForVirtualBoundary(mapping, end, true)
	if !startOK || !endOK {
		return value
	}
	return map[string]any{"start": sourceStart, "end": sourceEnd}
}

func sourcePositionForVirtualBoundary(mapping *javaScriptVirtualFile, position lsp.Position, end bool) (lsp.Position, bool) {
	virtualDocument := mapping.virtualDocument
	if virtualDocument == nil {
		virtualDocument = core.NewTextDocument(mapping.uri, mapping.virtual.LanguageID, 0, mapping.virtual.Text)
	}
	if sourceOffset, ok := mapping.virtual.ToSourceOffset(virtualDocument.OffsetAt(position)); ok {
		return mapping.source.PositionAt(sourceOffset), true
	}
	if len(mapping.virtual.Segments) == 0 {
		return lsp.Position{}, false
	}
	offset := virtualDocument.OffsetAt(position)
	segments := mapping.virtual.Segments
	if offset <= segments[0].VirtualStart {
		return mapping.source.PositionAt(segments[0].SourceStart), true
	}
	last := segments[len(segments)-1]
	if offset >= last.VirtualEnd {
		return mapping.source.PositionAt(last.SourceEnd), true
	}
	for index := 1; index < len(segments); index++ {
		if offset < segments[index].VirtualStart {
			if end {
				return mapping.source.PositionAt(segments[index].SourceStart), true
			}
			return mapping.source.PositionAt(segments[index-1].SourceEnd), true
		}
	}
	return lsp.Position{}, false
}

func javaScriptPositionFromJSON(value any) (lsp.Position, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return lsp.Position{}, false
	}
	line, lineOK := object["line"].(float64)
	character, characterOK := object["character"].(float64)
	return lsp.Position{Line: int(line), Character: int(character)}, lineOK && characterOK
}

func relocateJavaScriptCompletionImportEdits(value any, mapping *javaScriptVirtualFile, completionOffset int) {
	object, ok := value.(map[string]any)
	if !ok || mapping == nil || len(mapping.virtual.Segments) < 2 {
		return
	}
	var activeSegment *core.SourceMapSegment
	for index := range mapping.virtual.Segments {
		segment := &mapping.virtual.Segments[index]
		if completionOffset >= segment.VirtualStart && completionOffset <= segment.VirtualEnd {
			activeSegment = segment
			break
		}
	}
	if activeSegment == nil || activeSegment == &mapping.virtual.Segments[0] {
		return
	}
	edits, ok := object["additionalTextEdits"].([]any)
	if !ok {
		return
	}
	virtualDocument := mapping.virtualDocument
	if virtualDocument == nil {
		virtualDocument = core.NewTextDocument(mapping.uri, mapping.virtual.LanguageID, 0, mapping.virtual.Text)
	}
	position := virtualDocument.PositionAt(activeSegment.VirtualStart)
	positionValue := map[string]any{"line": float64(position.Line), "character": float64(position.Character)}
	for _, editValue := range edits {
		edit, ok := editValue.(map[string]any)
		if !ok {
			continue
		}
		rangeValue, ok := edit["range"].(map[string]any)
		if !ok {
			continue
		}
		start, startOK := javaScriptPositionFromJSON(rangeValue["start"])
		end, endOK := javaScriptPositionFromJSON(rangeValue["end"])
		if !startOK || !endOK || start != (lsp.Position{}) || end != (lsp.Position{}) {
			continue
		}
		edit["range"] = map[string]any{"start": positionValue, "end": positionValue}
	}
}

func toVirtualJavaScriptServiceValue(value any, mapping *javaScriptVirtualFile) (any, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var generic any
	if json.Unmarshal(raw, &generic) != nil {
		return nil, false
	}
	var walk func(any) any
	walk = func(node any) any {
		switch typed := node.(type) {
		case []any:
			for index := range typed {
				typed[index] = walk(typed[index])
			}
		case map[string]any:
			for key, child := range typed {
				if key == "range" {
					if object, ok := child.(map[string]any); ok {
						start, startOK := javaScriptPositionFromJSON(object["start"])
						end, endOK := javaScriptPositionFromJSON(object["end"])
						virtualStart, virtualStartOK := mapping.virtual.ToVirtualPosition(start, mapping.source)
						virtualEnd, virtualEndOK := mapping.virtual.ToVirtualPosition(end, mapping.source)
						if startOK && endOK && virtualStartOK && virtualEndOK {
							typed[key] = map[string]any{"start": virtualStart, "end": virtualEnd}
						}
					}
					continue
				}
				typed[key] = walk(child)
			}
		}
		return node
	}
	return walk(generic), true
}

func firstJavaScriptPosition(doc *core.TextDocument, parsed *core.ParsedDocument) (lsp.Position, bool) {
	if doc == nil || parsed == nil {
		return lsp.Position{}, false
	}
	for _, region := range parsed.Regions {
		if region.Language == core.LanguageJavaScript || region.Language == core.LanguageJScript {
			return doc.PositionAt(region.ContentStart), true
		}
	}
	return lsp.Position{}, false
}

func firstJavaScriptPositionForLanguage(doc *core.TextDocument, parsed *core.ParsedDocument, language core.EmbeddedLanguage) (lsp.Position, bool) {
	if doc == nil || parsed == nil {
		return lsp.Position{}, false
	}
	for _, region := range parsed.Regions {
		if region.Language == language {
			return doc.PositionAt(region.ContentStart), true
		}
	}
	return lsp.Position{}, false
}
