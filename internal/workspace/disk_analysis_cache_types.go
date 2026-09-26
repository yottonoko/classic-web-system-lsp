package workspace

import (
	"encoding/json"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	bolt "go.etcd.io/bbolt"
)

const (
	diskAnalysisFormatVersion     = 7
	defaultDiskCacheTTL           = 14 * 24 * time.Hour
	defaultDiskCacheMaxSize       = 16 * 1024 * 1024 * 1024
	diskCacheCleanupHysteresisNum = 5
	diskCacheCleanupHysteresisDen = 4
	defaultDiskCacheSweepSize     = 128
	diskCacheWriteInterval        = 50 * time.Millisecond
	diskCacheWriteBatchSize       = 64
	diskCacheOpenTimeout          = 250 * time.Millisecond
	diskCacheCompactThreshold     = 1024 * 1024
	diskCacheParsedComponent      = byte(1)
	diskCacheDiagnosticsComponent = byte(2)
	diskReferenceSchemaSize       = 8
	diskReferenceStorageVersion   = uint32(2)
	diskDocumentStorageVersion    = uint32(1)
)

var (
	diskCacheMetaBucket          = []byte("meta")
	diskCacheFilesBucket         = []byte("files")
	diskCacheWorkspaceBucket     = []byte("workspace")
	diskCacheExpiryBucket        = []byte("expiry")
	diskCacheSchemaKey           = []byte("schema")
	diskCacheToolVersionKey      = []byte("toolVersion")
	diskCacheLogicalSizeKey      = []byte("logicalSize")
	diskCacheEntryMetaPrefix     = []byte("entry:")
	diskCacheSchemaValue         = []byte("asp-lsp-bbolt-v1")
	diskReferenceMetaBucket      = []byte("reference_meta")
	diskReferenceDocumentsBucket = []byte("reference_documents")
	diskReferencePostingsBucket  = []byte("reference_postings")
	diskReferenceQueriesBucket   = []byte("reference_queries")
	diskReferenceSchemaKey       = []byte("schema")
	diskWorkspaceManifestBucket  = []byte("workspace_manifest")
	diskDocumentHeadsBucket      = []byte("document_heads")
	diskDocumentArtifactsBucket  = []byte("document_artifacts")
	diskIncludeEdgesBucket       = []byte("include_edges")
)

// DiskReferenceBucket identifies one independently persisted reference-cache area.
type DiskReferenceBucket uint8

const (
	DiskReferenceDocuments DiskReferenceBucket = iota + 1
	DiskReferencePostings
	DiskReferenceQueries
)

// DiskReferenceDocumentReplacement atomically replaces one persisted document
// manifest and its changed posting records. OldPostingKeys are deleted,
// NewPostings are written, and a non-nil CurrentPostingKeys is the complete
// ownership set used to coalesce overlapping replacements. A nil ownership set
// retains the legacy full-replacement behavior.
type DiskReferenceDocumentReplacement struct {
	DocumentKey        []byte
	DocumentValue      []byte
	OldPostingKeys     [][]byte
	NewPostings        map[string][]byte
	CurrentPostingKeys [][]byte
}

// DiskReferenceQueryWrite writes or deletes one materialized reference query.
// An empty Value deletes Key.
type DiskReferenceQueryWrite struct {
	Key   []byte
	Value []byte
}

// DiskWorkspaceMembershipManifest records the normalized documents belonging
// to one workspace/settings combination.
type DiskWorkspaceMembershipManifest struct {
	SchemaVersion uint32   `json:"schemaVersion"`
	SettingsKey   string   `json:"settingsKey"`
	DocumentIDs   []string `json:"documentIds"`
	Fingerprint   string   `json:"fingerprint"`
}

// DiskDocumentHead records the current source revision for one document.
type DiskDocumentHead struct {
	DocumentID    string          `json:"documentId"`
	SchemaVersion uint32          `json:"schemaVersion"`
	SourceHash    string          `json:"sourceHash"`
	Fingerprint   string          `json:"fingerprint"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// DiskDocumentArtifact is one independently persisted artifact segment.
type DiskDocumentArtifact struct {
	DocumentID    string          `json:"documentId"`
	Kind          string          `json:"kind"`
	SchemaVersion uint32          `json:"schemaVersion"`
	SourceHash    string          `json:"sourceHash"`
	Fingerprint   string          `json:"fingerprint"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// DiskDocumentArtifactKey identifies one artifact without borrowing storage.
type DiskDocumentArtifactKey struct {
	DocumentID    string
	Kind          string
	SchemaVersion uint32
}

// DiskIncludeEdge is one direct include owned by DocumentID.
type DiskIncludeEdge struct {
	DocumentID       string          `json:"documentId"`
	TargetDocumentID string          `json:"targetDocumentId"`
	Offset           int             `json:"offset"`
	Mode             string          `json:"mode,omitempty"`
	Path             string          `json:"path,omitempty"`
	Payload          json.RawMessage `json:"payload,omitempty"`
}

// DiskDocumentIncludeEdges is the independently persisted direct-edge set for
// one document.
type DiskDocumentIncludeEdges struct {
	DocumentID    string            `json:"documentId"`
	SchemaVersion uint32            `json:"schemaVersion"`
	SourceHash    string            `json:"sourceHash"`
	Fingerprint   string            `json:"fingerprint"`
	Edges         []DiskIncludeEdge `json:"edges"`
}

// DiskDocumentCacheDelta atomically changes only the records named here.
type DiskDocumentCacheDelta struct {
	DocumentID         string
	Head               *DiskDocumentHead
	DeleteHead         bool
	ArtifactUpserts    []DiskDocumentArtifact
	ArtifactDeletes    []DiskDocumentArtifactKey
	IncludeEdges       *DiskDocumentIncludeEdges
	DeleteIncludeEdges bool
}

type diskDocumentEnvelope struct {
	StorageVersion uint32          `json:"storageVersion"`
	SchemaVersion  uint32          `json:"schemaVersion"`
	SourceHash     string          `json:"sourceHash,omitempty"`
	PayloadHash    string          `json:"payloadHash"`
	Payload        json.RawMessage `json:"payload"`
}

type pendingDocumentValue struct {
	bucket []byte
	key    []byte
	value  []byte
	delete bool
}

type DiskCacheEntryKind string

const (
	DiskCacheWorkspaceIndex                  DiskCacheEntryKind = "workspaceIndex"
	DiskCacheWorkspaceIncludeGraph           DiskCacheEntryKind = "workspaceIncludeGraph"
	DiskCacheGraphPayload                    DiskCacheEntryKind = "graphPayload"
	DiskCacheWorkspaceReferenceBatch         DiskCacheEntryKind = "workspaceReferenceBatch"
	DiskCacheWorkspaceLegacyUndefinedGlobals DiskCacheEntryKind = "workspaceLegacyUndefinedGlobals"
	DiskCacheFileBundle                      DiskCacheEntryKind = "fileBundle"
)

type DiskAnalysisCacheOptions struct {
	Enabled     bool
	Directory   string
	TTL         time.Duration
	MaxSize     int64
	Namespace   string
	ToolVersion string
	Gzip        bool
}

type DiskAnalysisSourceMetadata struct {
	FileName    string `json:"fileName"`
	MtimeMS     int64  `json:"mtimeMs"`
	Size        int64  `json:"size"`
	ContentHash string `json:"contentHash,omitempty"`
}

type DiskAnalysisCacheLookup struct {
	Source      DiskAnalysisSourceMetadata `json:"source"`
	SettingsKey string                     `json:"settingsKey"`
}

// DiskFileBundleCacheEntry contains all source-derived data persisted for one lookup.
type DiskFileBundleCacheEntry struct {
	DiskAnalysisCacheLookup `json:",inline"`
	Parsed                  *core.ParsedDocument      `json:"parsed,omitempty"`
	Summary                 DiskFileAnalysisSummary   `json:"summary,omitempty"`
	PublicSignature         any                       `json:"publicSignature,omitempty"`
	AnalysisSnapshot        json.RawMessage           `json:"analysisSnapshot,omitempty"`
	Diagnostics             []lsp.Diagnostic          `json:"diagnostics,omitempty"`
	BuilderState            *DiskAnalysisBuilderState `json:"builderState,omitempty"`
	UpdateParsed            bool                      `json:"-"`
	UpdateDiagnostics       bool                      `json:"-"`
}

type DiskWorkspaceIndexedDocument struct {
	URI         string `json:"uri"`
	FileName    string `json:"fileName"`
	MtimeMS     int64  `json:"mtimeMs"`
	Size        int64  `json:"size"`
	ContentHash string `json:"contentHash,omitempty"`
	Text        string `json:"text,omitempty"`
}

type DiskWorkspaceIndexCacheEntry struct {
	SettingsKey string                         `json:"settingsKey"`
	Entries     []DiskWorkspaceIndexedDocument `json:"entries"`
}

type DiskWorkspaceIncludeGraphCacheEntry struct {
	SettingsKey string              `json:"settingsKey"`
	Entries     []IncludeGraphEntry `json:"entries"`
}

type DiskGraphPayloadCacheEntry struct {
	SettingsKey string          `json:"settingsKey"`
	Payload     json.RawMessage `json:"payload"`
}

// DiskWorkspaceReferenceBatchCacheEntry stores an opaque workspace reference-analysis batch.
type DiskWorkspaceReferenceBatchCacheEntry struct {
	SettingsKey string          `json:"settingsKey"`
	Payload     json.RawMessage `json:"payload"`
}

// DiskWorkspaceLegacyUndefinedGlobalsCacheEntry stores an opaque workspace legacy undefined-global catalog.
type DiskWorkspaceLegacyUndefinedGlobalsCacheEntry struct {
	SettingsKey string          `json:"settingsKey"`
	Payload     json.RawMessage `json:"payload"`
}

type DiskAnalysisBuilderState struct {
	PublicSignature              any               `json:"publicSignature,omitempty"`
	IncludeDeps                  []any             `json:"includeDeps,omitempty"`
	ExternalRefUsageKeys         []string          `json:"externalRefUsageKeys,omitempty"`
	DiagnosticsLayerFingerprints map[string]string `json:"diagnosticsLayerFingerprints,omitempty"`
}

type DiskFileAnalysisSummary struct {
	URI                 string                `json:"uri"`
	Fingerprint         string                `json:"fingerprint"`
	PublicSignatureHash string                `json:"publicSignatureHash"`
	DefaultLanguage     core.EmbeddedLanguage `json:"defaultLanguage"`
	LanguageRegions     []core.Region         `json:"languageRegions"`
	IncludeRefs         []DiskIncludeRef      `json:"includeRefs"`
	Diagnostics         []lsp.Diagnostic      `json:"diagnostics"`
}

type DiskIncludeRef struct {
	Offset         int       `json:"offset"`
	Range          lsp.Range `json:"range"`
	DirectiveRange lsp.Range `json:"directiveRange"`
	Mode           string    `json:"mode"`
	ModeRange      lsp.Range `json:"modeRange"`
	Path           string    `json:"path"`
	PathRange      lsp.Range `json:"pathRange"`
}

type persistedDiskEntry struct {
	Kind                            DiskCacheEntryKind             `json:"kind"`
	FormatVersion                   int                            `json:"formatVersion"`
	ToolVersion                     string                         `json:"toolVersion"`
	Namespace                       string                         `json:"namespace"`
	WrittenAt                       int64                          `json:"writtenAt"`
	Source                          DiskAnalysisSourceMetadata     `json:"source"`
	ParsedSettingsKey               string                         `json:"parsedSettingsKey,omitempty"`
	DiagnosticsSettingsKey          string                         `json:"diagnosticsSettingsKey,omitempty"`
	SettingsKey                     string                         `json:"settingsKey,omitempty"`
	Diagnostics                     []lsp.Diagnostic               `json:"diagnostics,omitempty"`
	BuilderState                    *DiskAnalysisBuilderState      `json:"builderState,omitempty"`
	Summary                         DiskFileAnalysisSummary        `json:"summary,omitempty"`
	PublicSignature                 any                            `json:"publicSignature,omitempty"`
	Parsed                          *core.ParsedDocument           `json:"parsed,omitempty"`
	WorkspaceEntries                []DiskWorkspaceIndexedDocument `json:"entries,omitempty"`
	WorkspaceIncludeGraphEntries    []IncludeGraphEntry            `json:"workspaceIncludeGraphEntries,omitempty"`
	GraphPayload                    json.RawMessage                `json:"graphPayload,omitempty"`
	WorkspaceReferenceBatch         json.RawMessage                `json:"workspaceReferenceBatch,omitempty"`
	WorkspaceLegacyUndefinedGlobals json.RawMessage                `json:"workspaceLegacyUndefinedGlobals,omitempty"`
	FileAnalysisSnapshot            json.RawMessage                `json:"fileAnalysisSnapshot,omitempty"`
}

type pendingDiskWrite struct {
	bucket     []byte
	key        []byte
	entry      persistedDiskEntry
	components byte
}

type pendingReferenceWrite struct {
	bucket    []byte
	key       []byte
	value     []byte
	delete    bool
	writtenAt int64
}

type diskCacheWriterCommand struct {
	close bool
	done  chan error
}

type DiskAnalysisCache struct {
	root         string
	databasePath string
	ttl          time.Duration
	maxSize      int64
	enabled      bool
	namespace    string
	toolVersion  string
	gzip         bool

	dbMu       sync.RWMutex
	db         *bolt.DB
	mutationMu sync.Mutex

	lifecycleMu                   sync.RWMutex
	closed                        bool
	pendingMu                     sync.Mutex
	pending                       map[string]pendingDiskWrite
	inFlight                      map[string]pendingDiskWrite
	pendingReference              map[string]pendingReferenceWrite
	inFlightReference             map[string]pendingReferenceWrite
	pendingReferenceDocumentKeys  map[string]map[string]struct{}
	inFlightReferenceDocumentKeys map[string]map[string]struct{}
	notify                        chan struct{}
	commands                      chan diskCacheWriterCommand
	writerDone                    chan struct{}
	sweepWG                       sync.WaitGroup
	sweepScheduled                atomic.Bool
	nextSweepSize                 atomic.Int64
}

var diskAnalysisDecMode = func() cbor.DecMode {
	mode, err := (cbor.DecOptions{DefaultMapType: reflect.TypeOf(map[string]any{})}).DecMode()
	if err != nil {
		panic(err)
	}
	return mode
}()

// NewDiskAnalysisCache opens the workspace-scoped bbolt analysis cache.
