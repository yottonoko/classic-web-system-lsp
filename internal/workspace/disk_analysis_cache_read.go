package workspace

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

const maxDiskCacheRecordBytes = 64 * 1024 * 1024

var errDiskCacheRecordTooLarge = errors.New("disk analysis cache record exceeds size limit")

func (c *DiskAnalysisCache) readFileEntry(lookup DiskAnalysisCacheLookup) (persistedDiskEntry, bool) {
	entry, ok := c.readEntry(diskCacheFilesBucket, c.keyForLookup(lookup))
	if !ok || !c.matches(entry, lookup) {
		return persistedDiskEntry{}, false
	}
	return entry, true
}

func (c *DiskAnalysisCache) readWorkspaceEntry(settingsKey string, kind DiskCacheEntryKind) (persistedDiskEntry, bool) {
	entry, ok := c.readEntry(diskCacheWorkspaceBucket, c.keyForWorkspace(settingsKey, kind))
	if !ok || !c.matchesWorkspace(entry, settingsKey, kind) {
		return persistedDiskEntry{}, false
	}
	return entry, true
}

func (c *DiskAnalysisCache) readEntry(bucket, key []byte) (persistedDiskEntry, bool) {
	if !c.enabled || c.isClosed() {
		return persistedDiskEntry{}, false
	}
	mapKey := diskCachePendingKey(bucket, key)
	c.pendingMu.Lock()
	pending, ok := c.pending[mapKey]
	if !ok {
		pending, ok = c.inFlight[mapKey]
	}
	c.pendingMu.Unlock()
	if ok {
		if pending.components != 0 {
			if stored, storedOK := c.readStoredEntry(bucket, key); storedOK {
				return clonePersistedDiskEntry(mergeFileBundleEntry(stored, pending.entry, pending.components))
			}
		}
		return clonePersistedDiskEntry(pending.entry)
	}
	return c.readStoredEntry(bucket, key)
}

func clonePersistedDiskEntry(entry persistedDiskEntry) (persistedDiskEntry, bool) {
	return clonePersistedDiskEntryValue(entry), true
}

func clonePersistedDiskEntryValue(entry persistedDiskEntry) persistedDiskEntry {
	cloned := entry
	cloned.Parsed = cloneDiskParsedDocument(entry.Parsed)
	cloned.PublicSignature = cloneDiskCacheValue(entry.PublicSignature)
	cloned.Diagnostics = cloneDiskDiagnostics(entry.Diagnostics)
	cloned.BuilderState = cloneDiskBuilderState(entry.BuilderState)
	cloned.Summary = cloneDiskFileAnalysisSummary(entry.Summary)
	cloned.WorkspaceEntries = append([]DiskWorkspaceIndexedDocument(nil), entry.WorkspaceEntries...)
	cloned.WorkspaceIncludeGraphEntries = cloneIncludeGraphEntries(entry.WorkspaceIncludeGraphEntries)
	cloned.GraphPayload = append(json.RawMessage(nil), entry.GraphPayload...)
	cloned.WorkspaceReferenceBatch = append(json.RawMessage(nil), entry.WorkspaceReferenceBatch...)
	cloned.WorkspaceLegacyUndefinedGlobals = append(json.RawMessage(nil), entry.WorkspaceLegacyUndefinedGlobals...)
	cloned.FileAnalysisSnapshot = append(json.RawMessage(nil), entry.FileAnalysisSnapshot...)
	return cloned
}

func cloneDiskBuilderState(state *DiskAnalysisBuilderState) *DiskAnalysisBuilderState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.PublicSignature = cloneDiskCacheValue(state.PublicSignature)
	cloned.IncludeDeps = cloneDiskCacheValues(state.IncludeDeps)
	cloned.ExternalRefUsageKeys = append([]string(nil), state.ExternalRefUsageKeys...)
	if state.DiagnosticsLayerFingerprints != nil {
		cloned.DiagnosticsLayerFingerprints = make(map[string]string, len(state.DiagnosticsLayerFingerprints))
		for key, value := range state.DiagnosticsLayerFingerprints {
			cloned.DiagnosticsLayerFingerprints[key] = value
		}
	}
	return &cloned
}

func cloneDiskFileAnalysisSummary(summary DiskFileAnalysisSummary) DiskFileAnalysisSummary {
	cloned := summary
	cloned.LanguageRegions = append([]core.Region(nil), summary.LanguageRegions...)
	cloned.IncludeRefs = append([]DiskIncludeRef(nil), summary.IncludeRefs...)
	cloned.Diagnostics = cloneDiskDiagnostics(summary.Diagnostics)
	return cloned
}

func cloneIncludeGraphEntries(entries []IncludeGraphEntry) []IncludeGraphEntry {
	cloned := append([]IncludeGraphEntry(nil), entries...)
	for index := range cloned {
		cloned[index].TargetFileNames = append([]string(nil), entries[index].TargetFileNames...)
		cloned[index].References = append([]IncludeReference(nil), entries[index].References...)
	}
	return cloned
}

func cloneDiskDiagnostics(diagnostics []lsp.Diagnostic) []lsp.Diagnostic {
	cloned := append([]lsp.Diagnostic(nil), diagnostics...)
	for index := range cloned {
		cloned[index].Tags = append([]lsp.DiagnosticTag(nil), diagnostics[index].Tags...)
		cloned[index].Code = cloneDiskCacheValue(diagnostics[index].Code)
		cloned[index].Data = cloneDiskCacheValue(diagnostics[index].Data)
	}
	return cloned
}

func cloneDiskCacheValues(values []any) []any {
	if values == nil {
		return nil
	}
	cloned := make([]any, len(values))
	for index, value := range values {
		cloned[index] = cloneDiskCacheValue(value)
	}
	return cloned
}

func cloneDiskCacheValue(value any) (cloned any) {
	if value == nil {
		return nil
	}
	cloned = value
	defer func() {
		if recover() != nil {
			cloned = value
		}
	}()
	copy := cloneDiskCacheReflect(reflect.ValueOf(value))
	if copy.IsValid() && copy.CanInterface() {
		cloned = copy.Interface()
	}
	return cloned
}

func cloneDiskCacheReflect(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return cloneDiskCacheReflect(value.Elem())
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneDiskCacheReflect(value.Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			key := cloneDiskCacheReflect(iter.Key())
			item := cloneDiskCacheReflect(iter.Value())
			if !key.IsValid() || !item.IsValid() {
				continue
			}
			if !key.Type().AssignableTo(value.Type().Key()) || !item.Type().AssignableTo(value.Type().Elem()) {
				continue
			}
			cloned.SetMapIndex(key, item)
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := range value.Len() {
			cloned.Index(index).Set(cloneDiskCacheReflect(value.Index(index)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for index := range value.Len() {
			cloned.Index(index).Set(cloneDiskCacheReflect(value.Index(index)))
		}
		return cloned
	case reflect.Struct:
		cloned := reflect.New(value.Type()).Elem()
		cloned.Set(value)
		for index := range value.NumField() {
			field := cloned.Field(index)
			if !field.CanSet() || value.Type().Field(index).PkgPath != "" {
				continue
			}
			field.Set(cloneDiskCacheReflect(value.Field(index)))
		}
		return cloned
	default:
		return value
	}
}

func (c *DiskAnalysisCache) readStoredEntry(bucket, key []byte) (persistedDiskEntry, bool) {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	if c.db == nil {
		return persistedDiskEntry{}, false
	}
	var payload []byte
	err := c.db.View(func(tx *bolt.Tx) error {
		storedBucket := tx.Bucket(bucket)
		if storedBucket == nil {
			return bolterrors.ErrBucketNotFound
		}
		value := storedBucket.Get(key)
		if value == nil {
			return nil
		}
		if len(value) > maxDiskCacheRecordBytes {
			return errDiskCacheRecordTooLarge
		}
		payload = append([]byte(nil), value...)
		return nil
	})
	if err != nil || payload == nil {
		return persistedDiskEntry{}, false
	}
	entry, err := c.decodeEntry(payload)
	if err != nil {
		return persistedDiskEntry{}, false
	}
	return entry, true
}

func (c *DiskAnalysisCache) encodeEntry(entry persistedDiskEntry) ([]byte, error) {
	encoded, err := cbor.Marshal(entry)
	if err != nil {
		return encoded, err
	}
	if len(encoded) > maxDiskCacheRecordBytes {
		return nil, errDiskCacheRecordTooLarge
	}
	if !c.gzip {
		return encoded, err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(encoded); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if compressed.Len() > maxDiskCacheRecordBytes {
		return nil, errDiskCacheRecordTooLarge
	}
	return compressed.Bytes(), nil
}

func (c *DiskAnalysisCache) decodeEntry(payload []byte) (persistedDiskEntry, error) {
	if len(payload) > maxDiskCacheRecordBytes {
		return persistedDiskEntry{}, errDiskCacheRecordTooLarge
	}
	if len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return persistedDiskEntry{}, err
		}
		decoded, readErr := io.ReadAll(io.LimitReader(reader, maxDiskCacheRecordBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return persistedDiskEntry{}, errors.Join(readErr, closeErr)
		}
		if len(decoded) > maxDiskCacheRecordBytes {
			return persistedDiskEntry{}, errDiskCacheRecordTooLarge
		}
		payload = decoded
	}
	var entry persistedDiskEntry
	if err := diskAnalysisDecMode.Unmarshal(payload, &entry); err != nil {
		return persistedDiskEntry{}, err
	}
	return entry, nil
}

func (c *DiskAnalysisCache) matches(entry persistedDiskEntry, lookup DiskAnalysisCacheLookup) bool {
	return entry.Kind == DiskCacheFileBundle &&
		entry.FormatVersion == diskAnalysisFormatVersion &&
		entry.ToolVersion == c.toolVersion &&
		entry.Namespace == c.namespace &&
		(entry.ParsedSettingsKey == lookup.SettingsKey || entry.DiagnosticsSettingsKey == lookup.SettingsKey) &&
		sourceMetadataMatches(entry.Source, lookup.Source) &&
		time.Since(time.UnixMilli(entry.WrittenAt)) <= c.ttl
}

func (c *DiskAnalysisCache) matchesWorkspace(entry persistedDiskEntry, settingsKey string, kind DiskCacheEntryKind) bool {
	return entry.Kind == kind &&
		entry.FormatVersion == diskAnalysisFormatVersion &&
		entry.ToolVersion == c.toolVersion &&
		entry.Namespace == c.namespace &&
		entry.SettingsKey == settingsKey &&
		time.Since(time.UnixMilli(entry.WrittenAt)) <= c.ttl
}

func sourceMetadataMatches(entrySource, lookupSource DiskAnalysisSourceMetadata) bool {
	if entrySource.FileName != lookupSource.FileName {
		return false
	}
	if entrySource.ContentHash != "" && lookupSource.ContentHash != "" {
		return entrySource.ContentHash == lookupSource.ContentHash
	}
	return entrySource.MtimeMS == lookupSource.MtimeMS && entrySource.Size == lookupSource.Size
}

func (c *DiskAnalysisCache) keyForLookup(lookup DiskAnalysisCacheLookup) []byte {
	fileName := filepath.Clean(lookup.Source.FileName)
	if runtime.GOOS == "windows" {
		fileName = strings.ToLower(fileName)
	}
	return stableDiskHashBytes(fileName)
}

func (c *DiskAnalysisCache) keyForWorkspace(settingsKey string, kind DiskCacheEntryKind) []byte {
	payload, _ := json.Marshal(map[string]string{"kind": string(kind), "namespace": c.namespace, "key": settingsKey})
	return stableDiskHashBytes(string(payload))
}

func diskCachePendingKey(bucket, key []byte) string {
	return string(bucket) + "\x00" + string(key)
}

func diskCacheBucketID(bucket []byte) byte {
	switch {
	case bytes.Equal(bucket, diskCacheFilesBucket):
		return 1
	case bytes.Equal(bucket, diskCacheWorkspaceBucket):
		return 2
	case bytes.Equal(bucket, diskReferenceDocumentsBucket):
		return 3
	case bytes.Equal(bucket, diskReferencePostingsBucket):
		return 4
	case bytes.Equal(bucket, diskReferenceQueriesBucket):
		return 5
	case bytes.Equal(bucket, diskWorkspaceManifestBucket):
		return 6
	case bytes.Equal(bucket, diskDocumentHeadsBucket):
		return 7
	case bytes.Equal(bucket, diskDocumentArtifactsBucket):
		return 8
	case bytes.Equal(bucket, diskIncludeEdgesBucket):
		return 9
	default:
		return 0
	}
}

func diskCacheBucketName(bucketID byte) []byte {
	switch bucketID {
	case 1:
		return diskCacheFilesBucket
	case 2:
		return diskCacheWorkspaceBucket
	case 3:
		return diskReferenceDocumentsBucket
	case 4:
		return diskReferencePostingsBucket
	case 5:
		return diskReferenceQueriesBucket
	case 6:
		return diskWorkspaceManifestBucket
	case 7:
		return diskDocumentHeadsBucket
	case 8:
		return diskDocumentArtifactsBucket
	case 9:
		return diskIncludeEdgesBucket
	default:
		return nil
	}
}

func diskCacheEntryMetaKey(bucketID byte, key []byte) []byte {
	result := make([]byte, 0, len(diskCacheEntryMetaPrefix)+1+len(key))
	result = append(result, diskCacheEntryMetaPrefix...)
	result = append(result, bucketID)
	return append(result, key...)
}

func diskCacheExpiryKey(writtenAt int64, bucketID byte, key []byte) []byte {
	result := make([]byte, 9, 9+len(key))
	binary.BigEndian.PutUint64(result[:8], uint64(writtenAt))
	result[8] = bucketID
	return append(result, key...)
}

func diskCacheLogicalSize(meta *bolt.Bucket) int64 {
	value := meta.Get(diskCacheLogicalSizeKey)
	if len(value) != 8 {
		return 0
	}
	return int64(binary.BigEndian.Uint64(value))
}

func putDiskCacheLogicalSize(meta *bolt.Bucket, size int64) error {
	value := make([]byte, 8)
	binary.BigEndian.PutUint64(value, uint64(size))
	return meta.Put(diskCacheLogicalSizeKey, value)
}

func DiskContentHash(text string) string { return contentHashes.hash(text) }

func stableDiskHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func stableDiskHashBytes(text string) []byte {
	sum := sha256.Sum256([]byte(text))
	encoded := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(encoded, sum[:])
	return encoded
}

var safeDiskNamespacePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var legacyDiskShardPattern = regexp.MustCompile(`^[0-9A-Fa-f]{2}$`)
