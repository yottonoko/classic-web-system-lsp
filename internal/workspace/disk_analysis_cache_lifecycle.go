package workspace

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

type encodedDiskWrite struct {
	bucket    []byte
	key       []byte
	value     []byte
	writtenAt int64
}

func putDiskCacheValue(tx *bolt.Tx, write encodedDiskWrite) (int64, error) {
	bucket := tx.Bucket(write.bucket)
	meta := tx.Bucket(diskCacheMetaBucket)
	expiry := tx.Bucket(diskCacheExpiryBucket)
	if bucket == nil || meta == nil || expiry == nil {
		return 0, bolterrors.ErrBucketNotFound
	}
	bucketID := diskCacheBucketID(write.bucket)
	if bucketID == 0 {
		return 0, bolterrors.ErrBucketNotFound
	}
	entryMetaKey := diskCacheEntryMetaKey(bucketID, write.key)
	oldSize := int64(0)
	if oldMeta := meta.Get(entryMetaKey); len(oldMeta) == 16 {
		oldWrittenAt := int64(binary.BigEndian.Uint64(oldMeta[:8]))
		oldSize = int64(binary.BigEndian.Uint64(oldMeta[8:]))
		if err := expiry.Delete(diskCacheExpiryKey(oldWrittenAt, bucketID, write.key)); err != nil {
			return 0, err
		}
	}
	if err := bucket.Put(write.key, write.value); err != nil {
		return 0, err
	}
	expiryKey := diskCacheExpiryKey(write.writtenAt, bucketID, write.key)
	expiryValue := append([]byte{bucketID}, write.key...)
	if err := expiry.Put(expiryKey, expiryValue); err != nil {
		return 0, err
	}
	entryMeta := make([]byte, 16)
	binary.BigEndian.PutUint64(entryMeta[:8], uint64(write.writtenAt))
	binary.BigEndian.PutUint64(entryMeta[8:], uint64(len(write.value)))
	if err := meta.Put(entryMetaKey, entryMeta); err != nil {
		return 0, err
	}
	return int64(len(write.value)) - oldSize, nil
}

func deleteDiskCacheValue(tx *bolt.Tx, bucketName, key []byte) (int64, error) {
	bucket := tx.Bucket(bucketName)
	meta := tx.Bucket(diskCacheMetaBucket)
	expiry := tx.Bucket(diskCacheExpiryBucket)
	bucketID := diskCacheBucketID(bucketName)
	if bucket == nil || meta == nil || expiry == nil || bucketID == 0 {
		return 0, bolterrors.ErrBucketNotFound
	}
	entryMetaKey := diskCacheEntryMetaKey(bucketID, key)
	oldSize := int64(0)
	if oldMeta := meta.Get(entryMetaKey); len(oldMeta) == 16 {
		oldWrittenAt := int64(binary.BigEndian.Uint64(oldMeta[:8]))
		oldSize = int64(binary.BigEndian.Uint64(oldMeta[8:]))
		if err := expiry.Delete(diskCacheExpiryKey(oldWrittenAt, bucketID, key)); err != nil {
			return 0, err
		}
	}
	if err := bucket.Delete(key); err != nil {
		return 0, err
	}
	if err := meta.Delete(entryMetaKey); err != nil {
		return 0, err
	}
	return -oldSize, nil
}

// Flush commits all queued analysis cache writes.
func (c *DiskAnalysisCache) Flush() error {
	if !c.enabled {
		return nil
	}
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	if c.closed {
		return bolterrors.ErrDatabaseNotOpen
	}
	done := make(chan error, 1)
	c.commands <- diskCacheWriterCommand{done: done}
	return <-done
}

// Close flushes queued writes and closes the bbolt database.
func (c *DiskAnalysisCache) Close() error {
	if !c.enabled {
		return nil
	}
	c.lifecycleMu.Lock()
	if c.closed {
		c.lifecycleMu.Unlock()
		return nil
	}
	c.closed = true
	done := make(chan error, 1)
	c.commands <- diskCacheWriterCommand{close: true, done: done}
	c.lifecycleMu.Unlock()
	flushErr := <-done
	<-c.writerDone
	c.sweepWG.Wait()
	c.dbMu.Lock()
	var closeErr error
	if c.db != nil {
		closeErr = c.db.Close()
		c.db = nil
	}
	c.dbMu.Unlock()
	return errors.Join(flushErr, closeErr)
}

// Clear closes the cache before deleting all cache databases and legacy entries.
func (c *DiskAnalysisCache) Clear() error {
	closeErr := c.Close()
	cacheRoot, rootErr := openDiskCacheDirectoryRoot(c.root, false)
	if os.IsNotExist(rootErr) {
		return closeErr
	}
	if rootErr != nil {
		return errors.Join(closeErr, rootErr)
	}
	defer cacheRoot.Close()
	removeErr := removeDiskCacheDirectory(cacheRoot, "bbolt-v1")
	legacyErr := removeLegacyDiskCacheEntries(cacheRoot)
	return errors.Join(closeErr, removeErr, legacyErr)
}

func removeDiskCacheDirectory(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errDiskCacheSymlink
	}
	return root.RemoveAll(name)
}

func removeLegacyDiskCacheEntries(root *os.Root) error {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	var result error
	for _, entry := range entries {
		if !entry.IsDir() {
			if strings.EqualFold(filepath.Ext(entry.Name()), ".cbor") {
				result = errors.Join(result, root.Remove(entry.Name()))
			}
			continue
		}
		if !legacyDiskShardPattern.MatchString(entry.Name()) {
			continue
		}
		shardRoot, openErr := root.OpenRoot(entry.Name())
		if openErr != nil {
			result = errors.Join(result, openErr)
			continue
		}
		children, readErr := fs.ReadDir(shardRoot.FS(), ".")
		if readErr != nil {
			result = errors.Join(result, readErr)
			_ = shardRoot.Close()
			continue
		}
		for _, child := range children {
			if !child.IsDir() && strings.EqualFold(filepath.Ext(child.Name()), ".cbor") {
				result = errors.Join(result, shardRoot.Remove(child.Name()))
			}
		}
		_ = shardRoot.Close()
		result = errors.Join(result, root.Remove(entry.Name()))
	}
	return result
}

func (c *DiskAnalysisCache) Sweep() error { return c.SweepBatch(defaultDiskCacheSweepSize) }

func (c *DiskAnalysisCache) SweepBatch(batchSize int) error {
	if !c.enabled || c.isClosed() {
		return nil
	}
	if err := c.Flush(); err != nil {
		return err
	}
	if batchSize < 1 {
		batchSize = 1
	}
	for {
		removed, err := c.sweepTransaction(batchSize, time.Now())
		if err != nil {
			return err
		}
		if removed < batchSize {
			return c.enforceDirectoryQuota()
		}
		time.Sleep(0)
	}
}

type diskCacheDatabaseFile struct {
	path       string
	size       int64
	modifiedAt time.Time
}

func (c *DiskAnalysisCache) enforceDirectoryQuota() error {
	databaseDirectory := filepath.Dir(c.databasePath)
	databaseRoot, err := openDiskCacheDirectoryRoot(databaseDirectory, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer databaseRoot.Close()
	entries, err := fs.ReadDir(databaseRoot.FS(), ".")
	if err != nil {
		return err
	}
	files := make([]diskCacheDatabaseFile, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !isDiskCacheDatabaseArtifact(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		file := diskCacheDatabaseFile{path: filepath.Join(databaseDirectory, entry.Name()), size: info.Size(), modifiedAt: info.ModTime()}
		files = append(files, file)
		total += file.size
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modifiedAt.Before(files[j].modifiedAt) })
	for _, file := range files {
		if total <= c.maxSize {
			break
		}
		if isCurrentDiskCacheDatabaseArtifact(file.path, c.databasePath) || !removeInactiveDiskCacheDatabase(databaseRoot, filepath.Base(file.path)) {
			continue
		}
		total -= file.size
	}
	if total <= c.maxSize {
		return nil
	}
	return c.compactCurrentDatabase()
}

func isDiskCacheDatabaseArtifact(name string) bool {
	return strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db.compact") || strings.HasSuffix(name, ".db.backup")
}

func isCurrentDiskCacheDatabaseArtifact(path, databasePath string) bool {
	return path == databasePath || path == databasePath+".compact" || path == databasePath+".backup"
}

func removeInactiveDiskCacheDatabase(root *os.Root, name string) bool {
	info, err := diskCacheArtifactInfo(root, name)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	path := filepath.Join(root.Name(), name)
	db, err := openDiskCacheDatabase(path, 0o600, &bolt.Options{ReadOnly: true, Timeout: time.Millisecond}, root)
	if err != nil {
		return false
	}
	if err := db.Close(); err != nil {
		return false
	}
	return root.Remove(name) == nil
}

func (c *DiskAnalysisCache) compactCurrentDatabase() error {
	databaseRoot, err := openDiskCacheDirectoryRoot(filepath.Dir(c.databasePath), false)
	if err != nil {
		return err
	}
	defer databaseRoot.Close()
	info, err := diskCacheArtifactInfo(databaseRoot, filepath.Base(c.databasePath))
	if err != nil || info.Size() < diskCacheCompactThreshold {
		return err
	}
	c.dbMu.Lock()
	defer c.dbMu.Unlock()
	if c.db == nil {
		return bolterrors.ErrDatabaseNotOpen
	}
	if info.Size() <= c.maxSize {
		return nil
	}
	temporaryName := filepath.Base(c.databasePath) + ".compact"
	backupName := filepath.Base(c.databasePath) + ".backup"
	temporaryPath := filepath.Join(databaseRoot.Name(), temporaryName)
	if err := removeDiskCacheArtifact(databaseRoot, temporaryName); err != nil {
		return err
	}
	if err := removeDiskCacheArtifact(databaseRoot, backupName); err != nil {
		return err
	}
	temporary, err := openDiskCacheDatabase(temporaryPath, 0o600, nil, databaseRoot)
	if err != nil {
		return err
	}
	compactErr := bolt.Compact(temporary, c.db, 64*1024*1024)
	closeTemporaryErr := temporary.Close()
	if compactErr != nil || closeTemporaryErr != nil {
		return errors.Join(compactErr, closeTemporaryErr, removeDiskCacheArtifact(databaseRoot, temporaryName))
	}
	if err := c.db.Close(); err != nil {
		return errors.Join(err, removeDiskCacheArtifact(databaseRoot, temporaryName))
	}
	c.db = nil
	if err := databaseRoot.Rename(filepath.Base(c.databasePath), backupName); err != nil {
		return errors.Join(err, removeDiskCacheArtifact(databaseRoot, temporaryName), c.reopenAfterCompactionFailure(err, databaseRoot, backupName))
	}
	if err := databaseRoot.Rename(temporaryName, filepath.Base(c.databasePath)); err != nil {
		restoreErr := databaseRoot.Rename(backupName, filepath.Base(c.databasePath))
		return errors.Join(err, restoreErr, c.reopenAfterCompactionFailure(err, databaseRoot, backupName))
	}
	newDB, err := openDiskCacheDatabase(c.databasePath, 0o600, &bolt.Options{Timeout: diskCacheOpenTimeout}, databaseRoot)
	if err != nil {
		removeErr := databaseRoot.Remove(filepath.Base(c.databasePath))
		restoreErr := databaseRoot.Rename(backupName, filepath.Base(c.databasePath))
		return errors.Join(err, removeErr, restoreErr, c.reopenAfterCompactionFailure(err, databaseRoot, backupName))
	}
	c.db = newDB
	return removeDiskCacheArtifact(databaseRoot, backupName)
}

func (c *DiskAnalysisCache) reopenAfterCompactionFailure(cause error, root *os.Root, backupName string) error {
	db, openErr := openDiskCacheDatabase(c.databasePath, 0o600, &bolt.Options{Timeout: diskCacheOpenTimeout}, root)
	if openErr == nil {
		c.db = db
		openErr = removeDiskCacheArtifact(root, backupName)
	}
	return errors.Join(cause, openErr)
}

func removeDiskCacheArtifact(root *os.Root, name string) error {
	if _, err := diskCacheArtifactInfo(root, name); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return root.Remove(name)
}

func (c *DiskAnalysisCache) sweepTransaction(limit int, now time.Time) (int, error) {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	if c.db == nil {
		return 0, bolterrors.ErrDatabaseNotOpen
	}
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	removed := 0
	err := c.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(diskCacheMetaBucket)
		expiry := tx.Bucket(diskCacheExpiryBucket)
		if meta == nil || expiry == nil {
			return bolterrors.ErrBucketNotFound
		}
		logicalSize := diskCacheLogicalSize(meta)
		cursor := expiry.Cursor()
		for expiryKey, value := cursor.First(); expiryKey != nil && removed < limit; expiryKey, value = cursor.Next() {
			if len(expiryKey) < 9 || len(value) < 2 {
				if err := cursor.Delete(); err != nil {
					return err
				}
				removed++
				continue
			}
			writtenAt := int64(binary.BigEndian.Uint64(expiryKey[:8]))
			// maxSize is a soft target. Fresh entries stay available even when the
			// current workspace needs more space than the target.
			if now.Sub(time.UnixMilli(writtenAt)) <= c.ttl {
				break
			}
			bucketID := value[0]
			key := value[1:]
			bucket := tx.Bucket(diskCacheBucketName(bucketID))
			entryMetaKey := diskCacheEntryMetaKey(bucketID, key)
			entrySize := int64(0)
			if entryMeta := meta.Get(entryMetaKey); len(entryMeta) == 16 {
				entrySize = int64(binary.BigEndian.Uint64(entryMeta[8:]))
			}
			if bucket != nil {
				if err := bucket.Delete(key); err != nil {
					return err
				}
			}
			if err := meta.Delete(entryMetaKey); err != nil {
				return err
			}
			if err := cursor.Delete(); err != nil {
				return err
			}
			logicalSize -= entrySize
			if logicalSize < 0 {
				logicalSize = 0
			}
			removed++
		}
		return putDiskCacheLogicalSize(meta, logicalSize)
	})
	return removed, err
}
