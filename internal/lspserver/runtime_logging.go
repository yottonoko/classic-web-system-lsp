package lspserver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) logDebugSummaryEvent(category, message string, metadata map[string]any) {
	s.logDebugFileWithMetadata("DEBUG", category, message, metadata)
	if s.isDebugSummaryEnabled() {
		s.reportAsyncRPCWriteError(s.writeRPCMessage(rpcMessage{Method: "window/logMessage", Params: mustRaw(map[string]any{"type": 3, "message": message})}))
	}
}

func (s *Server) logDebugVerboseEvent(category, message string, metadata map[string]any) {
	s.logDebugFileWithMetadata("DEBUG", category, message, metadata)
	if s.isDebugVerboseEnabled() {
		s.reportAsyncRPCWriteError(s.writeRPCMessage(rpcMessage{Method: "window/logMessage", Params: mustRaw(map[string]any{"type": 3, "message": message})}))
	}
}

func (s *Server) logAnalysisDatabaseEvent(component, operation string, metadata map[string]any) {
	fields := cloneLogMetadata(metadata)
	fields["component"] = component
	fields["operation"] = operation
	message := "[asp-lsp] database." + component + "." + operation + formatLogFields(metadata)
	s.logDebugSummaryEvent("database."+component, message, fields)
}

func cloneLogMetadata(metadata map[string]any) map[string]any {
	cloned := make(map[string]any, len(metadata)+2)
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func formatLogFields(fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteByte(' ')
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(formatLogValue(fields[key]))
	}
	return builder.String()
}

func formatLogValue(value any) string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return `""`
		}
		if strings.ContainsAny(typed, " \t\r\n\"") {
			return strconv.Quote(typed)
		}
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return `"unavailable"`
		}
		return string(encoded)
	}
}

func shortLogKey(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func (s *Server) logInboundLSPEvent(message *rpcMessage, kind string, receivedAt time.Time) {
	if message == nil {
		return
	}
	metadata := inboundLSPMetadata(message.Method, message.Params)
	if message.logSpan.id != "" {
		message.logSpan.addFields(metadata)
	}
	metadata["direction"] = "in"
	metadata["kind"] = kind
	metadata["method"] = message.Method
	metadata["paramsBytes"] = len(message.Params)
	if message.idPresent || message.ID != nil {
		metadata["requestId"] = safeRequestID(rpcMessageIDForWire(message))
	}
	if kind == "response" {
		status := "ok"
		if message.Error != nil {
			status = "error"
			metadata["code"] = message.Error.Code
		}
		metadata["resultBytes"] = marshaledValueSize(message.Result)
		metadata["status"] = status
	}
	metadata["receivedAt"] = receivedAt.UTC().Format(time.RFC3339Nano)
	event := "lsp." + kind + ".received"
	messageText := "[asp-lsp] " + event + formatLogFields(withoutLogField(metadata, "receivedAt"))
	s.logDebugSummaryEvent(event, messageText, metadata)
}

func (s *Server) logCompletedLSPEvent(requestID any, method, kind string, started time.Time, rpcErr *rpcError, err error, spans ...runtimeLogSpan) {
	status := "ok"
	metadata := map[string]any{
		"direction":  "in",
		"durationMs": float64(time.Since(started).Microseconds()) / 1000,
		"kind":       kind,
		"method":     method,
	}
	if requestID != nil {
		metadata["requestId"] = safeRequestID(requestID)
	}
	if rpcErr != nil {
		status = "error"
		metadata["code"] = rpcErr.Code
		if rpcErr.Code == requestCancelledError().Code {
			status = "cancelled"
		}
	} else if err != nil {
		status = "error"
	}
	metadata["status"] = status
	event := "lsp." + kind + ".completed"
	message := "[asp-lsp] " + event + formatLogFields(metadata)
	correlation := map[string]any{}
	for _, span := range spans {
		if span.id != "" {
			span.addFields(metadata)
			span.addFields(correlation)
		}
	}
	message += formatLogFields(correlation)
	s.logDebugSummaryEvent(event, message, metadata)
}

func (s *Server) logInboundClientResponse(message *rpcMessage, receivedAt time.Time, pending pendingClientRequest, matched bool) {
	if message == nil {
		return
	}
	metadata := clientResponseLogMetadata(message, pending, matched)
	metadata["direction"] = "in"
	metadata["kind"] = "response"
	metadata["receivedAt"] = receivedAt.UTC().Format(time.RFC3339Nano)
	event := "lsp.response.received"
	s.logDebugSummaryEvent(event, "[asp-lsp] "+event+formatLogFields(withoutLogField(metadata, "receivedAt")), metadata)
}

func (s *Server) logCompletedClientResponse(message *rpcMessage, receivedAt time.Time, pending pendingClientRequest, matched bool) {
	if message == nil {
		return
	}
	metadata := clientResponseLogMetadata(message, pending, matched)
	metadata["deliveryMs"] = float64(time.Since(receivedAt).Microseconds()) / 1000
	metadata["direction"] = "in"
	metadata["kind"] = "response"
	event := "lsp.response.completed"
	s.logDebugSummaryEvent(event, "[asp-lsp] "+event+formatLogFields(metadata), metadata)
}

func clientResponseLogMetadata(message *rpcMessage, pending pendingClientRequest, matched bool) map[string]any {
	status := "ok"
	if !matched {
		status = "unmatched"
	}
	metadata := map[string]any{
		"matched": matched, "method": pending.method, "requestId": safeRequestID(message.ID),
		"resultBytes": marshaledValueSize(message.Result), "status": status,
	}
	if matched && !pending.startedAt.IsZero() {
		metadata["roundTripMs"] = float64(time.Since(pending.startedAt).Microseconds()) / 1000
	}
	if message.Error != nil {
		metadata["code"] = message.Error.Code
		metadata["status"] = "error"
	}
	return metadata
}

func safeRequestID(id any) string {
	if value, ok := id.(string); ok && len(value) <= 80 {
		return value
	}
	raw, err := json.Marshal(id)
	if err != nil {
		return "unavailable"
	}
	if len(raw) <= 80 {
		return string(raw)
	}
	return "sha256:" + shortLogKey(fmt.Sprintf("%x", sha256.Sum256(raw)))
}

func marshaledValueSize(value any) int {
	if value == nil {
		return 0
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return len(raw)
}

func withoutLogField(fields map[string]any, omitted string) map[string]any {
	result := make(map[string]any, len(fields)-1)
	for key, value := range fields {
		if key != omitted {
			result[key] = value
		}
	}
	return result
}

func inboundLSPMetadata(method string, params json.RawMessage) map[string]any {
	metadata := map[string]any{}
	var value struct {
		URI          string `json:"uri"`
		Command      string `json:"command"`
		TextDocument struct {
			URI     string `json:"uri"`
			Version *int   `json:"version"`
		} `json:"textDocument"`
		Position *struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
		ContentChanges []json.RawMessage `json:"contentChanges"`
		Changes        []json.RawMessage `json:"changes"`
	}
	if len(params) > 0 && json.Unmarshal(params, &value) == nil {
		uri := value.TextDocument.URI
		if uri == "" {
			uri = value.URI
		}
		if uri != "" {
			metadata["uri"] = uri
		}
		if value.TextDocument.Version != nil {
			metadata["version"] = *value.TextDocument.Version
		}
		if value.Position != nil {
			metadata["position"] = strconv.Itoa(value.Position.Line) + ":" + strconv.Itoa(value.Position.Character)
		}
		if len(value.ContentChanges) > 0 {
			metadata["changes"] = len(value.ContentChanges)
		}
		if len(value.Changes) > 0 {
			metadata["files"] = len(value.Changes)
		}
		if method == "workspace/executeCommand" && value.Command != "" {
			metadata["command"] = value.Command
		}
	}
	return metadata
}

func (s *Server) logMeasuredStep(uri, step string, started time.Time, count int, spans ...runtimeLogSpan) {
	if !s.isDebugVerboseEnabled() && !s.debugLogFileEnabled() {
		return
	}
	durationMs := float64(time.Since(started).Microseconds()) / 1000
	metadata := map[string]any{
		"count":      count,
		"durationMs": durationMs,
		"step":       step,
		"uri":        uri,
	}
	for _, span := range spans {
		span.addFields(metadata)
	}
	messageFields := cloneLogMetadata(metadata)
	delete(messageFields, "durationMs")
	s.logDebugVerboseEvent("diagnostics.step", "[asp-lsp] "+step+" "+formatElapsedSince(started)+formatLogFields(messageFields), metadata)
}

func (s *Server) logMeasuredStepStarted(uri, step string, spans ...runtimeLogSpan) {
	if !s.isDebugVerboseEnabled() && !s.debugLogFileEnabled() {
		return
	}
	metadata := map[string]any{"step": step, "uri": uri}
	for _, span := range spans {
		span.addFields(metadata)
	}
	s.logDebugVerboseEvent("diagnostics.step.started", "[asp-lsp] "+step+".started"+formatLogFields(metadata), metadata)
}

func (s *Server) logMeasuredStepTerminated(uri, step, state string, started time.Time, count int, spans ...runtimeLogSpan) {
	if !s.isDebugVerboseEnabled() && !s.debugLogFileEnabled() {
		return
	}
	durationMs := float64(time.Since(started).Microseconds()) / 1000
	metadata := map[string]any{
		"count":      count,
		"durationMs": durationMs,
		"state":      state,
		"step":       step,
		"uri":        uri,
	}
	for _, span := range spans {
		span.addFields(metadata)
	}
	messageFields := cloneLogMetadata(metadata)
	delete(messageFields, "durationMs")
	s.logDebugVerboseEvent("diagnostics.step."+state, "[asp-lsp] "+step+"."+state+" "+formatElapsedSince(started)+formatLogFields(messageFields), metadata)
}
