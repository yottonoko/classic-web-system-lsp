package workspace

import (
	"math"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

func (c *DiskAnalysisCache) runWriter() {
	defer close(c.writerDone)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false
	for {
		select {
		case <-c.notify:
			c.pendingMu.Lock()
			pendingCount := c.pendingWriteCountLocked()
			c.pendingMu.Unlock()
			if pendingCount >= diskCacheWriteBatchSize {
				if timerActive && !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timerActive = false
				_ = c.flushPending()
			} else if pendingCount > 0 && !timerActive {
				timer.Reset(diskCacheWriteInterval)
				timerActive = true
			}
		case <-timer.C:
			timerActive = false
			_ = c.flushPending()
		case command := <-c.commands:
			if timerActive && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timerActive = false
			err := c.flushPending()
			command.done <- err
			if command.close {
				return
			}
		}
	}
}

func (c *DiskAnalysisCache) flushPending() error {
	c.pendingMu.Lock()
	if len(c.pending) == 0 && len(c.pendingReference) == 0 {
		c.pendingMu.Unlock()
		return nil
	}
	writes := c.pending
	referenceWrites := c.pendingReference
	c.inFlight = writes
	c.inFlightReference = referenceWrites
	c.inFlightReferenceDocumentKeys = c.pendingReferenceDocumentKeys
	c.pending = make(map[string]pendingDiskWrite)
	c.pendingReference = make(map[string]pendingReferenceWrite)
	c.pendingReferenceDocumentKeys = make(map[string]map[string]struct{})
	c.pendingMu.Unlock()

	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	if c.db == nil {
		c.finishPendingWrites(false)
		return bolterrors.ErrDatabaseNotOpen
	}
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	storedValues := make(map[string][]byte)
	if err := c.db.View(func(tx *bolt.Tx) error {
		for mapKey, pending := range writes {
			if pending.components == 0 {
				continue
			}
			bucket := tx.Bucket(pending.bucket)
			if bucket == nil {
				return bolterrors.ErrBucketNotFound
			}
			if value := bucket.Get(pending.key); value != nil {
				storedValues[mapKey] = append([]byte(nil), value...)
			}
		}
		return nil
	}); err != nil {
		c.finishPendingWrites(false)
		return err
	}
	encoded := make([]encodedDiskWrite, 0, len(writes))
	for mapKey, pending := range writes {
		entry := pending.entry
		if storedValue := storedValues[mapKey]; storedValue != nil {
			if stored, decodeErr := c.decodeEntry(storedValue); decodeErr == nil {
				entry = mergeFileBundleEntry(stored, entry, pending.components)
			}
		}
		value, encodeErr := c.encodeEntry(entry)
		if encodeErr != nil {
			c.finishPendingWrites(false)
			return encodeErr
		}
		encoded = append(encoded, encodedDiskWrite{
			bucket:    pending.bucket,
			key:       pending.key,
			value:     value,
			writtenAt: entry.WrittenAt,
		})
	}
	err := c.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(diskCacheMetaBucket)
		if meta == nil {
			return bolterrors.ErrBucketNotFound
		}
		logicalSize := diskCacheLogicalSize(meta)
		for _, write := range encoded {
			delta, putErr := putDiskCacheValue(tx, write)
			if putErr != nil {
				return putErr
			}
			logicalSize += delta
		}
		for _, write := range referenceWrites {
			var delta int64
			var writeErr error
			if write.delete {
				delta, writeErr = deleteDiskCacheValue(tx, write.bucket, write.key)
			} else {
				delta, writeErr = putDiskCacheValue(tx, encodedDiskWrite{
					bucket: write.bucket, key: write.key, value: write.value, writtenAt: write.writtenAt,
				})
			}
			if writeErr != nil {
				return writeErr
			}
			logicalSize += delta
		}
		if err := putDiskCacheLogicalSize(meta, logicalSize); err != nil {
			return err
		}
		return nil
	})
	c.finishPendingWrites(err == nil)
	if err == nil {
		c.scheduleSweepIfOversizedLocked()
	}
	return err
}

// scheduleSweepIfOversizedLocked schedules a sweep while the caller holds
// dbMu.RLock. CommitIf also holds lifecycleMu.RLock, which keeps Close from
// reaching its sweep Wait until the WaitGroup registration is complete.
func (c *DiskAnalysisCache) scheduleSweepIfOversizedLocked() {
	logicalSize, err := c.currentLogicalSizeLocked()
	if err != nil || logicalSize <= c.nextSweepSize.Load() || !c.sweepScheduled.CompareAndSwap(false, true) {
		return
	}
	c.sweepWG.Add(1)
	go func() {
		defer c.sweepWG.Done()
		defer c.sweepScheduled.Store(false)
		if err := c.Sweep(); err != nil {
			return
		}
		if currentSize, err := c.currentLogicalSize(); err == nil {
			c.nextSweepSize.Store(c.nextCleanupSize(currentSize))
		}
	}()
}

func (c *DiskAnalysisCache) cleanupTriggerSize() int64 {
	if c.maxSize > math.MaxInt64/diskCacheCleanupHysteresisNum {
		return math.MaxInt64
	}
	return c.maxSize * diskCacheCleanupHysteresisNum / diskCacheCleanupHysteresisDen
}

func (c *DiskAnalysisCache) nextCleanupSize(currentSize int64) int64 {
	triggerSize := c.cleanupTriggerSize()
	if currentSize <= c.maxSize {
		return triggerSize
	}
	increment := c.maxSize / diskCacheCleanupHysteresisDen
	if increment < 1 {
		increment = 1
	}
	if currentSize > math.MaxInt64-increment {
		return math.MaxInt64
	}
	return max(triggerSize, currentSize+increment)
}

func (c *DiskAnalysisCache) currentLogicalSize() (int64, error) {
	c.dbMu.RLock()
	defer c.dbMu.RUnlock()
	return c.currentLogicalSizeLocked()
}

func (c *DiskAnalysisCache) currentLogicalSizeLocked() (int64, error) {
	if c.db == nil {
		return 0, bolterrors.ErrDatabaseNotOpen
	}
	var logicalSize int64
	err := c.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(diskCacheMetaBucket)
		if meta == nil {
			return bolterrors.ErrBucketNotFound
		}
		logicalSize = diskCacheLogicalSize(meta)
		return nil
	})
	return logicalSize, err
}

func (c *DiskAnalysisCache) finishPendingWrites(committed bool) {
	c.pendingMu.Lock()
	if !committed {
		for mapKey, write := range c.inFlight {
			if _, superseded := c.pending[mapKey]; !superseded {
				c.pending[mapKey] = write
			}
		}
		for mapKey, write := range c.inFlightReference {
			if _, superseded := c.pendingReference[mapKey]; !superseded {
				c.pendingReference[mapKey] = write
			}
		}
		for documentKey, postingKeys := range c.inFlightReferenceDocumentKeys {
			if _, superseded := c.pendingReferenceDocumentKeys[documentKey]; superseded {
				continue
			}
			c.pendingReferenceDocumentKeys[documentKey] = postingKeys
		}
	}
	c.inFlight = make(map[string]pendingDiskWrite)
	c.inFlightReference = make(map[string]pendingReferenceWrite)
	c.inFlightReferenceDocumentKeys = make(map[string]map[string]struct{})
	hasPending := c.pendingWriteCountLocked() > 0
	c.pendingMu.Unlock()
	if hasPending {
		select {
		case c.notify <- struct{}{}:
		default:
		}
	}
}
