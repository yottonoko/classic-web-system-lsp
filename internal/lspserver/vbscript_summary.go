package lspserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type vbPublicSummarySymbol struct {
	Name         string
	Kind         string
	Range        lsp.Range
	TypeName     string
	ExplicitType bool
	MemberOf     string
	Visibility   string
}

type vbExportSummary struct {
	Name       string
	Kind       string
	Range      lsp.Range
	TypeName   string
	MemberOf   string
	Visibility string
	Members    []vbExportSummary
}

type vbFileAnalysisSummary struct {
	Fingerprint         string
	PublicSignatureHash string
	VBScript            vbLocalSummary
}

type vbLocalSummary struct {
	Fingerprint                  string
	PublicSymbols                []vbPublicSummarySymbol
	Exports                      []vbExportSummary
	ExternalRefs                 []vbExternalRef
	ExternalRefUsages            []vbExternalRefUsage
	ImplicitGlobalCandidateNames []string
}

type vbExternalRef struct {
	Name       string
	KindHint   string
	MemberName string
	Range      lsp.Range
}

type vbExternalRefUsage struct {
	Key    string
	Count  int
	Ranges []lsp.Range
}

func collectVBScriptPublicSummarySymbols(parsed *core.ParsedDocument) []vbPublicSummarySymbol {
	types := graphAnalysisTypes(parsed)
	declarations := collectVBNamingDeclarations(parsed)
	symbols := make([]vbPublicSummarySymbol, 0, len(declarations))
	seen := map[string]struct{}{}
	for _, declaration := range declarations {
		if !isVBPublicSummaryDeclaration(parsed, declaration) {
			continue
		}
		key := strings.ToLower(declaration.Kind + ":" + declaration.MemberOf + ":" + declaration.Name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		symbols = append(symbols, vbPublicSummarySymbol{
			Name:         declaration.Name,
			Kind:         publicSummaryKind(declaration),
			Range:        declaration.Range,
			TypeName:     publicSummaryTypeName(parsed, declaration, types),
			ExplicitType: publicSummaryHasExplicitType(declaration, types),
			MemberOf:     declaration.MemberOf,
			Visibility:   publicSummaryVisibility(parsed, declaration),
		})
	}
	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].Range.Start.Line != symbols[j].Range.Start.Line {
			return symbols[i].Range.Start.Line < symbols[j].Range.Start.Line
		}
		return symbols[i].Range.Start.Character < symbols[j].Range.Start.Character
	})
	return symbols
}

func summarizeVBScriptFileAnalysis(parsed *core.ParsedDocument) vbFileAnalysisSummary {
	const analysisKey = "lspserver.vb-file-summary.v2"
	if value, ok := parsed.LoadRuntimeAnalysis(analysisKey); ok {
		if cached, ok := value.(vbFileAnalysisSummary); ok {
			return cached
		}
	}
	if !parsed.ChangeImpact.Affects(core.LanguageVBScript) {
		if value, ok := parsed.LoadPreviousRuntimeAnalysis(analysisKey); ok {
			if cached, ok := value.(vbFileAnalysisSummary); ok {
				if core.IncrementalChangeAfterLanguage(parsed, core.LanguageVBScript) {
					cached.Fingerprint = textSummaryFingerprint(parsed.Text)
					parsed.StoreRuntimeAnalysis(analysisKey, cached)
					return cached
				}
				if shifted, ok := remapVBFileAnalysisSummary(parsed, cached); ok {
					parsed.StoreRuntimeAnalysis(analysisKey, shifted)
					return shifted
				}
			}
		}
	}
	var cached vbFileAnalysisSummary
	if parsed.LoadAnalysis(analysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(analysisKey, cached)
		return cached
	}
	publicSymbols := collectVBScriptPublicSummarySymbols(parsed)
	exports := exportSummariesForVBScriptPublicSymbols(publicSymbols)
	externalRefs := collectVBScriptExternalRefs(parsed)
	summary := vbFileAnalysisSummary{
		Fingerprint: textSummaryFingerprint(parsed.Text),
		VBScript: vbLocalSummary{
			Fingerprint:                  textSummaryFingerprint(vbScriptRegionText(parsed)),
			PublicSymbols:                publicSymbols,
			Exports:                      exports,
			ExternalRefs:                 externalRefs,
			ExternalRefUsages:            externalRefUsagesForRefs(externalRefs),
			ImplicitGlobalCandidateNames: implicitGlobalCandidateNamesForSummary(parsed),
		},
	}
	summary.PublicSignatureHash = publicSignatureHashForVBScriptSummary(parsed, summary.VBScript)
	parsed.StoreRuntimeAnalysis(analysisKey, summary)
	return summary
}

func remapVBFileAnalysisSummary(parsed *core.ParsedDocument, previous vbFileAnalysisSummary) (vbFileAnalysisSummary, bool) {
	mapper, ok := core.IncrementalRangeMapperFor(parsed)
	if !ok {
		return vbFileAnalysisSummary{}, false
	}
	shifted := previous
	shifted.Fingerprint = textSummaryFingerprint(parsed.Text)
	if previous.VBScript.PublicSymbols != nil {
		shifted.VBScript.PublicSymbols = append([]vbPublicSummarySymbol{}, previous.VBScript.PublicSymbols...)
	}
	for index := range shifted.VBScript.PublicSymbols {
		shifted.VBScript.PublicSymbols[index].Range, ok = mapper.Range(shifted.VBScript.PublicSymbols[index].Range)
		if !ok {
			return vbFileAnalysisSummary{}, false
		}
	}
	if previous.VBScript.Exports != nil {
		shifted.VBScript.Exports = make([]vbExportSummary, len(previous.VBScript.Exports))
	}
	for index, export := range previous.VBScript.Exports {
		shifted.VBScript.Exports[index], ok = remapVBExportSummary(mapper, export)
		if !ok {
			return vbFileAnalysisSummary{}, false
		}
	}
	if previous.VBScript.ExternalRefs != nil {
		shifted.VBScript.ExternalRefs = append([]vbExternalRef{}, previous.VBScript.ExternalRefs...)
	}
	for index := range shifted.VBScript.ExternalRefs {
		shifted.VBScript.ExternalRefs[index].Range, ok = mapper.Range(shifted.VBScript.ExternalRefs[index].Range)
		if !ok {
			return vbFileAnalysisSummary{}, false
		}
	}
	if previous.VBScript.ExternalRefUsages != nil {
		shifted.VBScript.ExternalRefUsages = append([]vbExternalRefUsage{}, previous.VBScript.ExternalRefUsages...)
	}
	for index := range shifted.VBScript.ExternalRefUsages {
		ranges := make([]lsp.Range, len(shifted.VBScript.ExternalRefUsages[index].Ranges))
		for rangeIndex, value := range shifted.VBScript.ExternalRefUsages[index].Ranges {
			ranges[rangeIndex], ok = mapper.Range(value)
			if !ok {
				return vbFileAnalysisSummary{}, false
			}
		}
		shifted.VBScript.ExternalRefUsages[index].Ranges = ranges
	}
	shifted.PublicSignatureHash = publicSignatureHashForVBScriptSummary(parsed, shifted.VBScript)
	return shifted, true
}

func remapVBExportSummary(mapper *core.IncrementalRangeMapper, previous vbExportSummary) (vbExportSummary, bool) {
	shifted := previous
	var ok bool
	shifted.Range, ok = mapper.Range(previous.Range)
	if !ok {
		return vbExportSummary{}, false
	}
	if previous.Members != nil {
		shifted.Members = make([]vbExportSummary, len(previous.Members))
	}
	for index, member := range previous.Members {
		shifted.Members[index], ok = remapVBExportSummary(mapper, member)
		if !ok {
			return vbExportSummary{}, false
		}
	}
	return shifted, true
}

func collectVBScriptExternalRefs(parsed *core.ParsedDocument) []vbExternalRef {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	declared := map[string]struct{}{}
	declarationRanges := map[string]struct{}{}
	for _, declaration := range collectVBNamingDeclarations(parsed) {
		declared[strings.ToLower(declaration.Name)] = struct{}{}
		declarationRanges[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
	}
	refs := make([]vbExternalRef, 0)
	seen := map[string]struct{}{}
	add := func(ref vbExternalRef) {
		if ref.Name == "" {
			return
		}
		key := externalRefUsageKey(ref.Name, ref.MemberName) + ":" + strconv.Itoa(ref.Range.Start.Line) + ":" + strconv.Itoa(ref.Range.Start.Character)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, match := range graphMemberChainPattern.FindAllStringIndex(text, -1) {
			start := region.ContentStart + match[0]
			end := region.ContentStart + match[1]
			if isVBScriptCommentOffset(parsed.Text, start) {
				continue
			}
			chain := parsed.Text[start:end]
			parts := splitVBMemberChain(chain)
			if len(parts) < 2 {
				continue
			}
			name := parts[0]
			lower := strings.ToLower(name)
			if _, ok := declared[lower]; ok || isDeclaredVBBuiltinOrKeywordForDocument(parsed, lower) {
				continue
			}
			nameEnd := start + len(name)
			add(vbExternalRef{Name: name, MemberName: parts[1], Range: doc.Range(start, nameEnd)})
		}
		for _, span := range vbIdentifierSpans(text) {
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			if isVBScriptCommentOffset(parsed.Text, start) {
				continue
			}
			if _, ok := declarationRanges[offsetRangeKey(start, end)]; ok {
				continue
			}
			name := parsed.Text[start:end]
			lower := strings.ToLower(name)
			if _, ok := declared[lower]; ok || isDeclaredVBBuiltinOrKeywordForDocument(parsed, lower) {
				continue
			}
			if previousNonSpace(parsed.Text, start) == '.' {
				continue
			}
			if next := nextNonSpaceByte(parsed.Text, end); next < 0 || parsed.Text[next] != '(' {
				continue
			}
			add(vbExternalRef{Name: name, KindHint: "function", Range: doc.Range(start, end)})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Range.Start.Line != refs[j].Range.Start.Line {
			return refs[i].Range.Start.Line < refs[j].Range.Start.Line
		}
		return refs[i].Range.Start.Character < refs[j].Range.Start.Character
	})
	return refs
}

func isVBScriptCommentOffset(text string, offset int) bool {
	lineStart := strings.LastIndexByte(text[:offset], '\n') + 1
	linePrefix := text[lineStart:offset]
	if strings.Contains(linePrefix, "'") {
		return true
	}
	statementPrefix := linePrefix
	if colon := strings.LastIndexByte(statementPrefix, ':'); colon >= 0 {
		statementPrefix = statementPrefix[colon+1:]
	}
	fields := strings.Fields(statementPrefix)
	return len(fields) > 0 && strings.EqualFold(fields[0], "Rem")
}

func splitVBMemberChain(chain string) []string {
	raw := strings.Split(chain, ".")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func externalRefUsagesForRefs(refs []vbExternalRef) []vbExternalRefUsage {
	byKey := map[string]*vbExternalRefUsage{}
	for _, ref := range refs {
		keys := []string{externalRefUsageKey(ref.Name, ref.MemberName)}
		if ref.MemberName != "" {
			keys = append(keys, strings.ToLower(ref.Name))
		}
		for _, key := range keys {
			usage := byKey[key]
			if usage == nil {
				usage = &vbExternalRefUsage{Key: key}
				byKey[key] = usage
			}
			usage.Count++
			usage.Ranges = append(usage.Ranges, ref.Range)
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	usages := make([]vbExternalRefUsage, 0, len(keys))
	for _, key := range keys {
		usages = append(usages, *byKey[key])
	}
	return usages
}

func externalRefUsageKey(name string, memberName string) string {
	if memberName != "" {
		return strings.ToLower(name) + "." + strings.ToLower(memberName)
	}
	return strings.ToLower(name)
}

func vbScriptRegionText(parsed *core.ParsedDocument) string {
	var builder strings.Builder
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		builder.WriteString(parsed.Text[region.ContentStart:region.ContentEnd])
	}
	return builder.String()
}

func implicitGlobalCandidateNamesForSummary(parsed *core.ParsedDocument) []string {
	seen := map[string]struct{}{}
	for _, declaration := range implicitAssignmentInlayDeclarations(parsed, true, nil) {
		if !declaration.Implicit || declaration.Local {
			continue
		}
		seen[strings.ToLower(declaration.Name)] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func publicSignatureHashForVBScriptSummary(parsed *core.ParsedDocument, summary vbLocalSummary) string {
	languages := map[string]struct{}{}
	kinds := map[string]struct{}{}
	for _, region := range parsed.Regions {
		languages[string(region.Language)] = struct{}{}
		kinds[string(region.Kind)] = struct{}{}
	}
	payload := struct {
		DefaultLanguage string              `json:"defaultLanguage"`
		Languages       []string            `json:"languages"`
		RegionKinds     []string            `json:"regionKinds"`
		VBScript        vbPublicHashPayload `json:"vbscript"`
	}{
		DefaultLanguage: string(parsed.DefaultLanguage),
		Languages:       sortedKeys(languages),
		RegionKinds:     sortedKeys(kinds),
		VBScript: vbPublicHashPayload{
			Exports:                      publicExportBoundaries(summary.Exports),
			ImplicitGlobalCandidateNames: append([]string(nil), summary.ImplicitGlobalCandidateNames...),
		},
	}
	return stableSummaryHash(payload)
}

type vbPublicHashPayload struct {
	Exports                      []vbExportBoundary `json:"exports"`
	ImplicitGlobalCandidateNames []string           `json:"implicitGlobalCandidateNames"`
}

type vbExportBoundary struct {
	Name       string             `json:"name"`
	Kind       string             `json:"kind"`
	TypeName   string             `json:"typeName,omitempty"`
	MemberOf   string             `json:"memberOf,omitempty"`
	Visibility string             `json:"visibility,omitempty"`
	Members    []vbExportBoundary `json:"members,omitempty"`
}

func publicExportBoundaries(exports []vbExportSummary) []vbExportBoundary {
	boundaries := make([]vbExportBoundary, 0, len(exports))
	for _, export := range exports {
		boundaries = append(boundaries, publicExportBoundary(export))
	}
	sort.SliceStable(boundaries, func(i, j int) bool {
		left, _ := json.Marshal(boundaries[i])
		right, _ := json.Marshal(boundaries[j])
		return string(left) < string(right)
	})
	return boundaries
}

func publicExportBoundary(summary vbExportSummary) vbExportBoundary {
	return vbExportBoundary{
		Name:       summary.Name,
		Kind:       summary.Kind,
		TypeName:   summary.TypeName,
		MemberOf:   summary.MemberOf,
		Visibility: summary.Visibility,
		Members:    publicExportBoundaries(summary.Members),
	}
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stableSummaryHash(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func textSummaryFingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return strconv.Itoa(len(text)) + ":" + hex.EncodeToString(sum[:8])
}

func exportSummariesForVBScriptPublicSymbols(symbols []vbPublicSummarySymbol) []vbExportSummary {
	membersByOwner := map[string][]vbPublicSummarySymbol{}
	for _, symbol := range symbols {
		if symbol.MemberOf != "" {
			membersByOwner[strings.ToLower(symbol.MemberOf)] = append(membersByOwner[strings.ToLower(symbol.MemberOf)], symbol)
		}
	}
	exports := make([]vbExportSummary, 0, len(symbols))
	for _, symbol := range symbols {
		if symbol.MemberOf != "" {
			continue
		}
		exports = append(exports, exportSummaryForVBScriptSymbol(symbol, membersByOwner, map[string]struct{}{}))
	}
	return exports
}

func exportSummaryForVBScriptSymbol(symbol vbPublicSummarySymbol, membersByOwner map[string][]vbPublicSummarySymbol, seenOwners map[string]struct{}) vbExportSummary {
	ownerKey := strings.ToLower(symbol.Name)
	nextSeen := make(map[string]struct{}, len(seenOwners)+1)
	for key := range seenOwners {
		nextSeen[key] = struct{}{}
	}
	nextSeen[ownerKey] = struct{}{}
	summary := vbExportSummary{
		Name:       symbol.Name,
		Kind:       symbol.Kind,
		Range:      symbol.Range,
		TypeName:   symbol.TypeName,
		MemberOf:   symbol.MemberOf,
		Visibility: symbol.Visibility,
	}
	if _, recursive := seenOwners[ownerKey]; recursive {
		return summary
	}
	for _, member := range membersByOwner[ownerKey] {
		summary.Members = append(summary.Members, exportSummaryForVBScriptSymbol(member, membersByOwner, nextSeen))
	}
	if len(summary.Members) == 0 {
		summary.Members = nil
	}
	return summary
}

func isVBPublicSummaryDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration) bool {
	if declaration.Name == "" || declaration.Implicit || declaration.Local || declaration.Scope != "" || declaration.Kind == "parameter" {
		return false
	}
	if publicSummaryVisibility(parsed, declaration) == "private" {
		return false
	}
	if declaration.MemberOf != "" {
		switch publicSummaryKind(declaration) {
		case "field", "method", "property":
			return true
		default:
			return false
		}
	}
	switch publicSummaryKind(declaration) {
	case "variable", "constant", "function", "sub", "class":
		return true
	default:
		return false
	}
}

func publicSummaryKind(declaration vbUsageDeclaration) string {
	if declaration.Kind == "const" {
		return "constant"
	}
	return declaration.Kind
}

func publicSummaryTypeName(parsed *core.ParsedDocument, declaration vbUsageDeclaration, types vbGraphAnalysisTypes) string {
	switch publicSummaryKind(declaration) {
	case "function", "property", "method":
		if typeName := graphReturnTypeForDeclaration(declaration, &types); typeName != "" {
			return typeName
		}
	case "variable", "constant", "field":
		return graphMemberDeclarationType(parsed, declaration, &types)
	default:
		return ""
	}
	return "Variant"
}

func publicSummaryHasExplicitType(declaration vbUsageDeclaration, types vbGraphAnalysisTypes) bool {
	lower := strings.ToLower(declaration.Name)
	switch publicSummaryKind(declaration) {
	case "function", "property", "method":
		return graphReturnTypeForDeclaration(declaration, &types) != ""
	case "variable", "constant", "field":
		if graphTypeAnnotationForDeclaration(declaration, &types) != "" {
			return true
		}
		return strings.TrimSpace(types.Types[lower]) != ""
	default:
		return false
	}
}

func publicSummaryVisibility(parsed *core.ParsedDocument, declaration vbUsageDeclaration) string {
	line := strings.TrimSpace(lineTextAtOffset(parsed.Text, declaration.Start))
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(lower, "private "):
		return "private"
	case strings.HasPrefix(lower, "public "):
		return "public"
	default:
		return ""
	}
}
