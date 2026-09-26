package workspace

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

func (c *DiskAnalysisCache) readDocumentValuesAligned(bucket []byte, keys [][]byte) [][]byte {
	result := make([][]byte, len(keys))
	if !c.enabled || c.isClosed() || len(keys) == 0 {
		return result
	}
	resolved := make([]bool, len(keys))
	c.pendingMu.Lock()
	for index, key := range keys {
		mapKey := diskCachePendingKey(bucket, key)
		write, found := c.pendingReference[mapKey]
		if !found {
			write, found = c.inFlightReference[mapKey]
		}
		if !found {
			continue
		}
		resolved[index] = true
		if !write.delete && time.Since(time.UnixMilli(write.writtenAt)) <= c.ttl {
			result[index] = append([]byte(nil), write.value...)
		}
	}
	c.pendingMu.Unlock()
	c.dbMu.RLock()
	if c.db == nil {
		c.dbMu.RUnlock()
		return result
	}
	_ = c.db.View(func(tx *bolt.Tx) error {
		stored := tx.Bucket(bucket)
		meta := tx.Bucket(diskCacheMetaBucket)
		bucketID := diskCacheBucketID(bucket)
		if stored == nil || meta == nil || bucketID == 0 {
			return nil
		}
		for index, key := range keys {
			if resolved[index] {
				continue
			}
			value := stored.Get(key)
			if value == nil {
				continue
			}
			entryMeta := meta.Get(diskCacheEntryMetaKey(bucketID, key))
			if len(entryMeta) != 16 || time.Since(time.UnixMilli(int64(binary.BigEndian.Uint64(entryMeta[:8])))) > c.ttl {
				continue
			}
			result[index] = append([]byte(nil), value...)
		}
		return nil
	})
	c.dbMu.RUnlock()
	return result
}

func encodeDiskDocumentValue(schemaVersion uint32, sourceHash string, value any) ([]byte, error) {
	if schemaVersion == 0 {
		schemaVersion = 1
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	envelope := diskDocumentEnvelope{
		StorageVersion: diskDocumentStorageVersion,
		SchemaVersion:  schemaVersion,
		SourceHash:     sourceHash,
		PayloadHash:    DiskContentHash(string(payload)),
		Payload:        payload,
	}
	return json.Marshal(envelope)
}

func decodeDiskDocumentValue(value []byte, schemaVersion uint32, target any) bool {
	var envelope diskDocumentEnvelope
	if err := json.Unmarshal(value, &envelope); err != nil ||
		envelope.StorageVersion != diskDocumentStorageVersion || envelope.SchemaVersion == 0 ||
		(schemaVersion != 0 && envelope.SchemaVersion != schemaVersion) ||
		envelope.PayloadHash != DiskContentHash(string(envelope.Payload)) {
		return false
	}
	if err := json.Unmarshal(envelope.Payload, target); err != nil {
		return false
	}
	return documentValueMetadataMatches(target, envelope)
}

func documentValueMetadataMatches(value any, envelope diskDocumentEnvelope) bool {
	switch typed := value.(type) {
	case *DiskWorkspaceMembershipManifest:
		return typed.SchemaVersion == 0 || typed.SchemaVersion == envelope.SchemaVersion
	case *DiskDocumentHead:
		return (typed.SchemaVersion == 0 || typed.SchemaVersion == envelope.SchemaVersion) && typed.SourceHash == envelope.SourceHash
	case *DiskDocumentArtifact:
		return (typed.SchemaVersion == 0 || typed.SchemaVersion == envelope.SchemaVersion) && typed.SourceHash == envelope.SourceHash
	case *DiskDocumentIncludeEdges:
		return (typed.SchemaVersion == 0 || typed.SchemaVersion == envelope.SchemaVersion) && typed.SourceHash == envelope.SourceHash
	default:
		return false
	}
}

func normalizeDiskWorkspaceMembershipManifest(manifest DiskWorkspaceMembershipManifest) DiskWorkspaceMembershipManifest {
	if manifest.SchemaVersion == 0 {
		manifest.SchemaVersion = 1
	}
	seen := make(map[string]struct{}, len(manifest.DocumentIDs))
	documentIDs := make([]string, 0, len(manifest.DocumentIDs))
	for _, documentID := range manifest.DocumentIDs {
		normalized := normalizeDiskDocumentID(documentID)
		if normalized == "" {
			continue
		}
		if _, found := seen[normalized]; found {
			continue
		}
		seen[normalized] = struct{}{}
		documentIDs = append(documentIDs, normalized)
	}
	sort.Strings(documentIDs)
	manifest.DocumentIDs = documentIDs
	if manifest.Fingerprint == "" {
		payload, _ := json.Marshal(documentIDs)
		manifest.Fingerprint = DiskContentHash(string(payload))
	}
	return manifest
}

func normalizeDiskDocumentID(documentID string) string {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return ""
	}
	slashPath := strings.ReplaceAll(documentID, "\\", "/")
	if isWindowsDrivePath(slashPath) {
		return strings.ToLower(stripLeadingDriveSlash(slashPath))
	}
	if strings.HasPrefix(slashPath, "//") {
		return strings.ToLower(slashPath)
	}
	if strings.Contains(documentID, "://") || strings.HasPrefix(strings.ToLower(documentID), "file:") {
		return SourceURIIdentityKey(documentID)
	}
	if colon := strings.IndexByte(documentID, ':'); colon >= 0 && (strings.IndexByte(documentID, '/') < 0 || colon < strings.IndexByte(documentID, '/')) && !isWindowsDrivePath(documentID) {
		return SourceURIIdentityKey(documentID)
	}
	if strings.HasPrefix(slashPath, "/") {
		return slashPath
	}
	return FileIdentityKeyFromFileName(documentID)
}

func diskDocumentArtifactKey(documentID, kind string) []byte {
	key := make([]byte, 0, len(documentID)+1+len(kind))
	key = append(key, documentID...)
	key = append(key, 0)
	return append(key, kind...)
}

func normalizeDiskIncludeEdges(documentID string, edges []DiskIncludeEdge) []DiskIncludeEdge {
	cloned := cloneDiskIncludeEdges(edges)
	for index := range cloned {
		cloned[index].DocumentID = documentID
		cloned[index].TargetDocumentID = normalizeDiskDocumentID(cloned[index].TargetDocumentID)
	}
	return cloned
}

func cloneDiskIncludeEdges(edges []DiskIncludeEdge) []DiskIncludeEdge {
	cloned := append([]DiskIncludeEdge(nil), edges...)
	for index := range cloned {
		cloned[index].Payload = append(json.RawMessage(nil), edges[index].Payload...)
	}
	return cloned
}

func (c *DiskAnalysisCache) keyForDocumentManifest(settingsKey string) []byte {
	payload, _ := json.Marshal(map[string]string{"namespace": c.namespace, "settingsKey": settingsKey})
	return stableDiskHashBytes(string(payload))
}

func (c *DiskAnalysisCache) pendingWriteCountLocked() int {
	return len(c.pending) + len(c.pendingReference)
}

func (c *DiskAnalysisCache) notifyPendingWrites(pendingCount int) {
	select {
	case c.notify <- struct{}{}:
	default:
	}
	if pendingCount >= diskCacheWriteBatchSize {
		select {
		case c.notify <- struct{}{}:
		default:
		}
	}
}

func resetReferenceBuckets(tx *bolt.Tx) error {
	if err := recreateReferenceBucket(tx, diskReferenceMetaBucket); err != nil {
		return err
	}
	return resetReferenceDataBuckets(tx)
}

func resetReferenceDataBuckets(tx *bolt.Tx) error {
	meta := tx.Bucket(diskCacheMetaBucket)
	if meta == nil || tx.Bucket(diskCacheExpiryBucket) == nil {
		return bolterrors.ErrBucketNotFound
	}
	logicalSize := diskCacheLogicalSize(meta)
	for _, name := range [][]byte{diskReferenceDocumentsBucket, diskReferencePostingsBucket, diskReferenceQueriesBucket} {
		delta, err := clearDiskCacheBucketAccounting(tx, name)
		if err != nil {
			return err
		}
		logicalSize += delta
		if err := recreateReferenceBucket(tx, name); err != nil {
			return err
		}
	}
	if logicalSize < 0 {
		logicalSize = 0
	}
	return putDiskCacheLogicalSize(meta, logicalSize)
}

func clearDiskCacheBucketAccounting(tx *bolt.Tx, bucketName []byte) (int64, error) {
	meta := tx.Bucket(diskCacheMetaBucket)
	expiry := tx.Bucket(diskCacheExpiryBucket)
	bucketID := diskCacheBucketID(bucketName)
	if meta == nil || expiry == nil || bucketID == 0 {
		return 0, bolterrors.ErrBucketNotFound
	}
	prefix := append(append([]byte(nil), diskCacheEntryMetaPrefix...), bucketID)
	delta := int64(0)
	cursor := meta.Cursor()
	for key, value := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, value = cursor.Next() {
		if len(value) == 16 {
			writtenAt := int64(binary.BigEndian.Uint64(value[:8]))
			delta -= int64(binary.BigEndian.Uint64(value[8:]))
			if err := expiry.Delete(diskCacheExpiryKey(writtenAt, bucketID, key[len(prefix):])); err != nil {
				return 0, err
			}
		}
		if err := cursor.Delete(); err != nil {
			return 0, err
		}
	}
	return delta, nil
}

func recreateReferenceBucket(tx *bolt.Tx, name []byte) error {
	if tx.Bucket(name) != nil {
		if err := tx.DeleteBucket(name); err != nil {
			return err
		}
	} else {
		cursor := tx.Cursor()
		key, _ := cursor.Seek(name)
		if bytes.Equal(key, name) {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
	}
	_, err := tx.CreateBucket(name)
	return err
}

func ensureReferenceBucket(tx *bolt.Tx, name []byte) error {
	if tx.Bucket(name) != nil {
		return nil
	}
	if _, err := tx.CreateBucket(name); err == nil {
		return nil
	} else if !errors.Is(err, bolterrors.ErrIncompatibleValue) {
		return err
	}
	return recreateReferenceBucket(tx, name)
}

func diskReferenceBucketName(bucket DiskReferenceBucket) ([]byte, bool) {
	switch bucket {
	case DiskReferenceDocuments:
		return diskReferenceDocumentsBucket, true
	case DiskReferencePostings:
		return diskReferencePostingsBucket, true
	case DiskReferenceQueries:
		return diskReferenceQueriesBucket, true
	default:
		return nil, false
	}
}
