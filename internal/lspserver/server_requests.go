package lspserver

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

const workspaceReferencePartialResultChunkSize = 512

// workspaceReferencePartialLocationQueue keeps source slices and advances a
// cursor through them. Only chunks spanning source-slice boundaries need a
// bounded scratch copy.
type workspaceReferencePartialLocationQueue struct {
	chunks     [][]lsp.Location
	head       int
	headOffset int
	count      int
	scratch    []lsp.Location
}

func (q *workspaceReferencePartialLocationQueue) append(locations []lsp.Location) {
	if len(locations) == 0 {
		return
	}
	q.chunks = append(q.chunks, locations)
	q.count += len(locations)
}

func (q *workspaceReferencePartialLocationQueue) len() int {
	return q.count
}

func (q *workspaceReferencePartialLocationQueue) take(count int) []lsp.Location {
	if count <= 0 || count > q.count || q.head >= len(q.chunks) {
		return nil
	}
	first := q.chunks[q.head]
	available := len(first) - q.headOffset
	if available >= count {
		locations := first[q.headOffset : q.headOffset+count]
		q.advance(count)
		return locations
	}
	if cap(q.scratch) < count {
		q.scratch = make([]lsp.Location, count)
	}
	locations := q.scratch[:count]
	remaining := count
	position := 0
	for remaining > 0 {
		chunk := q.chunks[q.head]
		available = len(chunk) - q.headOffset
		copied := available
		if copied > remaining {
			copied = remaining
		}
		copy(locations[position:position+copied], chunk[q.headOffset:q.headOffset+copied])
		position += copied
		remaining -= copied
		q.advance(copied)
	}
	return locations
}

func (q *workspaceReferencePartialLocationQueue) advance(count int) {
	q.count -= count
	for count > 0 {
		chunk := q.chunks[q.head]
		available := len(chunk) - q.headOffset
		if count < available {
			q.headOffset += count
			break
		}
		count -= available
		q.chunks[q.head] = nil
		q.head++
		q.headOffset = 0
	}
	if q.head == len(q.chunks) {
		q.chunks = q.chunks[:0]
		q.head = 0
		return
	}
	if q.head >= 64 {
		q.compact()
	}
}

func (q *workspaceReferencePartialLocationQueue) compact() {
	if q.head == 0 {
		return
	}
	remaining := copy(q.chunks, q.chunks[q.head:])
	clear(q.chunks[remaining:])
	q.chunks = q.chunks[:remaining]
	q.head = 0
}

func (q *workspaceReferencePartialLocationQueue) reset() {
	clear(q.chunks)
	q.chunks = nil
	q.head = 0
	q.headOffset = 0
	q.count = 0
}

func emitWorkspaceReferencePartialLocations(ctx context.Context, pending *workspaceReferencePartialLocationQueue, emit func([]lsp.Location) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for pending.len() >= workspaceReferencePartialResultChunkSize && ctx.Err() == nil {
		if err := emit(pending.take(workspaceReferencePartialResultChunkSize)); err != nil {
			return err
		}
	}
	return nil
}

type workspaceReferencePartialResultState struct {
	ctx      context.Context
	pending  workspaceReferencePartialLocationQueue
	received bool
	writeErr error
	emit     func([]lsp.Location) error
}

func (state *workspaceReferencePartialResultState) receive(locations []lsp.Location) {
	state.received = true
	if state.writeErr != nil {
		state.pending.reset()
		return
	}
	if state.ctx != nil && state.ctx.Err() != nil {
		state.pending.reset()
		return
	}
	state.pending.append(locations)
	state.writeErr = emitWorkspaceReferencePartialLocations(state.ctx, &state.pending, state.emit)
	if state.writeErr != nil {
		state.pending.reset()
	}
}

func (s *Server) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	if s.requestDispatchTestHook != nil {
		s.requestDispatchTestHook(ctx, method)
	}
	if ctx.Err() != nil {
		return nil, requestCancelledError()
	}
	if waitForRequestTestDelay(ctx) {
		return nil, requestCancelledError()
	}
	if rpcErr := validateRequestParams(method, params); rpcErr != nil {
		return nil, rpcErr
	}
	s.mu.Lock()
	shuttingDown := s.shutdown
	s.mu.Unlock()
	if shuttingDown {
		return nil, invalidRPCRequestError()
	}
	switch method {
	case "initialize":
		var p initializeParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, invalidParams(err)
			}
		}
		s.configureWorkspace(p)
		s.clientLocale = normalizeLocale(p.Locale)
		s.settings.Locale = s.clientLocale
		s.mu.Lock()
		s.semanticTokensRefreshSupported = p.Capabilities.Workspace.SemanticTokens.RefreshSupport
		s.inlayHintRefreshSupported = p.Capabilities.Workspace.InlayHint.RefreshSupport
		s.codeLensRefreshSupported = p.Capabilities.Workspace.CodeLens.RefreshSupport
		s.workspaceConfigurationSupported = p.Capabilities.Workspace.Configuration
		s.mu.Unlock()
		return initializeResult(), nil
	case "shutdown":
		s.shutdownRuntimeCaches()
		return nil, nil
	case "aspLsp/textDocument/lineCommentEdits":
		var p lineCommentEditsParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		result := s.lineCommentEdits(p)
		if result == nil {
			return nil, nil
		}
		return result, nil
	case "textDocument/completion":
		var p completionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.completion(ctx, p.TextDocument.URI, p.Position, p.Context), nil
	case "completionItem/resolve":
		var item lsp.CompletionItem
		if err := json.Unmarshal(params, &item); err != nil {
			return nil, invalidParams(err)
		}
		resolved := s.resolveCompletionItemContext(ctx, item)
		if ctx.Err() != nil {
			return nil, requestCancelledError()
		}
		return resolved, nil
	case "codeLens/resolve":
		var lens lsp.CodeLens
		if err := json.Unmarshal(params, &lens); err != nil {
			return nil, invalidParams(err)
		}
		resolved := s.resolveCodeLens(ctx, lens)
		if ctx.Err() != nil {
			return nil, nil
		}
		return resolved, nil
	case "codeAction/resolve", "documentLink/resolve", "inlayHint/resolve":
		var value any
		if len(params) > 0 {
			if err := json.Unmarshal(params, &value); err != nil {
				return nil, invalidParams(err)
			}
		}
		return value, nil
	case "textDocument/hover":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.hoverContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/definition", "textDocument/declaration":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.definitionContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/typeDefinition", "textDocument/implementation":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		if method == "textDocument/typeDefinition" {
			return s.typeDefinitionContext(ctx, p.TextDocument.URI, p.Position), nil
		}
		return s.implementationContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/references":
		var p referenceParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		if p.PartialResultToken != nil {
			var partialMu sync.Mutex
			partial := &workspaceReferencePartialResultState{ctx: ctx}
			emitChunk := func(locations []lsp.Location) error {
				if partial.writeErr != nil {
					return partial.writeErr
				}
				if err := s.sendNotification("$/progress", map[string]any{"token": p.PartialResultToken, "value": locations}); err != nil {
					partial.writeErr = err
				}
				return partial.writeErr
			}
			partial.emit = emitChunk
			sink := workspaceReferenceLocationSink(func(locations []lsp.Location) {
				partialMu.Lock()
				defer partialMu.Unlock()
				partial.receive(locations)
			})
			ctx = context.WithValue(ctx, workspaceReferenceLocationSinkContextKey{}, sink)
			locations := s.referencesContext(ctx, p.TextDocument.URI, p.Position, p.Context.IncludeDeclaration)
			partialMu.Lock()
			streamed := partial.received
			partialMu.Unlock()
			if !streamed && len(locations) > 0 {
				sink(locations)
			}
			partialMu.Lock()
			if partial.pending.len() > 0 && partial.writeErr == nil && ctx.Err() == nil {
				partial.writeErr = emitChunk(partial.pending.take(partial.pending.len()))
			}
			partial.pending.reset()
			partialMu.Unlock()
			partialMu.Lock()
			writeErr := partial.writeErr
			partialMu.Unlock()
			if writeErr != nil {
				s.reportAsyncRPCWriteError(writeErr)
				if isRPCWriteFatal(writeErr) {
					return nil, requestCancelledError()
				}
			}
			if ctx.Err() != nil {
				return nil, requestCancelledError()
			}
			return []lsp.Location{}, nil
		}
		locations := s.referencesContext(ctx, p.TextDocument.URI, p.Position, p.Context.IncludeDeclaration)
		return locations, nil
	case "textDocument/documentHighlight":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.highlightsContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/selectionRange":
		var p selectionRangeParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.selectionRangesContext(ctx, p.TextDocument.URI, p.Positions), nil
	case "textDocument/documentColor":
		var p textDocumentIdentifierParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.documentColors(p.TextDocument.URI), nil
	case "textDocument/inlayHint":
		var p inlayHintParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.inlayHintsContext(ctx, p.TextDocument.URI, p.Range), nil
	case "textDocument/codeLens":
		var p textDocumentIdentifierParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.codeLensContext(ctx, p.TextDocument.URI), nil
	case "textDocument/codeAction":
		var p codeActionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.codeActions(ctx, p), nil
	case "textDocument/inlineValue":
		var p inlineValueParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.inlineValues(p.TextDocument.URI, p.Range), nil
	case "textDocument/signatureHelp":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.signatureHelpContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/linkedEditingRange":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.linkedEditingRange(p.TextDocument.URI, p.Position), nil
	case "textDocument/prepareRename":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.renameRangeContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/rename":
		var p renameParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.renameContext(ctx, p.TextDocument.URI, p.Position, p.NewName), nil
	case "workspace/willRenameFiles":
		var p willRenameFilesParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.willRenameFiles(p), nil
	case "textDocument/onTypeFormatting":
		var p onTypeFormattingParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.onTypeFormatting(p), nil
	case "textDocument/willSaveWaitUntil":
		var p willSaveWaitUntilParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.willSaveWaitUntil(p), nil
	case "workspace/symbol":
		var p workspaceSymbolParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.workspaceSymbols(ctx, p.Query), nil
	case "textDocument/colorPresentation":
		var p colorPresentationParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.colorPresentations(p.TextDocument.URI, p.Color, p.Range), nil
	case "callHierarchy/incomingCalls":
		var p callHierarchyItemParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.incomingCallsContext(ctx, p.Item), nil
	case "callHierarchy/outgoingCalls":
		var p callHierarchyItemParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.outgoingCallsContext(ctx, p.Item), nil
	case "typeHierarchy/supertypes":
		var p typeHierarchyItemParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return []lsp.TypeHierarchyItem{}, nil
	case "typeHierarchy/subtypes":
		var p typeHierarchyItemParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.typeHierarchyRelationsContext(ctx, p.Item), nil
	case "textDocument/prepareCallHierarchy":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.prepareCallHierarchyContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/moniker":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.monikersContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/prepareTypeHierarchy":
		var p textDocumentPositionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.prepareTypeHierarchyContext(ctx, p.TextDocument.URI, p.Position), nil
	case "textDocument/formatting":
		var p formattingParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.formatting(p.TextDocument.URI, nil, p.Options), nil
	case "textDocument/rangeFormatting":
		var p rangeFormattingParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.formatting(p.TextDocument.URI, &p.Range, p.Options), nil
	case "textDocument/documentSymbol":
		var p textDocumentIdentifierParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.documentSymbolsContext(ctx, p.TextDocument.URI), nil
	case "textDocument/foldingRange":
		var p textDocumentIdentifierParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.foldingRangesContext(ctx, p.TextDocument.URI), nil
	case "textDocument/documentLink":
		var p textDocumentIdentifierParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.documentLinks(p.TextDocument.URI), nil
	case "textDocument/semanticTokens/full":
		var p textDocumentIdentifierParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.semanticTokensContext(ctx, p.TextDocument.URI), nil
	case "textDocument/semanticTokens/range":
		var p semanticTokensRangeParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.semanticTokensRangeContext(ctx, p.TextDocument.URI, p.Range), nil
	case "textDocument/semanticTokens/full/delta":
		var p semanticTokensDeltaParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		tokens := s.semanticTokensContext(ctx, p.TextDocument.URI)
		if ctx.Err() != nil {
			return nil, nil
		}
		s.mu.Lock()
		previous, ok := s.semanticHistory[p.PreviousResultID]
		s.mu.Unlock()
		if ctx.Err() != nil {
			return nil, nil
		}
		previousURI, hasPreviousURI := semanticTokenResultURI(p.PreviousResultID)
		if !ok || !hasPreviousURI || previousURI != p.TextDocument.URI {
			return tokens, nil
		}
		s.logDebugVerbose("[asp-lsp] semanticTokens.full.incrementalReuse: " + p.TextDocument.URI)
		return map[string]any{
			"resultId": tokens.ResultID,
			"edits":    semanticTokenDeltaEdits(previous, tokens.Data),
		}, nil
	case "textDocument/diagnostic":
		var p textDocumentIdentifierParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		snapshot := s.diagnosticsSnapshot(ctx, p.TextDocument.URI)
		items := []lsp.Diagnostic{}
		if snapshot.ok {
			items = nonNilDiagnostics(diagnosticsForDocumentURI(p.TextDocument.URI, snapshot.diagnostics))
		}
		return map[string]any{"kind": "full", "items": items}, nil
	case "workspace/diagnostic":
		return s.workspaceDiagnostics(ctx), nil
	case "workspace/executeCommand":
		var p executeCommandParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, invalidParams(err)
		}
		if p.Command == "aspLsp.server.exportAnalysisExcel" {
			return s.exportAnalysisExcelRequest(ctx, p)
		}
		if p.Command == "aspLsp.server.buildNavigationGraph" {
			return s.buildNavigationGraphRequest(ctx, p)
		}
		return s.executeCommandContext(ctx, p), nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

func waitForRequestTestDelay(ctx context.Context) bool {
	delayMS, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ASP_LSP_TEST_REQUEST_DELAY_MS")))
	if err != nil || delayMS <= 0 {
		return false
	}
	timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}

func (s *Server) refreshWorkspaceConfiguration(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	supported := s.workspaceConfigurationSupported
	s.mu.Unlock()
	if !supported {
		return nil
	}
	timeout := 5 * time.Second
	if timeoutMS, parseErr := strconv.Atoi(strings.TrimSpace(os.Getenv("ASP_LSP_TEST_WORKSPACE_CONFIGURATION_TIMEOUT_MS"))); parseErr == nil && timeoutMS > 0 {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := s.requestClient(ctx, "workspace/configuration", map[string]any{
		"items": []map[string]any{{"section": "aspLsp"}},
	})
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		s.logServerWarning("[asp-lsp] workspace.configuration.failed: " + err.Error())
		return nil
	}
	if response.Error != nil {
		s.logServerWarning("[asp-lsp] workspace.configuration.failed: " + response.Error.Message)
		return nil
	}
	var configurations []json.RawMessage
	encoded, err := json.Marshal(response.Result)
	if err != nil || json.Unmarshal(encoded, &configurations) != nil || len(configurations) == 0 {
		return nil
	}
	configuration := configurations[0]
	if len(configuration) == 0 || string(configuration) == "null" {
		return nil
	}
	return s.handleNotification(ctx, "workspace/didChangeConfiguration", mustRaw(map[string]any{
		"settings": map[string]any{"aspLsp": configuration},
	}))
}
