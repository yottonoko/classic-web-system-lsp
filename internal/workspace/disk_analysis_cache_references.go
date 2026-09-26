package workspace

import (
	"encoding/binary"
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

func (c *DiskAnalysisCache) EnsureReferenceSchema(version uint32) (bool, error) {
	if !c.enabled {
		return false, nil
	}
	if version == 0 {
		return false, errors.New("reference cache schema version must be non-zero")
	}
	if err := c.Flush(); err != nil {
		return false, err
	}
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	if c.closed {
		return false, bolterrors.ErrDatabaseNotOpen
	}
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	if c.db == nil {
		return false, bolterrors.ErrDatabaseNotOpen
	}
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	valid := false
	if err := c.db.View(func(tx *bolt.Tx) error {
		valid = referenceSchemaMatches(tx, version)
		return nil
	}); err != nil {
		return false, err
	}
	if valid {
		return false, nil
	}
	reset := false
	err := c.db.Update(func(tx *bolt.Tx) error {
		if !referenceSchemaMatches(tx, version) {
			reset = true
			if err := resetReferenceBuckets(tx); err != nil {
				return err
			}
		}
		meta := tx.Bucket(diskReferenceMetaBucket)
		if meta == nil {
			return bolterrors.ErrBucketNotFound
		}
		encoded := make([]byte, diskReferenceSchemaSize)
		binary.BigEndian.PutUint32(encoded, version)
		binary.BigEndian.PutUint32(encoded[4:], diskReferenceStorageVersion)
		return meta.Put(diskReferenceSchemaKey, encoded)
	})
	return reset, err
}

func referenceSchemaMatches(tx *bolt.Tx, version uint32) bool {
	meta := tx.Bucket(diskReferenceMetaBucket)
	if meta == nil || tx.Bucket(diskReferenceDocumentsBucket) == nil ||
		tx.Bucket(diskReferencePostingsBucket) == nil || tx.Bucket(diskReferenceQueriesBucket) == nil {
		return false
	}
	stored := meta.Get(diskReferenceSchemaKey)
	return len(stored) == diskReferenceSchemaSize &&
		binary.BigEndian.Uint32(stored) == version &&
		binary.BigEndian.Uint32(stored[4:]) == diskReferenceStorageVersion
}

// ReadReferenceValues reads reference cache values in one transaction. Both
// keys and returned values are owned by the caller.
func (c *DiskAnalysisCache) ReadReferenceValues(bucket DiskReferenceBucket, keys [][]byte) map[string][]byte {
	result := make(map[string][]byte, len(keys))
	for index, value := range c.ReadReferenceValuesAligned(bucket, keys) {
		if value != nil {
			result[string(keys[index])] = value
		}
	}
	return result
}

// ReadReferenceValuesAligned reads reference values in one transaction and
// returns them in input order. Missing and queued deletions are nil. Returned
// byte slices are caller-owned; the input keys are only borrowed for the call.
func (c *DiskAnalysisCache) ReadReferenceValuesAligned(bucket DiskReferenceBucket, keys [][]byte) [][]byte {
	result := make([][]byte, len(keys))
	if !c.enabled || c.isClosed() || len(keys) == 0 {
		return result
	}
	bucketName, ok := diskReferenceBucketName(bucket)
	if !ok {
		return result
	}
	var resolved []bool
	c.pendingMu.Lock()
	hasPending := len(c.pendingReference) > 0 || len(c.inFlightReference) > 0
	if hasPending {
		resolved = make([]bool, len(keys))
		for index, key := range keys {
			mapKey := diskCachePendingKey(bucketName, key)
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
	}
	c.pendingMu.Unlock()
	c.dbMu.RLock()
	if c.db == nil {
		c.dbMu.RUnlock()
		return result
	}
	expired := make([]DiskReferenceQueryWrite, 0)
	_ = c.db.View(func(tx *bolt.Tx) error {
		stored := tx.Bucket(bucketName)
		meta := tx.Bucket(diskCacheMetaBucket)
		bucketID := diskCacheBucketID(bucketName)
		if stored == nil || meta == nil || bucketID == 0 {
			return nil
		}
		for index, key := range keys {
			if resolved != nil && resolved[index] {
				continue
			}
			value := stored.Get(key)
			if value == nil {
				continue
			}
			entryMeta := meta.Get(diskCacheEntryMetaKey(bucketID, key))
			if len(entryMeta) != 16 || time.Since(time.UnixMilli(int64(binary.BigEndian.Uint64(entryMeta[:8])))) > c.ttl {
				if bucket == DiskReferenceQueries {
					expired = append(expired, DiskReferenceQueryWrite{Key: append([]byte(nil), key...)})
				}
				continue
			}
			result[index] = append([]byte(nil), value...)
		}
		return nil
	})
	c.dbMu.RUnlock()
	if len(expired) > 0 {
		_ = c.QueueReferenceQueryWrites(expired)
	}
	return result
}

// ReferenceValuesPresentAligned reports unexpired reference values in input
// order without copying their payloads. Queued writes and deletions take
// precedence over the backing database.
func (c *DiskAnalysisCache) ReferenceValuesPresentAligned(bucket DiskReferenceBucket, keys [][]byte) []bool {
	result := make([]bool, len(keys))
	if !c.enabled || c.isClosed() || len(keys) == 0 {
		return result
	}
	bucketName, ok := diskReferenceBucketName(bucket)
	if !ok {
		return result
	}
	resolved := make([]bool, len(keys))
	c.pendingMu.Lock()
	for index, key := range keys {
		mapKey := diskCachePendingKey(bucketName, key)
		write, found := c.pendingReference[mapKey]
		if !found {
			write, found = c.inFlightReference[mapKey]
		}
		if !found {
			continue
		}
		resolved[index] = true
		result[index] = !write.delete && time.Since(time.UnixMilli(write.writtenAt)) <= c.ttl
	}
	c.pendingMu.Unlock()
	c.dbMu.RLock()
	if c.db == nil {
		c.dbMu.RUnlock()
		return result
	}
	_ = c.db.View(func(tx *bolt.Tx) error {
		stored := tx.Bucket(bucketName)
		meta := tx.Bucket(diskCacheMetaBucket)
		bucketID := diskCacheBucketID(bucketName)
		if stored == nil || meta == nil || bucketID == 0 {
			return nil
		}
		for index, key := range keys {
			if resolved[index] || stored.Get(key) == nil {
				continue
			}
			entryMeta := meta.Get(diskCacheEntryMetaKey(bucketID, key))
			result[index] = len(entryMeta) == 16 && time.Since(time.UnixMilli(int64(binary.BigEndian.Uint64(entryMeta[:8])))) <= c.ttl
		}
		return nil
	})
	c.dbMu.RUnlock()
	return result
}

// ReplaceReferenceDocument atomically replaces one document manifest and its
// changed posting records.
func (c *DiskAnalysisCache) ReplaceReferenceDocument(documentKey, documentValue []byte, oldPostingKeys [][]byte, newPostings map[string][]byte) error {
	return c.ReplaceReferenceDocuments([]DiskReferenceDocumentReplacement{{
		DocumentKey: documentKey, DocumentValue: documentValue, OldPostingKeys: oldPostingKeys, NewPostings: newPostings,
	}})
}

// ReplaceReferenceDocuments queues a bulk atomic replacement and flushes it.
func (c *DiskAnalysisCache) ReplaceReferenceDocuments(replacements []DiskReferenceDocumentReplacement) error {
	if err := c.QueueReferenceDocumentReplacements(replacements); err != nil {
		return err
	}
	return c.Flush()
}

// QueueReferenceDocumentReplacements queues document and posting replacements
// for the shared 50 ms/64-write database transaction cadence.
func (c *DiskAnalysisCache) QueueReferenceDocumentReplacements(replacements []DiskReferenceDocumentReplacement) error {
	if !c.enabled || len(replacements) == 0 {
		return nil
	}
	for _, replacement := range replacements {
		if len(replacement.DocumentKey) == 0 {
			return errors.New("reference document key must not be empty")
		}
	}
	return c.queueReferenceDocumentReplacements(replacements)
}

// WriteReferenceQuery persists or deletes one materialized reference query.
func (c *DiskAnalysisCache) WriteReferenceQuery(key, value []byte) error {
	return c.WriteReferenceQueries([]DiskReferenceQueryWrite{{Key: key, Value: value}})
}

// WriteReferenceQueries queues a bulk query update and flushes it atomically.
func (c *DiskAnalysisCache) WriteReferenceQueries(writes []DiskReferenceQueryWrite) error {
	if err := c.QueueReferenceQueryWrites(writes); err != nil {
		return err
	}
	return c.Flush()
}

// QueueReferenceQueryWrites queues materialized query writes for the shared
// 50 ms/64-write database transaction cadence.
func (c *DiskAnalysisCache) QueueReferenceQueryWrites(writes []DiskReferenceQueryWrite) error {
	if !c.enabled || len(writes) == 0 {
		return nil
	}
	for _, write := range writes {
		if len(write.Key) == 0 {
			return errors.New("reference query key must not be empty")
		}
	}
	return c.queueReferenceQueryWrites(writes)
}

// ClearReferenceData discards only reference-specific persisted data.
func (c *DiskAnalysisCache) ClearReferenceData() error {
	if err := c.Flush(); err != nil {
		return err
	}
	return c.updateReferenceData(resetReferenceDataBuckets)
}

func (c *DiskAnalysisCache) updateReferenceData(update func(*bolt.Tx) error) error {
	if !c.enabled {
		return nil
	}
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	if c.closed {
		return bolterrors.ErrDatabaseNotOpen
	}
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	if c.db == nil {
		return bolterrors.ErrDatabaseNotOpen
	}
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	return c.db.Update(update)
}

func (c *DiskAnalysisCache) queueReferenceDocumentReplacements(replacements []DiskReferenceDocumentReplacement) error {
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	if c.closed {
		return bolterrors.ErrDatabaseNotOpen
	}
	now := time.Now().UnixMilli()
	c.pendingMu.Lock()
	for _, replacement := range replacements {
		documentID := string(replacement.DocumentKey)
		currentKeys := make(map[string]struct{}, len(replacement.CurrentPostingKeys))
		for _, key := range replacement.CurrentPostingKeys {
			currentKeys[string(key)] = struct{}{}
		}
		// A nil complete key set preserves the original full-replacement API.
		// Delta callers provide a non-nil set, including an empty set for a
		// document with no postings or a tombstone.
		if replacement.CurrentPostingKeys == nil {
			for key := range c.inFlightReferenceDocumentKeys[documentID] {
				c.putPendingReferenceWriteLocked(diskReferencePostingsBucket, []byte(key), nil, true, now)
			}
			for key := range c.pendingReferenceDocumentKeys[documentID] {
				c.putPendingReferenceWriteLocked(diskReferencePostingsBucket, []byte(key), nil, true, now)
			}
		} else {
			for key := range c.inFlightReferenceDocumentKeys[documentID] {
				if _, retained := currentKeys[key]; !retained {
					c.putPendingReferenceWriteLocked(diskReferencePostingsBucket, []byte(key), nil, true, now)
				}
			}
			for key := range c.pendingReferenceDocumentKeys[documentID] {
				if _, retained := currentKeys[key]; !retained {
					c.putPendingReferenceWriteLocked(diskReferencePostingsBucket, []byte(key), nil, true, now)
				}
			}
		}
		for _, key := range replacement.OldPostingKeys {
			c.putPendingReferenceWriteLocked(diskReferencePostingsBucket, key, nil, true, now)
		}
		newKeys := currentKeys
		if replacement.CurrentPostingKeys == nil {
			newKeys = make(map[string]struct{}, len(replacement.NewPostings))
		}
		for key, value := range replacement.NewPostings {
			newKeys[key] = struct{}{}
			c.putPendingReferenceWriteLocked(diskReferencePostingsBucket, []byte(key), value, false, now)
		}
		c.pendingReferenceDocumentKeys[documentID] = newKeys
		c.putPendingReferenceWriteLocked(diskReferenceDocumentsBucket, replacement.DocumentKey, replacement.DocumentValue, len(replacement.DocumentValue) == 0, now)
	}
	pendingCount := c.pendingWriteCountLocked()
	c.pendingMu.Unlock()
	c.notifyPendingWrites(pendingCount)
	return nil
}

func (c *DiskAnalysisCache) queueReferenceQueryWrites(writes []DiskReferenceQueryWrite) error {
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	if c.closed {
		return bolterrors.ErrDatabaseNotOpen
	}
	now := time.Now().UnixMilli()
	c.pendingMu.Lock()
	for _, write := range writes {
		c.putPendingReferenceWriteLocked(diskReferenceQueriesBucket, write.Key, write.Value, len(write.Value) == 0, now)
	}
	pendingCount := c.pendingWriteCountLocked()
	c.pendingMu.Unlock()
	c.notifyPendingWrites(pendingCount)
	return nil
}

func (c *DiskAnalysisCache) putPendingReferenceWriteLocked(bucket, key, value []byte, deleteValue bool, writtenAt int64) {
	ownedBucket := append([]byte(nil), bucket...)
	ownedKey := append([]byte(nil), key...)
	c.pendingReference[diskCachePendingKey(ownedBucket, ownedKey)] = pendingReferenceWrite{
		bucket: ownedBucket, key: ownedKey, value: append([]byte(nil), value...), delete: deleteValue, writtenAt: writtenAt,
	}
}

func (c *DiskAnalysisCache) queueDocumentValues(writes []pendingDocumentValue) error {
	if len(writes) == 0 {
		return nil
	}
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	if c.closed {
		return bolterrors.ErrDatabaseNotOpen
	}
	now := time.Now().UnixMilli()
	c.pendingMu.Lock()
	for _, write := range writes {
		c.putPendingReferenceWriteLocked(write.bucket, write.key, write.value, write.delete, now)
	}
	pendingCount := c.pendingWriteCountLocked()
	c.pendingMu.Unlock()
	c.notifyPendingWrites(pendingCount)
	return nil
}

func (c *DiskAnalysisCache) queueDocumentDeletes(bucket []byte, keys [][]byte) {
	if len(keys) == 0 {
		return
	}
	writes := make([]pendingDocumentValue, len(keys))
	for index, key := range keys {
		writes[index] = pendingDocumentValue{bucket: bucket, key: key, delete: true}
	}
	_ = c.queueDocumentValues(writes)
}
