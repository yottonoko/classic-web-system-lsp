package lspserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/embedded"
	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const requestAdmissionCapacity = 32

// Editor interactions arrive in mixed bursts across hover, completion, and
// navigation providers. Keep their count bound above routine client fan-out;
// the lane's retained-byte limit remains the primary memory bound.
const interactiveRequestAdmissionCapacity = 256
const lifecycleRequestAdmissionCapacity = 1
const requestAdmissionByteCapacity = maxRPCMessageBytes + 1024
const notificationQueueCapacity = 1024
const notificationQueueByteCapacity = 2 * maxRPCMessageBytes

// Serve accepts an input only when it can terminate a blocked read by closing
// it, or when it is one of the standard finite in-memory reader types. A closer
// supplied by a caller must make Close interrupt Read; the server does not wrap
// readers in a no-op closer or create an unbounded read goroutine.
var errServerInputNotInterruptible = errors.New("lsp server input reader must be closable or a standard finite reader")

func validateServerInput(in io.Reader) error {
	if in == nil {
		return errServerInputNotInterruptible
	}
	if _, ok := in.(io.Closer); ok {
		return nil
	}
	switch in.(type) {
	case *bytes.Buffer, *bytes.Reader, *strings.Reader:
		return nil
	}
	return errServerInputNotInterruptible
}

type requestAdmissionLane struct {
	mu       sync.Mutex
	slots    chan struct{}
	bytes    int
	maxBytes int
}

type requestAdmissionToken struct {
	once  sync.Once
	lane  *requestAdmissionLane
	bytes int
}

func newRequestAdmissionLane(capacity int) *requestAdmissionLane {
	return &requestAdmissionLane{
		slots:    make(chan struct{}, capacity),
		maxBytes: requestAdmissionByteCapacity,
	}
}

func (lane *requestAdmissionLane) tryAcquire(bytes int) *requestAdmissionToken {
	select {
	case lane.slots <- struct{}{}:
	default:
		return nil
	}
	lane.mu.Lock()
	if bytes > lane.maxBytes-lane.bytes {
		lane.mu.Unlock()
		<-lane.slots
		return nil
	}
	lane.bytes += bytes
	lane.mu.Unlock()
	return &requestAdmissionToken{lane: lane, bytes: bytes}
}

func (token *requestAdmissionToken) release() {
	if token == nil || token.lane == nil {
		return
	}
	token.once.Do(func() {
		token.lane.mu.Lock()
		token.lane.bytes -= token.bytes
		token.lane.mu.Unlock()
		<-token.lane.slots
	})
}

type serverWorkItem struct {
	message    *rpcMessage
	ctx        context.Context
	cancel     context.CancelFunc
	kind       rpcMessageKind
	class      requestWorkClass
	admission  *requestAdmissionToken
	sequence   uint64
	receivedAt time.Time
}

// serverWorkQueue preserves wire order without letting notification backlog
// occupy the request admission lanes. Control notifications and client
// responses remain on the reader's direct path.
type serverWorkQueue struct {
	mu                sync.Mutex
	items             []serverWorkItem
	notificationCount int
	notificationBytes int
	wake              chan struct{}
	closed            bool
}

func newServerWorkQueue() *serverWorkQueue {
	return &serverWorkQueue{wake: make(chan struct{}, 1)}
}

func (q *serverWorkQueue) push(item serverWorkItem) bool {
	q.mu.Lock()
	notificationBytes := len(item.message.Method) + len(item.message.Params)
	if q.closed || (item.kind == rpcNotification &&
		(q.notificationCount >= notificationQueueCapacity || q.notificationBytes+notificationBytes > notificationQueueByteCapacity)) {
		q.mu.Unlock()
		return false
	}
	q.items = append(q.items, item)
	if item.kind == rpcNotification {
		q.notificationCount++
		q.notificationBytes += notificationBytes
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return true
}

func (q *serverWorkQueue) pop() (serverWorkItem, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			item := q.items[0]
			q.items[0] = serverWorkItem{}
			q.items = q.items[1:]
			if len(q.items) == 0 {
				q.items = nil
			}
			if item.kind == rpcNotification {
				q.notificationCount--
				q.notificationBytes -= len(item.message.Method) + len(item.message.Params)
			}
			q.mu.Unlock()
			return item, true
		}
		if q.closed {
			q.mu.Unlock()
			return serverWorkItem{}, false
		}
		q.mu.Unlock()
		<-q.wake
	}
}

func (q *serverWorkQueue) close(drain bool) []serverWorkItem {
	q.mu.Lock()
	q.closed = true
	var dropped []serverWorkItem
	if !drain {
		dropped = q.items
		q.items = nil
		q.notificationCount = 0
		q.notificationBytes = 0
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return dropped
}

func New(in io.Reader, out io.Writer, err io.Writer) *Server {
	if err == nil {
		err = io.Discard
	}
	server := &Server{
		in:                              in,
		out:                             out,
		err:                             err,
		writeFatalCh:                    make(chan struct{}),
		documents:                       map[string]*core.TextDocument{},
		documentStore:                   workspacepkg.NewDocumentStore(),
		sourceSnapshots:                 map[string]*sourceFileSnapshot{},
		sourceReadInflight:              map[string]*sourceReadInflight{},
		parsedCache:                     map[string]parsedDocumentCacheEntry{},
		parsedInflight:                  map[string]*parsedAnalysisInflight{},
		parsedCacheRevisions:            map[string]uint64{},
		analysisCache:                   newAnalysisCache(),
		analysisWorkers:                 newAnalysisWorkerPoolFromEnv(),
		workspace:                       map[string]*core.TextDocument{},
		html:                            embedded.NewHTML(),
		css:                             embedded.NewCSS(),
		semantic:                        map[string]semanticTokenCache{},
		semanticInflight:                map[string]*semanticTokensInflight{},
		semanticHistory:                 map[string][]int{},
		referenceBatch:                  map[workspaceReferenceBatchKey]*workspaceReferenceBatchState{},
		referenceCounts:                 map[workspaceReferenceTargetKey]int{},
		referencePartialCounts:          map[workspaceReferenceTargetKey]int{},
		referenceResults:                map[workspaceReferenceTargetKey][]lsp.Location{},
		referenceInflight:               map[workspaceReferenceTargetKey]*workspaceReferenceInflight{},
		referenceDocuments:              map[string][]*core.ParsedDocument{},
		referenceScopes:                 map[workspaceReferenceScopeCacheKey]workspaceReferenceScopeSnapshot{},
		referenceImplicitPlans:          map[workspaceReferenceImplicitPlanKey]map[string]map[string]struct{}{},
		referencePreviousCounts:         map[workspaceReferencePreviousKey]int{},
		referenceNameRevisions:          map[string]uint64{},
		referenceCountSummariesRestored: map[*core.ParsedDocument]struct{}{},
		referenceDeclarationPlans:       map[*core.ParsedDocument]workspaceReferenceDeclarationPlan{},
		referenceDescriptorFingerprints: map[string]*workspaceReferenceDescriptorFingerprintCache{},
		referenceWorkspaceIndex:         newWorkspaceReferenceIndex(),
		diagnosticTimers:                map[string]*time.Timer{},
		diagnosticJobs:                  map[string]*diagnosticRevisionJob{},
		diagnosticRetiredJobs:           map[*diagnosticRevisionJob]struct{}{},
		documentOpenAnalysisJobs:        map[string]*documentOpenAnalysisJob{},
		validatedDocumentVersions:       map[string]int{},
		publishedDiagnosticTargets:      map[string]map[string]string{},
		publishedDiagnosticItems:        map[string]map[string][]lsp.Diagnostic{},
		publishedDiagnosticRevisions:    map[string]map[string]diagnosticTargetRevision{},
		workspaceDiagnosticsItems:       map[string]workspaceDiagnosticsItemCacheEntry{},
		workspaceDiagnosticsRevisions:   map[string]uint64{},
		workspaceArtifacts:              map[workspaceDocumentID]*workspaceDocumentArtifactManifest{},
		workspaceArtifactRevisions:      map[workspaceDocumentID]uint64{},
		workspaceVBAutoIncludeCatalog:   newWorkspaceVBAutoIncludeCatalog(0, false, nil),
		consumersByExportName:           map[string]map[workspaceDocumentID]struct{}{},
		queriesByDocumentName:           map[workspaceDocumentID]map[string]struct{}{},
		scopesByDocument:                map[workspaceDocumentID]map[string]struct{}{},
		implicitPlansByFamilyName:       map[string]map[string]struct{}{},
		graphCache:                      map[string]graph.Payload{},
		graphBackgroundTasks:            map[string]context.CancelFunc{},
		graphBackgroundBuilds:           map[string]*graphBackgroundBuild{},
		workspaceIncludeGraph:           workspacepkg.NewWorkspaceIncludeGraph(),
		workspaceCacheRestoreLimiter:    make(chan struct{}, 1),
		javascriptProjectConfigCache:    map[javaScriptProjectConfigCacheKey]javaScriptProjectConfigCacheEntry{},
		requestCancellations:            map[string]requestCancellationEntry{},
		pendingClientRequests:           map[string]pendingClientRequest{},
		progressCancellations:           map[string]context.CancelFunc{},
		maintenanceCommandAdmission:     make(chan struct{}, 1),
		progressTasks:                   map[string]*serverProgressTask{},
		settings:                        defaultServerSettings(),
	}
	server.referencePendingDocumentCountReuse = map[workspaceDocumentID]*workspaceReferenceDocumentCountSnapshot{}
	server.debugLogWriter = newDebugLogFileWriter(func(message string) {
		server.reportAsyncRPCWriteError(server.writeRPCMessage(rpcMessage{Method: "window/logMessage", Params: mustRaw(map[string]any{"type": 2, "message": message})}))
	})
	server.referenceWorkspaceIndex.setWorkerPool(server.analysisWorkers)
	server.configureRuntimeCaches()
	server.configureFsGateway()
	server.configureAnalysisWorkers()
	return server
}

func (s *Server) Serve(ctx context.Context) error {
	if err := validateServerInput(s.in); err != nil {
		return err
	}
	defer func() {
		s.debugLogWriter.wait()
		s.debugLogWriter.closeRoots()
	}()
	defer s.shutdownRuntimeCaches()
	defer s.installEmbeddedPanicReporter()()
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	fatalSignal := s.writeFatalSignal()
	var closeInputOnce sync.Once
	closeInput := func() {
		closeInputOnce.Do(func() {
			if closer, ok := s.in.(io.Closer); ok {
				_ = closer.Close()
			}
		})
	}
	fatalWatcherDone := make(chan struct{})
	go func() {
		defer close(fatalWatcherDone)
		select {
		case <-fatalSignal:
			closeInput()
		case <-serveCtx.Done():
			closeInput()
		}
	}()
	defer func() {
		cancelServe()
		<-fatalWatcherDone
	}()
	reader := bufio.NewReader(s.in)
	work := newServerWorkQueue()
	interactiveRequestAdmission := newRequestAdmissionLane(interactiveRequestAdmissionCapacity)
	regularRequestAdmission := newRequestAdmissionLane(requestAdmissionCapacity)
	backgroundRequestAdmission := newRequestAdmissionLane(requestAdmissionCapacity)
	// initialize and shutdown use separate lanes so a client that pipelines
	// shutdown before the initialize response is not rejected as overloaded.
	initializeRequestAdmission := newRequestAdmissionLane(lifecycleRequestAdmissionCapacity)
	shutdownRequestAdmission := newRequestAdmissionLane(lifecycleRequestAdmissionCapacity)
	requestAdmissionFor := func(method string, class requestWorkClass) *requestAdmissionLane {
		switch method {
		case "initialize":
			return initializeRequestAdmission
		case "shutdown":
			return shutdownRequestAdmission
		}
		switch class {
		case requestWorkInteractive:
			return interactiveRequestAdmission
		case requestWorkBackground:
			return backgroundRequestAdmission
		default:
			return regularRequestAdmission
		}
	}
	workerDone := make(chan error, 1)
	workerFailed := make(chan error, 1)
	rejectRequest := func(message *rpcMessage, receivedAt time.Time, rpcErr *rpcError) {
		requestID := rpcMessageIDForWire(message)
		writeErr := s.writeRPCMessage(rpcMessage{ID: requestID, Error: rpcErr})
		s.reportAsyncRPCWriteError(writeErr)
		s.logCompletedLSPEvent(requestID, message.Method, "request", receivedAt, rpcErr, writeErr, message.logSpan)
	}
	completeDroppedWork := func(items []serverWorkItem, cause error) {
		for _, item := range items {
			if item.kind == rpcRequest {
				s.unregisterRequestCancellationAtSequence(item.message.ID, item.cancel, item.sequence)
				item.admission.release()
				rejectRequest(item.message, item.receivedAt, requestCancelledError())
				continue
			}
			s.logCompletedLSPEvent(nil, item.message.Method, "notification", item.receivedAt, nil, cause, item.message.logSpan)
		}
	}
	go func() {
		var requests sync.WaitGroup
		requestScheduler := newRequestExecutionScheduler()
		var firstErr error
		var firstErrOnce sync.Once
		recordError := func(err error) {
			if err != nil {
				firstErrOnce.Do(func() {
					firstErr = err
					workerFailed <- err
				})
			}
		}
		handleRequestItem := func(item serverWorkItem) {
			if item.admission != nil {
				defer item.admission.release()
			}
			message := item.message
			var result any
			var rpcErr *rpcError
			if item.ctx.Err() != nil {
				rpcErr = requestCancelledError()
			} else {
				result, rpcErr = s.handleRequest(item.ctx, message.Method, message.Params)
				if item.ctx.Err() != nil {
					result = nil
					rpcErr = requestCancelledError()
				}
			}
			response := rpcMessage{ID: rpcMessageIDForWire(message)}
			requestID := rpcMessageIDForWire(message)
			if rpcErr != nil {
				response.Error = rpcErr
			} else if result == nil {
				response.Result = json.RawMessage("null")
			} else {
				response.Result = result
			}
			writeErr := s.writeRPCMessage(response)
			// Keep cancellation registered until the complete response frame has
			// either been delivered or the transport has become fatal. A zero-byte
			// write is retried by writeRPCMessage; it must not silently turn into a
			// completed request with no response.
			s.unregisterRequestCancellationAtSequence(message.ID, item.cancel, item.sequence)
			s.logCompletedLSPEvent(requestID, message.Method, "request", item.receivedAt, rpcErr, writeErr, message.logSpan)
			if writeErr != nil && !isRPCWriteZeroFailure(writeErr) {
				recordError(writeErr)
			}
		}
		for {
			item, ok := work.pop()
			if !ok {
				break
			}
			message := item.message
			if item.kind == rpcRequest {
				if serialLSPRequest(message.Method, message.Params) {
					requests.Wait()
					handleRequestItem(item)
					continue
				}
				requests.Add(1)
				go func(item serverWorkItem) {
					defer requests.Done()
					release, acquired := requestScheduler.acquire(item.ctx, item.class)
					if acquired {
						defer release()
					}
					handleRequestItem(item)
				}(item)
				continue
			}
			validRevision := message.revisionNotificationChecked && message.revisionNotificationValid
			if !validRevision {
				requests.Wait()
			} else {
				s.cancelRequestsBefore(item.sequence)
			}
			err := s.handleNotificationMessage(item.ctx, message)
			s.logCompletedLSPEvent(nil, message.Method, "notification", item.receivedAt, nil, err, message.logSpan)
			var malformed *malformedNotificationError
			if !errors.As(err, &malformed) && !isRPCWriteZeroFailure(err) {
				recordError(err)
			}
		}
		requests.Wait()
		workerDone <- firstErr
	}()
	var closeWorkOnce sync.Once
	var droppedWork []serverWorkItem
	closeWork := func(drain bool) {
		closeWorkOnce.Do(func() { droppedWork = work.close(drain) })
	}
	var stopWorkerOnce sync.Once
	var stopWorkerErr error
	stopWorker := func(drain bool, cause error) error {
		stopWorkerOnce.Do(func() {
			closeWork(drain)
			completeDroppedWork(droppedWork, cause)
			stopWorkerErr = <-workerDone
		})
		return stopWorkerErr
	}
	// Close the queue before cancelling: cancelling one in-flight request can
	// release the worker, which must not then start a queued request whose
	// cancellation has not been applied yet.
	abortWorker := func(cause error) error {
		closeWork(false)
		s.cancelAllRequests()
		cancelServe()
		return stopWorker(false, cause)
	}
	stopOnFatal := func() error {
		fatal := s.writeFatalError()
		if fatal == nil {
			return nil
		}
		_ = abortWorker(fatal)
		return fatal
	}
	stopOnContextDone := func() error {
		if fatal := s.writeFatalError(); fatal != nil {
			return stopOnFatal()
		}
		// A parent cancellation must interrupt every in-flight request. Normal
		// EOF and the exit notification still drain lifecycle requests so their
		// responses remain observable before Serve returns.
		if ctx.Err() != nil {
			s.cancelAllRequests()
		}
		cancelServe()
		workerErr := stopWorker(true, nil)
		if fatal := s.writeFatalError(); fatal != nil {
			return fatal
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return workerErr
	}
	var incomingSequence uint64
	for {
		select {
		case <-serveCtx.Done():
			return stopOnContextDone()
		case <-fatalSignal:
			return stopOnFatal()
		case err := <-workerDone:
			if fatal := s.writeFatalError(); fatal != nil {
				return fatal
			}
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			return err
		case err := <-workerFailed:
			if fatal := s.writeFatalError(); fatal != nil {
				_ = abortWorker(fatal)
				return fatal
			}
			_ = abortWorker(err)
			if fatal := s.writeFatalError(); fatal != nil {
				return fatal
			}
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			return err
		default:
		}
		message, err := readMessage(reader)
		if err != nil {
			if fatal := s.writeFatalError(); fatal != nil {
				_ = abortWorker(fatal)
				return fatal
			}
			if contextErr := ctx.Err(); contextErr != nil {
				return stopOnContextDone()
			}
			var bodyParseErr *rpcBodyParseError
			if errors.As(err, &bodyParseErr) {
				s.reportAsyncRPCWriteError(s.writeRPCMessage(rpcMessage{ID: json.RawMessage("null"), Error: parseError()}))
				continue
			}
			var invalidBodyErr *rpcInvalidRequestBodyError
			if errors.As(err, &invalidBodyErr) {
				s.reportAsyncRPCWriteError(s.writeRPCMessage(rpcMessage{ID: json.RawMessage("null"), Error: invalidRPCRequestError()}))
				continue
			}
			if errors.Is(err, io.EOF) {
				return stopOnContextDone()
			}
			_ = abortWorker(err)
			if fatal := s.writeFatalError(); fatal != nil {
				return fatal
			}
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			return err
		}
		if fatal := s.writeFatalError(); fatal != nil {
			_ = abortWorker(fatal)
			return fatal
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return stopOnContextDone()
		}
		receivedAt := time.Now()
		kind, rpcErr := classifyRPCMessage(message)
		if rpcErr != nil {
			// A notification cannot receive a response, even if its payload is
			// otherwise invalid. Requests and malformed envelopes do receive the
			// standard Invalid Request response.
			if !isValidRPCNotification(message) {
				id := any(json.RawMessage("null"))
				if message.idPresent && validRPCResponseID(message.ID) {
					id = rpcMessageIDForWire(message)
				}
				s.reportAsyncRPCWriteError(s.writeRPCMessage(rpcMessage{ID: id, Error: rpcErr}))
			}
			continue
		}
		_, message.logSpan = newRuntimeLogSpan(serveCtx)
		if kind == rpcNotification && message.Method == "$/cancelRequest" {
			s.logInboundLSPEvent(message, "notification", receivedAt)
			s.cancelRequest(message.Params)
			s.logCompletedLSPEvent(nil, message.Method, "notification", receivedAt, nil, nil, message.logSpan)
			continue
		}
		if kind == rpcResponse {
			pending, matched := s.deliverClientResponse(*message)
			s.logInboundClientResponse(message, receivedAt, pending, matched)
			s.logCompletedClientResponse(message, receivedAt, pending, matched)
			continue
		}
		kindName := "notification"
		if kind == rpcRequest {
			kindName = "request"
		}
		s.logInboundLSPEvent(message, kindName, receivedAt)
		if kind == rpcNotification && message.Method == "exit" {
			s.logCompletedLSPEvent(nil, message.Method, "notification", receivedAt, nil, nil, message.logSpan)
			return stopOnContextDone()
		}
		var validRevision bool
		if kind == rpcNotification {
			validRevision = prepareRevisionAdvancingNotification(message)
		}
		incomingSequence++
		if validRevision {
			s.cancelRequestsBefore(incomingSequence)
		}
		item := serverWorkItem{message: message, ctx: context.WithValue(serveCtx, runtimeLogSpanKey{}, message.logSpan), kind: kind, sequence: incomingSequence, receivedAt: receivedAt}
		if kind == rpcRequest {
			item.class = classifyRequestWork(message.Method, message.Params)
			parent := serveCtx
			// Lifecycle responses must survive an immediately following exit or EOF.
			if lifecycleRequest(message.Method) {
				parent = ctx
			}
			item.ctx, item.cancel = context.WithCancel(context.WithValue(parent, runtimeLogSpanKey{}, message.logSpan))
			if !s.registerRequestCancellationEntry(message.ID, requestCancellationEntry{cancel: item.cancel, sequence: item.sequence, revisionIndependent: revisionIndependentRequest(message.Method)}) {
				item.cancel()
				rejectRequest(message, receivedAt, duplicateRequestIDError())
				continue
			}
			admission := requestAdmissionFor(message.Method, item.class)
			item.admission = admission.tryAcquire(requestRetainedBytes(message))
			if item.admission == nil {
				s.unregisterRequestCancellationAtSequence(message.ID, item.cancel, item.sequence)
				rejectRequest(message, receivedAt, requestOverloadedError())
				continue
			}
		}
		if work.push(item) {
			continue
		}
		queueErr := errors.New("lsp notification queue capacity exceeded")
		if kind == rpcRequest {
			item.admission.release()
			s.unregisterRequestCancellationAtSequence(message.ID, item.cancel, item.sequence)
			rejectRequest(message, receivedAt, requestCancelledError())
		} else {
			s.logCompletedLSPEvent(nil, message.Method, "notification", receivedAt, nil, queueErr, message.logSpan)
		}
		_ = abortWorker(queueErr)
		if fatal := s.writeFatalError(); fatal != nil {
			return fatal
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return queueErr
	}
}

func isValidRPCNotification(message *rpcMessage) bool {
	return message != nil &&
		!message.invalidFields &&
		message.JSONRPC == "2.0" &&
		message.methodPresent &&
		!message.idPresent &&
		!message.resultPresent &&
		!message.errorPresent
}

// Keep one bounded admission slot for each lifecycle transition. initialize is
// included with shutdown because this server has no separate pre-initialized
// admission state; a saturated invalidly ordered stream must not prevent the
// client from completing the LSP handshake.
func lifecycleRequest(method string) bool {
	return method == "initialize" || method == "shutdown"
}

// revisionIndependentRequest reports requests that edits must not cancel. A
// workspace diagnostic pass checks every document; restarting it on each
// keystroke means a large workspace never finishes a pass.
func revisionIndependentRequest(method string) bool {
	return lifecycleRequest(method) || method == "workspace/diagnostic"
}

func serialLSPRequest(method string, _ json.RawMessage) bool {
	return lifecycleRequest(method)
}

func (s *Server) requestClient(ctx context.Context, method string, params any) (rpcMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.nextClientRequestID++
	id := "asp-lsp-go-" + strconv.FormatUint(s.nextClientRequestID, 10)
	pending := pendingClientRequest{method: method, startedAt: time.Now(), response: make(chan rpcMessage, 1)}
	s.pendingClientRequests[requestIDKey(id)] = pending
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pendingClientRequests, requestIDKey(id))
		s.mu.Unlock()
	}()
	if err := s.writeRPCMessage(rpcMessage{ID: id, Method: method, Params: mustRaw(params)}); err != nil {
		return rpcMessage{}, err
	}
	fatalSignal := s.writeFatalSignal()
	select {
	case message := <-pending.response:
		return message, nil
	case <-ctx.Done():
		return rpcMessage{}, ctx.Err()
	case <-fatalSignal:
		if fatal := s.writeFatalError(); fatal != nil {
			return rpcMessage{}, fatal
		}
		return rpcMessage{}, context.Canceled
	}
}

func (s *Server) deliverClientResponse(message rpcMessage) (pendingClientRequest, bool) {
	key := requestIDKey(message.ID)
	s.mu.Lock()
	pending, ok := s.pendingClientRequests[key]
	s.mu.Unlock()
	if !ok || pending.response == nil {
		return pendingClientRequest{}, false
	}
	select {
	case pending.response <- message:
	default:
	}
	return pending, true
}

func requestIDKey(id any) string {
	raw, err := json.Marshal(id)
	if err != nil {
		return ""
	}
	return string(raw)
}

func requestRetainedBytes(message *rpcMessage) int {
	if message == nil {
		return 0
	}
	idBytes := len(requestIDKey(message.ID))
	return len(message.Method) + len(message.Params) + 2*idBytes + 256
}

func (s *Server) registerRequestCancellation(id any, cancel context.CancelFunc, sequence uint64) bool {
	return s.registerRequestCancellationEntry(id, requestCancellationEntry{cancel: cancel, sequence: sequence})
}

func (s *Server) registerRequestCancellationEntry(id any, entry requestCancellationEntry) bool {
	key := requestIDKey(id)
	if key == "" || entry.cancel == nil {
		return false
	}
	s.mu.Lock()
	if _, exists := s.requestCancellations[key]; exists {
		s.mu.Unlock()
		return false
	}
	s.requestCancellations[key] = entry
	s.mu.Unlock()
	return true
}

func (s *Server) unregisterRequestCancellationAtSequence(id any, cancel context.CancelFunc, sequence uint64) {
	key := requestIDKey(id)
	if key == "" {
		return
	}
	s.mu.Lock()
	if entry, ok := s.requestCancellations[key]; ok && (sequence == 0 || entry.sequence == sequence) {
		delete(s.requestCancellations, key)
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) cancelRequest(params json.RawMessage) {
	var request struct {
		ID any `json:"id"`
	}
	if json.Unmarshal(params, &request) != nil {
		return
	}
	key := requestIDKey(request.ID)
	s.mu.Lock()
	entry := s.requestCancellations[key]
	s.mu.Unlock()
	if entry.cancel != nil {
		entry.cancel()
	}
}

func (s *Server) cancelRequestsBefore(sequence uint64) {
	s.mu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(s.requestCancellations))
	for _, entry := range s.requestCancellations {
		if entry.cancel != nil && !entry.revisionIndependent && entry.sequence < sequence {
			cancellations = append(cancellations, entry.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func (s *Server) cancelAllRequests() {
	s.mu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(s.requestCancellations))
	for _, entry := range s.requestCancellations {
		cancellations = append(cancellations, entry.cancel)
	}
	s.mu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func (s *Server) registerProgressCancellation(id string) context.Context {
	return s.registerProgressCancellationWithParent(id, context.Background())
}

func (s *Server) registerProgressCancellationWithParent(id string, parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	if strings.TrimSpace(id) == "" {
		cancel()
		return ctx
	}
	s.mu.Lock()
	if previous := s.progressCancellations[id]; previous != nil {
		previous()
	}
	s.progressCancellations[id] = cancel
	s.mu.Unlock()
	return ctx
}

func (s *Server) unregisterProgressCancellation(id string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	s.mu.Lock()
	cancel := s.progressCancellations[id]
	delete(s.progressCancellations, id)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func requestCancelledError() *rpcError {
	return &rpcError{Code: -32800, Message: "request cancelled"}
}

func duplicateRequestIDError() *rpcError {
	return &rpcError{Code: -32600, Message: "duplicate request id"}
}

func requestOverloadedError() *rpcError {
	return &rpcError{Code: -32000, Message: "server overloaded"}
}

func revisionAdvancingNotification(method string) bool {
	switch method {
	case "textDocument/didOpen", "textDocument/didChange", "textDocument/didSave", "textDocument/didClose",
		"workspace/didChangeConfiguration", "workspace/didChangeWatchedFiles",
		"workspace/didChangeWorkspaceFolders", "workspace/didRenameFiles",
		"workspace/didCreateFiles", "workspace/didDeleteFiles":
		return true
	default:
		return false
	}
}

func prepareRevisionAdvancingNotification(message *rpcMessage) bool {
	if message == nil || !revisionAdvancingNotification(message.Method) {
		return false
	}
	if message.revisionNotificationChecked {
		return message.revisionNotificationValid
	}
	message.revisionNotificationChecked = true
	if err := validateNotificationParams(message.Method, message.Params); err != nil {
		message.revisionNotificationErr = err
		return false
	}
	params := message.Params
	valid := false
	switch message.Method {
	case "textDocument/didOpen":
		var value didOpenParams
		valid = json.Unmarshal(params, &value) == nil && value.TextDocument.URI != "" && value.TextDocument.LanguageID != ""
	case "textDocument/didChange":
		var value didChangeParams
		if json.Unmarshal(params, &value) == nil {
			message.didChangeParams = &value
			valid = value.TextDocument.URI != "" && value.ContentChanges != nil
		}
	case "textDocument/didSave", "textDocument/didClose":
		var value textDocumentIdentifierParams
		valid = json.Unmarshal(params, &value) == nil && value.TextDocument.URI != ""
	case "workspace/didChangeConfiguration":
		var value changeConfigurationParams
		valid = json.Unmarshal(params, &value) == nil && jsonObjectHasNonNullField(params, "settings")
	case "workspace/didChangeWatchedFiles":
		var value didChangeWatchedFilesParams
		valid = json.Unmarshal(params, &value) == nil && value.Changes != nil
	case "workspace/didChangeWorkspaceFolders":
		var value didChangeWorkspaceFoldersParams
		valid = json.Unmarshal(params, &value) == nil && (value.Event.Added != nil || value.Event.Removed != nil)
	case "workspace/didRenameFiles":
		var value didRenameFilesParams
		valid = json.Unmarshal(params, &value) == nil && value.Files != nil
	case "workspace/didCreateFiles", "workspace/didDeleteFiles":
		var value didFileOperationParams
		valid = json.Unmarshal(params, &value) == nil && value.Files != nil
	}
	message.revisionNotificationValid = valid
	return valid
}

func jsonObjectHasNonNullField(params json.RawMessage, field string) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(params, &object) != nil {
		return false
	}
	value, ok := object[field]
	return ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}
