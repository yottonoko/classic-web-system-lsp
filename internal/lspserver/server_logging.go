package lspserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) publishStatus(params map[string]any) error {
	return s.writeRPCMessage(rpcMessage{Method: "aspLsp/status", Params: mustRaw(params)})
}

func (s *Server) logDebugSummary(message string) {
	s.logDebugFile("DEBUG", "debug.summary", message)
	if s.isDebugSummaryEnabled() {
		s.reportAsyncRPCWriteError(s.writeRPCMessage(rpcMessage{Method: "window/logMessage", Params: mustRaw(map[string]any{"type": 3, "message": message})}))
	}
}

func (s *Server) logDebugVerbose(message string) {
	s.logDebugVerboseWithMetadata(message, nil)
}

func (s *Server) logDebugVerboseWithMetadata(message string, metadata map[string]any) {
	s.logDebugFileWithMetadata("DEBUG", "debug.elapsed", message, metadata)
	if s.isDebugVerboseEnabled() {
		s.reportAsyncRPCWriteError(s.writeRPCMessage(rpcMessage{Method: "window/logMessage", Params: mustRaw(map[string]any{"type": 3, "message": message})}))
	}
}

func formatElapsedSince(started time.Time) string {
	elapsedMs := float64(time.Since(started).Microseconds()) / 1000
	return "in " + strconv.FormatFloat(elapsedMs, 'f', 1, 64) + " ms"
}

func (s *Server) isDebugSummaryEnabled() bool {
	s.mu.Lock()
	output := s.settings.DebugOutput
	s.mu.Unlock()
	return output == "summary" || output == "verbose"
}

func (s *Server) isDebugVerboseEnabled() bool {
	s.mu.Lock()
	output := s.settings.DebugOutput
	s.mu.Unlock()
	return output == "verbose"
}

func (s *Server) reportAsyncRPCWriteError(err error) {
	if !isRPCWriteFatal(err) {
		return
	}
	s.writeMu.Lock()
	if s.writeFatalErr == nil {
		s.latchWriteFatalLocked(err)
	}
	s.writeMu.Unlock()
}

func (s *Server) logDebugTrace(category, message string) {
	s.logDebugFile("TRACE", category, message)
}

func (s *Server) logServerWarning(message string) {
	s.reportAsyncRPCWriteError(s.writeRPCMessage(rpcMessage{Method: "window/logMessage", Params: mustRaw(map[string]any{"type": 2, "message": message})}))
	s.logDebugFile("WARN", "server.warning", message)
}

func (s *Server) logDebugFile(level, category, message string) {
	s.logDebugFileWithMetadata(level, category, message, nil)
}

func (s *Server) logDebugFileWithMetadata(level, category, message string, metadata map[string]any) {
	if s.debugLogWriter == nil {
		return
	}
	target, ok := s.cachedDebugLogFileTarget()
	if !ok {
		return
	}
	s.debugLogWriter.enqueue(debugLogFileEntry{filePath: target.filePath, root: target.root, relative: target.relative, level: level, category: category, message: message, metadata: metadata})
}

// debugLogTargetRecheckInterval bounds how long a resolved debug log target is
// reused. Resolving it stats every workspace root, which made each log line
// cost several network round trips on network drives.
const debugLogTargetRecheckInterval = 5 * time.Second

type debugLogTargetCache struct {
	mu       sync.Mutex
	key      string
	filePath string
	ok       bool
	expires  time.Time
}

func (s *Server) debugLogFileTargetKey() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.settings.DebugLogFileEnabled {
		return "", false
	}
	var key strings.Builder
	for _, part := range []string{s.settings.DebugLogFilePath, s.settings.CacheDirectory, s.rootPath, os.Getenv("ASP_LSP_DEFAULT_DEBUG_LOG_FILE")} {
		key.WriteString(part)
		key.WriteByte(0)
	}
	for _, root := range s.workspaceRoots {
		key.WriteString(root.Path)
		key.WriteByte(0)
	}
	return key.String(), true
}

// cachedDebugLogFileTarget reuses the root handle the writer already holds for
// the resolved log file until the settings change or the recheck interval ends.
func (s *Server) cachedDebugLogFileTarget() (debugLogFileTarget, bool) {
	key, enabled := s.debugLogFileTargetKey()
	if !enabled {
		return debugLogFileTarget{}, false
	}
	now := time.Now()
	cache := &s.debugLogTargets
	cache.mu.Lock()
	fresh := cache.key == key && now.Before(cache.expires)
	filePath, ok := cache.filePath, cache.ok
	cache.mu.Unlock()
	if fresh {
		if !ok {
			return debugLogFileTarget{}, false
		}
		if target, adopted := s.debugLogWriter.adoptedTarget(filePath); adopted {
			return target, true
		}
	}
	target, ok := s.debugLogFileTarget()
	cache.mu.Lock()
	cache.key, cache.filePath, cache.ok, cache.expires = key, target.filePath, ok, now.Add(debugLogTargetRecheckInterval)
	cache.mu.Unlock()
	return target, ok
}

func (s *Server) debugLogFilePath() string {
	path, _ := s.resolveDebugLogFilePath()
	return path
}

// debugLogFileEnabled reports whether file logging is configured without
// resolving the log target on the filesystem.
func (s *Server) debugLogFileEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.DebugLogFileEnabled
}

func (s *Server) resolveDebugLogFilePath() (string, bool) {
	s.mu.Lock()
	enabled := s.settings.DebugLogFileEnabled
	configured := strings.TrimSpace(s.settings.DebugLogFilePath)
	rootPath := s.rootPath
	workspaceRoots := append([]workspaceRoot(nil), s.workspaceRoots...)
	cacheDirectory := strings.TrimSpace(s.settings.CacheDirectory)
	s.mu.Unlock()
	if !enabled {
		return "", false
	}
	trustedRoots := make([]string, 0, len(workspaceRoots)+3)
	for _, root := range workspaceRoots {
		if root.Path != "" {
			trustedRoots = append(trustedRoots, root.Path)
		}
	}
	if rootPath != "" {
		trustedRoots = append(trustedRoots, rootPath)
	}
	trustedRoots = append(trustedRoots, os.TempDir())
	if cacheDirectory != "" {
		trustedRoots = append(trustedRoots, cacheDirectory)
	}
	// Capture configured trust-root identities before resolving the log path.
	// The target later reuses these cached identities when it opens its root
	// handle, so a configured root replaced after this point is rejected.
	s.trustedFilesystemRootEntries(trustedRoots)
	resolve := func(raw string) string {
		if raw == "" {
			return ""
		}
		candidate := raw
		if !filepath.IsAbs(candidate) {
			if pathHasParentTraversal(candidate) {
				return ""
			}
			base := rootPath
			if base == "" && len(workspaceRoots) > 0 {
				base = workspaceRoots[0].Path
			}
			if base == "" {
				base = os.TempDir()
			}
			candidate = filepath.Join(base, candidate)
		}
		resolved, ok := trustedPathForRoots(candidate, trustedRoots)
		if !ok {
			return ""
		}
		return resolved
	}
	if configured != "" {
		return resolve(configured), false
	}
	if envPath := strings.TrimSpace(os.Getenv("ASP_LSP_DEFAULT_DEBUG_LOG_FILE")); envPath != "" {
		if filepath.IsAbs(envPath) && !pathHasParentTraversal(envPath) {
			s.trustedFilesystemRootEntries([]string{filepath.Dir(filepath.Clean(envPath))})
		}
		if resolved := trustedDefaultDebugLogPath(envPath); resolved != "" {
			return resolved, true
		}
	}
	return resolve(filepath.Join(os.TempDir(), "asp-lsp-debug.log")), false
}

func (s *Server) debugLogFileTarget() (debugLogFileTarget, bool) {
	path, exactDefaultRoot := s.resolveDebugLogFilePath()
	if path == "" {
		return debugLogFileTarget{}, false
	}
	s.mu.Lock()
	cacheDirectory := strings.TrimSpace(s.settings.CacheDirectory)
	s.mu.Unlock()
	extraRoots := []string{os.TempDir()}
	if cacheDirectory != "" {
		extraRoots = append(extraRoots, cacheDirectory)
	}
	if exactDefaultRoot {
		extraRoots = append(extraRoots, filepath.Dir(path))
	}
	root, relative, ok := openTrustedFilesystemPathWithRoots(path, s.trustedFilesystemRootEntries(extraRoots))
	if !ok {
		return debugLogFileTarget{}, false
	}
	return debugLogFileTarget{filePath: path, root: root, relative: relative}, true
}

func trustedDefaultDebugLogPath(raw string) string {
	if raw == "" || !filepath.IsAbs(raw) || pathHasParentTraversal(raw) {
		return ""
	}
	candidate := filepath.Clean(raw)
	resolved, ok := trustedPathForRoots(candidate, []string{filepath.Dir(candidate)})
	if !ok || filepath.Clean(resolved) != candidate {
		return ""
	}
	return resolved
}

func debugLogFileMaxBytes() int64 {
	return int64(boundedEnvInt("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", 10*1024*1024, 0, 1<<30))
}

func debugLogFileMaxBackups() int {
	return boundedEnvInt("ASP_LSP_TEST_DEBUG_LOG_MAX_BACKUPS", 5, 0, 100)
}

func boundedEnvInt(name string, fallback, minimum, maximum int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		for _, character := range raw {
			if character < '0' || character > '9' {
				return fallback
			}
		}
		return maximum
	}
	if value < minimum {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func positiveEnvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func (s *Server) writeRPCMessage(message rpcMessage) error {
	return s.writeRPCMessageResult(message).err
}

type rpcWriteOutcome uint8

const (
	rpcWriteZeroFailure rpcWriteOutcome = iota + 1
	rpcWriteFullSuccess
	rpcWriteFatal
)

type rpcWriteResult struct {
	outcome rpcWriteOutcome
	err     error
}

type rpcTransportFatalError struct {
	err error
}

type rpcTransportZeroWriteError struct {
	err error
}

func (e *rpcTransportZeroWriteError) Error() string {
	if e == nil || e.err == nil {
		return "recoverable RPC transport write failure"
	}
	return "recoverable RPC transport write failure: " + e.err.Error()
}

func (e *rpcTransportZeroWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *rpcTransportFatalError) Error() string {
	if e == nil || e.err == nil {
		return "fatal RPC transport write"
	}
	return "fatal RPC transport write: " + e.err.Error()
}

func (e *rpcTransportFatalError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

var errRPCWriteNoProgress = errors.Join(io.ErrShortWrite, io.ErrNoProgress)

// A zero-byte write does not put any part of the frame on the wire, so the
// complete frame can be retried safely. Keep the budget small because a
// writer that repeatedly makes no progress must eventually terminate the
// transport rather than spin while holding the serialization lock.
const rpcZeroWriteRetryLimit = 2

func (s *Server) writeFatalError() error {
	s.writeMu.Lock()
	err := s.writeFatalErr
	s.writeMu.Unlock()
	return err
}

func (s *Server) writeFatalSignal() <-chan struct{} {
	s.writeMu.Lock()
	if s.writeFatalCh == nil {
		s.writeFatalCh = make(chan struct{})
	}
	signal := s.writeFatalCh
	s.writeMu.Unlock()
	return signal
}

func (s *Server) latchWriteFatalLocked(err error) error {
	if s.writeFatalErr != nil {
		return s.writeFatalErr
	}
	if err == nil {
		err = &rpcTransportFatalError{err: errors.New("unknown RPC transport write failure")}
	}
	s.writeFatalErr = err
	if s.writeFatalCh == nil {
		s.writeFatalCh = make(chan struct{})
	}
	close(s.writeFatalCh)
	return err
}

func isRPCWriteFatal(err error) bool {
	var fatal *rpcTransportFatalError
	return errors.As(err, &fatal)
}

func isRPCWriteZeroFailure(err error) bool {
	var zero *rpcTransportZeroWriteError
	return errors.As(err, &zero)
}

func (s *Server) writeRPCMessageResult(message rpcMessage) rpcWriteResult {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeFatalErr != nil {
		return rpcWriteResult{outcome: rpcWriteFatal, err: s.writeFatalErr}
	}
	encoded, err := encodeMessage(message)
	if err != nil {
		return rpcWriteResult{outcome: rpcWriteZeroFailure, err: err}
	}
	var zeroWriteCause error
	for attempt := 0; attempt <= rpcZeroWriteRetryLimit; attempt++ {
		total := 0
		for total < len(encoded) {
			written, writeErr := s.out.Write(encoded[total:])
			remaining := len(encoded) - total
			if written < 0 || written > remaining {
				fatal := s.latchWriteFatalLocked(&rpcTransportFatalError{err: fmt.Errorf("invalid RPC transport write count %d for %d bytes", written, remaining)})
				return rpcWriteResult{outcome: rpcWriteFatal, err: fatal}
			}
			if written == 0 {
				if total > 0 {
					cause := writeErr
					if cause == nil {
						cause = errRPCWriteNoProgress
					}
					fatal := s.latchWriteFatalLocked(&rpcTransportFatalError{err: cause})
					return rpcWriteResult{outcome: rpcWriteFatal, err: fatal}
				}
				if zeroWriteCause == nil && writeErr != nil {
					zeroWriteCause = writeErr
				}
				if attempt == rpcZeroWriteRetryLimit {
					if zeroWriteCause == nil {
						zeroWriteCause = errRPCWriteNoProgress
					}
					fatal := s.latchWriteFatalLocked(&rpcTransportFatalError{err: zeroWriteCause})
					return rpcWriteResult{outcome: rpcWriteFatal, err: fatal}
				}
				// No bytes escaped, so restart the complete frame on the next
				// bounded attempt. A partial frame must never take this path.
				break
			}
			if writeErr != nil {
				fatal := s.latchWriteFatalLocked(&rpcTransportFatalError{err: writeErr})
				return rpcWriteResult{outcome: rpcWriteFatal, err: fatal}
			}
			total += written
		}
		if total == len(encoded) {
			return rpcWriteResult{outcome: rpcWriteFullSuccess}
		}
	}
	// The loop always returns after a full write or a fatal zero-write
	// exhaustion. Keep a defensive result in case that invariant changes.
	fatal := s.latchWriteFatalLocked(&rpcTransportFatalError{err: errRPCWriteNoProgress})
	return rpcWriteResult{outcome: rpcWriteFatal, err: fatal}
}

func (s *Server) sendNotification(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return s.writeRPCMessage(rpcMessage{Method: method, Params: raw})
}

func (s *Server) requestVisualRefresh(reason string) error {
	s.mu.Lock()
	semanticSupported := s.semanticTokensRefreshSupported
	inlaySupported := s.inlayHintRefreshSupported
	codeLensSupported := s.codeLensRefreshSupported
	s.mu.Unlock()
	var firstErr error
	if semanticSupported {
		if err := s.sendNotification("workspace/semanticTokens/refresh", map[string]any{}); isRPCWriteFatal(err) {
			firstErr = err
		}
	}
	if inlaySupported {
		if err := s.sendNotification("workspace/inlayHint/refresh", map[string]any{}); isRPCWriteFatal(err) && firstErr == nil {
			firstErr = err
		}
	}
	if codeLensSupported {
		s.requestCodeLensRefresh(reason)
	}
	if semanticSupported || inlaySupported || codeLensSupported {
		s.logDebugSummary("[asp-lsp] visual.refresh: " + reason)
	}
	return firstErr
}

func (s *Server) requestCodeLensRefresh(reason string) {
	s.mu.Lock()
	supported := s.codeLensRefreshSupported
	if !supported {
		s.mu.Unlock()
		return
	}
	s.codeLensRefreshSequence++
	sequence := s.codeLensRefreshSequence
	if s.codeLensRefreshTimer != nil {
		s.codeLensRefreshTimer.Stop()
	}
	s.codeLensRefreshTimer = time.AfterFunc(50*time.Millisecond, func() {
		s.mu.Lock()
		if s.codeLensRefreshSequence != sequence || !s.codeLensRefreshSupported {
			s.mu.Unlock()
			return
		}
		s.codeLensRefreshTimer = nil
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		response, err := s.requestClient(ctx, "workspace/codeLens/refresh", map[string]any{})
		s.reportAsyncRPCWriteError(err)
		if err != nil || response.Error != nil {
			return
		}
		s.logDebugSummaryEvent("referenceCache.refresh", "[asp-lsp] referenceCache.refresh reason="+reason, map[string]any{"reason": reason})
	})
	s.mu.Unlock()
}

func initializeResult() map[string]any {
	return map[string]any{
		"capabilities": map[string]any{
			"textDocumentSync": map[string]any{
				"openClose":         true,
				"change":            2,
				"save":              map[string]any{"includeText": false},
				"willSave":          true,
				"willSaveWaitUntil": true,
			},
			"completionProvider": map[string]any{
				"triggerCharacters": []string{"<", ".", "\"", "'", ":", "#", "(", ";"},
				"resolveProvider":   true,
			},
			"hoverProvider":             true,
			"definitionProvider":        true,
			"declarationProvider":       true,
			"typeDefinitionProvider":    true,
			"implementationProvider":    true,
			"referencesProvider":        true,
			"documentHighlightProvider": true,
			"signatureHelpProvider":     map[string]any{"triggerCharacters": []string{" ", "(", ","}},
			"renameProvider":            map[string]any{"prepareProvider": true},
			"documentSymbolProvider":    true,
			"foldingRangeProvider":      true,
			"documentLinkProvider":      map[string]any{"resolveProvider": true},
			"selectionRangeProvider":    true,
			"inlayHintProvider":         map[string]any{"resolveProvider": false},
			"codeLensProvider":          map[string]any{"resolveProvider": true},
			"codeActionProvider": map[string]any{
				"resolveProvider": true,
				"codeActionKinds": []string{
					"quickfix",
					"refactor",
					"source",
					"source.organizeImports",
					"source.organizeImports.aspLsp.javascript",
				},
			},
			"documentFormattingProvider":      true,
			"documentRangeFormattingProvider": true,
			"documentOnTypeFormattingProvider": map[string]any{
				"firstTriggerCharacter": "\n",
				"moreTriggerCharacter":  []string{">"},
			},
			"colorProvider":              true,
			"workspaceSymbolProvider":    true,
			"callHierarchyProvider":      true,
			"typeHierarchyProvider":      true,
			"monikerProvider":            true,
			"inlineValueProvider":        true,
			"linkedEditingRangeProvider": true,
			"semanticTokensProvider": map[string]any{
				"legend": map[string]any{"tokenTypes": vbscript.SemanticTokenTypes, "tokenModifiers": vbscript.SemanticTokenModifiers},
				"full":   map[string]any{"delta": true},
				"range":  true,
			},
			"diagnosticProvider": map[string]any{
				"interFileDependencies": true,
				"workspaceDiagnostics":  true,
			},
			"executeCommandProvider": map[string]any{"commands": []string{
				"aspLsp.server.reindexWorkspace",
				"aspLsp.server.clearCache",
				"aspLsp.server.clearDiskCache",
				"aspLsp.server.clearProcessCache",
				"aspLsp.server.buildFlowchart",
				"aspLsp.server.buildNavigationGraph",
				"aspLsp.server.exportAnalysisExcel",
				"aspLsp.server.previewWorkspaceFiles",
				"aspLsp.server.cancelProgressTask",
			}},
			"workspace": map[string]any{
				"workspaceFolders": map[string]any{"supported": true, "changeNotifications": true},
				"fileOperations": map[string]any{
					"willRename": map[string]any{"filters": []any{aspFileOperationFilter()}},
					"didRename":  map[string]any{"filters": []any{aspFileOperationFilter()}},
					"didCreate":  map[string]any{"filters": []any{aspFileOperationFilter()}},
					"didDelete":  map[string]any{"filters": []any{aspFileOperationFilter()}},
				},
			},
		},
		"serverInfo": map[string]any{"name": "asp-lsp-go", "version": "0.9.4-go"},
	}
}

func aspFileOperationFilter() map[string]any {
	return map[string]any{
		"scheme": "file",
		"pattern": map[string]any{
			"glob":    "**/*.{asp,asa,inc,vbs}",
			"matches": "file",
			"options": map[string]any{"ignoreCase": true},
		},
	}
}

func invalidParams(err error) *rpcError {
	return &rpcError{Code: -32602, Message: err.Error()}
}

func mustRaw(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func remarshal(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func isServerIdent(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func fileURIPath(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") {
		return ""
	}
	path, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return ""
	}
	if isWindowsDriveSegment(parsed.Host) {
		return filepath.FromSlash(parsed.Host + path)
	}
	if parsed.Host != "" {
		path = "//" + parsed.Host + path
	} else if len(path) >= 3 && path[0] == '/' && isWindowsDriveSegment(path[1:3]) {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

func (s *Server) includeTargetPathForMode(ownerURI, includePath, mode string) (string, bool) {
	return s.includeTargetPathForModeContext(context.Background(), ownerURI, includePath, mode)
}

func (s *Server) includeTargetPathForModeContext(ctx context.Context, ownerURI, includePath, mode string) (string, bool) {
	details, ok := s.includeTargetDetailsForModeContext(ctx, ownerURI, includePath, mode)
	if !ok {
		return "", false
	}
	return details.Path, true
}

type includeTargetDetails struct {
	Path              string
	Exists            bool
	CaseMismatch      bool
	ActualIncludePath string
}

func (s *Server) includeTargetDetailsForMode(ownerURI, includePath, mode string) (includeTargetDetails, bool) {
	return s.includeTargetDetailsForModeContext(context.Background(), ownerURI, includePath, mode)
}

func (s *Server) includeTargetDetailsForModeContext(ctx context.Context, ownerURI, includePath, mode string) (includeTargetDetails, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return includeTargetDetails{}, false
	}
	if includePath == "" {
		return includeTargetDetails{}, false
	}
	if memo := includeResolutionMemoFromContext(ctx); memo != nil {
		if key, ok := newIncludeResolutionMemoKey(ownerURI, includePath, mode); ok {
			return memo.resolve(ctx, key, func() (includeTargetDetails, bool) {
				return s.resolveIncludeTargetDetailsContext(ctx, ownerURI, includePath, mode)
			})
		}
	}
	return s.resolveIncludeTargetDetailsContext(ctx, ownerURI, includePath, mode)
}

func (s *Server) resolveIncludeTargetDetailsContext(ctx context.Context, ownerURI, includePath, mode string) (includeTargetDetails, bool) {
	candidatePaths := []string{}
	s.mu.Lock()
	workspaceRoots := append([]workspaceRoot(nil), s.workspaceRoots...)
	includePaths := append([]string(nil), s.settings.IncludePaths...)
	virtualRoots := append([]string(nil), s.settings.VirtualRoots...)
	virtualRoot := s.settings.VirtualRoot
	s.mu.Unlock()
	seenCandidates := map[string]struct{}{}
	addCandidate := func(candidate string) {
		if ctx.Err() != nil {
			return
		}
		if candidate == "" {
			return
		}
		cleaned, ok := s.trustedFilesystemPathContext(ctx, candidate)
		if ctx.Err() != nil {
			return
		}
		if !ok {
			if pathHasParentTraversal(includePath) {
				return
			}
			// Keep missing-include diagnostics read-free. Existing and symlinked
			// paths remain rejected by the trusted filesystem checks.
			cleaned = filepath.Clean(candidate)
			if _, err := os.Lstat(cleaned); err == nil || !os.IsNotExist(err) {
				return
			}
		}
		key := workspacepkg.FileIdentityKeyFromFileName(cleaned)
		if _, seen := seenCandidates[key]; seen {
			return
		}
		seenCandidates[key] = struct{}{}
		candidatePaths = append(candidatePaths, cleaned)
	}
	if mode == "virtual" {
		relativeIncludePath := strings.TrimPrefix(filepath.ToSlash(includePath), "/")
		for _, root := range virtualRoots {
			addCandidate(filepath.Join(root, relativeIncludePath))
		}
		if virtualRoot != "" {
			addCandidate(filepath.Join(virtualRoot, relativeIncludePath))
		}
		for _, root := range workspaceRoots {
			addCandidate(filepath.Join(root.Path, relativeIncludePath))
		}
		ownerPath := fileURIPath(ownerURI)
		ownerRoot := workspaceRootPathForPath(ownerPath, workspaceRoots)
		if ownerRoot == "" && ownerPath != "" {
			ownerRoot = filepath.Dir(ownerPath)
		}
		if ownerRoot != "" {
			addCandidate(filepath.Join(ownerRoot, relativeIncludePath))
		}
	} else {
		ownerPath := fileURIPath(ownerURI)
		if ownerPath == "" {
			return includeTargetDetails{}, false
		}
		includeFilePath := filepath.FromSlash(includePath)
		addCandidate(filepath.Join(filepath.Dir(ownerPath), includeFilePath))
		for _, root := range includePaths {
			addCandidate(filepath.Join(root, includeFilePath))
		}
		for _, root := range virtualRoots {
			addCandidate(filepath.Join(root, includeFilePath))
		}
	}
	if len(candidatePaths) == 0 {
		return includeTargetDetails{}, false
	}
	for _, targetPath := range candidatePaths {
		if ctx.Err() != nil {
			return includeTargetDetails{}, false
		}
		if details, ok := s.existingIncludeTargetDetailsContext(ctx, targetPath); ok {
			return details, ctx.Err() == nil
		}
	}
	if ctx.Err() != nil {
		return includeTargetDetails{}, false
	}
	targetPath := candidatePaths[0]
	details := s.missingIncludeTargetDetailsContext(ctx, targetPath, includePath)
	return details, ctx.Err() == nil
}

func (s *Server) existingIncludeTargetDetailsContext(ctx context.Context, targetPath string) (includeTargetDetails, bool) {
	if s.inMemoryDocumentAtPath(targetPath) {
		return includeTargetDetails{
			Path:              filepath.Clean(targetPath),
			Exists:            true,
			ActualIncludePath: filepath.Base(targetPath),
		}, true
	}
	windowsPathResolution := s.windowsPathResolutionEnabled()
	if _, ok := s.fsStatContext(ctx, targetPath); ok {
		actualPath := targetPath
		caseMismatch := false
		if windowsPathResolution && s.includeCaseResolutionEnabled() {
			if resolvedPath, ok := s.resolveCaseInsensitivePathContext(ctx, targetPath); ok {
				actualPath = resolvedPath
				caseMismatch = filepath.Clean(actualPath) != filepath.Clean(targetPath)
			}
		}
		return includeTargetDetails{
			Path:              actualPath,
			Exists:            true,
			CaseMismatch:      caseMismatch,
			ActualIncludePath: filepath.Base(actualPath),
		}, true
	}
	if windowsPathResolution && s.includeCaseResolutionEnabled() {
		actualPath, ok := s.resolveCaseInsensitivePathContext(ctx, targetPath)
		if ok {
			return includeTargetDetails{
				Path:              actualPath,
				Exists:            true,
				CaseMismatch:      filepath.Clean(actualPath) != filepath.Clean(targetPath),
				ActualIncludePath: filepath.Base(actualPath),
			}, true
		}
	}
	return includeTargetDetails{}, false
}

func (s *Server) inMemoryDocumentAtPath(path string) bool {
	cleanPath := filepath.Clean(path)
	if cleanPath == "." || pathHasParentTraversal(path) {
		return false
	}
	s.mu.Lock()
	roots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(roots) == 0 && s.rootPath != "" {
		roots = []workspaceRoot{{Path: s.rootPath, URI: s.rootURI}}
	}
	includePaths := append([]string(nil), s.settings.IncludePaths...)
	virtualRoots := append([]string(nil), s.settings.VirtualRoots...)
	if s.settings.VirtualRoot != "" {
		virtualRoots = append(virtualRoots, s.settings.VirtualRoot)
	}
	for _, root := range s.openDocumentBoundaryRootsLocked() {
		roots = append(roots, workspaceRoot{Path: root})
	}
	uri := filePathURI(cleanPath)
	found := s.documents[uri] != nil || s.workspace[uri] != nil
	if !found {
		for candidateURI, document := range s.documents {
			if document != nil && workspacepkg.SameFileIdentityURI(candidateURI, uri) {
				found = true
				break
			}
		}
	}
	if !found {
		found = s.workspaceDocumentWithIdentityLocked(uri) != nil
	}
	s.mu.Unlock()
	return found && workspacePathWithinAnyBoundary(&s.trustedPaths, cleanPath, roots, includePaths, virtualRoots)
}

func (s *Server) missingIncludeTargetDetailsContext(ctx context.Context, targetPath, includePath string) includeTargetDetails {
	if !s.windowsPathResolutionEnabled() || !s.includeCaseResolutionEnabled() {
		return includeTargetDetails{Path: targetPath, Exists: false, ActualIncludePath: includePath}
	}
	actualPath, ok := s.resolveCaseInsensitivePathContext(ctx, targetPath)
	if !ok {
		return includeTargetDetails{Path: targetPath, Exists: false, ActualIncludePath: includePath}
	}
	return includeTargetDetails{
		Path:              actualPath,
		Exists:            true,
		CaseMismatch:      filepath.Clean(actualPath) != filepath.Clean(targetPath),
		ActualIncludePath: filepath.Base(actualPath),
	}
}

func (s *Server) windowsPathResolutionEnabled() bool {
	s.mu.Lock()
	enabled := s.settings.WindowsPathResolution
	s.mu.Unlock()
	return enabled
}

func filePathURI(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	return absoluteFilePathURI(absolute)
}

func absoluteFilePathURI(absolute string) string {
	slashPath := strings.ReplaceAll(absolute, "\\", "/")
	lowerPath := strings.ToLower(slashPath)
	if strings.HasPrefix(lowerPath, "//?/unc/") {
		slashPath = "//" + slashPath[len("//?/UNC/"):]
	} else if strings.HasPrefix(slashPath, "//?/") {
		slashPath = slashPath[len("//?/"):]
	}
	if strings.HasPrefix(slashPath, "///") {
		slashPath = "/" + strings.TrimLeft(slashPath, "/")
	}
	if strings.HasPrefix(slashPath, "//") {
		host, uriPath, _ := strings.Cut(strings.TrimPrefix(slashPath, "//"), "/")
		if host != "" {
			if uriPath != "" {
				uriPath = "/" + uriPath
			}
			return (&url.URL{Scheme: "file", Host: host, Path: uriPath}).String()
		}
	}
	if len(slashPath) >= 2 && isWindowsDriveSegment(slashPath[:2]) {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func isWindowsDriveSegment(value string) bool {
	return len(value) == 2 && (value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z') && value[1] == ':'
}

func canonicalGraphURI(uri string) string {
	if !strings.HasPrefix(strings.ToLower(uri), "file:") {
		return uri
	}
	path := fileURIPath(uri)
	if path == "" {
		return uri
	}
	return filePathURI(path)
}

func (s *Server) graphDisplayFileName(path string) string {
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	roots := s.workspaceRootsSnapshot()
	root := workspaceRootPathForPath(path, roots)
	if root == "" {
		return path
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "" {
		return filepath.Base(path)
	}
	return filepath.ToSlash(relative)
}

func isWorkspaceASPFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".asp", ".asa", ".inc", ".vbs":
		return true
	default:
		return false
	}
}

type workspaceGlobMatcher struct {
	pattern string
	regexp  *regexp.Regexp
}

func compileWorkspaceGlob(pattern string) workspaceGlobMatcher {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	source, ok := workspaceGlobRegexpSource(pattern)
	if !ok {
		return workspaceGlobMatcher{pattern: pattern}
	}
	matcher, err := regexp.Compile("(?i)^" + source + "$")
	if err != nil {
		return workspaceGlobMatcher{pattern: pattern}
	}
	return workspaceGlobMatcher{pattern: pattern, regexp: matcher}
}

func (matcher workspaceGlobMatcher) matches(relative string) bool {
	if matcher.regexp == nil {
		return false
	}
	relative = strings.TrimPrefix(filepath.ToSlash(relative), "./")
	if matcher.regexp.MatchString(relative) {
		return true
	}
	return !strings.Contains(matcher.pattern, "/") && matcher.regexp.MatchString(filepath.Base(relative))
}

func compileWorkspaceGlobs(patterns []string) []workspaceGlobMatcher {
	matchers := make([]workspaceGlobMatcher, len(patterns))
	for index, pattern := range patterns {
		matchers[index] = compileWorkspaceGlob(pattern)
	}
	return matchers
}

func matchWorkspaceGlob(pattern, relative string) bool {
	return compileWorkspaceGlob(pattern).matches(relative)
}

func workspaceGlobRegexpSource(pattern string) (string, bool) {
	var source strings.Builder
	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index += 2
				if index < len(pattern) && pattern[index] == '/' {
					index++
					source.WriteString("(?:.*/)?")
				} else {
					source.WriteString(".*")
				}
			} else {
				index++
				source.WriteString("[^/]*")
			}
		case '?':
			index++
			source.WriteString("[^/]")
		case '{':
			close := strings.IndexByte(pattern[index+1:], '}')
			if close < 0 {
				source.WriteString(`\{`)
				index++
				continue
			}
			close += index + 1
			parts := strings.Split(pattern[index+1:close], ",")
			source.WriteString("(?:")
			for partIndex, part := range parts {
				compiled, valid := workspaceGlobRegexpSource(part)
				if !valid {
					return "", false
				}
				if partIndex > 0 {
					source.WriteByte('|')
				}
				source.WriteString(compiled)
			}
			source.WriteByte(')')
			index = close + 1
		case '[':
			close := strings.IndexByte(pattern[index+1:], ']')
			if close <= 0 {
				source.WriteString(`\[`)
				index++
				continue
			}
			close += index + 1
			content := pattern[index+1 : close]
			if strings.HasPrefix(content, "!") {
				content = "^" + strings.TrimPrefix(content, "!")
			}
			source.WriteByte('[')
			source.WriteString(content)
			source.WriteByte(']')
			index = close + 1
		default:
			source.WriteString(regexp.QuoteMeta(string(pattern[index])))
			index++
		}
	}
	return source.String(), true
}

func workspaceGraphFileAllowed(relative string, includeGlobs, excludeGlobs, gitIgnoreGlobs []string) bool {
	includeMatch := len(includeGlobs) == 0
	for _, pattern := range includeGlobs {
		if matchWorkspaceGlob(pattern, relative) {
			includeMatch = true
			break
		}
	}
	if !includeMatch {
		return false
	}
	for _, pattern := range excludeGlobs {
		if matchWorkspaceGlob(pattern, relative) || workspaceGlobMatchesAncestor(pattern, relative) {
			return false
		}
	}
	return !workspaceGitIgnoreGlobsIgnorePath(relative, gitIgnoreGlobs)
}

type workspaceGitIgnoreMatcher struct {
	negated bool
	matcher workspaceGlobMatcher
	base    workspaceGlobMatcher
	hasBase bool
}

type workspaceGraphFileFilter struct {
	include         []workspaceGlobMatcher
	exclude         []workspaceGlobMatcher
	gitIgnoreByRoot map[string][]workspaceGitIgnoreMatcher
}

func newWorkspaceGraphFileFilter(includeGlobs, excludeGlobs []string, gitIgnoreGlobs map[string][]string) workspaceGraphFileFilter {
	filter := workspaceGraphFileFilter{
		include:         compileWorkspaceGlobs(includeGlobs),
		exclude:         compileWorkspaceGlobs(excludeGlobs),
		gitIgnoreByRoot: make(map[string][]workspaceGitIgnoreMatcher, len(gitIgnoreGlobs)),
	}
	for rootPath, rules := range gitIgnoreGlobs {
		compiled := make([]workspaceGitIgnoreMatcher, 0, len(rules))
		for _, rule := range rules {
			pattern := strings.TrimPrefix(rule, "!")
			if pattern == "" {
				continue
			}
			matcher := workspaceGitIgnoreMatcher{
				negated: strings.HasPrefix(rule, "!"),
				matcher: compileWorkspaceGlob(pattern),
			}
			if strings.HasSuffix(pattern, "/**") {
				matcher.base = compileWorkspaceGlob(strings.TrimSuffix(pattern, "/**"))
				matcher.hasBase = true
			}
			compiled = append(compiled, matcher)
		}
		filter.gitIgnoreByRoot[rootPath] = compiled
	}
	return filter
}

func (filter workspaceGraphFileFilter) allows(relative, rootPath string) bool {
	includeMatch := len(filter.include) == 0
	for _, matcher := range filter.include {
		if matcher.matches(relative) {
			includeMatch = true
			break
		}
	}
	if !includeMatch {
		return false
	}
	for _, matcher := range filter.exclude {
		if matcher.matches(relative) || workspaceGlobMatcherMatchesAncestor(matcher, relative) {
			return false
		}
	}
	ignored := false
	for _, matcher := range filter.gitIgnoreByRoot[rootPath] {
		if workspaceGitIgnoreMatcherMatches(matcher, relative) || workspaceGitIgnoreMatcherMatchesAncestor(matcher, relative) {
			ignored = !matcher.negated
		}
	}
	return !ignored
}

func workspaceGlobMatchesAncestor(pattern, relative string) bool {
	return workspaceGlobMatcherMatchesAncestor(compileWorkspaceGlob(pattern), relative)
}

func workspaceGlobMatcherMatchesAncestor(matcher workspaceGlobMatcher, relative string) bool {
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	for index := 1; index < len(parts); index++ {
		ancestor := strings.Join(parts[:index], "/")
		if matcher.matches(ancestor) || matcher.matches(ancestor+"/") {
			return true
		}
	}
	return false
}

func workspaceGitIgnoreGlobsIgnorePath(relative string, gitIgnoreGlobs []string) bool {
	ignored := false
	for _, rule := range gitIgnoreGlobs {
		negated := strings.HasPrefix(rule, "!")
		pattern := strings.TrimPrefix(rule, "!")
		if pattern == "" {
			continue
		}
		if gitIgnorePatternMatches(pattern, relative) || gitIgnorePatternMatchesAncestor(pattern, relative) {
			ignored = !negated
		}
	}
	return ignored
}

func gitIgnorePatternMatches(pattern, relative string) bool {
	if matchWorkspaceGlob(pattern, relative) || matchWorkspaceGlob(pattern, relative+"/") {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		base := strings.TrimSuffix(pattern, "/**")
		return matchWorkspaceGlob(base, relative) || matchWorkspaceGlob(base, relative+"/")
	}
	return false
}

func workspaceGitIgnoreMatcherMatches(matcher workspaceGitIgnoreMatcher, relative string) bool {
	if matcher.matcher.matches(relative) || matcher.matcher.matches(relative+"/") {
		return true
	}
	if matcher.hasBase {
		return matcher.base.matches(relative) || matcher.base.matches(relative+"/")
	}
	return false
}

func gitIgnorePatternMatchesAncestor(pattern, relative string) bool {
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	for index := 1; index < len(parts); index++ {
		ancestor := strings.Join(parts[:index], "/")
		if gitIgnorePatternMatches(pattern, ancestor) {
			return true
		}
	}
	return false
}

func workspaceGitIgnoreMatcherMatchesAncestor(matcher workspaceGitIgnoreMatcher, relative string) bool {
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	for index := 1; index < len(parts); index++ {
		ancestor := strings.Join(parts[:index], "/")
		if workspaceGitIgnoreMatcherMatches(matcher, ancestor) {
			return true
		}
	}
	return false
}

func (s *Server) readGitIgnoreGlobs(rootPath string) []string {
	return s.readGitIgnoreGlobsContext(withSourceReadBoundaries(context.Background(), rootPath), rootPath)
}

func (s *Server) readGitIgnoreGlobsContext(ctx context.Context, rootPath string) []string {
	globs := []string{}
	_ = filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == "dist" || name == "out" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != ".gitignore" {
			return nil
		}
		content, err := s.readSourceFileBytes(ctx, path, s.includeReadLimiter)
		if err != nil {
			return nil
		}
		directory, err := filepath.Rel(rootPath, filepath.Dir(path))
		if err != nil || directory == "." {
			directory = ""
		} else {
			directory = filepath.ToSlash(directory)
		}
		for _, rawLine := range strings.Split(string(content), "\n") {
			line := strings.TrimSpace(rawLine)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			negated := strings.HasPrefix(line, "!")
			if negated {
				line = strings.TrimPrefix(line, "!")
			}
			line = strings.ReplaceAll(line, `\#`, "#")
			line = strings.ReplaceAll(line, `\!`, "!")
			if line == "" {
				continue
			}
			anchored := strings.HasPrefix(line, "/")
			line = filepath.ToSlash(strings.TrimPrefix(line, "/"))
			if strings.HasSuffix(line, "/") {
				line += "**"
			}
			if directory != "" {
				if anchored || strings.Contains(line, "/") {
					line = directory + "/" + line
				} else {
					line = directory + "/**/" + line
				}
			} else if anchored && !strings.Contains(line, "/") {
				line += "{,/**}"
			}
			if negated {
				line = "!" + line
			}
			globs = append(globs, line)
		}
		return nil
	})
	return globs
}
