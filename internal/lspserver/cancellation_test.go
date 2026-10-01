package lspserver

import (
	"bufio"
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

func TestServerCancelsRequestsByJSONRPCID(t *testing.T) {
	server := New(nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	server.registerRequestCancellation("request-42", cancel, 1)
	server.cancelRequest(mustRaw(map[string]any{"id": "request-42"}))
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("$/cancelRequest did not cancel the matching request")
	}
}

func TestServerCancelsOnlyRequestsOlderThanPublishedRevision(t *testing.T) {
	server := New(nil, nil, nil)
	older, cancelOlder := context.WithCancel(t.Context())
	newer, cancelNewer := context.WithCancel(t.Context())
	defer cancelOlder()
	defer cancelNewer()
	server.registerRequestCancellation("older", cancelOlder, 4)
	server.registerRequestCancellation("newer", cancelNewer, 6)

	server.cancelRequestsBefore(5)
	select {
	case <-older.Done():
	case <-time.After(time.Second):
		t.Fatal("request older than the revision was not cancelled")
	}
	select {
	case <-newer.Done():
		t.Fatal("request newer than the revision was cancelled")
	default:
	}
}

func TestServeRejectsDuplicateActiveRequestIDWithoutOrphaningCancellation(t *testing.T) {
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()
	server := New(serverReader, serverWriter, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	server.requestDispatchTestHook = func(ctx context.Context, method string) {
		if method != "workspace/symbol" {
			return
		}
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background()) }()
	reader := bufio.NewReader(clientReader)
	defer func() {
		close(release)
		_ = writeMessage(clientWriter, rpcMessage{Method: "exit"})
		_ = clientWriter.Close()
		_ = clientReader.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("stdio server did not stop after duplicate request test")
		}
	}()

	requestParams := mustRaw(map[string]any{"query": "duplicate"})
	if err := writeMessage(clientWriter, rpcMessage{ID: "duplicate", Method: "workspace/symbol", Params: requestParams}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first duplicate-ID request did not start")
	}
	if err := writeMessage(clientWriter, rpcMessage{ID: "duplicate", Method: "workspace/symbol", Params: requestParams}); err != nil {
		t.Fatal(err)
	}
	duplicateResponse, err := readMessage(reader)
	if err != nil {
		t.Fatal(err)
	}
	if duplicateResponse.ID != "duplicate" || duplicateResponse.Error == nil || duplicateResponse.Error.Code != -32600 {
		t.Fatalf("duplicate request response = %#v, want duplicate request error", duplicateResponse)
	}

	if err := writeMessage(clientWriter, rpcMessage{
		Method: "$/cancelRequest",
		Params: mustRaw(map[string]any{"id": "duplicate"}),
	}); err != nil {
		t.Fatal(err)
	}
	cancelledResponse, err := readMessage(reader)
	if err != nil {
		t.Fatal(err)
	}
	if cancelledResponse.ID != "duplicate" || cancelledResponse.Error == nil || cancelledResponse.Error.Code != -32800 {
		t.Fatalf("first request response after duplicate = %#v, want cancellation", cancelledResponse)
	}
}

func TestStdioParityCancelsQueuedRequestWithoutBlockingNotifications(t *testing.T) {
	if err := os.Setenv("ASP_LSP_TEST_REQUEST_DELAY_MS", "1000"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("ASP_LSP_TEST_REQUEST_DELAY_MS")

	client := startStdioTestClient(t)
	defer client.close()
	response := client.requestAsync("workspace/symbol", map[string]any{"query": "anything"})
	if err := client.notify("$/cancelRequest", map[string]any{"id": 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-response:
		if message.Error == nil || message.Error.Code != -32800 {
			t.Fatalf("cancelled request error = %#v, want code -32800", message.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled request did not return promptly")
	}
}

func TestServerKeepsWorkspaceDiagnosticPullsAcrossRevisions(t *testing.T) {
	server := New(nil, nil, nil)
	pull, cancelPull := context.WithCancel(t.Context())
	document, cancelDocument := context.WithCancel(t.Context())
	defer cancelPull()
	defer cancelDocument()
	server.registerRequestCancellationEntry("pull", requestCancellationEntry{cancel: cancelPull, sequence: 4, revisionIndependent: revisionIndependentRequest("workspace/diagnostic")})
	server.registerRequestCancellationEntry("document", requestCancellationEntry{cancel: cancelDocument, sequence: 4, revisionIndependent: revisionIndependentRequest("textDocument/diagnostic")})

	server.cancelRequestsBefore(5)
	select {
	case <-document.Done():
	case <-time.After(time.Second):
		t.Fatal("document diagnostic pull older than the revision was not cancelled")
	}
	select {
	case <-pull.Done():
		t.Fatal("workspace diagnostic pass was cancelled by an unrelated revision")
	default:
	}
}
