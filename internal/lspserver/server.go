package lspserver

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/embedded"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type Server struct {
	in                                               io.Reader
	out                                              io.Writer
	err                                              io.Writer
	mu                                               sync.Mutex
	writeMu                                          sync.Mutex
	writeFatalErr                                    error
	writeFatalCh                                     chan struct{}
	diagnosticPublicationGateOnce                    sync.Once
	diagnosticPublicationGate                        chan struct{}
	debugLogWriter                                   *debugLogFileWriter
	debugLogTargets                                  debugLogTargetCache
	htmlMu                                           sync.Mutex
	cssMu                                            sync.Mutex
	javascriptMu                                     sync.Mutex
	documents                                        map[string]*core.TextDocument
	documentStore                                    *workspacepkg.DocumentStore
	sourceSnapshots                                  map[string]*sourceFileSnapshot
	sourceReadInflight                               map[string]*sourceReadInflight
	parsedCache                                      map[string]parsedDocumentCacheEntry
	parsedInflight                                   map[string]*parsedAnalysisInflight
	parsedCacheRevisions                             map[string]uint64
	analysisCache                                    *analysisCache
	analysisWorkers                                  *analysisWorkerPool
	diskAnalysisCache                                *workspacepkg.DiskAnalysisCache
	diskCacheOpenWarningLogged                       bool
	fsGateway                                        *workspacepkg.FsGateway
	includeReadLimiter                               chan struct{}
	memoryBudget                                     *workspacepkg.MemoryBudgetManager
	memoryPressureTimer                              *time.Timer
	memoryPressureReason                             string
	memoryPressureCheckCost                          time.Duration
	memoryPressureCheckRunning                       bool
	memoryPressurePending                            bool
	rootPath                                         string
	rootURI                                          string
	workspaceRoots                                   []workspaceRoot
	trustedFilesystemRootCache                       map[string]trustedFilesystemRoot
	trustedFilesystemRootCacheGeneration             uint64
	trustedRootStats                                 trustedRootStatCoalescer
	trustedPaths                                     trustedPathCache
	workspace                                        map[string]*core.TextDocument
	workspaceURIs                                    workspaceURIIndex
	html                                             embedded.HTML
	css                                              embedded.CSS
	semantic                                         map[string]semanticTokenCache
	semanticInflight                                 map[string]*semanticTokensInflight
	semanticHistory                                  map[string][]int
	referenceBatch                                   map[workspaceReferenceBatchKey]*workspaceReferenceBatchState
	referenceCounts                                  map[workspaceReferenceTargetKey]int
	referencePartialCounts                           map[workspaceReferenceTargetKey]int
	referenceResults                                 map[workspaceReferenceTargetKey][]lsp.Location
	referenceInflight                                map[workspaceReferenceTargetKey]*workspaceReferenceInflight
	referenceDocuments                               map[string][]*core.ParsedDocument
	referenceScopes                                  map[workspaceReferenceScopeCacheKey]workspaceReferenceScopeSnapshot
	referenceImplicitPlans                           map[workspaceReferenceImplicitPlanKey]map[string]map[string]struct{}
	referencePreviousCounts                          map[workspaceReferencePreviousKey]int
	referenceNameRevisions                           map[string]uint64
	referenceNameIndexReady                          bool
	referenceTargetsByName                           map[string]map[workspaceReferenceTargetKey]workspaceReferenceTargetCacheKind
	referenceUnnamedTargets                          map[workspaceReferenceTargetKey]workspaceReferenceTargetCacheKind
	referenceBatchesByName                           map[string]map[workspaceReferenceBatchKey]struct{}
	referenceImplicitPlansByName                     map[string]map[workspaceReferenceImplicitPlanKey]struct{}
	referenceCountSummariesRestored                  map[*core.ParsedDocument]struct{}
	referenceDeclarationPlans                        map[*core.ParsedDocument]workspaceReferenceDeclarationPlan
	referenceDescriptorFingerprints                  map[string]*workspaceReferenceDescriptorFingerprintCache
	referenceWorkspaceIndex                          *workspaceReferenceIndex
	referenceGeneration                              uint64
	referencePendingDocumentCountReuse               map[workspaceDocumentID]*workspaceReferenceDocumentCountSnapshot
	workspaceReferenceReadyFingerprint               string
	workspaceReferencePendingPromotion               *workspaceReferenceCountPromotion
	diagnosticTimers                                 map[string]*time.Timer
	diagnosticJobs                                   map[string]*diagnosticRevisionJob
	diagnosticRetiredJobs                            map[*diagnosticRevisionJob]struct{}
	diagnosticJobSequence                            uint64
	documentOpenAnalysisJobs                         map[string]*documentOpenAnalysisJob
	documentOpenAnalysisSequence                     uint64
	documentOpenAnalysisWorkers                      sync.WaitGroup
	backgroundAnalysisWorkers                        sync.WaitGroup
	validatedDocumentVersions                        map[string]int
	publishedDiagnosticTargets                       map[string]map[string]string
	publishedDiagnosticItems                         map[string]map[string][]lsp.Diagnostic
	publishedDiagnosticRevisions                     map[string]map[string]diagnosticTargetRevision
	diagnosticPublicationBatchTestHook               func()
	workspaceDiagnosticsProcessCache                 bool
	workspaceDiagnosticsItems                        map[string]workspaceDiagnosticsItemCacheEntry
	workspaceDiagnosticsRevisions                    map[string]uint64
	workspaceDiagnosticsEpoch                        atomic.Uint64
	workspaceDiagnosticsLastPass                     workspaceDiagnosticsPassState
	workspaceArtifacts                               map[workspaceDocumentID]*workspaceDocumentArtifactManifest
	workspaceArtifactRevisions                       map[workspaceDocumentID]uint64
	workspaceVBAutoIncludeCatalog                    *workspaceVBAutoIncludeCatalog
	consumersByExportName                            map[string]map[workspaceDocumentID]struct{}
	queriesByDocumentName                            map[workspaceDocumentID]map[string]struct{}
	scopesByDocument                                 map[workspaceDocumentID]map[string]struct{}
	implicitPlansByFamilyName                        map[string]map[string]struct{}
	graphCache                                       map[string]graph.Payload
	graphBackgroundTasks                             map[string]context.CancelFunc
	graphBackgroundBuilds                            map[string]*graphBackgroundBuild
	graphBackgroundWorkers                           sync.WaitGroup
	graphGeneration                                  uint64
	legacyUndefinedGlobalCatalog                     *LegacyUndefinedGlobalCatalog
	legacyUndefinedGlobalCatalogGeneration           uint64
	legacyUndefinedGlobalCatalogSettingsFingerprint  string
	legacyUndefinedGlobalBuilds                      map[legacyUndefinedGlobalBuildKey]*legacyUndefinedGlobalBuild
	legacyUndefinedGlobalBackgroundCancel            context.CancelFunc
	legacyUndefinedGlobalProgressTestHook            func(label, detail string, current, total int)
	workspaceIncludeGraph                            *workspacepkg.WorkspaceIncludeGraph
	workspaceIncludeGraphRevision                    uint64
	workspaceIncludeGraphComplete                    bool
	javascriptProject                                *tsgoadapter.Project
	javascriptPreparation                            *javaScriptProjectPreparation
	javascriptDocumentGeneration                     uint64
	javascriptMappingGeneration                      uint64
	javascriptProjectConfigCache                     map[javaScriptProjectConfigCacheKey]javaScriptProjectConfigCacheEntry
	requestCancellations                             map[string]requestCancellationEntry
	pendingClientRequests                            map[string]pendingClientRequest
	nextClientRequestID                              uint64
	progressCancellations                            map[string]context.CancelFunc
	progressTaskSequence                             uint64
	progressTasks                                    map[string]*serverProgressTask
	progressPublishTimer                             *time.Timer
	progressLastPublished                            time.Time
	progressPendingReason                            string
	progressPublisherClosed                          bool
	progressPublishInFlight                          bool
	progressPendingStatus                            *progressStatusPublication
	progressPublishWG                                sync.WaitGroup
	diskCacheWrites                                  sync.WaitGroup
	diskCacheWritesClosed                            bool
	diskCacheWriteQueue                              []diskCacheWriteTask
	diskCacheWritePending                            map[string]func()
	diskCacheWriteDeferred                           map[string]func()
	diskCacheWriteWorkerRunning                      bool
	workspaceIncludeGraphPersisting                  bool
	workspaceIncludeGraphPersistKey                  string
	workspaceIncludeGraphPersistAt                   time.Time
	workspaceIndexWorkers                            sync.WaitGroup
	workspaceIndexStateMu                            sync.RWMutex
	workspaceIndexCancel                             context.CancelFunc
	workspaceIndexParent                             context.Context
	workspaceIndexDone                               chan struct{}
	workspaceReferenceIndexReadySignal               chan struct{}
	workspaceIndexDiskCacheUseMu                     sync.Mutex
	workspaceIndexDiskCommitMu                       sync.Mutex
	workspaceCacheRestoreLimiter                     chan struct{}
	workspaceIndexGeneration                         uint64
	workspaceReferenceIndexReadyGeneration           uint64
	workspaceIndexEnabled                            bool
	workspaceIndexSchedulingPaused                   bool
	workspaceIndexClosed                             bool
	workspaceIndexTestHook                           func(context.Context, workspaceIndexTestPhase, uint64)
	workspaceCacheReadTestHook                       func(string)
	workspaceMetadataValidationTestHook              func(context.Context, string)
	workspaceIncludeGraphSyncTestHook                func() bool
	workspaceWalkDirTestHook                         func(string) error
	workspaceFileReadTestHook                        func(string)
	workspaceIncludeGraphDocumentTestHook            func(context.Context, string)
	includeExpansionTestHook                         func()
	serverObjectLookupTestHook                       func()
	documentParseTestHook                            func(string)
	workspaceVBAutoIncludeTransientParseTestHook     func(string)
	fileAnalysisSnapshotTestHook                     func()
	javascriptWorkspaceWalkTestHook                  func(string)
	javascriptDocumentsTestHook                      func(int)
	javascriptProjectDeltaTestHook                   func(int, int)
	workspaceReferencePersistenceBeforeQueueTestHook func()
	workspaceReferenceTombstoneBeforeQueueTestHook   func()
	requestDispatchTestHook                          func(context.Context, string)
	graphBackgroundTransitionTestHook                func(string)
	runtimeCacheDiskClearBeforeLifecycleTestHook     func(context.Context)
	maintenanceCommandAdmission                      chan struct{}
	semanticTokensRefreshSupported                   bool
	inlayHintRefreshSupported                        bool
	codeLensRefreshSupported                         bool
	codeLensRefreshTimer                             *time.Timer
	codeLensRefreshSequence                          uint64
	workspaceConfigurationSupported                  bool
	shutdown                                         bool
	clientLocale                                     string
	settings                                         serverSettings
}
