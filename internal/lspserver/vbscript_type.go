package lspserver

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const vbscriptTypeDiagnosticSource = "asp-lsp-vbscript-type"

type vbscriptTypeInfo struct {
	variableTypes           map[string]string
	explicitTypes           map[string]string
	globalContracts         map[string]vbscriptGlobalTypeContract
	variableTypeExpressions map[string]vbscriptType
	explicitTypeExpressions map[string]vbscriptType
	scopedVariableTypeNames map[string]string
	scopedExplicitTypeNames map[string]string
	scopedVariableTypes     map[string]vbscriptType
	scopedExplicitTypes     map[string]vbscriptType
	members                 map[string]map[string]vbscriptTypedMember
}

const vbscriptTypeInfoCacheAnalysisKey = "lspserver.vbscript-type-info-cache.v1"

// vbscriptTypeInfoCache retains complete and position-specific immutable type
// states for one parsed revision. Settings are copied into the cache because
// globals and COM contracts are server configuration rather than document
// state; a configuration change naturally selects a fresh cache generation.
type vbscriptTypeInfoCache struct {
	Globals        map[string]vbscriptGlobalSetting
	ComTypes       map[string]vbscriptComTypeSetting
	Full           *vbscriptTypeInfo
	PositionOffset int
	Position       *vbscriptTypeInfo
}

func (cache *vbscriptTypeInfoCache) matches(settings serverSettings) bool {
	return cache != nil && reflect.DeepEqual(cache.Globals, settings.VBScriptGlobals) && reflect.DeepEqual(cache.ComTypes, settings.VBScriptComTypes)
}

func vbscriptTypeInfoCacheFor(parsed *core.ParsedDocument, settings serverSettings) *vbscriptTypeInfoCache {
	if parsed == nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(vbscriptTypeInfoCacheAnalysisKey); ok {
		if cache, ok := value.(*vbscriptTypeInfoCache); ok && cache.matches(settings) {
			return cache
		}
	}
	return &vbscriptTypeInfoCache{
		Globals:  cloneVBScriptGlobals(settings.VBScriptGlobals),
		ComTypes: cloneVBScriptComTypes(settings.VBScriptComTypes),
	}
}

func vbscriptTypeInfoCacheWithPosition(parsed *core.ParsedDocument, settings serverSettings, offset int, info vbscriptTypeInfo) {
	if parsed == nil {
		return
	}
	cache := vbscriptTypeInfoCacheFor(parsed, settings)
	next := *cache
	next.PositionOffset = offset
	next.Position = &info
	parsed.StoreRuntimeAnalysis(vbscriptTypeInfoCacheAnalysisKey, &next)
}

func vbscriptTypeInfoCacheWithFull(parsed *core.ParsedDocument, settings serverSettings, info vbscriptTypeInfo) {
	if parsed == nil {
		return
	}
	cache := vbscriptTypeInfoCacheFor(parsed, settings)
	next := *cache
	next.Full = &info
	parsed.StoreRuntimeAnalysis(vbscriptTypeInfoCacheAnalysisKey, &next)
}

// vbscriptGlobalTypeContract is scoped to one root document's textual include
// expansion. A parsed include can have a different contract when it is used by
// another root, so this state must never be stored on the included document.
type vbscriptGlobalTypeContract struct {
	Name     string
	TypeName string
	TypeExpr vbscriptType
	URI      string
	Start    int
}

type vbscriptTypedMember struct {
	Name                  string
	TypeName              string
	Kind                  string
	Parameters            []vbscriptTypedParameter
	ParameterCount        int
	ChecksArgumentCount   bool
	MinimumParameterCount int
	MaximumParameterCount int
	ParameterRangeKnown   bool
}

type vbscriptTypedParameter struct {
	Name     string
	TypeName string
	Mode     string
	Optional bool
}

type vbscriptMemberContract struct {
	kind                   string
	minimum                int
	maximum                int
	parameterKnown         bool
	parameterMetadataKnown bool
	parameters             []vbscriptTypedParameter
	returnType             string
}

func (member vbscriptTypedMember) contract() vbscriptMemberContract {
	contract := vbscriptMemberContract{
		kind:       strings.ToLower(strings.TrimSpace(member.Kind)),
		returnType: strings.TrimSpace(member.TypeName),
	}
	if len(member.Parameters) > 0 {
		contract.parameterKnown = true
		contract.parameterMetadataKnown = true
		contract.parameters = normalizedVBScriptTypedParameters(member.Parameters)
		contract.minimum, contract.maximum = vbscriptTypedParameterRange(contract.parameters)
	} else if member.ParameterRangeKnown {
		contract.minimum = member.MinimumParameterCount
		contract.maximum = member.MaximumParameterCount
		contract.parameterKnown = true
	} else if member.ChecksArgumentCount {
		contract.minimum = member.ParameterCount
		contract.maximum = member.ParameterCount
		contract.parameterKnown = true
	}
	return contract
}

func vbscriptTypedMembersCompatible(left, right vbscriptTypedMember) bool {
	_, ok := vbscriptIntersectTypedMembers(left, right)
	return ok
}

func vbscriptIntersectMemberContracts(left, right vbscriptMemberContract) (vbscriptMemberContract, bool) {
	if left.kind != right.kind || left.parameterKnown != right.parameterKnown {
		return vbscriptMemberContract{}, false
	}
	merged := left
	if left.parameterMetadataKnown && right.parameterMetadataKnown {
		if len(left.parameters) != len(right.parameters) {
			return vbscriptMemberContract{}, false
		}
		merged.parameters = make([]vbscriptTypedParameter, len(left.parameters))
		for index := range left.parameters {
			leftParameter := left.parameters[index]
			rightParameter := right.parameters[index]
			if !strings.EqualFold(leftParameter.Name, rightParameter.Name) ||
				leftParameter.Optional != rightParameter.Optional ||
				normalizeVBScriptParameterMode(leftParameter.Mode) != normalizeVBScriptParameterMode(rightParameter.Mode) {
				return vbscriptMemberContract{}, false
			}
			typeName, compatible := vbscriptCommonParameterType(leftParameter.TypeName, rightParameter.TypeName)
			if !compatible {
				return vbscriptMemberContract{}, false
			}
			merged.parameters[index] = vbscriptTypedParameter{
				Name:     leftParameter.Name,
				TypeName: typeName,
				Mode:     normalizeVBScriptParameterMode(leftParameter.Mode),
				Optional: leftParameter.Optional,
			}
		}
		merged.parameterMetadataKnown = true
		merged.minimum, merged.maximum = vbscriptTypedParameterRange(merged.parameters)
	} else if left.parameterKnown {
		merged.minimum = max(left.minimum, right.minimum)
		merged.maximum = min(left.maximum, right.maximum)
		if merged.minimum > merged.maximum {
			return vbscriptMemberContract{}, false
		}
		if left.parameterMetadataKnown {
			merged.parameters = append([]vbscriptTypedParameter(nil), left.parameters...)
			merged.parameterMetadataKnown = true
		} else if right.parameterMetadataKnown {
			merged.parameters = append([]vbscriptTypedParameter(nil), right.parameters...)
			merged.parameterMetadataKnown = true
		}
	}
	if left.returnType == "" {
		merged.returnType = right.returnType
	} else if right.returnType != "" {
		returnType, compatible := vbscriptCommonReturnType(left.returnType, right.returnType)
		if !compatible {
			return vbscriptMemberContract{}, false
		}
		merged.returnType = returnType
	}
	return merged, true
}

func vbscriptIntersectTypedMembers(left, right vbscriptTypedMember) (vbscriptTypedMember, bool) {
	mergedContract, ok := vbscriptIntersectMemberContracts(left.contract(), right.contract())
	if !ok {
		return vbscriptTypedMember{}, false
	}
	merged := left
	merged.TypeName = mergedContract.returnType
	if mergedContract.parameterMetadataKnown {
		merged.Parameters = append([]vbscriptTypedParameter(nil), mergedContract.parameters...)
	}
	if mergedContract.parameterKnown {
		merged.ParameterCount = mergedContract.maximum
		merged.ChecksArgumentCount = true
		merged.MinimumParameterCount = mergedContract.minimum
		merged.MaximumParameterCount = mergedContract.maximum
		merged.ParameterRangeKnown = true
	} else {
		merged.ChecksArgumentCount = false
		merged.ParameterCount = 0
		merged.MinimumParameterCount = 0
		merged.MaximumParameterCount = 0
		merged.ParameterRangeKnown = false
	}
	return merged, true
}

func normalizedVBScriptTypedParameters(parameters []vbscriptTypedParameter) []vbscriptTypedParameter {
	if len(parameters) == 0 {
		return nil
	}
	normalized := make([]vbscriptTypedParameter, len(parameters))
	for index, parameter := range parameters {
		normalized[index] = vbscriptTypedParameter{
			Name:     strings.TrimSpace(parameter.Name),
			TypeName: strings.TrimSpace(parameter.TypeName),
			Mode:     normalizeVBScriptParameterMode(parameter.Mode),
			Optional: parameter.Optional,
		}
	}
	return normalized
}

func normalizeVBScriptParameterMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "byval", "by-value", "value":
		return "byval"
	case "byref", "by-reference", "reference", "":
		return "byref"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func vbscriptTypedParameterRange(parameters []vbscriptTypedParameter) (int, int) {
	minimum := 0
	for _, parameter := range parameters {
		if !parameter.Optional {
			minimum++
		}
	}
	return minimum, len(parameters)
}

func vbscriptTypedMemberFromComSetting(name string, setting vbscriptComMemberSetting) vbscriptTypedMember {
	kind := setting.declarationKind()
	parameters := make([]vbscriptTypedParameter, 0, len(setting.Parameters))
	for _, parameter := range setting.Parameters {
		parameters = append(parameters, vbscriptTypedParameter{
			Name:     strings.TrimSpace(parameter.Name),
			TypeName: strings.TrimSpace(parameter.Type),
			Mode:     parameter.modeName(),
			Optional: parameter.Optional,
		})
	}
	minimum, maximum := vbscriptTypedParameterRange(parameters)
	parameterCount := len(parameters)
	return vbscriptTypedMember{
		Name:                  strings.TrimSpace(name),
		TypeName:              setting.declaredTypeName(),
		Kind:                  kind,
		Parameters:            parameters,
		ParameterCount:        parameterCount,
		ChecksArgumentCount:   kind == "method" || parameterCount > 0,
		MinimumParameterCount: minimum,
		MaximumParameterCount: maximum,
		ParameterRangeKnown:   kind == "method" || parameterCount > 0,
	}
}

func vbscriptCommonParameterType(leftText, rightText string) (string, bool) {
	leftText = strings.TrimSpace(leftText)
	rightText = strings.TrimSpace(rightText)
	if leftText == "" {
		return rightText, true
	}
	if rightText == "" {
		return leftText, true
	}
	left, leftErr := parseVBScriptType(leftText)
	right, rightErr := parseVBScriptType(rightText)
	if leftErr != nil || rightErr != nil {
		if strings.EqualFold(leftText, rightText) {
			return canonicalVBScriptReturnText(leftText, rightText), true
		}
		return "", false
	}
	if left.isUnknown() {
		return right.String(), true
	}
	if right.isUnknown() {
		return left.String(), true
	}
	parts := make([]vbscriptType, 0, 2)
	for _, leftPart := range vbscriptTypeParts(left) {
		for _, rightPart := range vbscriptTypeParts(right) {
			part, ok := vbscriptCommonParameterTypeParts(leftPart, rightPart)
			if !ok {
				continue
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return canonicalVBScriptReturnUnion(parts...).String(), true
}

func vbscriptTypeParts(value vbscriptType) []vbscriptType {
	if value.kind != vbscriptTypeUnion {
		return []vbscriptType{value}
	}
	return value.parts
}

func vbscriptCommonParameterTypeParts(left, right vbscriptType) (vbscriptType, bool) {
	if left.isUnknown() {
		return right, true
	}
	if right.isUnknown() {
		return left, true
	}
	if strings.EqualFold(left.String(), right.String()) {
		return left, true
	}
	if left.kind == vbscriptTypePrimitive && right.kind == vbscriptTypePrimitive &&
		isVBScriptNumericFamilyType(left.name) && isVBScriptNumericFamilyType(right.name) {
		return vbscriptType{kind: vbscriptTypePrimitive, name: "Number"}, true
	}
	if vbscriptTypeAssignable(left, right) {
		return right, true
	}
	if vbscriptTypeAssignable(right, left) {
		return left, true
	}
	return vbscriptType{}, false
}

type vbAssignment struct {
	Name      string
	Scope     string
	HasSet    bool
	Value     string
	NameRange lsp.Range
	SetRange  lsp.Range
}

func (s *Server) vbscriptTypeDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	return s.vbscriptTypeDiagnosticsContext(context.Background(), parsed)
}

func (s *Server) vbscriptTypeDiagnosticsContext(ctx context.Context, parsed *core.ParsedDocument) []lsp.Diagnostic {
	diagnostics, _ := s.vbscriptTypeDiagnosticsContextResult(ctx, parsed)
	return diagnostics
}

func (s *Server) vbscriptTypeDiagnosticsContextResult(ctx context.Context, parsed *core.ParsedDocument) ([]lsp.Diagnostic, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	if !strings.EqualFold(s.settings.VBScriptTypeChecking, "strict") {
		return nil, true
	}
	info, complete := s.vbscriptTypeInfoContext(ctx, parsed)
	if ctx.Err() != nil || !complete {
		return nil, false
	}
	diagnostics := vbscriptTypeAnnotationDiagnostics(parsed)
	if ctx.Err() != nil {
		return nil, false
	}
	diagnostics = append(diagnostics, s.vbscriptAssignmentTypeDiagnostics(parsed, info)...)
	if ctx.Err() != nil {
		return nil, false
	}
	includedDiagnostics, includedComplete := s.vbscriptIncludedAssignmentTypeDiagnosticsContextResult(ctx, parsed, info)
	if ctx.Err() != nil || !includedComplete {
		return nil, false
	}
	diagnostics = append(diagnostics, includedDiagnostics...)
	diagnostics = append(diagnostics, s.vbscriptMemberTypeDiagnostics(parsed, info)...)
	if ctx.Err() != nil {
		return nil, false
	}
	callDiagnostics, callComplete := s.vbscriptCallTypeDiagnosticsContextResult(ctx, parsed, info)
	if ctx.Err() != nil || !callComplete {
		return nil, false
	}
	diagnostics = append(diagnostics, callDiagnostics...)
	return dedupeDiagnostics(diagnostics), true
}

func (s *Server) vbscriptTypeInfo(parsed *core.ParsedDocument) vbscriptTypeInfo {
	info, _ := s.vbscriptTypeInfoContext(context.Background(), parsed)
	return info
}

// vbscriptTypeInfoAtOffsetContext evaluates the same source-ordered state as
// vbscriptTypeInfoContext, but stops the root document at offset. The full
// document metadata is still used for member and procedure descriptions; only
// value state and root contracts are activated by the execution prefix.
func (s *Server) vbscriptTypeInfoAtOffsetContext(ctx context.Context, parsed *core.ParsedDocument, offset int) (vbscriptTypeInfo, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return vbscriptTypeInfo{}, false
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(parsed.Text) {
		offset = len(parsed.Text)
	}
	cacheable := len(parsed.Includes) == 0
	if cacheable {
		if value, ok := parsed.LoadRuntimeAnalysis(vbscriptTypeInfoCacheAnalysisKey); ok {
			if cache, ok := value.(*vbscriptTypeInfoCache); ok && cache.matches(s.settings) {
				if cache.Position != nil && cache.PositionOffset == offset {
					return *cache.Position, true
				}
			}
		}
	}

	// Build the complete metadata first. Include expansion is deliberately
	// atomic, so an incomplete or cancelled traversal cannot publish a prefix.
	full, complete := s.vbscriptTypeInfoContext(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return vbscriptTypeInfo{}, false
	}
	allUnits, complete := s.vbscriptIncludeExecutionUnitsContext(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return vbscriptTypeInfo{}, false
	}
	units := vbscriptIncludeExecutionUnitsThroughOffset(allUnits, parsed, offset)

	// Keep configured globals and explicit declaration metadata as the initial
	// state. Inferred values from the final document are intentionally omitted:
	// they may come from an assignment after the query position.
	info := full
	info.variableTypes = map[string]string{}
	info.explicitTypes = map[string]string{}
	info.variableTypeExpressions = map[string]vbscriptType{}
	info.explicitTypeExpressions = map[string]vbscriptType{}
	info.scopedVariableTypeNames = map[string]string{}
	info.scopedExplicitTypeNames = map[string]string{}
	info.scopedVariableTypes = map[string]vbscriptType{}
	info.scopedExplicitTypes = map[string]vbscriptType{}
	for name, setting := range s.settings.VBScriptGlobals {
		if ctx.Err() != nil {
			return vbscriptTypeInfo{}, false
		}
		typeName := strings.TrimSpace(setting.typeName())
		if typeName == "" {
			continue
		}
		lower := strings.ToLower(name)
		info.variableTypes[lower] = typeName
		key := vbscriptTypeScopeKey(parsed, "", name)
		info.scopedVariableTypeNames[key] = typeName
		if typeExpr, err := parseVBScriptType(typeName); err == nil {
			info.variableTypeExpressions[lower] = typeExpr
			info.scopedVariableTypes[key] = typeExpr
		}
	}

	// A document is active when one of its expanded units occurs in the prefix.
	// The root is special: its final unit is truncated at the query offset.
	activeEnds := map[string]int{}
	for _, unit := range units {
		if ctx.Err() != nil {
			return vbscriptTypeInfo{}, false
		}
		if unit.document == nil || unit.start >= unit.end {
			continue
		}
		end := unit.end
		if unit.document == parsed && end > offset {
			end = offset
		}
		key := workspacepkg.FileIdentityKeyFromURI(unit.document.URI)
		if end > activeEnds[key] {
			activeEnds[key] = end
		}
	}
	if _, exists := activeEnds[workspacepkg.FileIdentityKeyFromURI(parsed.URI)]; !exists {
		activeEnds[workspacepkg.FileIdentityKeyFromURI(parsed.URI)] = offset
	}

	// Reconstruct explicit declaration state only for active source spans. A
	// root declaration after the query must not seed the prefix, while a
	// declaration in an included document is active once that include executes.
	activeDocuments := map[string]*core.ParsedDocument{workspacepkg.FileIdentityKeyFromURI(parsed.URI): parsed}
	for _, unit := range units {
		if unit.document == nil {
			continue
		}
		activeDocuments[workspacepkg.FileIdentityKeyFromURI(unit.document.URI)] = unit.document
	}
	for key, document := range activeDocuments {
		if ctx.Err() != nil {
			return vbscriptTypeInfo{}, false
		}
		limit := activeEnds[key]
		for _, declaration := range variableInlayDeclarations(document, true, nil) {
			if declaration.Start < 0 || declaration.Start >= limit || vbscriptDeclarationIsClassMember(document, declaration) {
				continue
			}
			annotation := strings.TrimSpace(precedingVBTypeAnnotation(document, declaration.Line, declaration.Name))
			typeName := strings.TrimSpace(declaration.TypeName)
			// Implicit TypeName values are assignment inference, not declaration
			// metadata. An annotation remains explicit even for an implicit name.
			if declaration.Implicit {
				typeName = ""
			}
			if typeName == "" {
				typeName = annotation
			}
			if typeName == "" {
				continue
			}
			typeExpr, err := parseVBScriptType(typeName)
			if err != nil {
				continue
			}
			scopeKey := vbscriptTypeScopeKey(document, declaration.Scope, declaration.Name)
			info.scopedVariableTypeNames[scopeKey] = typeName
			info.scopedVariableTypes[scopeKey] = typeExpr
			if annotation != "" {
				info.scopedExplicitTypeNames[scopeKey] = typeName
				info.scopedExplicitTypes[scopeKey] = typeExpr
			}
			if declaration.Scope == "" {
				info.variableTypes[strings.ToLower(declaration.Name)] = typeName
				info.variableTypeExpressions[strings.ToLower(declaration.Name)] = typeExpr
				if annotation != "" {
					info.explicitTypes[strings.ToLower(declaration.Name)] = typeName
					info.explicitTypeExpressions[strings.ToLower(declaration.Name)] = typeExpr
				}
			}
		}
	}

	analyses := make(map[string]vbGraphAnalysisTypes, len(activeDocuments))
	for key, document := range activeDocuments {
		if ctx.Err() != nil {
			return vbscriptTypeInfo{}, false
		}
		analysis := graphAnalysisTypes(document)
		analyses[key] = analysis
		analyses[document.URI] = analysis
	}
	info.members = s.vbscriptTypeMembersForDocumentsContext(ctx, activeDocuments, activeEnds, parsed, analyses)
	if ctx.Err() != nil {
		return vbscriptTypeInfo{}, false
	}
	if !s.applyVBScriptSourceOrderedTypesContext(ctx, parsed, units, analyses, info) || ctx.Err() != nil {
		return vbscriptTypeInfo{}, false
	}
	if cacheable {
		vbscriptTypeInfoCacheWithPosition(parsed, s.settings, offset, info)
	}
	return info, true
}

func (s *Server) vbscriptTypeMembersForDocumentsContext(ctx context.Context, documents map[string]*core.ParsedDocument, activeEnds map[string]int, root *core.ParsedDocument, analyses map[string]vbGraphAnalysisTypes) map[string]map[string]vbscriptTypedMember {
	if ctx == nil {
		ctx = context.Background()
	}
	membersByType := map[string]map[string]vbscriptTypedMember{}
	for typeName, setting := range s.settings.VBScriptComTypes {
		if ctx.Err() != nil {
			return nil
		}
		key := strings.ToLower(strings.TrimSpace(typeName))
		if key == "" {
			continue
		}
		if membersByType[key] == nil {
			membersByType[key] = map[string]vbscriptTypedMember{}
		}
		for memberName, settingMember := range setting.Members {
			name := strings.TrimSpace(memberName)
			if name == "" {
				continue
			}
			membersByType[key][strings.ToLower(name)] = vbscriptTypedMemberFromComSetting(name, settingMember)
		}
	}
	for key, document := range documents {
		if ctx.Err() != nil {
			return nil
		}
		if document == nil {
			continue
		}
		analysis := analyses[key]
		limit, rootLimited := activeEnds[key]
		visibleMembers := map[string]map[string]struct{}{}
		if rootLimited && document == root {
			for _, declaration := range collectVBNamingDeclarations(document) {
				if declaration.MemberOf == "" || declaration.Start > limit {
					continue
				}
				memberKey := strings.ToLower(declaration.MemberOf)
				if visibleMembers[memberKey] == nil {
					visibleMembers[memberKey] = map[string]struct{}{}
				}
				visibleMembers[memberKey][strings.ToLower(declaration.Name)] = struct{}{}
			}
		}
		for owner, declaredMembers := range analysis.Members {
			ownerType := firstVBTypeName(owner)
			if ownerType == "" {
				continue
			}
			ownerKey := strings.ToLower(ownerType)
			if membersByType[ownerKey] == nil {
				membersByType[ownerKey] = map[string]vbscriptTypedMember{}
			}
			for memberName, typeName := range declaredMembers {
				if rootLimited && document == root {
					if _, visible := visibleMembers[ownerKey][strings.ToLower(memberName)]; !visible {
						continue
					}
				}
				if typeName = strings.TrimSpace(typeName); typeName != "" {
					membersByType[ownerKey][strings.ToLower(memberName)] = vbscriptTypedMember{Name: memberName, TypeName: typeName, Kind: "property"}
				}
			}
		}
		for classKey, classMembers := range vbClassMemberCompletionsByClass(document) {
			if ctx.Err() != nil {
				return nil
			}
			if membersByType[classKey] == nil {
				membersByType[classKey] = map[string]vbscriptTypedMember{}
			}
			for memberKey, member := range classMembers {
				if rootLimited && document == root {
					if _, visible := visibleMembers[classKey][memberKey]; !visible {
						continue
					}
				}
				if _, exists := membersByType[classKey][memberKey]; exists {
					continue
				}
				kind := "property"
				if member.Kind == lsp.CompletionItemKindMethod {
					kind = "method"
				}
				membersByType[classKey][memberKey] = vbscriptTypedMember{
					Name:                  member.Name,
					TypeName:              member.TypeName,
					Kind:                  kind,
					ParameterCount:        member.ParameterCount,
					MinimumParameterCount: member.MinimumParameterCount,
					MaximumParameterCount: member.MaximumParameterCount,
					ParameterRangeKnown:   member.ParameterRangeKnown,
					ChecksArgumentCount:   member.ParameterRangeKnown,
				}
			}
		}
	}
	return membersByType
}

// vbscriptIncludeExecutionUnitsThroughOffset applies the root-only truncation
// used by position-sensitive requests without repeating include expansion.
func vbscriptIncludeExecutionUnitsThroughOffset(units []vbscriptIncludeExecutionUnit, root *core.ParsedDocument, offset int) []vbscriptIncludeExecutionUnit {
	if root == nil || len(units) == 0 {
		return units
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(root.Text) {
		offset = len(root.Text)
	}
	result := make([]vbscriptIncludeExecutionUnit, 0, len(units))
	for _, unit := range units {
		if unit.document != root {
			result = append(result, unit)
			continue
		}
		if offset < unit.start {
			break
		}
		if offset <= unit.end {
			unit.end = offset
			if unit.start < unit.end {
				result = append(result, unit)
			}
			break
		}
		result = append(result, unit)
	}
	return result
}

// vbscriptTypeInfoContext builds type state only after include expansion has
// completed. A deterministic prefix is not safe to publish because a later
// include occurrence can replace or widen the inferred value.
func (s *Server) vbscriptTypeInfoContext(ctx context.Context, parsed *core.ParsedDocument) (vbscriptTypeInfo, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed != nil && len(parsed.Includes) == 0 {
		if value, ok := parsed.LoadRuntimeAnalysis(vbscriptTypeInfoCacheAnalysisKey); ok {
			if cache, ok := value.(*vbscriptTypeInfoCache); ok && cache.matches(s.settings) && cache.Full != nil {
				return *cache.Full, true
			}
		}
	}
	info := vbscriptTypeInfo{
		variableTypes:           map[string]string{},
		explicitTypes:           map[string]string{},
		globalContracts:         map[string]vbscriptGlobalTypeContract{},
		variableTypeExpressions: map[string]vbscriptType{},
		explicitTypeExpressions: map[string]vbscriptType{},
		scopedVariableTypeNames: map[string]string{},
		scopedExplicitTypeNames: map[string]string{},
		scopedVariableTypes:     map[string]vbscriptType{},
		scopedExplicitTypes:     map[string]vbscriptType{},
		members:                 map[string]map[string]vbscriptTypedMember{},
	}
	for name, setting := range s.settings.VBScriptGlobals {
		if ctx.Err() != nil {
			return vbscriptTypeInfo{}, false
		}
		if typeName := setting.typeName(); typeName != "" {
			lower := strings.ToLower(name)
			info.variableTypes[lower] = typeName
			contract := vbscriptGlobalTypeContract{Name: name, TypeName: typeName, Start: -1}
			if typeExpr, err := parseVBScriptType(typeName); err == nil {
				contract.TypeExpr = typeExpr
			}
			info.globalContracts[lower] = contract
			info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, "", name)] = typeName
			if typeExpr, err := parseVBScriptType(typeName); err == nil {
				info.variableTypeExpressions[lower] = typeExpr
				info.scopedVariableTypes[vbscriptTypeScopeKey(parsed, "", name)] = typeExpr
			}
		}
	}
	for name, contract := range vbscriptOwnerGlobalTypeContracts(parsed) {
		info.globalContracts[name] = contract
	}
	for typeName, setting := range s.settings.VBScriptComTypes {
		if ctx.Err() != nil {
			return vbscriptTypeInfo{}, false
		}
		canonicalTypeName := strings.TrimSpace(typeName)
		if canonicalTypeName == "" {
			continue
		}
		typeKey := strings.ToLower(canonicalTypeName)
		if info.members[typeKey] == nil {
			info.members[typeKey] = map[string]vbscriptTypedMember{}
		}
		for memberName, member := range setting.Members {
			if ctx.Err() != nil {
				return vbscriptTypeInfo{}, false
			}
			canonicalMemberName := strings.TrimSpace(memberName)
			if canonicalMemberName == "" {
				continue
			}
			typed := vbscriptTypedMemberFromComSetting(canonicalMemberName, member)
			info.members[typeKey][strings.ToLower(canonicalMemberName)] = typed
		}
	}
	units, complete := s.vbscriptIncludeExecutionUnitsContext(ctx, parsed)
	if ctx.Err() != nil || !complete {
		return vbscriptTypeInfo{}, false
	}
	documents := make([]*core.ParsedDocument, 0, len(units)+1)
	documents = append(documents, parsed)
	seenDocuments := map[string]struct{}{}
	if parsed != nil {
		seenDocuments[parsed.URI] = struct{}{}
	}
	for _, unit := range units {
		if ctx.Err() != nil {
			return vbscriptTypeInfo{}, false
		}
		if unit.document == nil {
			continue
		}
		if _, seen := seenDocuments[unit.document.URI]; seen {
			continue
		}
		seenDocuments[unit.document.URI] = struct{}{}
		documents = append(documents, unit.document)
	}
	analyses := make(map[string]vbGraphAnalysisTypes, len(documents))
	for _, document := range documents {
		if ctx.Err() != nil {
			return vbscriptTypeInfo{}, false
		}
		if document == nil {
			continue
		}
		analysis := graphAnalysisTypes(document)
		analyses[document.URI] = analysis
		for owner, members := range analysis.Members {
			if ctx.Err() != nil {
				return vbscriptTypeInfo{}, false
			}
			ownerType := firstVBTypeName(owner)
			if ownerType == "" {
				continue
			}
			ownerKey := strings.ToLower(ownerType)
			if info.members[ownerKey] == nil {
				info.members[ownerKey] = map[string]vbscriptTypedMember{}
			}
			for memberName, typeName := range members {
				if ctx.Err() != nil {
					return vbscriptTypeInfo{}, false
				}
				canonicalType := strings.TrimSpace(typeName)
				if canonicalType == "" {
					continue
				}
				info.members[ownerKey][strings.ToLower(memberName)] = vbscriptTypedMember{
					Name:     memberName,
					TypeName: canonicalType,
					Kind:     "property",
				}
			}
		}
		for _, declaration := range variableInlayDeclarations(document, true, nil) {
			if ctx.Err() != nil {
				return vbscriptTypeInfo{}, false
			}
			if vbscriptDeclarationIsClassMember(document, declaration) {
				continue
			}
			annotation := graphTypeAnnotationForDeclaration(declaration, &analysis)
			if declaration.TypeName == "" && annotation == "" {
				continue
			}
			typeName := strings.TrimSpace(annotation)
			if typeName == "" {
				typeName = strings.TrimSpace(declaration.TypeName)
			}
			if !vbscriptHasConcreteType(typeName) {
				continue
			}
			if typeExpr, err := parseVBScriptType(typeName); err == nil {
				key := vbscriptTypeScopeKey(document, declaration.Scope, declaration.Name)
				info.scopedVariableTypes[key] = typeExpr
				info.scopedVariableTypeNames[key] = typeName
				if annotation != "" {
					info.scopedExplicitTypes[key] = typeExpr
					info.scopedExplicitTypeNames[key] = typeName
				}
			}
		}
		for classKey, members := range vbClassMemberCompletionsByClass(document) {
			if ctx.Err() != nil {
				return vbscriptTypeInfo{}, false
			}
			if info.members[classKey] == nil {
				info.members[classKey] = map[string]vbscriptTypedMember{}
			}
			for memberKey, member := range members {
				if ctx.Err() != nil {
					return vbscriptTypeInfo{}, false
				}
				if _, annotated := info.members[classKey][memberKey]; annotated {
					continue
				}
				kind := "property"
				if member.Kind == lsp.CompletionItemKindMethod {
					kind = "method"
				}
				info.members[classKey][memberKey] = vbscriptTypedMember{
					Name:                  member.Name,
					TypeName:              member.TypeName,
					Kind:                  kind,
					ParameterCount:        member.ParameterCount,
					MinimumParameterCount: member.MinimumParameterCount,
					MaximumParameterCount: member.MaximumParameterCount,
					ParameterRangeKnown:   member.ParameterRangeKnown,
					ChecksArgumentCount:   member.ParameterRangeKnown,
				}
			}
		}
	}
	if !s.applyVBScriptSourceOrderedTypesContext(ctx, parsed, units, analyses, info) || ctx.Err() != nil {
		return vbscriptTypeInfo{}, false
	}
	if parsed != nil && len(parsed.Includes) == 0 {
		vbscriptTypeInfoCacheWithFull(parsed, s.settings, info)
	}
	return info, true
}

// vbscriptOwnerGlobalTypeContracts collects only metadata declared by the
// request's root document. An annotation in an included file describes that
// file's own declaration; it must not leak into another owner of the same
// include or replace a contract already declared by the owner.
func vbscriptOwnerGlobalTypeContracts(parsed *core.ParsedDocument) map[string]vbscriptGlobalTypeContract {
	contracts := map[string]vbscriptGlobalTypeContract{}
	if parsed == nil {
		return contracts
	}
	declarations := variableInlayDeclarations(parsed, true, nil)
	for _, declaration := range declarations {
		if declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" || declaration.Implicit || !isVBVariableInlayDeclaration(declaration) {
			continue
		}
		if vbscriptDeclarationIsClassMember(parsed, declaration) {
			continue
		}
		typeName := strings.TrimSpace(declaration.TypeName)
		if typeName == "" {
			typeName = strings.TrimSpace(precedingVBTypeAnnotation(parsed, declaration.Line, declaration.Name))
		}
		if typeName == "" {
			continue
		}
		lowerName := strings.ToLower(declaration.Name)
		if _, exists := contracts[lowerName]; exists {
			continue
		}
		contract := vbscriptGlobalTypeContract{Name: declaration.Name, TypeName: typeName, URI: parsed.URI, Start: declaration.Start}
		if typeExpr, err := parseVBScriptType(typeName); err == nil {
			contract.TypeExpr = typeExpr
		}
		contracts[lowerName] = contract
	}
	return contracts
}

const vbscriptClassMemberDeclarationsAnalysisKey = "lspserver.vb-class-member-declarations.v1"

func vbscriptDeclarationIsClassMember(parsed *core.ParsedDocument, declaration vbUsageDeclaration) bool {
	if parsed == nil {
		return false
	}
	value, cached := parsed.LoadRuntimeAnalysis(vbscriptClassMemberDeclarationsAnalysisKey)
	var declarations map[[2]int]struct{}
	if cached {
		declarations, _ = value.(map[[2]int]struct{})
	} else {
		declarations = map[[2]int]struct{}{}
		for _, candidate := range collectVBNamingDeclarations(parsed) {
			if candidate.MemberOf == "" {
				continue
			}
			declarations[[2]int{candidate.Start, candidate.End}] = struct{}{}
		}
		parsed.StoreRuntimeAnalysis(vbscriptClassMemberDeclarationsAnalysisKey, declarations)
	}
	_, member := declarations[[2]int{declaration.Start, declaration.End}]
	return member
}

type vbscriptTypeSourceEvent struct {
	offset      int
	declaration *vbUsageDeclaration
	assignment  *vbAssignment
}

// applyVBScriptSourceOrderedTypesContext evaluates assignments in the source order
// produced by include expansion. Assignments in one source unit retain the
// existing union inference; a later unit replaces an earlier global value.
func (s *Server) applyVBScriptSourceOrderedTypesContext(ctx context.Context, root *core.ParsedDocument, units []vbscriptIncludeExecutionUnit, analyses map[string]vbGraphAnalysisTypes, info vbscriptTypeInfo) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if root == nil || len(units) == 0 {
		return ctx.Err() == nil
	}
	// The metadata pass sees the whole document, but declaration metadata is
	// only state from the declaration's source position onward. Keep that
	// metadata aside for the declaration event instead of allowing a future
	// global contract to seed the replay prefix and reinterpret earlier copies.
	declaredNames := cloneStringMap(info.scopedVariableTypeNames)
	declaredExplicitNames := cloneStringMap(info.scopedExplicitTypeNames)
	declaredExplicitExpressions := cloneVBScriptTypeMap(info.scopedExplicitTypes)
	globalDeclarationKeys := map[string]struct{}{}
	globalDeclarationNames := map[string]struct{}{}
	for _, unit := range units {
		if ctx.Err() != nil {
			return false
		}
		if unit.document == nil {
			continue
		}
		for _, declaration := range variableInlayDeclarations(unit.document, true, nil) {
			if declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" || !isVBVariableInlayDeclaration(declaration) || vbscriptDeclarationIsClassMember(unit.document, declaration) {
				continue
			}
			globalDeclarationKeys[vbscriptTypeScopeKey(unit.document, "", declaration.Name)] = struct{}{}
			globalDeclarationNames[strings.ToLower(declaration.Name)] = struct{}{}
		}
	}
	baseNames := cloneStringMap(info.scopedVariableTypeNames)
	for key := range globalDeclarationKeys {
		delete(baseNames, key)
	}
	baseExpressions := cloneVBScriptTypeMap(info.scopedVariableTypes)
	for key := range globalDeclarationKeys {
		delete(baseExpressions, key)
	}
	for lowerName := range globalDeclarationNames {
		delete(info.variableTypes, lowerName)
		delete(info.variableTypeExpressions, lowerName)
		delete(info.explicitTypes, lowerName)
		delete(info.explicitTypeExpressions, lowerName)
	}
	for key := range globalDeclarationKeys {
		delete(info.scopedExplicitTypeNames, key)
		delete(info.scopedExplicitTypes, key)
	}
	for key := range info.scopedVariableTypeNames {
		delete(info.scopedVariableTypeNames, key)
	}
	for key, value := range baseNames {
		info.scopedVariableTypeNames[key] = value
	}
	for key := range info.scopedVariableTypes {
		delete(info.scopedVariableTypes, key)
	}
	for key, value := range baseExpressions {
		info.scopedVariableTypes[key] = value
	}
	currentNames := cloneStringMap(baseNames)
	currentExpressions := cloneVBScriptTypeMap(baseExpressions)
	activeContracts := map[string]vbscriptGlobalTypeContract{}
	for lowerName, contract := range info.globalContracts {
		if contract.Start < 0 {
			activeContracts[lowerName] = contract
			key := vbscriptTypeScopeKey(root, "", contract.Name)
			baseNames[key] = contract.TypeName
			currentNames[key] = contract.TypeName
			info.scopedVariableTypeNames[key] = contract.TypeName
			if contract.TypeExpr.isUnknown() {
				delete(baseExpressions, key)
				delete(currentExpressions, key)
				delete(info.scopedVariableTypes, key)
			} else {
				baseExpressions[key] = contract.TypeExpr
				currentExpressions[key] = contract.TypeExpr
				info.scopedVariableTypes[key] = contract.TypeExpr
			}
			info.variableTypes[lowerName] = contract.TypeName
			if contract.TypeExpr.isUnknown() {
				delete(info.variableTypeExpressions, lowerName)
			} else {
				info.variableTypeExpressions[lowerName] = contract.TypeExpr
			}
		}
	}
	globalDocuments := make([]*core.ParsedDocument, 0, len(units)+1)
	seenGlobalDocuments := map[string]struct{}{}
	if root != nil {
		seenGlobalDocuments[strings.ToLower(root.URI)] = struct{}{}
		globalDocuments = append(globalDocuments, root)
	}
	for _, unit := range units {
		if unit.document == nil {
			continue
		}
		key := strings.ToLower(unit.document.URI)
		if _, seen := seenGlobalDocuments[key]; seen {
			continue
		}
		seenGlobalDocuments[key] = struct{}{}
		globalDocuments = append(globalDocuments, unit.document)
	}
	globalScopeKeys := map[string][]string{}
	keysForGlobal := func(lowerName string) []string {
		if keys, ok := globalScopeKeys[lowerName]; ok {
			return keys
		}
		keys := make([]string, 0, len(globalDocuments))
		for _, document := range globalDocuments {
			keys = append(keys, vbscriptTypeScopeKey(document, "", lowerName))
		}
		globalScopeKeys[lowerName] = keys
		return keys
	}
	for _, unit := range units {
		if ctx.Err() != nil {
			return false
		}
		if unit.document == nil || unit.start >= unit.end {
			continue
		}
		document := vbTextDocument(unit.document)
		analysis := analyses[unit.document.URI]
		events := make([]vbscriptTypeSourceEvent, 0)
		declarations := variableInlayDeclarations(unit.document, true, nil)
		for index := range declarations {
			declaration := declarations[index]
			if declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" || !isVBVariableInlayDeclaration(declaration) || vbscriptDeclarationIsClassMember(unit.document, declaration) || declaration.Start < unit.start || declaration.Start >= unit.end {
				continue
			}
			copy := declaration
			events = append(events, vbscriptTypeSourceEvent{offset: declaration.Start, declaration: &copy})
		}
		assignments := vbscriptAssignments(unit.document)
		for index := range assignments {
			assignment := assignments[index]
			offset := document.OffsetAt(assignment.NameRange.Start)
			if offset < unit.start || offset >= unit.end {
				continue
			}
			copy := assignment
			events = append(events, vbscriptTypeSourceEvent{offset: offset, assignment: &copy})
		}
		sort.SliceStable(events, func(left, right int) bool {
			if events[left].offset != events[right].offset {
				return events[left].offset < events[right].offset
			}
			return events[left].declaration != nil
		})
		unitNames := map[string]string{}
		unitExpressions := map[string]vbscriptType{}
		seenUnitNames := map[string]struct{}{}
		seenUnitExpressions := map[string]struct{}{}
		clearGlobalInferredState := func(lowerName string) {
			for _, key := range keysForGlobal(lowerName) {
				for _, values := range []map[string]string{baseNames, currentNames, info.scopedVariableTypeNames, unitNames} {
					delete(values, key)
				}
				for _, values := range []map[string]vbscriptType{baseExpressions, currentExpressions, info.scopedVariableTypes, unitExpressions} {
					delete(values, key)
				}
				for _, values := range []map[string]struct{}{seenUnitNames, seenUnitExpressions} {
					delete(values, key)
				}
			}
			delete(info.variableTypes, lowerName)
			delete(info.variableTypeExpressions, lowerName)
		}
		for _, event := range events {
			if ctx.Err() != nil {
				return false
			}
			if unit.document == root {
				for lowerName, contract := range info.globalContracts {
					if contract.URI == root.URI && contract.Start >= 0 && contract.Start <= event.offset {
						activeContracts[lowerName] = contract
					}
				}
			}
			if event.declaration != nil {
				declaration := *event.declaration
				lowerName := strings.ToLower(declaration.Name)
				if contract, fixed := activeContracts[lowerName]; fixed && declaration.Scope == "" && declaration.MemberOf == "" {
					key := vbscriptTypeScopeKey(unit.document, "", declaration.Name)
					info.variableTypes[lowerName] = contract.TypeName
					if contract.TypeExpr.isUnknown() {
						delete(info.variableTypeExpressions, lowerName)
					} else {
						info.variableTypeExpressions[lowerName] = contract.TypeExpr
					}
					info.scopedVariableTypeNames[key] = contract.TypeName
					if contract.TypeExpr.isUnknown() {
						delete(info.scopedVariableTypes, key)
					} else {
						info.scopedVariableTypes[key] = contract.TypeExpr
					}
					if explicitName, explicit := declaredExplicitNames[key]; explicit {
						info.scopedExplicitTypeNames[key] = explicitName
						if explicitExpr, ok := declaredExplicitExpressions[key]; ok {
							info.scopedExplicitTypes[key] = explicitExpr
						}
						info.explicitTypes[lowerName] = explicitName
						if explicitExpr, ok := declaredExplicitExpressions[key]; ok {
							info.explicitTypeExpressions[lowerName] = explicitExpr
						}
					}
					continue
				}
				key := vbscriptTypeScopeKey(unit.document, "", declaration.Name)
				typeName := strings.TrimSpace(declaredNames[key])
				if typeName == "" {
					continue
				}
				info.scopedVariableTypeNames[key] = typeName
				info.variableTypes[lowerName] = typeName
				if typeExpr, err := parseVBScriptType(typeName); err == nil {
					info.scopedVariableTypes[key] = typeExpr
					info.variableTypeExpressions[lowerName] = typeExpr
					if explicitName, explicit := declaredExplicitNames[key]; explicit {
						info.scopedExplicitTypeNames[key] = explicitName
						if explicitExpr, ok := declaredExplicitExpressions[key]; ok {
							info.scopedExplicitTypes[key] = explicitExpr
						}
						info.explicitTypes[lowerName] = explicitName
						if explicitExpr, ok := declaredExplicitExpressions[key]; ok {
							info.explicitTypeExpressions[lowerName] = explicitExpr
						}
					}
				}
				continue
			}
			if event.assignment == nil {
				continue
			}
			assignment := *event.assignment
			if assignment.Scope == "" {
				if contract, fixed := activeContracts[strings.ToLower(assignment.Name)]; fixed {
					lowerName := strings.ToLower(assignment.Name)
					info.variableTypes[lowerName] = contract.TypeName
					if contract.TypeExpr.isUnknown() {
						delete(info.variableTypeExpressions, lowerName)
					} else {
						info.variableTypeExpressions[lowerName] = contract.TypeExpr
					}
					key := vbscriptTypeScopeKey(unit.document, assignment.Scope, assignment.Name)
					info.scopedVariableTypeNames[key] = contract.TypeName
					if contract.TypeExpr.isUnknown() {
						delete(info.scopedVariableTypes, key)
					} else {
						info.scopedVariableTypes[key] = contract.TypeExpr
					}
					continue
				}
			}
			key := vbscriptTypeScopeKey(unit.document, assignment.Scope, assignment.Name)
			if _, explicit := info.scopedExplicitTypeNames[key]; explicit {
				continue
			}
			typeName := strings.TrimSpace(s.inferVBScriptAssignmentType(unit.document, assignment.Scope, assignment.Value, analysis, info))
			if typeName == "" || !vbscriptHasConcreteType(typeName) && !strings.EqualFold(typeName, "Nothing") {
				// A dynamic assignment invalidates every inferred value that was
				// visible before it. Keep explicit contracts separate: those are
				// handled above through activeContracts and explicit type maps.
				if assignment.Scope == "" {
					clearGlobalInferredState(strings.ToLower(assignment.Name))
				} else {
					delete(currentNames, key)
					delete(currentExpressions, key)
					delete(info.scopedVariableTypeNames, key)
					delete(info.scopedVariableTypes, key)
				}
				continue
			}
			if assignment.Scope == "" {
				if _, seen := seenUnitNames[key]; !seen {
					unitNames[key] = baseNames[key]
					seenUnitNames[key] = struct{}{}
				}
				unitNames[key] = combineVBScriptTypeNames(unitNames[key], typeName)
				currentNames[key] = unitNames[key]
				info.variableTypes[strings.ToLower(assignment.Name)] = unitNames[key]
			} else {
				currentNames[key] = combineVBScriptTypeNames(currentNames[key], typeName)
			}
			info.scopedVariableTypeNames[key] = currentNames[key]
			if assignment.Scope == "" {
				if _, seen := seenUnitExpressions[key]; !seen {
					unitExpressions[key] = baseExpressions[key]
					seenUnitExpressions[key] = struct{}{}
				}
				literal := s.inferVBScriptAssignmentLiteralType(unit.document, assignment, analysis, info)
				if literal.isUnknown() {
					currentExpressions[key] = unitExpressions[key]
					info.variableTypeExpressions[strings.ToLower(assignment.Name)] = currentExpressions[key]
					info.scopedVariableTypes[key] = currentExpressions[key]
					continue
				}
				unitExpressions[key] = mergeVBScriptMutableTypes(unitExpressions[key], literal)
				currentExpressions[key] = unitExpressions[key]
				info.variableTypeExpressions[strings.ToLower(assignment.Name)] = currentExpressions[key]
			} else {
				literal := s.inferVBScriptAssignmentLiteralType(unit.document, assignment, analysis, info)
				if literal.isUnknown() {
					continue
				}
				currentExpressions[key] = mergeVBScriptMutableTypes(currentExpressions[key], literal)
			}
			info.scopedVariableTypes[key] = currentExpressions[key]
		}
	}
	return ctx.Err() == nil
}

func cloneStringMap(source map[string]string) map[string]string {
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func cloneVBScriptTypeMap(source map[string]vbscriptType) map[string]vbscriptType {
	copy := make(map[string]vbscriptType, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func (s *Server) inferVBScriptAssignmentLiteralType(document *core.ParsedDocument, assignment vbAssignment, analysis vbGraphAnalysisTypes, info vbscriptTypeInfo) vbscriptType {
	value := strings.TrimSpace(assignment.Value)
	if literal := inferVBScriptValueLiteralType(value); !literal.isUnknown() {
		return literal
	}
	if callee := vbscriptSimpleCallName(value); callee != "" {
		if typeName := analysis.Returns[strings.ToLower(callee)]; typeName != "" {
			if typeExpr, err := parseVBScriptType(typeName); err == nil {
				return typeExpr
			}
		}
	}
	if identifier := vbscriptBareIdentifier(value); identifier != "" {
		if typeExpr, ok := info.scopedVariableTypes[vbscriptTypeScopeKey(document, assignment.Scope, identifier)]; ok {
			return typeExpr
		}
		if typeExpr, ok := info.scopedVariableTypes[vbscriptTypeScopeKey(document, "", identifier)]; ok {
			return typeExpr
		}
		if typeExpr, ok := info.variableTypeExpressions[strings.ToLower(identifier)]; ok && !vbscriptNameBoundInScope(document, identifier, assignment.Scope) {
			return typeExpr
		}
	}
	return vbscriptType{kind: vbscriptTypeUnknown, name: "Variant"}
}

func vbscriptTypeScopeKey(document *core.ParsedDocument, scope, name string) string {
	uri := ""
	if document != nil {
		uri = strings.ToLower(document.URI)
	}
	return uri + "\x00" + strings.ToLower(strings.TrimSpace(scope)) + "\x00" + strings.ToLower(strings.TrimSpace(name))
}

func vbscriptScopeAtOffset(parsed *core.ParsedDocument, offset int) string {
	if parsed == nil {
		return ""
	}
	return vbProcedureScopeAtOffset(vbProcedureScopes(parsed), offset)
}

func vbscriptAssignmentScope(parsed *core.ParsedDocument, offset int, name string) string {
	scope := vbscriptScopeAtOffset(parsed, offset)
	if scope == "" || parsed == nil {
		return scope
	}
	doc := vbTextDocument(parsed)
	position := doc.PositionAt(offset)
	if !vbLocalDeclarationShadowsNameAt(parsed, name, position) && !vbscriptClassMemberShadowsNameAt(parsed, name, scope) {
		return ""
	}
	return scope
}

func vbscriptClassMemberShadowsNameAt(parsed *core.ParsedDocument, name, scope string) bool {
	classScope := vbscriptClassScopeForProcedure(parsed, scope)
	if classScope == "" {
		return false
	}
	if names := vbClassMemberShadowNames(parsed)[strings.ToLower(classScope)]; names != nil {
		_, shadowed := names[strings.ToLower(name)]
		return shadowed
	}
	return false
}

const vbClassMemberShadowNamesAnalysisKey = "lspserver.vb-class-member-shadow-names.v1"

func vbClassMemberShadowNames(parsed *core.ParsedDocument) map[string]map[string]bool {
	if parsed == nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(vbClassMemberShadowNamesAnalysisKey); ok {
		if cached, ok := value.(map[string]map[string]bool); ok {
			return cached
		}
	}
	shadowNames := map[string]map[string]bool{}
	for _, declaration := range collectVBNamingDeclarations(parsed) {
		if declaration.Scope != "" || declaration.MemberOf == "" {
			continue
		}
		switch strings.ToLower(declaration.Kind) {
		case "field", "property", "constant", "const", "variable":
		default:
			continue
		}
		classScope := strings.ToLower(strings.TrimSpace(declaration.MemberOf))
		name := strings.ToLower(strings.TrimSpace(declaration.Name))
		if classScope == "" || name == "" {
			continue
		}
		if shadowNames[classScope] == nil {
			shadowNames[classScope] = map[string]bool{}
		}
		shadowNames[classScope][name] = true
	}
	parsed.StoreRuntimeAnalysis(vbClassMemberShadowNamesAnalysisKey, shadowNames)
	return shadowNames
}

func vbscriptClassScopeForProcedure(parsed *core.ParsedDocument, scope string) string {
	if parsed == nil || scope == "" {
		return ""
	}
	if owner := vbProcedureScopeOwner(scope); owner != "" {
		return owner
	}
	procedureName := vbProcedureScopeName(scope)
	owners := vbClassMemberLineOwners(parsed)
	for _, procedure := range vbProcedureScopes(parsed) {
		if !strings.EqualFold(procedure.Name, procedureName) {
			continue
		}
		owner := procedure.Owner
		if owner == "" {
			owner = owners[procedure.StartLine]
		}
		if owner != "" {
			return owner
		}
	}
	return ""
}

func vbscriptNameBoundInScope(parsed *core.ParsedDocument, name, scope string) bool {
	if parsed == nil {
		return false
	}
	return vbscriptNameBoundInScopeAtOffset(parsed, name, scope, len(parsed.Text))
}

const vbscriptNameBindingCacheAnalysisKey = "lspserver.vb-name-binding-cache.v1"

type vbscriptNameBindingCache struct {
	name   string
	scope  string
	offset int
	bound  bool
}

func vbscriptNameBoundInScopeAtOffset(parsed *core.ParsedDocument, name, scope string, offset int) bool {
	if parsed == nil || name == "" {
		return false
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(parsed.Text) {
		offset = len(parsed.Text)
	}
	cacheKeyName := strings.ToLower(strings.TrimSpace(name))
	cacheKeyScope := strings.ToLower(strings.TrimSpace(scope))
	if value, ok := parsed.LoadRuntimeAnalysis(vbscriptNameBindingCacheAnalysisKey); ok {
		if cached, ok := value.(vbscriptNameBindingCache); ok && cached.name == cacheKeyName && cached.scope == cacheKeyScope && cached.offset == offset {
			return cached.bound
		}
	}
	bound := vbscriptNameBoundInScopeAtOffsetUncached(parsed, name, scope, offset)
	parsed.StoreRuntimeAnalysis(vbscriptNameBindingCacheAnalysisKey, vbscriptNameBindingCache{
		name:   cacheKeyName,
		scope:  cacheKeyScope,
		offset: offset,
		bound:  bound,
	})
	return bound
}

func vbscriptNameBoundInScopeAtOffsetUncached(parsed *core.ParsedDocument, name, scope string, offset int) bool {
	scopeName := vbProcedureScopeName(scope)
	classScope := vbscriptClassScopeForProcedure(parsed, scope)
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		if declaration.Start > offset {
			continue
		}
		if !strings.EqualFold(declaration.Name, name) {
			continue
		}
		if declaration.Implicit {
			continue
		}
		if declaration.Local && strings.EqualFold(declaration.Scope, scope) {
			return true
		}
		if scope == "" && !declaration.Local && declaration.Scope == "" {
			return true
		}
	}
	for _, declaration := range vbParameterDeclarationsFromTokens(parsed) {
		if strings.EqualFold(declaration.Name, name) && (strings.EqualFold(declaration.Scope, scope) ||
			(scope == "" && strings.EqualFold(vbProcedureScopeName(declaration.Scope), scopeName) &&
				strings.EqualFold(vbProcedureScopeOwner(declaration.Scope), vbProcedureScopeOwner(scope)))) {
			return true
		}
	}
	if classScope != "" {
		for _, declaration := range collectVBNamingDeclarations(parsed) {
			if declaration.Start > offset {
				continue
			}
			if !strings.EqualFold(declaration.Name, name) || declaration.MemberOf == "" || !strings.EqualFold(declaration.MemberOf, classScope) || declaration.Scope != "" {
				continue
			}
			switch declaration.Kind {
			case "field", "property", "constant", "const", "variable":
				return true
			}
		}
	}
	doc := vbTextDocument(parsed)
	for _, assignment := range vbscriptAssignments(parsed) {
		if doc.OffsetAt(assignment.NameRange.Start) > offset {
			continue
		}
		if strings.EqualFold(assignment.Name, name) && strings.EqualFold(assignment.Scope, scope) {
			return true
		}
	}
	return false
}

func (s *Server) inferVBScriptAssignmentType(parsed *core.ParsedDocument, scope, value string, analysis vbGraphAnalysisTypes, info vbscriptTypeInfo) string {
	if strings.EqualFold(strings.TrimSpace(value), "Nothing") {
		return "Nothing"
	}
	if typeName := inferVBValueTypeForDocument(parsed, value); !isLooseVBTypeName(typeName) {
		return typeName
	}
	callee := vbscriptSimpleCallName(value)
	if callee != "" {
		if typeName := analysis.Returns[strings.ToLower(callee)]; typeName != "" {
			return typeName
		}
	}
	if identifier := vbscriptBareIdentifier(value); identifier != "" {
		if typeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, identifier)]; typeName != "" {
			return typeName
		}
		if typeName := info.variableTypes[strings.ToLower(identifier)]; typeName != "" && !vbscriptNameBoundInScope(parsed, identifier, scope) {
			return typeName
		}
	}
	return ""
}

func combineVBScriptTypeNames(values ...string) string {
	seen := map[string]struct{}{}
	types := []string{}
	for _, value := range values {
		parts := strings.Split(value, "|")
		if parsed, err := splitVBScriptTypeUnion(value); err == nil {
			parts = parsed
		}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key := strings.ToLower(part)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			types = append(types, part)
		}
	}
	return strings.Join(types, " | ")
}

func vbscriptAssignments(parsed *core.ParsedDocument) []vbAssignment {
	if parsed == nil {
		return nil
	}
	const analysisKey = "lspserver.vb-assignments.v2"
	if value, ok := parsed.LoadRuntimeAnalysis(analysisKey); ok {
		if cached, ok := value.([]vbAssignment); ok {
			return cached
		}
	}
	var cached []vbAssignment
	if parsed.LoadAnalysis(analysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(analysisKey, cached)
		return cached
	}
	doc := vbTextDocument(parsed)
	assignments := []vbAssignment{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			for _, statement := range splitVBTypeAssignmentStatementSegments(line, lineStart) {
				if vbscriptTypeStatementStartsRemComment(statement.Text) {
					break
				}
				if assignment, ok := parseVBAssignment(statement.Text, statement.Start, doc); ok {
					assignment.Scope = vbscriptAssignmentScope(parsed, doc.OffsetAt(assignment.NameRange.Start), assignment.Name)
					assignments = append(assignments, assignment)
				}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	parsed.StoreRuntimeAnalysis(analysisKey, assignments)
	parsed.StoreAnalysis(analysisKey, assignments)
	return assignments
}

// splitVBTypeAssignmentStatementSegments keeps assignment inference and type
// diagnostics on the same statement boundaries. Date literals may contain
// colons, just like quoted strings.
func splitVBTypeAssignmentStatementSegments(line string, lineOffset int) []vbStatementSegment {
	segments := make([]vbStatementSegment, 0, 1)
	start := 0
	depth := 0
	for index := 0; index < len(line); index++ {
		switch line[index] {
		case '\'':
			index = len(line)
		case '"':
			index = skipVBString(line, index) - 1
		case '#':
			index = skipVBTypeDateLiteral(line, index) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				segments = appendVBStatementSegment(segments, line, lineOffset, start, index)
				start = index + 1
			}
		}
	}
	return appendVBStatementSegment(segments, line, lineOffset, start, len(line))
}

func skipVBTypeDateLiteral(line string, start int) int {
	for index := start + 1; index < len(line); index++ {
		if line[index] == '#' {
			return index + 1
		}
	}
	return len(line)
}

func vbscriptTypeStatementStartsRemComment(statement string) bool {
	start := leadingVBWhitespace(statement)
	end := readVBIdentifier(statement, start)
	if end == start || !strings.EqualFold(statement[start:end], "Rem") {
		return false
	}
	return true
}

func vbscriptHasConcreteType(typeName string) bool {
	return len(vbscriptConcreteTypeNames(typeName)) > 0
}

func vbscriptSimpleCallName(value string) string {
	value = strings.TrimSpace(value)
	nameEnd := readVBIdentifier(value, 0)
	if nameEnd == 0 {
		return ""
	}
	cursor := nameEnd
	for cursor < len(value) && isVBWhitespace(value[cursor]) {
		cursor++
	}
	if cursor >= len(value) || value[cursor] != '(' {
		return ""
	}
	return value[:nameEnd]
}

func vbscriptBareIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || readVBIdentifier(value, 0) != len(value) {
		return ""
	}
	return value
}

func (s *Server) vbscriptAssignmentTypeDiagnostics(parsed *core.ParsedDocument, info vbscriptTypeInfo) []lsp.Diagnostic {
	var diagnostics []lsp.Diagnostic
	for _, assignment := range vbscriptAssignments(parsed) {
		diagnostics = append(diagnostics, vbscriptAssignmentDiagnostics(parsed, assignment, info)...)
	}
	return diagnostics
}

// vbscriptIncludedAssignmentTypeDiagnosticsContextResult checks each included
// source span against the root owner's contract. The range and URI are
// produced from the included document so publication and quick fixes can
// target that document.
func (s *Server) vbscriptIncludedAssignmentTypeDiagnosticsContextResult(ctx context.Context, parsed *core.ParsedDocument, info vbscriptTypeInfo) ([]lsp.Diagnostic, bool) {
	if parsed == nil || len(parsed.Includes) == 0 {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	units, complete := s.vbscriptIncludeExecutionUnitsContext(ctx, parsed)
	if !complete {
		return nil, false
	}
	activeContracts := map[string]vbscriptGlobalTypeContract{}
	for lowerName, contract := range info.globalContracts {
		if contract.Start < 0 {
			activeContracts[lowerName] = contract
		}
	}
	diagnostics := make([]lsp.Diagnostic, 0)
	for _, unit := range units {
		if ctx.Err() != nil {
			return nil, false
		}
		if unit.document == nil || unit.start >= unit.end {
			continue
		}
		if unit.document == parsed {
			for lowerName, contract := range info.globalContracts {
				if contract.URI == parsed.URI && contract.Start >= 0 && contract.Start < unit.end {
					activeContracts[lowerName] = contract
				}
			}
			continue
		}
		unitInfo := info
		unitInfo.globalContracts = activeContracts
		for _, assignment := range vbscriptAssignments(unit.document) {
			if ctx.Err() != nil {
				return nil, false
			}
			if assignment.Scope != "" {
				continue
			}
			doc := core.NewTextDocument(unit.document.URI, "classic-asp", 0, unit.document.Text)
			offset := doc.OffsetAt(assignment.NameRange.Start)
			if offset < unit.start || offset >= unit.end {
				continue
			}
			for _, diagnostic := range vbscriptAssignmentDiagnostics(unit.document, assignment, unitInfo) {
				diagnostic.Data = vbscriptDiagnosticDataWithURI(diagnostic.Data, unit.document.URI)
				diagnostics = append(diagnostics, diagnostic)
			}
		}
	}
	return diagnostics, true
}

func vbscriptDiagnosticDataWithURI(value any, uri string) map[string]any {
	data, ok := value.(map[string]any)
	if !ok {
		data = map[string]any{}
	}
	data["uri"] = uri
	return data
}

func vbscriptGlobalContractAppliesToAssignment(parsed *core.ParsedDocument, assignment vbAssignment, contract vbscriptGlobalTypeContract) bool {
	if contract.Start < 0 || parsed == nil || contract.URI == "" || !strings.EqualFold(contract.URI, parsed.URI) {
		return true
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	return doc.OffsetAt(assignment.NameRange.Start) >= contract.Start
}

func vbscriptAssignmentDiagnostics(parsed *core.ParsedDocument, assignment vbAssignment, info vbscriptTypeInfo) []lsp.Diagnostic {
	actualExpression := inferVBScriptValueLiteralType(assignment.Value)
	actualType := firstVBTypeName(inferVBValueTypeForDocument(parsed, assignment.Value))
	if actualType == "" && !actualExpression.isUnknown() {
		actualType = actualExpression.primitiveName()
	}
	diagnostics := []lsp.Diagnostic{}
	if actualType != "" && isVBScriptObjectType(actualType, info) && !assignment.HasSet {
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    assignment.NameRange,
			Severity: lsp.DiagnosticSeverityWarning,
			Code:     "objectNeedsSet",
			Source:   vbscriptTypeDiagnosticSource,
			Message:  "objectNeedsSet: Object assignment to '" + assignment.Name + "' requires Set.",
			Data:     map[string]any{"name": assignment.Name, "type": actualType},
		})
	}
	if assignment.HasSet && isVBScriptScalarType(actualType) {
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    assignment.NameRange,
			Severity: lsp.DiagnosticSeverityWarning,
			Code:     "setScalar",
			Source:   vbscriptTypeDiagnosticSource,
			Message:  "setScalar: Scalar assignment to '" + assignment.Name + "' should not use Set.",
			Data:     map[string]any{"name": assignment.Name, "type": actualType},
		})
	}
	expectedTypeText := info.scopedExplicitTypeNames[vbscriptTypeScopeKey(parsed, assignment.Scope, assignment.Name)]
	if assignment.Scope == "" {
		if contract, ok := info.globalContracts[strings.ToLower(assignment.Name)]; ok && vbscriptGlobalContractAppliesToAssignment(parsed, assignment, contract) {
			expectedTypeText = contract.TypeName
		}
	}
	expectedType := strings.TrimSpace(expectedTypeText)
	actualCompatibilityType := actualType
	if !actualExpression.isUnknown() && (actualExpression.isLiteral() || actualExpression.kind == vbscriptTypeTemplate) {
		actualCompatibilityType = actualExpression.String()
	}
	if expectedType != "" && !vbscriptTypeExpressionsCompatible(expectedTypeText, actualCompatibilityType) {
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    assignment.NameRange,
			Severity: lsp.DiagnosticSeverityWarning,
			Code:     "typeMismatch",
			Source:   vbscriptTypeDiagnosticSource,
			Message:  "typeMismatch: Cannot assign " + actualType + " to '" + assignment.Name + "' typed as " + expectedType + ".",
			Data: map[string]any{
				"name": assignment.Name, "actual": actualType, "expected": expectedType,
				"actualType": actualType, "expectedType": expectedType,
			},
		})
	}
	return diagnostics
}

func parseVBAssignment(line string, lineOffset int, doc *core.TextDocument) (vbAssignment, bool) {
	cursor := leadingVBWhitespace(line)
	if cursor >= len(line) || line[cursor] == '\'' {
		return vbAssignment{}, false
	}
	firstEnd := readVBIdentifier(line, cursor)
	if firstEnd == cursor {
		return vbAssignment{}, false
	}
	first := strings.ToLower(line[cursor:firstEnd])
	hasSet := false
	setStart, setEnd := 0, 0
	if first == "set" || first == "let" {
		hasSet = first == "set"
		setStart, setEnd = lineOffset+cursor, lineOffset+firstEnd
		cursor = firstEnd
		for cursor < len(line) && isVBWhitespace(line[cursor]) {
			cursor++
		}
	}
	nameStart := cursor
	nameEnd := readVBIdentifier(line, nameStart)
	if nameEnd == nameStart {
		return vbAssignment{}, false
	}
	cursor = nameEnd
	for cursor < len(line) && isVBWhitespace(line[cursor]) {
		cursor++
	}
	if cursor >= len(line) || line[cursor] != '=' {
		return vbAssignment{}, false
	}
	value := stripVBScriptTrailingComment(line[cursor+1:])
	if value == "" {
		return vbAssignment{}, false
	}
	absoluteNameStart := lineOffset + nameStart
	absoluteNameEnd := lineOffset + nameEnd
	return vbAssignment{
		Name:      line[nameStart:nameEnd],
		HasSet:    hasSet,
		Value:     value,
		NameRange: doc.Range(absoluteNameStart, absoluteNameEnd),
		SetRange:  doc.Range(setStart, setEnd),
	}, true
}

func stripVBScriptTrailingComment(text string) string {
	inString := false
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '"':
			if inString && index+1 < len(text) && text[index+1] == '"' {
				index++
				continue
			}
			inString = !inString
		case '\'':
			if !inString {
				return strings.TrimSpace(text[:index])
			}
		}
	}
	return strings.TrimSpace(text)
}

func (s *Server) vbscriptMemberTypeDiagnostics(parsed *core.ParsedDocument, info vbscriptTypeInfo) []lsp.Diagnostic {
	var diagnostics []lsp.Diagnostic
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	for _, occurrence := range vbscriptTypeMemberOccurrences(parsed) {
		if len(occurrence.Parts) < 2 {
			continue
		}
		receiver := occurrence.Parts[0]
		memberName := occurrence.Parts[1]
		typeName := info.variableTypes[strings.ToLower(receiver)]
		scope := vbscriptScopeAtOffset(parsed, doc.OffsetAt(occurrence.Range.Start))
		if scopedTypeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, receiver)]; scopedTypeName != "" {
			typeName = scopedTypeName
		} else if vbscriptNameBoundInScopeAtOffset(parsed, receiver, scope, doc.OffsetAt(occurrence.Range.Start)) {
			typeName = ""
		}
		if typeName == "" {
			continue
		}
		member, checked := vbscriptUnionMember(typeName, memberName, info)
		if !checked {
			continue
		}
		if member == nil {
			diagnostics = append(diagnostics, lsp.Diagnostic{
				Range:    occurrence.Range,
				Severity: lsp.DiagnosticSeverityWarning,
				Code:     "missingMember",
				Source:   vbscriptTypeDiagnosticSource,
				Message:  "Type '" + typeName + "' has no member '" + memberName + "'.",
				Data:     map[string]any{"receiver": receiver, "member": memberName, "type": typeName, "typeName": typeName},
			})
			continue
		}
		contract := member.contract()
		if !contract.parameterKnown {
			continue
		}
		end := doc.OffsetAt(occurrence.Range.End)
		if actualCount, ok := vbscriptCallArgumentCountAt(parsed.Text, end); ok && (actualCount < contract.minimum || actualCount > contract.maximum) {
			diagnostic := vbscriptArgumentCountDiagnosticRange(occurrence.Range, memberName, contract.minimum, contract.maximum, actualCount)
			diagnostic.Data = map[string]any{
				"receiver": receiver, "name": memberName, "member": memberName,
				"type": typeName, "expected": contract.maximum, "expectedMin": contract.minimum,
				"expectedMax": contract.maximum, "actual": actualCount,
			}
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return diagnostics
}

func (s *Server) vbscriptCallTypeDiagnosticsContextResult(ctx context.Context, parsed *core.ParsedDocument, info vbscriptTypeInfo) ([]lsp.Diagnostic, bool) {
	if parsed == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	signatures := map[string]vbscript.Signature{}
	units, complete := s.vbscriptIncludeExecutionUnitsContext(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	included := make([]*core.ParsedDocument, 0, len(units))
	seenIncluded := map[string]struct{}{workspacepkg.FileIdentityKeyFromURI(parsed.URI): {}}
	for _, unit := range units {
		if unit.document == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(unit.document.URI)
		if _, seen := seenIncluded[key]; seen {
			continue
		}
		seenIncluded[key] = struct{}{}
		included = append(included, unit.document)
	}
	for _, document := range append([]*core.ParsedDocument{parsed}, included...) {
		if document == nil {
			continue
		}
		for key, signature := range vbscript.BuildSignatures(document) {
			if _, exists := signatures[key]; !exists {
				signatures[key] = signature
			}
		}
	}
	declarations := graphVBDeclarations(parsed)
	diagnostics := []lsp.Diagnostic{}
	for _, region := range parsed.Regions {
		if ctx.Err() != nil {
			return nil, false
		}
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, span := range vbIdentifierSpans(text) {
			if ctx.Err() != nil {
				return nil, false
			}
			start := region.ContentStart + span.Start
			end := region.ContentStart + span.End
			cursor := end
			for cursor < region.ContentEnd && isVBWhitespace(parsed.Text[cursor]) {
				cursor++
			}
			if cursor >= region.ContentEnd || parsed.Text[cursor] != '(' {
				continue
			}
			name := parsed.Text[start:end]
			nameRange := doc.Range(start, end)
			if signature, ok := signatures[strings.ToLower(name)]; ok && signature.NameRange == nameRange {
				continue
			}
			known := false
			minimum, maximum := -1, -1
			fullName := name
			if ownerStart, ownerEnd := vbMemberOwnerBeforeOffset(parsed.Text, start); ownerStart >= 0 {
				owner := parsed.Text[ownerStart:ownerEnd]
				fullName = owner + "." + name
				known, minimum, maximum = vbscriptKnownMemberCall(parsed, start, owner, name, info, declarations)
			} else if signature, ok := signatures[strings.ToLower(name)]; ok {
				known = true
				minimum, maximum = vbscriptParameterRange(signature.Parameters)
			} else if isDeclaredVBBuiltinOrKeywordForDocument(parsed, strings.ToLower(name)) {
				known = true
			}
			if known {
				if minimum >= 0 {
					if actual, ok := vbscriptCallArgumentCountAt(parsed.Text, end); ok && (actual < minimum || actual > maximum) {
						diagnostics = append(diagnostics, vbscriptArgumentCountDiagnosticRange(nameRange, fullName, minimum, maximum, actual))
					}
				}
				continue
			}
			if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
				continue
			}
			diagnostics = append(diagnostics, lsp.Diagnostic{
				Range:    nameRange,
				Severity: lsp.DiagnosticSeverityWarning,
				Code:     "unknownCall",
				Source:   vbscriptTypeDiagnosticSource,
				Message:  "Call target '" + fullName + "' is not known.",
				Data:     map[string]any{"name": fullName},
			})
		}
	}
	return diagnostics, true
}

func vbscriptKnownMemberCall(parsed *core.ParsedDocument, offset int, owner, name string, info vbscriptTypeInfo, declarations []vbUsageDeclaration) (bool, int, int) {
	if typeName, ok := vbscriptBuiltinGlobalObjectType(parsed, owner); ok {
		if member, exists := vbscriptBuiltinTypeMembers(typeName)[strings.ToLower(name)]; exists {
			if member.Signature != "" {
				count := len(vbscriptBuiltinMemberParameters(member))
				return true, count, count
			}
			return true, -1, -1
		}
		return false, -1, -1
	}
	if strings.EqualFold(owner, "Me") {
		for _, declaration := range declarations {
			if declaration.MemberOf != "" && strings.EqualFold(declaration.Name, name) {
				return true, -1, -1
			}
		}
		return false, -1, -1
	}
	typeName := info.variableTypes[strings.ToLower(owner)]
	scope := vbscriptScopeAtOffset(parsed, offset)
	if scopedTypeName := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, owner)]; scopedTypeName != "" {
		typeName = scopedTypeName
	} else if vbscriptNameBoundInScopeAtOffset(parsed, owner, scope, offset) {
		typeName = ""
	}
	if typeName == "" {
		return false, -1, -1
	}
	member, checked := vbscriptUnionMember(typeName, name, info)
	if !checked || member == nil {
		return false, -1, -1
	}
	return true, -1, -1
}

func vbscriptArgumentCountDiagnosticRange(r lsp.Range, name string, minimum, maximum, actual int) lsp.Diagnostic {
	expected := intString(maximum)
	if minimum != maximum {
		expected = intString(minimum) + "-" + intString(maximum)
	}
	return lsp.Diagnostic{
		Range:    r,
		Severity: lsp.DiagnosticSeverityWarning,
		Code:     "argumentCountMismatch",
		Source:   vbscriptTypeDiagnosticSource,
		Message:  "Argument count mismatch for '" + name + "': expected " + expected + ", got " + intString(actual) + ".",
		Data: map[string]any{
			"name": name, "expected": maximum, "expectedMin": minimum,
			"expectedMax": maximum, "actual": actual,
		},
	}
}

func vbscriptUnionMember(typeName string, memberName string, info vbscriptTypeInfo) (*vbscriptTypedMember, bool) {
	checked := false
	var common *vbscriptTypedMember
	candidates := vbscriptConcreteTypeNames(typeName)
	for _, candidate := range candidates {
		members := info.members[strings.ToLower(candidate)]
		if len(members) == 0 {
			if len(candidates) > 1 {
				return nil, true
			}
			return nil, false
		}
		checked = true
		member, ok := members[strings.ToLower(memberName)]
		if !ok {
			return nil, true
		}
		if common == nil {
			value := member
			common = &value
		} else {
			merged, ok := vbscriptIntersectTypedMembers(*common, member)
			if !ok {
				return nil, true
			}
			*common = merged
		}
	}
	return common, checked
}

func vbscriptTypeMemberOccurrences(parsed *core.ParsedDocument) []graphMemberOccurrence {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	var occurrences []graphMemberOccurrence
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			for _, match := range graphMemberChainPattern.FindAllStringIndex(line, -1) {
				start := lineStart + match[0]
				end := lineStart + match[1]
				if previousNonSpaceSameLine(parsed.Text, start) == '.' {
					continue
				}
				raw := parsed.Text[start:end]
				parts := graphMemberPathParts(raw)
				if len(parts) < 2 || isDeclaredVBBuiltinOrKeyword(strings.ToLower(parts[0])) {
					continue
				}
				occurrences = append(occurrences, graphMemberOccurrence{
					URI:          parsed.URI,
					Range:        doc.Range(start, end),
					FullPath:     strings.Join(parts, "."),
					ReceiverName: strings.Join(parts[:len(parts)-1], "."),
					MemberName:   parts[len(parts)-1],
					Parts:        parts,
				})
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	return occurrences
}

func previousNonSpaceSameLine(text string, offset int) byte {
	for i := offset - 1; i >= 0; i-- {
		switch text[i] {
		case ' ', '\t':
			continue
		case '\r', '\n':
			return 0
		default:
			return text[i]
		}
	}
	return 0
}

func vbscriptCallArgumentCountAt(text string, offset int) (int, bool) {
	cursor := offset
	for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t') {
		cursor++
	}
	if cursor >= len(text) || text[cursor] != '(' {
		return 0, false
	}
	cursor++
	depth := 1
	count := 0
	hasValue := false
	for cursor < len(text) {
		switch text[cursor] {
		case '"':
			if depth == 1 {
				hasValue = true
			}
			cursor = skipVBString(text, cursor)
			continue
		case '(':
			depth++
			hasValue = true
		case ')':
			depth--
			if depth == 0 {
				if hasValue {
					count++
				}
				return count, true
			}
		case ',':
			if depth == 1 {
				count++
				hasValue = false
			} else {
				hasValue = true
			}
		default:
			if depth == 1 && !isVBWhitespace(text[cursor]) {
				hasValue = true
			}
		}
		cursor++
	}
	return 0, false
}

func isVBScriptObjectType(typeName string, info vbscriptTypeInfo) bool {
	typeName = firstVBTypeName(typeName)
	if isLooseVBTypeName(typeName) || isVBScriptScalarType(typeName) {
		return false
	}
	_, configured := info.members[strings.ToLower(typeName)]
	return configured || typeName != ""
}

func isVBScriptScalarType(typeName string) bool {
	switch strings.ToLower(firstVBTypeName(typeName)) {
	case "string", "number", "boolean", "currency", "date", "integer", "long", "double", "single", "byte", "array", "decimal", "error", "empty", "null":
		return true
	default:
		return false
	}
}

func vbscriptTypesCompatible(expected, actual string) bool {
	if expectedType, expectedErr := parseVBScriptType(expected); expectedErr == nil {
		if actualType, actualErr := parseVBScriptType(actual); actualErr == nil {
			return vbscriptTypeAssignable(expectedType, actualType)
		}
	}
	expectedTypes := vbscriptAssignableTypeNames(expected)
	actualTypes := vbscriptAssignableTypeNames(actual)
	if len(expectedTypes) == 0 || len(actualTypes) == 0 {
		return true
	}
	for _, expectedType := range expectedTypes {
		for _, actualType := range actualTypes {
			if strings.EqualFold(expectedType, actualType) ||
				isVBScriptNumericFamilyType(expectedType) && isVBScriptNumericFamilyType(actualType) {
				return true
			}
		}
	}
	return false
}

func vbscriptAssignableTypeNames(typeName string) []string {
	names := []string{}
	parts := strings.Split(typeName, "|")
	if parsed, err := splitVBScriptTypeUnion(typeName); err == nil {
		parts = parsed
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.EqualFold(part, "Variant") {
			continue
		}
		names = append(names, part)
	}
	return names
}

func isVBScriptNumericFamilyType(typeName string) bool {
	switch strings.ToLower(firstVBTypeName(typeName)) {
	case "number", "currency", "integer", "long", "double", "single", "byte", "decimal":
		return true
	default:
		return false
	}
}
