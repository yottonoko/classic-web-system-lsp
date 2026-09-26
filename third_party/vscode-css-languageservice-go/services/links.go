package services

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

// DocumentResolver resolves document references for links.
type DocumentResolver interface {
	ResolveReference(ref, baseURL string) (string, bool)
}

// DocumentContentResolver resolves linked document contents.
type DocumentContentResolver interface {
	ReadFile(ctx context.Context, uri string) (string, error)
}

type linkKind int

const (
	linkImport linkKind = iota
	linkURL
	linkSassModule
)

type linkCandidate struct {
	raw        string
	rangeStart int
	rangeEnd   int
	kind       linkKind
}

func FindDocumentLinks(ctx context.Context, document *lsp.TextDocument, resolver DocumentResolver) ([]lsp.DocumentLink, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	candidates := collectLinkCandidates(document.Text())
	links := make([]lsp.DocumentLink, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target := strings.Trim(candidate.raw, `"'`)
		if target == "" || strings.HasPrefix(strings.ToLower(target), "data:") {
			continue
		}
		if strings.EqualFold(document.LanguageID, "scss") {
			if candidate.kind != linkURL && strings.HasPrefix(strings.ToLower(target), "sass:") {
				continue
			}
			if (!hasURIProtocol(target) || strings.HasPrefix(target, "pkg:")) && resolver != nil {
				if resolved, ok := resolveSCSSReference(ctx, target, string(document.URI), resolver); ok {
					target = resolved
				} else {
					continue
				}
			}
			links = append(links, lsp.DocumentLink{
				Range:  rangeFromOffsets(document, candidate.rangeStart, candidate.rangeEnd),
				Target: lsp.DocumentURI(target),
			})
			continue
		}
		if !hasURIProtocol(target) && resolver != nil {
			if resolved, ok := resolver.ResolveReference(target, string(document.URI)); ok {
				target = resolved
			}
		}
		links = append(links, lsp.DocumentLink{
			Range:  rangeFromOffsets(document, candidate.rangeStart, candidate.rangeEnd),
			Target: lsp.DocumentURI(target),
		})
	}
	return links, nil
}

func collectLinkCandidates(text string) []linkCandidate {
	var candidates []linkCandidate
	lower := strings.ToLower(text)
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if strings.HasPrefix(lower[i:], "@import") {
			j := i + len("@import")
			for j < len(text) && isCSSSpace(text[j]) {
				j++
			}
			if j < len(text) && (text[j] == '\'' || text[j] == '"') {
				if end := quotedEnd(text, j); end != -1 {
					candidates = append(candidates, linkCandidate{raw: text[j : end+1], rangeStart: j, rangeEnd: end + 1, kind: linkImport})
					i = end
				}
			}
			continue
		}
		if strings.HasPrefix(lower[i:], "@use") || strings.HasPrefix(lower[i:], "@forward") {
			keywordEnd := i + len("@use")
			if strings.HasPrefix(lower[i:], "@forward") {
				keywordEnd = i + len("@forward")
			}
			if keywordEnd < len(text) && isCSSSpace(text[keywordEnd]) {
				j := keywordEnd
				for j < len(text) && isCSSSpace(text[j]) {
					j++
				}
				if j < len(text) && (text[j] == '\'' || text[j] == '"') {
					if end := quotedEnd(text, j); end != -1 {
						candidates = append(candidates, linkCandidate{raw: text[j : end+1], rangeStart: j, rangeEnd: end + 1, kind: linkSassModule})
						i = end
					}
				}
			}
			continue
		}
		if strings.HasPrefix(lower[i:], "url(") {
			open := i + len("url")
			close := matchingParenString(text, open)
			if close == -1 {
				continue
			}
			start, end := trimRange(text, open+1, close)
			if start < end {
				candidates = append(candidates, linkCandidate{raw: text[start:end], rangeStart: start, rangeEnd: end, kind: linkURL})
			}
			i = close
		}
	}
	return candidates
}

func resolveSCSSReference(ctx context.Context, ref, baseURL string, resolver DocumentResolver) (string, bool) {
	if strings.HasPrefix(ref, "pkg:") {
		return resolveSCSSPackageReference(ctx, ref, baseURL, resolver)
	}
	candidates := scssReferenceCandidates(ref)
	fallback := ""
	for _, candidate := range candidates {
		resolved, ok := resolver.ResolveReference(candidate, baseURL)
		if fallback == "" && resolved != "" {
			fallback = resolved
		}
		if ok && resolved != "" {
			return resolved, true
		}
	}
	if fallback != "" {
		return fallback, true
	}
	return "", false
}

type scssPackageJSON struct {
	Style   string         `json:"style"`
	Sass    string         `json:"sass"`
	Exports map[string]any `json:"exports"`
}

func resolveSCSSPackageReference(ctx context.Context, ref, baseURL string, resolver DocumentResolver) (string, bool) {
	contentResolver, ok := resolver.(DocumentContentResolver)
	if !ok {
		return "", false
	}
	bareTarget := strings.TrimPrefix(ref, "pkg:")
	moduleName := moduleNameFromPath(bareTarget)
	if moduleName == "" {
		return "", false
	}
	packageJSONRef := "node_modules/" + moduleName + "/package.json"
	packageJSONURI, ok := resolver.ResolveReference(packageJSONRef, baseURL)
	if !ok || packageJSONURI == "" {
		return "", false
	}
	content, err := contentResolver.ReadFile(ctx, packageJSONURI)
	if err != nil || strings.TrimSpace(content) == "" {
		return "", false
	}
	var packageJSON scssPackageJSON
	if err := json.Unmarshal([]byte(content), &packageJSON); err != nil {
		return "", false
	}
	moduleURI := strings.TrimSuffix(packageJSONURI, "/package.json")
	subpath := ""
	if len(bareTarget) > len(moduleName) && bareTarget[len(moduleName)] == '/' {
		subpath = bareTarget[len(moduleName)+1:]
	}
	entry := scssPackageEntry(packageJSON, subpath)
	if entry == "" || !strings.HasSuffix(entry, ".scss") {
		return "", false
	}
	return joinPackageEntry(moduleURI, entry), true
}

func scssPackageEntry(packageJSON scssPackageJSON, subpath string) string {
	if packageJSON.Exports != nil {
		if subpath == "" {
			rootExport := any(packageJSON.Exports)
			if dotExport, ok := packageJSON.Exports["."]; ok {
				rootExport = dotExport
			}
			if entry := scssExportEntry(rootExport, "sass", "style", "default"); entry != "" {
				return entry
			}
		} else {
			lookupSubpath := "./" + strings.TrimSuffix(subpath, ".scss")
			lookupSubpathSCSS := lookupSubpath + ".scss"
			if entry := scssPackageSubpathEntry(packageJSON.Exports, lookupSubpathSCSS, lookupSubpath); entry != "" {
				return entry
			}
			if entry := scssPackagePatternEntry(packageJSON.Exports, lookupSubpath); entry != "" {
				return entry
			}
		}
	} else if subpath == "" {
		if packageJSON.Sass != "" {
			return packageJSON.Sass
		}
		return packageJSON.Style
	}
	return ""
}

func scssPackageSubpathEntry(exports map[string]any, names ...string) string {
	for _, name := range names {
		if object, ok := exports[name]; ok {
			return scssExportEntry(object, "sass", "styles", "default")
		}
	}
	return ""
}

func scssPackagePatternEntry(exports map[string]any, lookupSubpath string) string {
	for pattern, object := range exports {
		if !strings.Contains(pattern, "*") {
			continue
		}
		normalizedPattern := strings.ReplaceAll(strings.TrimSuffix(pattern, ".scss"), "*", "\x00")
		parts := strings.Split(normalizedPattern, "\x00")
		if len(parts) != 2 || !strings.HasPrefix(lookupSubpath, parts[0]) || !strings.HasSuffix(lookupSubpath, parts[1]) {
			continue
		}
		matched := strings.TrimSuffix(strings.TrimPrefix(lookupSubpath, parts[0]), parts[1])
		entry := scssExportEntry(object, "sass", "styles", "default")
		if entry != "" {
			return strings.Replace(entry, "*", matched, 1)
		}
	}
	return ""
}

func scssExportEntry(object any, keys ...string) string {
	values, ok := object.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}

func joinPackageEntry(moduleURI, entry string) string {
	entry = strings.TrimPrefix(entry, "./")
	return joinPathURI(moduleURI, entry)
}

func scssReferenceCandidates(ref string) []string {
	moduleRef := ref
	if strings.HasPrefix(moduleRef, "~") && !strings.HasPrefix(moduleRef, "~/") {
		moduleRef = strings.TrimPrefix(moduleRef, "~")
	}
	result := scssLocalReferenceCandidates(moduleRef)
	result = append(result, scssModuleReferenceCandidates(moduleRef)...)
	return appendUniqueStrings(result)
}

func scssLocalReferenceCandidates(ref string) []string {
	result := []string{ref}
	withoutQuery := ref
	if index := strings.IndexAny(withoutQuery, "?#"); index != -1 {
		withoutQuery = withoutQuery[:index]
	}
	dir, base := splitSCSSReference(withoutQuery)
	if base == "" {
		return result
	}
	lowerBase := strings.ToLower(base)
	hasSCSS := strings.HasSuffix(lowerBase, ".scss")
	hasCSS := strings.HasSuffix(lowerBase, ".css")
	if hasCSS {
		return result
	}
	if hasSCSS {
		if !strings.HasPrefix(base, "_") {
			result = append(result, dir+"_"+base)
		}
		return appendUniqueStrings(result)
	}
	result = append(result, ref+".scss")
	if !strings.HasPrefix(base, "_") {
		result = append(result, dir+"_"+base+".scss")
	}
	result = append(result, ref+"/index.scss")
	result = append(result, ref+"/_index.scss")
	return appendUniqueStrings(result)
}

func scssModuleReferenceCandidates(ref string) []string {
	if ref == "" || strings.HasPrefix(ref, ".") || strings.HasPrefix(ref, "/") || hasURIProtocol(ref) {
		return nil
	}
	moduleName := moduleNameFromPath(ref)
	if moduleName == "" || moduleName == "." || moduleName == ".." {
		return nil
	}
	pathWithinModule := ""
	if len(ref) > len(moduleName) && ref[len(moduleName)] == '/' {
		pathWithinModule = ref[len(moduleName)+1:]
	}
	moduleBase := "node_modules/" + moduleName
	if pathWithinModule == "" {
		return []string{
			moduleBase + "/index.scss",
			moduleBase + "/_index.scss",
		}
	}
	return scssLocalReferenceCandidates(moduleBase + "/" + pathWithinModule)
}

func moduleNameFromPath(ref string) string {
	if strings.HasPrefix(ref, "@") {
		firstSlash := strings.Index(ref, "/")
		if firstSlash == -1 {
			return ref
		}
		secondSlash := strings.Index(ref[firstSlash+1:], "/")
		if secondSlash == -1 {
			return ref
		}
		return ref[:firstSlash+1+secondSlash]
	}
	if slash := strings.Index(ref, "/"); slash != -1 {
		return ref[:slash]
	}
	return ref
}

func splitSCSSReference(ref string) (string, string) {
	slash := strings.LastIndex(ref, "/")
	if slash == -1 {
		return "", ref
	}
	return ref[:slash+1], ref[slash+1:]
}

func appendUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := values[:0]
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func quotedEnd(text string, start int) int {
	if start >= len(text) || text[start] != '\'' && text[start] != '"' {
		return -1
	}
	end := skipCSSIgnored(text, start)
	if end == start {
		return -1
	}
	return end
}

func hasURIProtocol(target string) bool {
	colon := strings.IndexByte(target, ':')
	if colon <= 0 {
		return false
	}
	for i := 0; i < colon; i++ {
		c := target[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
