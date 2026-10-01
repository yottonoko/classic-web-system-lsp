package lspserver

import (
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

// runtimeCacheOwnerLedger reference-counts the owners charged to parsedCache
// and DocumentStore. Its total matches the parsed-cache estimate plus the
// DocumentStore estimate (which excludes parsed-cache owners), so eviction can
// track freed bytes per entry without rescanning both caches after each step.
type runtimeCacheOwnerLedger struct {
	entryBytes int64
	ownerBytes int64
	entries    map[runtimeCacheLedgerEntryKey]runtimeCacheLedgerEntry
	parsed     map[*core.ParsedDocument]*runtimeCacheLedgerParsed
	source     map[parsedDocumentSourceOwner]*runtimeCacheLedgerOwner
	runtime    map[any]*runtimeCacheLedgerOwner
}

type runtimeCacheLedgerEntryKey struct {
	store bool
	key   string
}

type runtimeCacheLedgerEntry struct {
	bytes     int64
	parsed    *core.ParsedDocument
	source    parsedDocumentSourceOwner
	hasSource bool
}

type runtimeCacheLedgerOwner struct {
	refs  int
	bytes int64
}

type runtimeCacheLedgerParsed struct {
	refs    int
	bytes   int64
	sources []runtimeCacheLedgerSource
	runtime []core.RuntimeAnalysisMemoryOwner
}

type runtimeCacheLedgerSource struct {
	owner parsedDocumentSourceOwner
	bytes int64
}

func newRuntimeCacheOwnerLedger() *runtimeCacheOwnerLedger {
	return &runtimeCacheOwnerLedger{
		entries: map[runtimeCacheLedgerEntryKey]runtimeCacheLedgerEntry{},
		parsed:  map[*core.ParsedDocument]*runtimeCacheLedgerParsed{},
		source:  map[parsedDocumentSourceOwner]*runtimeCacheLedgerOwner{},
		runtime: map[any]*runtimeCacheLedgerOwner{},
	}
}

// runtimeCacheOwnerLedgerLocked snapshots both caches. The caller must hold s.mu.
func (s *Server) runtimeCacheOwnerLedgerLocked() *runtimeCacheOwnerLedger {
	ledger := newRuntimeCacheOwnerLedger()
	for key, entry := range s.parsedCache {
		ledger.addParsedEntry(key, entry)
	}
	if s.documentStore != nil {
		for key, cached := range s.documentStore.Cache {
			ledger.addStoreEntry(key, cached)
		}
	}
	return ledger
}

func (ledger *runtimeCacheOwnerLedger) total() int64 {
	return addRuntimeCacheBytes(ledger.entryBytes, ledger.ownerBytes)
}

func (ledger *runtimeCacheOwnerLedger) addParsedEntry(key string, entry parsedDocumentCacheEntry) {
	record := runtimeCacheLedgerEntry{bytes: int64(len(key))*2 + 128}
	text := entry.Text
	if entry.Parsed == nil {
		record.bytes += int64(len(entry.DefaultLanguage))*2 + 16
	} else {
		record.parsed = entry.Parsed
		if stringBackingShared(entry.Text, entry.Parsed.Text) {
			text = ""
		}
	}
	record.source, record.hasSource = parsedDocumentSourceOwnerForText(text)
	ledger.addEntry(runtimeCacheLedgerEntryKey{key: key}, record, int64(len(text))*2+16)
}

func (ledger *runtimeCacheOwnerLedger) addStoreEntry(key string, cached *workspacepkg.CachedDocument) {
	record := runtimeCacheLedgerEntry{bytes: estimateDocumentStoreEntryMetadataBytes(key, cached)}
	text := ""
	if cached != nil {
		record.bytes = addRuntimeCacheBytes(record.bytes, estimateDocumentStoreAuxiliaryBytes(cached))
		if parsed, ok := cached.Parsed.(*core.ParsedDocument); ok {
			record.parsed = parsed
		} else if cached.Parsed != nil {
			record.bytes = addRuntimeCacheBytes(record.bytes, workspacepkg.EstimateJSONBytes(cached.Parsed, 256))
		}
		text = cached.Text
		record.source, record.hasSource = parsedDocumentSourceOwnerForText(text)
	}
	ledger.addEntry(runtimeCacheLedgerEntryKey{store: true, key: key}, record, int64(len(text))*2+16)
}

func (ledger *runtimeCacheOwnerLedger) removeParsedEntry(key string) {
	ledger.removeEntry(runtimeCacheLedgerEntryKey{key: key})
}

func (ledger *runtimeCacheOwnerLedger) removeStoreEntry(key string) {
	ledger.removeEntry(runtimeCacheLedgerEntryKey{store: true, key: key})
}

func (ledger *runtimeCacheOwnerLedger) addEntry(key runtimeCacheLedgerEntryKey, record runtimeCacheLedgerEntry, sourceBytes int64) {
	ledger.removeEntry(key)
	ledger.entries[key] = record
	ledger.entryBytes += nonNegativeParsedDocumentBytes(record.bytes)
	if record.parsed != nil {
		ledger.retainParsed(record.parsed)
	}
	if record.hasSource {
		ledger.retainSource(record.source, sourceBytes)
	}
}

func (ledger *runtimeCacheOwnerLedger) removeEntry(key runtimeCacheLedgerEntryKey) {
	record, ok := ledger.entries[key]
	if !ok {
		return
	}
	delete(ledger.entries, key)
	ledger.entryBytes -= nonNegativeParsedDocumentBytes(record.bytes)
	if record.parsed != nil {
		ledger.releaseParsed(record.parsed)
	}
	if record.hasSource {
		ledger.releaseSource(record.source)
	}
}

func (ledger *runtimeCacheOwnerLedger) retainParsed(parsed *core.ParsedDocument) {
	if owner := ledger.parsed[parsed]; owner != nil {
		owner.refs++
		return
	}
	owner := &runtimeCacheLedgerParsed{refs: 1, bytes: nonNegativeParsedDocumentBytes(parsed.EstimateStructuralBytesWithoutRevisionText())}
	if source, ok := parsedDocumentSourceOwnerForText(parsed.Text); ok {
		owner.sources = append(owner.sources, runtimeCacheLedgerSource{owner: source, bytes: int64(len(parsed.Text))*2 + 16})
	}
	if previous, ok := parsed.PreviousRevisionText(); ok {
		if source, ok := parsedDocumentSourceOwnerForText(previous); ok {
			owner.sources = append(owner.sources, runtimeCacheLedgerSource{owner: source, bytes: int64(len(previous))*2 + 16})
		}
	}
	owner.runtime = parsed.RuntimeAnalysisMemoryOwners()
	ledger.parsed[parsed] = owner
	ledger.ownerBytes += owner.bytes
	for _, source := range owner.sources {
		ledger.retainSource(source.owner, source.bytes)
	}
	for _, runtimeOwner := range owner.runtime {
		ledger.retainRuntime(runtimeOwner.Identity, runtimeOwner.Bytes)
	}
}

func (ledger *runtimeCacheOwnerLedger) releaseParsed(parsed *core.ParsedDocument) {
	owner := ledger.parsed[parsed]
	if owner == nil {
		return
	}
	owner.refs--
	if owner.refs > 0 {
		return
	}
	delete(ledger.parsed, parsed)
	ledger.ownerBytes -= owner.bytes
	for _, source := range owner.sources {
		ledger.releaseSource(source.owner)
	}
	for _, runtimeOwner := range owner.runtime {
		ledger.releaseRuntime(runtimeOwner.Identity)
	}
}

func (ledger *runtimeCacheOwnerLedger) retainSource(source parsedDocumentSourceOwner, bytes int64) {
	ledger.ownerBytes += retainRuntimeCacheLedgerOwner(ledger.source, source, bytes)
}

func (ledger *runtimeCacheOwnerLedger) releaseSource(source parsedDocumentSourceOwner) {
	ledger.ownerBytes -= releaseRuntimeCacheLedgerOwner(ledger.source, source)
}

func (ledger *runtimeCacheOwnerLedger) retainRuntime(identity any, bytes int64) {
	if identity == nil {
		return
	}
	ledger.ownerBytes += retainRuntimeCacheLedgerOwner(ledger.runtime, identity, bytes)
}

func (ledger *runtimeCacheOwnerLedger) releaseRuntime(identity any) {
	if identity == nil {
		return
	}
	ledger.ownerBytes -= releaseRuntimeCacheLedgerOwner(ledger.runtime, identity)
}

// retainRuntimeCacheLedgerOwner returns the growth of the charged bytes. Shared
// owners are charged at the largest size reported by any holder.
func retainRuntimeCacheLedgerOwner[K comparable](owners map[K]*runtimeCacheLedgerOwner, key K, bytes int64) int64 {
	bytes = nonNegativeParsedDocumentBytes(bytes)
	owner := owners[key]
	if owner == nil {
		owners[key] = &runtimeCacheLedgerOwner{refs: 1, bytes: bytes}
		return bytes
	}
	owner.refs++
	if bytes <= owner.bytes {
		return 0
	}
	growth := bytes - owner.bytes
	owner.bytes = bytes
	return growth
}

func releaseRuntimeCacheLedgerOwner[K comparable](owners map[K]*runtimeCacheLedgerOwner, key K) int64 {
	owner := owners[key]
	if owner == nil {
		return 0
	}
	owner.refs--
	if owner.refs > 0 {
		return 0
	}
	delete(owners, key)
	return owner.bytes
}
