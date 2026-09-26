package workspace

type SourceMetadata struct {
	FileName    string `json:"fileName"`
	MtimeMS     int64  `json:"mtimeMs"`
	Size        int64  `json:"size"`
	ContentHash string `json:"contentHash,omitempty"`
}

type IncludeGraphEntry struct {
	FileName        string             `json:"fileName"`
	Source          SourceMetadata     `json:"source"`
	TargetFileNames []string           `json:"targetFileNames"`
	References      []IncludeReference `json:"references,omitempty"`
	RefsFingerprint string             `json:"refsFingerprint"`
}

type IncludeReference struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

type IncludeGraphSnapshot struct {
	SettingsKey string              `json:"settingsKey"`
	Entries     []IncludeGraphEntry `json:"entries"`
}

// IncludeGraphClosure is one deterministic include-graph traversal result.
// IdentityKeys can be used directly as a membership input without normalizing
// FileNames again.
type IncludeGraphClosure struct {
	FileNames    []string
	IdentityKeys []string
	membership   map[string]struct{}
}

// Contains reports whether fileName belongs to the closure.
func (c IncludeGraphClosure) Contains(fileName string) bool {
	_, ok := c.membership[FileIdentityKeyFromFileName(fileName)]
	return ok
}

// IdentityMembership returns an owned identity-key set for bulk membership
// checks by callers.
func (c IncludeGraphClosure) IdentityMembership() map[string]struct{} {
	result := make(map[string]struct{}, len(c.membership))
	for key := range c.membership {
		result[key] = struct{}{}
	}
	return result
}

type includeForwardEntry struct {
	fileName        string
	source          SourceMetadata
	targetFileNames map[string]struct{}
	targetOrder     []string
	references      []IncludeReference
	refsFingerprint string
	ephemeral       bool
}

type WorkspaceIncludeGraph struct {
	forward           map[string]*includeForwardEntry
	ownerOrder        []string
	reverse           map[string]map[string]struct{}
	reverseOwnerOrder map[string][]string
	fileNames         map[string]string
	settingsKey       string
}

func NewWorkspaceIncludeGraph() *WorkspaceIncludeGraph {
	g := &WorkspaceIncludeGraph{}
	g.Reset("")
	return g
}

func (g *WorkspaceIncludeGraph) SettingsKey() string {
	return g.settingsKey
}

func (g *WorkspaceIncludeGraph) Size() int {
	return len(g.forward)
}

func (g *WorkspaceIncludeGraph) Reset(settingsKey string) {
	g.forward = map[string]*includeForwardEntry{}
	g.ownerOrder = nil
	g.reverse = map[string]map[string]struct{}{}
	g.reverseOwnerOrder = map[string][]string{}
	g.fileNames = map[string]string{}
	g.settingsKey = settingsKey
}

func (g *WorkspaceIncludeGraph) Restore(snapshot IncludeGraphSnapshot) {
	g.Reset(snapshot.SettingsKey)
	for _, entry := range snapshot.Entries {
		g.UpsertWithReferences(entry.FileName, entry.Source, entry.TargetFileNames, entry.References, entry.RefsFingerprint)
	}
}

func (g *WorkspaceIncludeGraph) Snapshot(settingsKey string) (IncludeGraphSnapshot, bool) {
	if settingsKey == "" {
		settingsKey = g.settingsKey
	}
	if settingsKey == "" {
		return IncludeGraphSnapshot{}, false
	}
	entries := make([]IncludeGraphEntry, 0, len(g.forward))
	for _, ownerKey := range g.ownerOrder {
		entry := g.forward[ownerKey]
		if entry == nil {
			continue
		}
		if entry.ephemeral {
			continue
		}
		entries = append(entries, IncludeGraphEntry{
			FileName:        entry.fileName,
			Source:          entry.source,
			TargetFileNames: g.targetFileNames(entry.targetOrder),
			References:      append([]IncludeReference(nil), entry.references...),
			RefsFingerprint: entry.refsFingerprint,
		})
	}
	return IncludeGraphSnapshot{SettingsKey: settingsKey, Entries: entries}, true
}

func (g *WorkspaceIncludeGraph) Get(fileName string) (IncludeGraphEntry, bool) {
	entry, ok := g.forward[FileIdentityKeyFromFileName(fileName)]
	if !ok {
		return IncludeGraphEntry{}, false
	}
	return IncludeGraphEntry{
		FileName:        entry.fileName,
		Source:          entry.source,
		TargetFileNames: g.targetFileNames(entry.targetOrder),
		References:      append([]IncludeReference(nil), entry.references...),
		RefsFingerprint: entry.refsFingerprint,
	}, true
}

func (g *WorkspaceIncludeGraph) Upsert(fileName string, source SourceMetadata, targetFileNames []string, refsFingerprint string) {
	g.UpsertWithReferences(fileName, source, targetFileNames, nil, refsFingerprint)
}

func (g *WorkspaceIncludeGraph) UpsertWithReferences(fileName string, source SourceMetadata, targetFileNames []string, references []IncludeReference, refsFingerprint string) {
	g.upsertEntry(fileName, source, targetFileNames, references, refsFingerprint, false)
}

// ApplyEdgeDelta updates one document's direct include edges and reports the
// origins whose transitive include topology may have changed. The affected
// closure is the stable union of reverse reachability before and after the
// update, including fileName exactly once.
func (g *WorkspaceIncludeGraph) ApplyEdgeDelta(fileName string, source SourceMetadata, targetFileNames []string, references []IncludeReference, refsFingerprint string) (IncludeGraphClosure, bool) {
	ownerKey := FileIdentityKeyFromFileName(fileName)
	targetKeys := orderedIncludeTargetKeys(targetFileNames)
	existing := g.forward[ownerKey]
	if existing == nil && len(targetKeys) == 0 {
		g.upsertEntry(fileName, source, targetFileNames, references, refsFingerprint, false)
		return IncludeGraphClosure{membership: map[string]struct{}{}}, false
	}
	if existing != nil && equalIdentityOrder(existing.targetOrder, targetKeys) {
		existing.fileName = fileName
		existing.source = source
		existing.references = append([]IncludeReference(nil), references...)
		existing.refsFingerprint = refsFingerprint
		existing.ephemeral = false
		g.fileNames[ownerKey] = fileName
		for _, targetFileName := range targetFileNames {
			g.rememberFileName(FileIdentityKeyFromFileName(targetFileName), targetFileName)
		}
		return IncludeGraphClosure{membership: map[string]struct{}{}}, false
	}

	oldClosure := g.ReverseClosure([]string{fileName})
	g.upsertEntry(fileName, source, targetFileNames, references, refsFingerprint, false)
	newClosure := g.ReverseClosure([]string{fileName})
	return unionIncludeGraphClosures(oldClosure, newClosure), true
}

func (g *WorkspaceIncludeGraph) UpsertEphemeral(fileName string, targetFileNames []string, refsFingerprint string) {
	if refsFingerprint == "" {
		refsFingerprint = "ephemeral"
	}
	g.upsertEntry(fileName, SourceMetadata{FileName: fileName}, targetFileNames, nil, refsFingerprint, true)
}

func (g *WorkspaceIncludeGraph) RecordEphemeralDependency(fileName, targetFileName string) {
	ownerKey := FileIdentityKeyFromFileName(fileName)
	targetKey := FileIdentityKeyFromFileName(targetFileName)
	g.rememberFileName(ownerKey, fileName)
	g.rememberFileName(targetKey, targetFileName)
	existing := g.forward[ownerKey]
	if existing == nil {
		g.UpsertEphemeral(fileName, []string{targetFileName}, "ephemeral")
		return
	}
	if _, ok := existing.targetFileNames[targetKey]; ok {
		return
	}
	existing.targetFileNames[targetKey] = struct{}{}
	existing.targetOrder = append(existing.targetOrder, targetKey)
	existing.ephemeral = true
	g.addReverse(targetKey, ownerKey)
}

func (g *WorkspaceIncludeGraph) Delete(fileName string) {
	g.remove(FileIdentityKeyFromFileName(fileName))
}

// ApplyDeleteDelta removes one document and reports the origins whose
// transitive include topology depended on the deleted entry.
func (g *WorkspaceIncludeGraph) ApplyDeleteDelta(fileName string) (IncludeGraphClosure, bool) {
	ownerKey := FileIdentityKeyFromFileName(fileName)
	if g.forward[ownerKey] == nil && len(g.reverse[ownerKey]) == 0 {
		return IncludeGraphClosure{membership: map[string]struct{}{}}, false
	}
	affected := g.ReverseClosure([]string{fileName})
	g.remove(ownerKey)
	for _, includingOwnerKey := range append([]string(nil), g.reverseOwnerOrder[ownerKey]...) {
		includingOwner := g.forward[includingOwnerKey]
		if includingOwner == nil {
			continue
		}
		delete(includingOwner.targetFileNames, ownerKey)
		for index, targetKey := range includingOwner.targetOrder {
			if targetKey == ownerKey {
				includingOwner.targetOrder = append(includingOwner.targetOrder[:index], includingOwner.targetOrder[index+1:]...)
				break
			}
		}
	}
	delete(g.reverse, ownerKey)
	delete(g.reverseOwnerOrder, ownerKey)
	delete(g.fileNames, ownerKey)
	return affected, true
}

// ForwardClosure returns each seed and every file it transitively includes in
// breadth-first include order. Each identity appears exactly once.
func (g *WorkspaceIncludeGraph) ForwardClosure(fileNames []string) IncludeGraphClosure {
	return g.closure(fileNames, false, true)
}

// ReverseClosure returns each seed and every transitive including owner in
// breadth-first dependency order. Each identity appears exactly once.
func (g *WorkspaceIncludeGraph) ReverseClosure(fileNames []string) IncludeGraphClosure {
	return g.closure(fileNames, true, true)
}

// ReferenceScope returns the complete include scope for workspace-reference
// analysis rooted at fileNames.
func (g *WorkspaceIncludeGraph) ReferenceScope(fileNames []string) IncludeGraphClosure {
	return g.ForwardClosure(fileNames)
}

// DependentClosure returns every direct or transitive owner affected by the
// target files, excluding the input identities.
func (g *WorkspaceIncludeGraph) DependentClosure(targetFileNames []string) IncludeGraphClosure {
	return g.closure(targetFileNames, true, false)
}

// AffectedScope returns changed files together with every direct or transitive
// owner that depends on them. It is suitable for targeted topology invalidation.
func (g *WorkspaceIncludeGraph) AffectedScope(changedFileNames []string) IncludeGraphClosure {
	return g.ReverseClosure(changedFileNames)
}

func (g *WorkspaceIncludeGraph) CandidatesForTargets(targetFileNames []string) []string {
	ownerKeys := map[string]struct{}{}
	for _, targetFileName := range targetFileNames {
		targetKey := FileIdentityKeyFromFileName(targetFileName)
		for _, ownerKey := range g.reverseOrder(targetKey) {
			ownerKeys[ownerKey] = struct{}{}
		}
	}
	result := []string{}
	seen := map[string]struct{}{}
	for _, targetFileName := range targetFileNames {
		targetKey := FileIdentityKeyFromFileName(targetFileName)
		for _, ownerKey := range g.reverseOrder(targetKey) {
			if _, ok := ownerKeys[ownerKey]; !ok {
				continue
			}
			if _, ok := seen[ownerKey]; ok {
				continue
			}
			if entry := g.forward[ownerKey]; entry != nil {
				result = append(result, entry.fileName)
				seen[ownerKey] = struct{}{}
			}
		}
	}
	return result
}

func (g *WorkspaceIncludeGraph) TargetFileNamesForOwner(fileName string) []string {
	entry := g.forward[FileIdentityKeyFromFileName(fileName)]
	if entry == nil {
		return nil
	}
	return g.targetFileNames(entry.targetOrder)
}

func (g *WorkspaceIncludeGraph) DependsOnAnyTarget(ownerFileName string, targetFileNames []string, transitive bool) bool {
	targetKeys := map[string]struct{}{}
	for _, targetFileName := range targetFileNames {
		targetKeys[FileIdentityKeyFromFileName(targetFileName)] = struct{}{}
	}
	if len(targetKeys) == 0 {
		return false
	}
	queue := []string{FileIdentityKeyFromFileName(ownerFileName)}
	visited := map[string]struct{}{}
	for len(queue) > 0 {
		ownerKey := queue[0]
		queue = queue[1:]
		if _, ok := visited[ownerKey]; ok {
			continue
		}
		visited[ownerKey] = struct{}{}
		entry := g.forward[ownerKey]
		if entry == nil {
			continue
		}
		for _, targetKey := range entry.targetOrder {
			if _, ok := targetKeys[targetKey]; ok {
				return true
			}
			if transitive {
				if _, ok := visited[targetKey]; !ok {
					queue = append(queue, targetKey)
				}
			}
		}
	}
	return false
}

func (g *WorkspaceIncludeGraph) DependentFileNamesForTargets(targetFileNames []string, transitive bool) []string {
	resultKeys := map[string]struct{}{}
	resultOrder := []string{}
	queue := make([]string, 0, len(targetFileNames))
	for _, targetFileName := range targetFileNames {
		queue = append(queue, FileIdentityKeyFromFileName(targetFileName))
	}
	visitedTargets := map[string]struct{}{}
	for len(queue) > 0 {
		targetKey := queue[0]
		queue = queue[1:]
		if _, ok := visitedTargets[targetKey]; ok {
			continue
		}
		visitedTargets[targetKey] = struct{}{}
		for _, ownerKey := range g.reverseOrder(targetKey) {
			if _, ok := resultKeys[ownerKey]; !ok {
				resultKeys[ownerKey] = struct{}{}
				resultOrder = append(resultOrder, ownerKey)
			}
			if transitive {
				if _, ok := visitedTargets[ownerKey]; !ok {
					queue = append(queue, ownerKey)
				}
			}
		}
	}
	result := make([]string, 0, len(resultKeys))
	for _, ownerKey := range resultOrder {
		if entry := g.forward[ownerKey]; entry != nil {
			result = append(result, entry.fileName)
		}
	}
	return result
}

func (g *WorkspaceIncludeGraph) closure(fileNames []string, reverse, includeSeeds bool) IncludeGraphClosure {
	queue := make([]string, 0, len(fileNames))
	seedKeys := make(map[string]struct{}, len(fileNames))
	seedFileNames := make(map[string]string, len(fileNames))
	for _, fileName := range fileNames {
		key := FileIdentityKeyFromFileName(fileName)
		if _, exists := seedKeys[key]; exists {
			continue
		}
		seedKeys[key] = struct{}{}
		seedFileNames[key] = fileName
		queue = append(queue, key)
	}
	visited := make(map[string]struct{}, len(queue))
	result := IncludeGraphClosure{membership: map[string]struct{}{}}
	for cursor := 0; cursor < len(queue); cursor++ {
		key := queue[cursor]
		if _, exists := visited[key]; exists {
			continue
		}
		visited[key] = struct{}{}
		_, seed := seedKeys[key]
		if includeSeeds || !seed {
			result.IdentityKeys = append(result.IdentityKeys, key)
			fileName := g.fileName(key)
			if seedName := seedFileNames[key]; seedName != "" {
				fileName = seedName
			}
			result.FileNames = append(result.FileNames, fileName)
			result.membership[key] = struct{}{}
		}
		if reverse {
			queue = append(queue, g.reverseOrder(key)...)
			continue
		}
		if entry := g.forward[key]; entry != nil {
			queue = append(queue, entry.targetOrder...)
		}
	}
	return result
}

func (g *WorkspaceIncludeGraph) ClearEphemeral() {
	for ownerKey, entry := range g.forward {
		if entry.ephemeral {
			g.remove(ownerKey)
		}
	}
}

func (g *WorkspaceIncludeGraph) upsertEntry(fileName string, source SourceMetadata, targetFileNames []string, references []IncludeReference, refsFingerprint string, ephemeral bool) {
	ownerKey := FileIdentityKeyFromFileName(fileName)
	g.remove(ownerKey)
	g.fileNames[ownerKey] = fileName
	targets := map[string]struct{}{}
	targetOrder := []string{}
	for _, targetFileName := range targetFileNames {
		targetKey := FileIdentityKeyFromFileName(targetFileName)
		g.rememberFileName(targetKey, targetFileName)
		if _, ok := targets[targetKey]; !ok {
			targets[targetKey] = struct{}{}
			targetOrder = append(targetOrder, targetKey)
		}
	}
	g.forward[ownerKey] = &includeForwardEntry{
		fileName:        fileName,
		source:          source,
		targetFileNames: targets,
		targetOrder:     targetOrder,
		references:      append([]IncludeReference(nil), references...),
		refsFingerprint: refsFingerprint,
		ephemeral:       ephemeral,
	}
	g.ownerOrder = append(g.ownerOrder, ownerKey)
	for _, targetKey := range targetOrder {
		g.addReverse(targetKey, ownerKey)
	}
}

func (g *WorkspaceIncludeGraph) addReverse(targetKey, ownerKey string) {
	owners := g.reverse[targetKey]
	if owners == nil {
		owners = map[string]struct{}{}
		g.reverse[targetKey] = owners
	}
	if _, exists := owners[ownerKey]; exists {
		return
	}
	owners[ownerKey] = struct{}{}
	g.reverseOwnerOrder[targetKey] = append(g.reverseOwnerOrder[targetKey], ownerKey)
}

func (g *WorkspaceIncludeGraph) remove(ownerKey string) {
	existing := g.forward[ownerKey]
	if existing == nil {
		return
	}
	for targetKey := range existing.targetFileNames {
		owners := g.reverse[targetKey]
		delete(owners, ownerKey)
		if len(owners) == 0 {
			delete(g.reverse, targetKey)
			delete(g.reverseOwnerOrder, targetKey)
		} else {
			ordered := g.reverseOwnerOrder[targetKey]
			for index, key := range ordered {
				if key == ownerKey {
					g.reverseOwnerOrder[targetKey] = append(ordered[:index], ordered[index+1:]...)
					break
				}
			}
		}
	}
	delete(g.forward, ownerKey)
	for i, key := range g.ownerOrder {
		if key == ownerKey {
			g.ownerOrder = append(g.ownerOrder[:i], g.ownerOrder[i+1:]...)
			break
		}
	}
}

func (g *WorkspaceIncludeGraph) reverseOrder(targetKey string) []string {
	return g.reverseOwnerOrder[targetKey]
}

func (g *WorkspaceIncludeGraph) rememberFileName(identityKey, fileName string) {
	if _, exists := g.fileNames[identityKey]; !exists {
		g.fileNames[identityKey] = fileName
	}
}

func (g *WorkspaceIncludeGraph) fileName(identityKey string) string {
	if fileName := g.fileNames[identityKey]; fileName != "" {
		return fileName
	}
	if entry := g.forward[identityKey]; entry != nil && entry.fileName != "" {
		return entry.fileName
	}
	return identityKey
}

func (g *WorkspaceIncludeGraph) targetFileNames(identityKeys []string) []string {
	result := make([]string, len(identityKeys))
	for index, identityKey := range identityKeys {
		result[index] = g.fileName(identityKey)
	}
	return result
}

func orderedIncludeTargetKeys(targetFileNames []string) []string {
	result := make([]string, 0, len(targetFileNames))
	seen := make(map[string]struct{}, len(targetFileNames))
	for _, targetFileName := range targetFileNames {
		key := FileIdentityKeyFromFileName(targetFileName)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func equalIdentityOrder(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func unionIncludeGraphClosures(first, second IncludeGraphClosure) IncludeGraphClosure {
	result := IncludeGraphClosure{membership: map[string]struct{}{}}
	appendClosure := func(closure IncludeGraphClosure) {
		for index, key := range closure.IdentityKeys {
			if _, exists := result.membership[key]; exists {
				continue
			}
			result.membership[key] = struct{}{}
			result.IdentityKeys = append(result.IdentityKeys, key)
			result.FileNames = append(result.FileNames, closure.FileNames[index])
		}
	}
	appendClosure(first)
	appendClosure(second)
	return result
}
