package workspace

import (
	"encoding/json"
	"errors"
)

func (c *DiskAnalysisCache) ReadWorkspaceMembershipManifest(settingsKey string) (DiskWorkspaceMembershipManifest, bool) {
	key := c.keyForDocumentManifest(settingsKey)
	values := c.readDocumentValuesAligned(diskWorkspaceManifestBucket, [][]byte{key})
	if len(values) == 0 || values[0] == nil {
		return DiskWorkspaceMembershipManifest{}, false
	}
	var manifest DiskWorkspaceMembershipManifest
	if !decodeDiskDocumentValue(values[0], 0, &manifest) || manifest.SettingsKey != settingsKey {
		c.queueDocumentDeletes(diskWorkspaceManifestBucket, [][]byte{key})
		return DiskWorkspaceMembershipManifest{}, false
	}
	manifest.DocumentIDs = append([]string(nil), manifest.DocumentIDs...)
	return manifest, true
}

// QueueWorkspaceMembershipManifest queues one normalized manifest using the
// shared writer cadence.
func (c *DiskAnalysisCache) QueueWorkspaceMembershipManifest(manifest DiskWorkspaceMembershipManifest) error {
	if !c.enabled {
		return nil
	}
	if manifest.SettingsKey == "" {
		return errors.New("workspace membership settings key must not be empty")
	}
	manifest = normalizeDiskWorkspaceMembershipManifest(manifest)
	value, err := encodeDiskDocumentValue(manifest.SchemaVersion, "", manifest)
	if err != nil {
		return err
	}
	return c.queueDocumentValues([]pendingDocumentValue{{
		bucket: diskWorkspaceManifestBucket, key: c.keyForDocumentManifest(manifest.SettingsKey), value: value,
	}})
}

// WriteWorkspaceMembershipManifest queues and synchronously flushes a manifest.
func (c *DiskAnalysisCache) WriteWorkspaceMembershipManifest(manifest DiskWorkspaceMembershipManifest) error {
	if err := c.QueueWorkspaceMembershipManifest(manifest); err != nil {
		return err
	}
	return c.Flush()
}

// ReadDocumentHeadsAligned restores heads in input order. A malformed, stale,
// or mismatched entry is an independent nil miss.
func (c *DiskAnalysisCache) ReadDocumentHeadsAligned(documentIDs []string) []*DiskDocumentHead {
	keys := make([][]byte, len(documentIDs))
	for index, documentID := range documentIDs {
		keys[index] = []byte(normalizeDiskDocumentID(documentID))
	}
	values := c.readDocumentValuesAligned(diskDocumentHeadsBucket, keys)
	result := make([]*DiskDocumentHead, len(values))
	corrupt := make([][]byte, 0)
	for index, value := range values {
		if value == nil {
			continue
		}
		var head DiskDocumentHead
		if !decodeDiskDocumentValue(value, 0, &head) || normalizeDiskDocumentID(head.DocumentID) != string(keys[index]) {
			corrupt = append(corrupt, keys[index])
			continue
		}
		head.DocumentID = string(keys[index])
		head.Payload = append(json.RawMessage(nil), head.Payload...)
		result[index] = &head
	}
	c.queueDocumentDeletes(diskDocumentHeadsBucket, corrupt)
	return result
}

// ReadDocumentArtifactsAligned restores independently keyed artifacts in input
// order without rewriting or decoding unrelated artifact records.
func (c *DiskAnalysisCache) ReadDocumentArtifactsAligned(keys []DiskDocumentArtifactKey) []*DiskDocumentArtifact {
	encodedKeys := make([][]byte, len(keys))
	for index, key := range keys {
		encodedKeys[index] = diskDocumentArtifactKey(normalizeDiskDocumentID(key.DocumentID), key.Kind)
	}
	values := c.readDocumentValuesAligned(diskDocumentArtifactsBucket, encodedKeys)
	result := make([]*DiskDocumentArtifact, len(values))
	corrupt := make([][]byte, 0)
	for index, value := range values {
		if value == nil {
			continue
		}
		var artifact DiskDocumentArtifact
		if !decodeDiskDocumentValue(value, 0, &artifact) ||
			normalizeDiskDocumentID(artifact.DocumentID) != normalizeDiskDocumentID(keys[index].DocumentID) || artifact.Kind != keys[index].Kind {
			corrupt = append(corrupt, encodedKeys[index])
			continue
		}
		if keys[index].SchemaVersion != 0 && artifact.SchemaVersion != keys[index].SchemaVersion {
			continue
		}
		artifact.DocumentID = normalizeDiskDocumentID(artifact.DocumentID)
		artifact.Payload = append(json.RawMessage(nil), artifact.Payload...)
		result[index] = &artifact
	}
	c.queueDocumentDeletes(diskDocumentArtifactsBucket, corrupt)
	return result
}

// ReadIncludeEdgesAligned restores direct include-edge sets in input order.
func (c *DiskAnalysisCache) ReadIncludeEdgesAligned(documentIDs []string) []*DiskDocumentIncludeEdges {
	keys := make([][]byte, len(documentIDs))
	for index, documentID := range documentIDs {
		keys[index] = []byte(normalizeDiskDocumentID(documentID))
	}
	values := c.readDocumentValuesAligned(diskIncludeEdgesBucket, keys)
	result := make([]*DiskDocumentIncludeEdges, len(values))
	corrupt := make([][]byte, 0)
	for index, value := range values {
		if value == nil {
			continue
		}
		var edges DiskDocumentIncludeEdges
		if !decodeDiskDocumentValue(value, 0, &edges) || normalizeDiskDocumentID(edges.DocumentID) != string(keys[index]) {
			corrupt = append(corrupt, keys[index])
			continue
		}
		edges.DocumentID = string(keys[index])
		edges.Edges = cloneDiskIncludeEdges(edges.Edges)
		result[index] = &edges
	}
	c.queueDocumentDeletes(diskIncludeEdgesBucket, corrupt)
	return result
}

// QueueDocumentCacheDeltas queues the whole batch while holding the pending
// writer lock once, so every named record is committed by one bbolt transaction.
func (c *DiskAnalysisCache) QueueDocumentCacheDeltas(deltas []DiskDocumentCacheDelta) error {
	if !c.enabled || len(deltas) == 0 {
		return nil
	}
	writes := make([]pendingDocumentValue, 0)
	for _, delta := range deltas {
		documentID := normalizeDiskDocumentID(delta.DocumentID)
		if documentID == "" {
			return errors.New("document cache delta document ID must not be empty")
		}
		if delta.Head != nil && delta.DeleteHead {
			return errors.New("document cache delta cannot upsert and delete the same head")
		}
		if delta.Head != nil {
			head := *delta.Head
			head.DocumentID = documentID
			if head.SchemaVersion == 0 {
				head.SchemaVersion = 1
			}
			value, err := encodeDiskDocumentValue(head.SchemaVersion, head.SourceHash, head)
			if err != nil {
				return err
			}
			writes = append(writes, pendingDocumentValue{bucket: diskDocumentHeadsBucket, key: []byte(documentID), value: value})
		} else if delta.DeleteHead {
			writes = append(writes, pendingDocumentValue{bucket: diskDocumentHeadsBucket, key: []byte(documentID), delete: true})
		}
		for _, artifact := range delta.ArtifactUpserts {
			if artifact.Kind == "" {
				return errors.New("document artifact kind must not be empty")
			}
			artifact.DocumentID = documentID
			if artifact.SchemaVersion == 0 {
				artifact.SchemaVersion = 1
			}
			value, err := encodeDiskDocumentValue(artifact.SchemaVersion, artifact.SourceHash, artifact)
			if err != nil {
				return err
			}
			writes = append(writes, pendingDocumentValue{
				bucket: diskDocumentArtifactsBucket, key: diskDocumentArtifactKey(documentID, artifact.Kind), value: value,
			})
		}
		for _, artifact := range delta.ArtifactDeletes {
			if artifact.Kind == "" {
				return errors.New("document artifact delete kind must not be empty")
			}
			writes = append(writes, pendingDocumentValue{
				bucket: diskDocumentArtifactsBucket, key: diskDocumentArtifactKey(documentID, artifact.Kind), delete: true,
			})
		}
		if delta.IncludeEdges != nil && delta.DeleteIncludeEdges {
			return errors.New("document cache delta cannot upsert and delete the same include edges")
		}
		if delta.IncludeEdges != nil {
			edges := *delta.IncludeEdges
			edges.DocumentID = documentID
			if edges.SchemaVersion == 0 {
				edges.SchemaVersion = 1
			}
			edges.Edges = normalizeDiskIncludeEdges(documentID, edges.Edges)
			value, err := encodeDiskDocumentValue(edges.SchemaVersion, edges.SourceHash, edges)
			if err != nil {
				return err
			}
			writes = append(writes, pendingDocumentValue{bucket: diskIncludeEdgesBucket, key: []byte(documentID), value: value})
		} else if delta.DeleteIncludeEdges {
			writes = append(writes, pendingDocumentValue{bucket: diskIncludeEdgesBucket, key: []byte(documentID), delete: true})
		}
	}
	return c.queueDocumentValues(writes)
}

// WriteDocumentCacheDeltas queues and synchronously flushes a delta batch.
func (c *DiskAnalysisCache) WriteDocumentCacheDeltas(deltas []DiskDocumentCacheDelta) error {
	if err := c.QueueDocumentCacheDeltas(deltas); err != nil {
		return err
	}
	return c.Flush()
}

// EnsureReferenceSchema prepares the reference-only cache schema. A true
// result means missing, stale, or malformed reference data was discarded.
