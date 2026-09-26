package lspserver

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestRPCWriteLoopsThroughShortNilWrites(t *testing.T) {
	writer := &storingDiagnosticTransportWriter{shortWrites: []int{3, 4}}
	server := New(strings.NewReader(""), writer, io.Discard)
	message := rpcMessage{Method: "diagnostic/test", Params: mustRaw(map[string]any{"value": "short-writes"})}
	result := server.writeRPCMessageResult(message)
	if result.outcome != rpcWriteFullSuccess || result.err != nil {
		t.Fatalf("short-nil write result = %#v, want full success", result)
	}
	decoded, err := readMessage(bufio.NewReader(bytes.NewReader(writer.output.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Method != message.Method || string(decoded.Params) != string(message.Params) {
		t.Fatalf("short-nil decoded message = %#v, want %#v", decoded, message)
	}
	if writer.calls != 3 {
		t.Fatalf("short-nil writer calls = %d, want three", writer.calls)
	}
}

func TestRPCWriteZeroProgressRetriesCompleteFrame(t *testing.T) {
	writer := &storingDiagnosticTransportWriter{zeroProgressAt: 1}
	server := New(strings.NewReader(""), writer, io.Discard)
	message := rpcMessage{Method: "diagnostic/test", Params: mustRaw(map[string]any{"value": "zero-then-success"})}
	result := server.writeRPCMessageResult(message)
	if result.outcome != rpcWriteFullSuccess || result.err != nil {
		t.Fatalf("zero-progress retry result = %#v, want full success", result)
	}
	decoded, err := readMessage(bufio.NewReader(bytes.NewReader(writer.output.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Method != message.Method || string(decoded.Params) != string(message.Params) {
		t.Fatalf("zero-progress decoded message = %#v, want %#v", decoded, message)
	}
	if writer.calls != 2 {
		t.Fatalf("zero-progress writer calls = %d, want one retry", writer.calls)
	}
}

func TestRPCWriteRepeatedZeroProgressLatchesBoundedFatal(t *testing.T) {
	writeErr := errors.New("transport would block forever")
	writer := &storingDiagnosticTransportWriter{zeroProgressAlways: true, zeroProgressErr: writeErr}
	server := New(strings.NewReader(""), writer, io.Discard)
	message := rpcMessage{Method: "diagnostic/retry", Params: mustRaw(map[string]any{})}
	result := server.writeRPCMessageResult(message)
	if result.outcome != rpcWriteFatal {
		t.Fatalf("repeated zero-progress outcome = %d, want fatal", result.outcome)
	}
	var fatal *rpcTransportFatalError
	if !errors.As(result.err, &fatal) || !errors.Is(result.err, writeErr) {
		t.Fatalf("repeated zero-progress error = %v, want fatal preserving writer cause", result.err)
	}
	if writer.calls != rpcZeroWriteRetryLimit+1 {
		t.Fatalf("repeated zero-progress writer calls = %d, want bounded %d", writer.calls, rpcZeroWriteRetryLimit+1)
	}
	if writer.output.Len() != 0 {
		t.Fatalf("repeated zero-progress escaped %d bytes", writer.output.Len())
	}
	if server.writeFatalError() != fatal {
		t.Fatalf("latched fatal = %v, want first fatal %v", server.writeFatalError(), fatal)
	}
	if retry := server.writeRPCMessageResult(message); retry.err != fatal || writer.calls != rpcZeroWriteRetryLimit+1 {
		t.Fatalf("post-fatal write = %#v, want sticky fatal without another writer call", retry)
	}
}

func TestRPCWritePartialProgressNeverRetriesCompleteFrame(t *testing.T) {
	writer := &storingDiagnosticTransportWriter{shortWrites: []int{1}, zeroProgressAt: 2}
	server := New(strings.NewReader(""), writer, io.Discard)
	result := server.writeRPCMessageResult(rpcMessage{Method: "diagnostic/partial", Params: mustRaw(map[string]any{})})
	if result.outcome != rpcWriteFatal {
		t.Fatalf("partial-then-zero outcome = %d, want fatal", result.outcome)
	}
	if writer.calls != 2 {
		t.Fatalf("partial-then-zero writer calls = %d, want no retry", writer.calls)
	}
	if writer.output.Len() == 0 {
		t.Fatal("partial-then-zero writer escaped no partial bytes")
	}
}

func TestRPCWriteZeroProgressNotificationDelivered(t *testing.T) {
	writer := &storingDiagnosticTransportWriter{zeroProgressAt: 1}
	server := New(strings.NewReader(""), writer, io.Discard)
	if err := server.sendNotification("$/progress", map[string]any{"token": "partial", "value": []any{map[string]any{"ok": true}}}); err != nil {
		t.Fatalf("progress notification write = %v", err)
	}
	message, err := readMessage(bufio.NewReader(bytes.NewReader(writer.output.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if message.Method != "$/progress" {
		t.Fatalf("progress notification method = %q, want $/progress", message.Method)
	}
}

func TestRPCWriteZeroProgressLogNotificationDelivered(t *testing.T) {
	writer := &storingDiagnosticTransportWriter{zeroProgressAt: 1}
	server := New(strings.NewReader(""), writer, io.Discard)
	server.mu.Lock()
	server.settings.DebugOutput = "summary"
	server.mu.Unlock()
	server.logDebugSummary("zero-write-log")
	message, err := readMessage(bufio.NewReader(bytes.NewReader(writer.output.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if message.Method != "window/logMessage" {
		t.Fatalf("log notification method = %q, want window/logMessage", message.Method)
	}
}

func TestServeResponseRetriesZeroWriteBeforeUnregisteringRequest(t *testing.T) {
	request, err := encodeMessage(rpcMessage{ID: "lifecycle", Method: "shutdown", Params: mustRaw(map[string]any{})})
	if err != nil {
		t.Fatal(err)
	}
	writer := &responseLifecycleWriter{}
	server := New(bytes.NewReader(request), writer, io.Discard)
	writer.server = server
	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("Serve = %v, want clean shutdown after EOF", err)
	}
	if !writer.registeredAtZero {
		t.Fatal("request cancellation was unregistered before zero-byte response retry")
	}
	if len(server.requestCancellations) != 0 {
		t.Fatalf("request cancellation entries after response = %d, want zero", len(server.requestCancellations))
	}
	response, err := readMessage(bufio.NewReader(bytes.NewReader(writer.output.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "lifecycle" || !response.resultPresent || response.errorPresent {
		t.Fatalf("lifecycle response = %#v, want successful response", response)
	}
}

func TestDiagnosticPublicationFatalPartialWriteDoesNotAppendCompensation(t *testing.T) {
	server, ownerURI, childURI, _ := newDiagnosticFailureFixture(t)
	writer := &storingDiagnosticTransportWriter{partialErrorAt: 1, writeErr: errors.New("transport disconnected")}
	server.out = writer
	_, err := server.publishDiagnosticSnapshotContext(context.Background(), ownerURI, 1, []lsp.Diagnostic{publicationTestDiagnostic(childURI, "fatal")})
	var fatal *rpcTransportFatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("fatal partial publication error = %v, want rpcTransportFatalError", err)
	}
	if writer.calls != 1 {
		t.Fatalf("fatal partial writer calls = %d, want no compensation/retry writes", writer.calls)
	}
	if writer.output.Len() == 0 {
		t.Fatal("fatal partial writer stored no partial frame")
	}
	items, targets, revisions := server.publishedDiagnosticContributionSnapshot(ownerURI)
	if len(items) != 0 || len(targets) != 0 || len(revisions) != 0 {
		t.Fatalf("fatal partial publication committed bookkeeping: items=%#v targets=%#v revisions=%#v", items, targets, revisions)
	}
}

type storingDiagnosticTransportWriter struct {
	output             bytes.Buffer
	shortWrites        []int
	zeroProgressAt     int
	zeroProgressAlways bool
	zeroProgressErr    error
	partialErrorAt     int
	writeErr           error
	calls              int
}

type responseLifecycleWriter struct {
	server           *Server
	output           bytes.Buffer
	registeredAtZero bool
	calls            int
}

func (w *responseLifecycleWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		w.server.mu.Lock()
		w.registeredAtZero = len(w.server.requestCancellations) > 0
		w.server.mu.Unlock()
		return 0, nil
	}
	return w.output.Write(data)
}

func (w *storingDiagnosticTransportWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.zeroProgressAlways || w.zeroProgressAt == w.calls {
		return 0, w.zeroProgressErr
	}
	if w.partialErrorAt == w.calls {
		n := len(data) / 2
		if n == 0 && len(data) > 0 {
			n = 1
		}
		_, _ = w.output.Write(data[:n])
		return n, w.writeErr
	}
	n := len(data)
	if index := w.calls - 1; index >= 0 && index < len(w.shortWrites) && w.shortWrites[index] >= 0 && w.shortWrites[index] < n {
		n = w.shortWrites[index]
	}
	_, _ = w.output.Write(data[:n])
	return n, nil
}
