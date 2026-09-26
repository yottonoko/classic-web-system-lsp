package workspace

import (
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

var errWorkspaceIndexTransactionIncomplete = errors.New("workspace index transaction is incomplete")

// DiskWorkspaceIndexWriteTransaction stages the workspace membership manifest
// and index as one conditional cache update. Staged records are not visible to
// the shared writer until CommitIf accepts them, so a cancelled generation
// cannot be flushed later by an unrelated cache write.
type DiskWorkspaceIndexWriteTransaction struct {
	cache *DiskAnalysisCache

	manifest    pendingReferenceWrite
	index       pendingDiskWrite
	settingsKey string

	hasManifest bool
	hasIndex    bool
	finished    bool
}

// BeginWorkspaceIndexWrite starts a private workspace membership/index
// transaction. Queue methods only copy and encode records; CommitIf performs
// the single bbolt transaction.
func (c *DiskAnalysisCache) BeginWorkspaceIndexWrite() (*DiskWorkspaceIndexWriteTransaction, error) {
	if c == nil {
		return nil, bolterrors.ErrDatabaseNotOpen
	}
	return &DiskWorkspaceIndexWriteTransaction{cache: c}, nil
}

// QueueWorkspaceMembershipManifest stages the membership manifest in this
// transaction without publishing it to the cache's shared pending queue.
func (tx *DiskWorkspaceIndexWriteTransaction) QueueWorkspaceMembershipManifest(manifest DiskWorkspaceMembershipManifest) error {
	if tx == nil || tx.cache == nil {
		return bolterrors.ErrDatabaseNotOpen
	}
	if tx.finished {
		return errors.New("workspace index transaction is already finished")
	}
	if !tx.cache.enabled {
		tx.hasManifest = true
		return nil
	}
	if manifest.SettingsKey == "" {
		return errors.New("workspace membership settings key must not be empty")
	}
	if tx.settingsKey != "" && tx.settingsKey != manifest.SettingsKey {
		return errors.New("workspace index transaction settings keys do not match")
	}
	manifest = normalizeDiskWorkspaceMembershipManifest(manifest)
	value, err := encodeDiskDocumentValue(manifest.SchemaVersion, "", manifest)
	if err != nil {
		return err
	}
	tx.manifest = pendingReferenceWrite{
		bucket:    append([]byte(nil), diskWorkspaceManifestBucket...),
		key:       tx.cache.keyForDocumentManifest(manifest.SettingsKey),
		value:     value,
		writtenAt: time.Now().UnixMilli(),
	}
	tx.settingsKey = manifest.SettingsKey
	tx.hasManifest = true
	return nil
}

// WriteWorkspaceIndex stages the workspace index in this transaction without
// publishing it to the cache's shared pending queue.
func (tx *DiskWorkspaceIndexWriteTransaction) WriteWorkspaceIndex(entry DiskWorkspaceIndexCacheEntry) error {
	if tx == nil || tx.cache == nil {
		return bolterrors.ErrDatabaseNotOpen
	}
	if tx.finished {
		return errors.New("workspace index transaction is already finished")
	}
	if !tx.cache.enabled {
		tx.hasIndex = true
		return nil
	}
	if entry.SettingsKey == "" {
		return errors.New("workspace index settings key must not be empty")
	}
	if tx.settingsKey != "" && tx.settingsKey != entry.SettingsKey {
		return errors.New("workspace index transaction settings keys do not match")
	}
	payload := tx.cache.baseEntry(DiskCacheWorkspaceIndex)
	payload.SettingsKey = entry.SettingsKey
	payload.WorkspaceEntries = append([]DiskWorkspaceIndexedDocument(nil), entry.Entries...)
	tx.index = pendingDiskWrite{
		bucket:     append([]byte(nil), diskCacheWorkspaceBucket...),
		key:        tx.cache.keyForWorkspace(entry.SettingsKey, DiskCacheWorkspaceIndex),
		entry:      clonePersistedDiskEntryValue(payload),
		components: 0,
	}
	tx.settingsKey = entry.SettingsKey
	tx.hasIndex = true
	return nil
}

// CommitIf atomically commits both staged records when allow returns true.
// The predicate is evaluated while the cache mutation lock is held, directly
// before the bbolt update, so the caller can coordinate invalidation without
// holding its own global state lock across disk I/O.
func (tx *DiskWorkspaceIndexWriteTransaction) CommitIf(allow func() bool) (bool, error) {
	if tx == nil || tx.cache == nil {
		return false, bolterrors.ErrDatabaseNotOpen
	}
	if tx.finished {
		return false, errors.New("workspace index transaction is already finished")
	}
	tx.finished = true
	if !tx.cache.enabled {
		if allow != nil && !allow() {
			return false, nil
		}
		return true, nil
	}
	if !tx.hasManifest || !tx.hasIndex {
		return false, errWorkspaceIndexTransactionIncomplete
	}

	// Finish writes already waiting in the shared writer before taking the
	// mutation lock. The private transaction then has a well-defined ordering
	// relative to normal cache writes without exposing staged records early.
	if err := tx.cache.Flush(); err != nil {
		return false, err
	}

	c := tx.cache
	c.lifecycleMu.RLock()
	if c.closed {
		c.lifecycleMu.RUnlock()
		return false, bolterrors.ErrDatabaseNotOpen
	}
	c.dbMu.RLock()
	if c.db == nil {
		c.dbMu.RUnlock()
		c.lifecycleMu.RUnlock()
		return false, bolterrors.ErrDatabaseNotOpen
	}
	c.mutationMu.Lock()
	if allow != nil && !allow() {
		c.mutationMu.Unlock()
		c.dbMu.RUnlock()
		c.lifecycleMu.RUnlock()
		return false, nil
	}
	indexValue, err := c.encodeEntry(tx.index.entry)
	if err == nil {
		err = c.db.Update(func(dbTx *bolt.Tx) error {
			meta := dbTx.Bucket(diskCacheMetaBucket)
			if meta == nil {
				return bolterrors.ErrBucketNotFound
			}
			logicalSize := diskCacheLogicalSize(meta)
			manifestDelta, manifestErr := putDiskCacheValue(dbTx, encodedDiskWrite{
				bucket: tx.manifest.bucket, key: tx.manifest.key, value: tx.manifest.value,
				writtenAt: tx.manifest.writtenAt,
			})
			if manifestErr != nil {
				return manifestErr
			}
			logicalSize += manifestDelta
			indexDelta, indexErr := putDiskCacheValue(dbTx, encodedDiskWrite{
				bucket: tx.index.bucket, key: tx.index.key, value: indexValue,
				writtenAt: tx.index.entry.WrittenAt,
			})
			if indexErr != nil {
				return indexErr
			}
			logicalSize += indexDelta
			return putDiskCacheLogicalSize(meta, logicalSize)
		})
	}
	if err == nil {
		// Keep the lifecycle and database read locks until scheduling has
		// registered the sweep. Close waits for the lifecycle lock before it
		// waits on sweepWG, so this prevents both a nil database read and an
		// Add/Wait race.
		c.scheduleSweepIfOversizedLocked()
	}
	c.mutationMu.Unlock()
	c.dbMu.RUnlock()
	c.lifecycleMu.RUnlock()
	if err != nil {
		return false, err
	}
	return true, nil
}
