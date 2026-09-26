package lspserver

import (
	"context"
	"encoding/json"
	"runtime"
)

type requestWorkClass uint8

const (
	requestWorkInteractive requestWorkClass = iota
	requestWorkRegular
	requestWorkBackground
)

// requestExecutionScheduler keeps independent execution lanes so queued bulk
// work can never consume the capacity reserved for editor interactions.
type requestExecutionScheduler struct {
	interactive chan struct{}
	regular     chan struct{}
	background  chan struct{}
}

func newRequestExecutionScheduler() *requestExecutionScheduler {
	processors := max(runtime.GOMAXPROCS(0), 1)
	return &requestExecutionScheduler{
		interactive: make(chan struct{}, min(8, max(2, processors))),
		regular:     make(chan struct{}, min(4, max(2, processors))),
		background:  make(chan struct{}, min(2, max(1, processors-1))),
	}
}

func (s *requestExecutionScheduler) acquire(ctx context.Context, class requestWorkClass) (func(), bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	slots := s.regular
	switch class {
	case requestWorkInteractive:
		slots = s.interactive
	case requestWorkBackground:
		slots = s.background
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	case <-ctx.Done():
		return func() {}, false
	}
}

func classifyRequestWork(method string, params json.RawMessage) requestWorkClass {
	switch method {
	case "aspLsp/textDocument/lineCommentEdits",
		"textDocument/completion", "completionItem/resolve",
		"textDocument/hover", "textDocument/signatureHelp",
		"textDocument/definition", "textDocument/declaration",
		"textDocument/typeDefinition", "textDocument/implementation",
		"textDocument/documentHighlight", "textDocument/selectionRange",
		"textDocument/documentColor", "textDocument/colorPresentation",
		"textDocument/linkedEditingRange", "textDocument/prepareRename",
		"textDocument/rename",
		"textDocument/codeAction", "codeAction/resolve",
		"textDocument/codeLens", "textDocument/inlineValue",
		"textDocument/inlayHint", "inlayHint/resolve",
		"textDocument/onTypeFormatting", "textDocument/willSaveWaitUntil",
		"textDocument/formatting", "textDocument/rangeFormatting",
		"textDocument/documentSymbol", "textDocument/foldingRange",
		"textDocument/documentLink", "documentLink/resolve",
		"textDocument/prepareCallHierarchy", "callHierarchy/incomingCalls", "callHierarchy/outgoingCalls",
		"textDocument/prepareTypeHierarchy", "typeHierarchy/supertypes", "typeHierarchy/subtypes",
		"textDocument/moniker",
		"textDocument/semanticTokens/full", "textDocument/semanticTokens/full/delta",
		"textDocument/semanticTokens/range":
		return requestWorkInteractive
	case "textDocument/references", "codeLens/resolve", "workspace/willRenameFiles",
		"workspace/symbol", "workspace/diagnostic", "textDocument/diagnostic":
		return requestWorkBackground
	case "workspace/executeCommand":
		var command executeCommandParams
		if json.Unmarshal(params, &command) == nil && backgroundExecuteCommand(command.Command) {
			return requestWorkBackground
		}
	}
	return requestWorkRegular
}

func backgroundExecuteCommand(command string) bool {
	switch command {
	case "aspLsp.server.buildGraph", "aspLsp.server.buildFlowchart",
		"aspLsp.server.buildNavigationGraph", "aspLsp.server.exportAnalysisExcel",
		"aspLsp.server.previewWorkspaceFiles",
		"aspLsp.reindexWorkspace", "aspLsp.server.reindexWorkspace",
		"aspLsp.clearProcessCache", "aspLsp.server.clearProcessCache",
		"aspLsp.clearDiskCache", "aspLsp.server.clearDiskCache",
		"aspLsp.clearCache", "aspLsp.server.clearCache":
		return true
	default:
		return false
	}
}
