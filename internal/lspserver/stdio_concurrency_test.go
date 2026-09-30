package lspserver

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestAdmissionLaneBoundsBytesAndReleasesOnce(t *testing.T) {
	largeIDMessage := &rpcMessage{ID: strings.Repeat("x", 600), Method: "textDocument/hover"}
	largeIDBytes := requestRetainedBytes(largeIDMessage)
	if largeIDBytes <= 1024 {
		t.Fatalf("large request ID retained bytes = %d, want more than test lane capacity", largeIDBytes)
	}
	smallLane := &requestAdmissionLane{slots: make(chan struct{}, 1), maxBytes: 1024}
	if token := smallLane.tryAcquire(largeIDBytes); token != nil {
		token.release()
		t.Fatal("request admission did not charge retained request ID and cancellation key bytes")
	}

	lane := newRequestAdmissionLane(2)
	token := lane.tryAcquire(requestAdmissionByteCapacity)
	if token == nil {
		t.Fatal("request admission rejected its exact byte capacity")
	}
	if overflow := lane.tryAcquire(1); overflow != nil {
		overflow.release()
		t.Fatal("request admission exceeded its byte capacity")
	}
	token.release()
	token.release()
	if next := lane.tryAcquire(requestAdmissionByteCapacity); next == nil {
		t.Fatal("request admission did not release byte and count capacity")
	} else {
		next.release()
	}
}

func TestServeRespondsToIndependentRequestsConcurrently(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	var startedOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			startedOnce.Do(func() { close(slowStarted) })
			select {
			case <-releaseSlow:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	slow := client.requestAsync("workspace/symbol", map[string]any{"query": "slow"})
	waitForConcurrencySignal(t, slowStarted, "slow request did not start")
	fast := client.requestAsync("codeAction/resolve", map[string]any{"title": "fast"})
	select {
	case response := <-fast:
		if response.Error != nil {
			t.Fatalf("fast request returned error: %#v", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("fast request was blocked behind an independent slow request")
	}

	close(releaseSlow)
	client.waitForResponse("workspace/symbol", slow)
}

func TestServeInteractiveRequestBypassesSaturatedBackgroundLane(t *testing.T) {
	releaseBackground := make(chan struct{})
	backgroundStarted := make(chan struct{}, 16)
	hoverStarted := make(chan struct{})
	var hoverOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			switch method {
			case "textDocument/references":
				backgroundStarted <- struct{}{}
				select {
				case <-releaseBackground:
				case <-ctx.Done():
				}
			case "textDocument/hover":
				hoverOnce.Do(func() { close(hoverStarted) })
			}
		}
	})
	defer client.close()

	background := make([]<-chan *rpcMessage, 16)
	for index := range background {
		background[index] = client.requestAsync("textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
			"position":     map[string]any{"line": 0, "character": 0},
			"context":      map[string]any{"includeDeclaration": true},
		})
	}
	for range 2 {
		waitForConcurrencySignal(t, backgroundStarted, "background request did not fill its lane")
	}
	hover := client.requestAsync("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	waitForConcurrencySignal(t, hoverStarted, "interactive request did not bypass the background lane")
	client.waitForResponse("textDocument/hover", hover)

	close(releaseBackground)
	for _, response := range background {
		client.waitForResponse("textDocument/references", response)
	}
}

func TestServeInteractiveRequestSurvivesFullBackgroundAdmission(t *testing.T) {
	releaseBackground := make(chan struct{})
	backgroundStarted := make(chan struct{}, requestAdmissionCapacity)
	hoverStarted := make(chan struct{})
	var hoverOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			switch method {
			case "textDocument/references":
				backgroundStarted <- struct{}{}
				select {
				case <-releaseBackground:
				case <-ctx.Done():
				}
			case "textDocument/hover":
				hoverOnce.Do(func() { close(hoverStarted) })
			}
		}
	})
	defer client.close()

	background := make([]<-chan *rpcMessage, requestAdmissionCapacity)
	for index := range background {
		background[index] = client.requestAsync("textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
			"position":     map[string]any{"line": 0, "character": 0},
			"context":      map[string]any{"includeDeclaration": true},
		})
	}
	for range 2 {
		waitForConcurrencySignal(t, backgroundStarted, "background request did not fill its execution lane")
	}

	hover := client.requestAsync("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	waitForConcurrencySignal(t, hoverStarted, "interactive request was rejected by background admission")
	client.waitForResponse("textDocument/hover", hover)

	close(releaseBackground)
	for _, response := range background {
		client.waitForResponse("textDocument/references", response)
	}
}

func TestServeInteractiveBurstDoesNotReturnOverload(t *testing.T) {
	const burstSize = requestAdmissionCapacity * 2
	methods := []string{
		"textDocument/hover",
		"textDocument/completion",
		"textDocument/definition",
		"textDocument/signatureHelp",
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRequests := func() {
		releaseOnce.Do(func() { close(release) })
	}
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if classifyRequestWork(method, nil) != requestWorkInteractive {
				return
			}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()
	defer releaseRequests()

	type indexedResponse struct {
		index   int
		method  string
		message *rpcMessage
	}
	responses := make(chan indexedResponse, burstSize)
	for index := range burstSize {
		method := methods[index%len(methods)]
		response := client.requestAsync(method, map[string]any{
			"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
			"position":     map[string]any{"line": 0, "character": index},
		})
		go func() {
			responses <- indexedResponse{index: index, method: method, message: <-response}
		}()
	}

	select {
	case response := <-responses:
		if response.message.Error != nil && response.message.Error.Code == -32000 {
			t.Fatalf("interactive request %d (%s) returned overload before execution capacity was released", response.index+1, response.method)
		}
		t.Fatalf("interactive request %d (%s) completed before execution capacity was released: %#v", response.index+1, response.method, response.message)
	case <-time.After(100 * time.Millisecond):
	}

	releaseRequests()
	for range burstSize {
		select {
		case response := <-responses:
			if response.message.Error != nil {
				t.Fatalf("interactive request %d (%s) returned error: %#v", response.index+1, response.method, response.message.Error)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("interactive request burst did not drain")
		}
	}
}

func TestServeNotificationBacklogDoesNotBlockCancellation(t *testing.T) {
	releaseRequest := make(chan struct{})
	requestStarted := make(chan struct{})
	var requestOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			requestOnce.Do(func() { close(requestStarted) })
			select {
			case <-releaseRequest:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	slow := client.requestAsync("workspace/symbol", map[string]any{"query": "backlog"})
	waitForConcurrencySignal(t, requestStarted, "blocking request did not start")

	// The notification worker waits for the active request. The transport must
	// continue reading after its notification queue reaches its old capacity.
	for index := 0; index < requestAdmissionCapacity+lifecycleRequestAdmissionCapacity; index++ {
		if err := client.notify("aspLsp/test/noop", map[string]any{"index": index}); err != nil {
			t.Fatal(err)
		}
	}
	hover := client.requestAsync("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	select {
	case response := <-hover:
		if response.Error != nil && response.Error.Code == -32000 {
			t.Fatalf("admitted request was rejected by the notification backlog: %#v", response.Error)
		}
		t.Fatalf("request crossed the blocking notification before cancellation: %#v", response)
	case <-time.After(100 * time.Millisecond):
	}

	overflowNotificationWritten := make(chan error, 1)
	go func() {
		overflowNotificationWritten <- client.notify("aspLsp/test/noop", map[string]any{"index": "overflow"})
	}()
	select {
	case err := <-overflowNotificationWritten:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("overflow notification did not reach the server")
	}

	startedAt := time.Now()
	cancelWritten := make(chan error, 1)
	go func() {
		cancelWritten <- client.notify("$/cancelRequest", map[string]any{"id": 1})
	}()
	cancellationBlocked := false
	select {
	case err := <-cancelWritten:
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
			t.Fatalf("cancellation notification write took %v with a full notification backlog", elapsed)
		}
	case <-time.After(time.Second):
		cancellationBlocked = true
	}

	if cancellationBlocked {
		close(releaseRequest)
		select {
		case <-slow:
		case <-time.After(time.Second):
		}
		t.Fatal("cancellation notification was blocked by the notification backlog")
	}
	select {
	case response := <-slow:
		if response.Error == nil || response.Error.Code != -32800 {
			t.Fatalf("cancelled request error = %#v, want request cancelled", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled request did not return promptly")
	}
	close(releaseRequest)
	client.waitForResponse("textDocument/hover", hover)
}

func TestServeNotificationOverflowCleansQueuedRequests(t *testing.T) {
	requestStarted := make(chan struct{})
	var requestOnce sync.Once
	var configured *Server
	client := startStdioTestClientWithServer(t, func(server *Server) {
		configured = server
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			requestOnce.Do(func() { close(requestStarted) })
			<-ctx.Done()
		}
	})
	defer func() {
		_ = client.serverInput.Close()
		_ = client.serverOutput.Close()
	}()

	slow := client.requestAsync("workspace/symbol", map[string]any{"query": "overflow"})
	waitForConcurrencySignal(t, requestStarted, "blocking request did not start")
	if err := client.notify("aspLsp/test/noop", nil); err != nil {
		t.Fatal(err)
	}
	queued := client.requestAsync("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
		"position":     map[string]any{"line": 0, "character": 0},
	})

	for index := 0; index < notificationQueueCapacity+2; index++ {
		if err := client.notify("aspLsp/test/noop", map[string]any{"index": index}); err != nil {
			break
		}
	}
	for name, response := range map[string]<-chan *rpcMessage{
		"active": slow,
		"queued": queued,
	} {
		select {
		case message := <-response:
			if message.Error == nil || message.Error.Code != -32800 {
				t.Fatalf("%s request error = %#v, want request cancelled", name, message.Error)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s request was not completed during notification overflow cleanup", name)
		}
	}
	select {
	case err := <-client.done:
		if err == nil || !strings.Contains(err.Error(), "notification queue capacity exceeded") {
			t.Fatalf("Serve error = %v, want notification queue capacity error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after notification queue overflow")
	}
	configured.mu.Lock()
	remainingCancellations := len(configured.requestCancellations)
	configured.mu.Unlock()
	if remainingCancellations != 0 {
		t.Fatalf("request cancellation registrations after overflow = %d, want 0", remainingCancellations)
	}
}

func TestServeRejectsExcessRequestsWithoutBlockingCancellation(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, requestAdmissionCapacity)
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	responses := make([]<-chan *rpcMessage, requestAdmissionCapacity*2)
	for index := range responses {
		responses[index] = client.requestAsync("workspace/symbol", map[string]any{
			"query": "saturation",
		})
	}
	waitForConcurrencySignal(t, started, "saturated request did not start")

	select {
	case response := <-responses[requestAdmissionCapacity]:
		if response.Error == nil || response.Error.Code != -32000 {
			t.Fatalf("excess request error = %#v, want server overload", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("excess request did not receive a nonblocking overload response")
	}

	startedAt := time.Now()
	cancelWritten := make(chan error, 1)
	go func() {
		cancelWritten <- client.notify("$/cancelRequest", map[string]any{"id": 1})
	}()
	select {
	case err := <-cancelWritten:
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
			t.Fatalf("cancellation notification write took %v while requests were saturated", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation notification was blocked by saturated requests")
	}
	select {
	case response := <-responses[0]:
		if response.Error == nil || response.Error.Code != -32800 {
			t.Fatalf("cancelled saturated request error = %#v, want request cancelled", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach the active request while saturated")
	}

	close(release)
	for index, response := range responses {
		if index == 0 || index == requestAdmissionCapacity {
			continue
		}
		select {
		case <-response:
		case <-time.After(2 * time.Second):
			t.Fatalf("request %d did not complete after releasing saturation", index+1)
		}
	}
}

func TestServeAdmitsShutdownWhenNormalRequestsAreSaturated(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, requestAdmissionCapacity)
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	normalResponses := make([]<-chan *rpcMessage, requestAdmissionCapacity)
	for index := range normalResponses {
		normalResponses[index] = client.requestAsync("workspace/symbol", map[string]any{
			"query": "saturation",
		})
	}
	waitForConcurrencySignal(t, started, "normal request did not start during saturation")
	overflow := client.requestAsync("workspace/symbol", map[string]any{
		"query": "overflow",
	})
	select {
	case response := <-overflow:
		if response.Error == nil || response.Error.Code != -32000 {
			t.Fatalf("normal overflow response = %#v, want server overload", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("normal overflow request did not receive a nonblocking overload response")
	}

	shutdown := client.requestAsync("shutdown", nil)
	secondShutdown := client.requestAsync("shutdown", nil)
	select {
	case response := <-secondShutdown:
		if response.Error == nil || response.Error.Code != -32000 {
			t.Fatalf("second shutdown response = %#v, want bounded lifecycle overload", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("second shutdown did not receive a bounded overload response")
	}
	select {
	case response := <-shutdown:
		if response.Error != nil && response.Error.Code == -32000 {
			t.Fatalf("shutdown was rejected while normal admission was saturated: %#v", response.Error)
		}
		t.Fatalf("shutdown completed before active requests were released: %#v", response)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	for _, response := range normalResponses {
		client.waitForResponse("workspace/symbol", response)
	}
	client.waitForResponse("shutdown", shutdown)
}

func TestServePublishesLifecycleRevisionWithoutWaitingForOlderRequest(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	observedDocument := make(chan bool, 1)
	var startedOnce sync.Once
	uri := "file:///concurrency/order.asp"
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method == "codeAction/resolve" {
				observedDocument <- server.documentByURI(uri) != nil
				return
			}
			if method != "workspace/symbol" {
				return
			}
			startedOnce.Do(func() { close(slowStarted) })
			select {
			case <-releaseSlow:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	slow := client.requestAsync("workspace/symbol", map[string]any{"query": "slow"})
	waitForConcurrencySignal(t, slowStarted, "slow request did not start")
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 1,
			"text": "<% Dim OrderedValue %>",
		},
	}); err != nil {
		t.Fatal(err)
	}
	afterNotification := client.requestAsync("codeAction/resolve", map[string]any{"title": "after notification"})
	select {
	case response := <-afterNotification:
		if response.Error != nil {
			t.Fatalf("request after lifecycle notification failed: %#v", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle notification waited for an obsolete slow request")
	}
	if !<-observedDocument {
		t.Fatal("request after didOpen did not observe the opened immutable revision")
	}

	select {
	case response := <-slow:
		if response.Error == nil || response.Error.Code != -32800 {
			t.Fatalf("obsolete request error = %#v, want code -32800", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle notification did not cancel the obsolete request")
	}
	close(releaseSlow)
}

func TestServeConfigurationChangeCancelsOlderRequestWithoutBlockingInteractiveWork(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	observedSetting := make(chan bool, 1)
	var startedOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			switch method {
			case "workspace/symbol":
				startedOnce.Do(func() { close(slowStarted) })
				select {
				case <-releaseSlow:
				case <-ctx.Done():
				}
			case "codeAction/resolve":
				server.mu.Lock()
				showCallLinks := server.settings.GraphShowCallLinks
				server.mu.Unlock()
				observedSetting <- showCallLinks
			}
		}
	})
	defer client.close()
	defer close(releaseSlow)

	slow := client.requestAsync("workspace/symbol", map[string]any{"query": "obsolete"})
	waitForConcurrencySignal(t, slowStarted, "slow request did not start")
	if err := client.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{"aspLsp": map[string]any{
			"graph": map[string]any{"showCallLinks": false},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	afterConfiguration := client.requestAsync("codeAction/resolve", map[string]any{"title": "after configuration"})
	select {
	case response := <-afterConfiguration:
		if response.Error != nil {
			t.Fatalf("request after configuration failed: %#v", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("configuration notification waited for an obsolete slow request")
	}
	if showCallLinks := <-observedSetting; showCallLinks {
		t.Fatal("request after configuration observed stale graph settings")
	}

	select {
	case response := <-slow:
		if response.Error == nil || response.Error.Code != -32800 {
			t.Fatalf("obsolete request error = %#v, want code -32800", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("configuration change did not cancel the obsolete request")
	}
}

func TestServeConfigurationChangeCancelsBackgroundCacheCommandWithoutBlockingHover(t *testing.T) {
	commandStarted := make(chan struct{})
	observedSetting := make(chan bool, 1)
	var startedOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			switch method {
			case "workspace/executeCommand":
				startedOnce.Do(func() { close(commandStarted) })
				<-ctx.Done()
			case "textDocument/hover":
				server.mu.Lock()
				showCallLinks := server.settings.GraphShowCallLinks
				server.mu.Unlock()
				observedSetting <- showCallLinks
			}
		}
	})
	defer client.close()

	command := client.requestAsync("workspace/executeCommand", map[string]any{
		"command": "aspLsp.server.clearDiskCache",
	})
	waitForConcurrencySignal(t, commandStarted, "background cache command did not start")
	if err := client.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{"aspLsp": map[string]any{
			"graph": map[string]any{"showCallLinks": false},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	hover := client.requestAsync("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	client.waitForResponse("textDocument/hover", hover)
	if showCallLinks := <-observedSetting; showCallLinks {
		t.Fatal("hover after cache command cancellation observed stale graph settings")
	}
	select {
	case response := <-command:
		if response.Error == nil || response.Error.Code != -32800 {
			t.Fatalf("cancelled cache command error = %#v, want code -32800", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("revision notification did not cancel the background cache command")
	}
}

func TestServeMalformedRevisionNotificationDoesNotCancelOlderRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var startedOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			startedOnce.Do(func() { close(requestStarted) })
			select {
			case <-releaseRequest:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	slow := client.requestAsync("workspace/symbol", map[string]any{"query": "valid"})
	waitForConcurrencySignal(t, requestStarted, "older request did not start")
	if err := client.notify("workspace/didChangeConfiguration", []any{}); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-slow:
		t.Fatalf("malformed revision notification completed the older request: %#v", response)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseRequest)
	response := client.waitForResponse("workspace/symbol", slow)
	if response.Error != nil {
		t.Fatalf("malformed revision notification cancelled the older request: %#v", response.Error)
	}
}

func TestServeMalformedTypedRevisionNotificationDoesNotCancelOlderRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var startedOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			startedOnce.Do(func() { close(requestStarted) })
			select {
			case <-releaseRequest:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	slow := client.requestAsync("workspace/symbol", map[string]any{"query": "typed"})
	waitForConcurrencySignal(t, requestStarted, "older request did not start")
	if err := client.notify("workspace/didChangeWatchedFiles", map[string]any{
		"changes": "invalid",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-slow:
		t.Fatalf("malformed typed revision notification completed the older request: %#v", response)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseRequest)
	response := client.waitForResponse("workspace/symbol", slow)
	if response.Error != nil {
		t.Fatalf("malformed typed revision notification cancelled the older request: %#v", response.Error)
	}
}

func TestServeRevisionNotificationMissingRequiredFieldsDoesNotCancelOlderRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var startedOnce sync.Once
	var configured *Server
	client := startStdioTestClientWithServer(t, func(server *Server) {
		configured = server
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			startedOnce.Do(func() { close(requestStarted) })
			select {
			case <-releaseRequest:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	slow := client.requestAsync("workspace/symbol", map[string]any{"query": "required fields"})
	waitForConcurrencySignal(t, requestStarted, "older request did not start")
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-slow:
		t.Fatalf("revision notification missing required fields completed the older request: %#v", response)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseRequest)
	response := client.waitForResponse("workspace/symbol", slow)
	if response.Error != nil {
		t.Fatalf("revision notification missing required fields cancelled the older request: %#v", response.Error)
	}
	configured.mu.Lock()
	_, openedEmptyURI := configured.documents[""]
	configured.mu.Unlock()
	if openedEmptyURI {
		t.Fatal("revision notification missing required fields opened an empty document URI")
	}
}

func TestServeLineCommentEditsBypassesBulkWorkspaceRequest(t *testing.T) {
	bulkStarted := make(chan struct{})
	releaseBulk := make(chan struct{})
	lineCommentStarted := make(chan struct{})
	var bulkOnce sync.Once
	var lineCommentOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			switch method {
			case "workspace/willRenameFiles":
				bulkOnce.Do(func() { close(bulkStarted) })
				select {
				case <-releaseBulk:
				case <-ctx.Done():
				}
			case "aspLsp/textDocument/lineCommentEdits":
				lineCommentOnce.Do(func() { close(lineCommentStarted) })
			}
		}
	})
	defer client.close()

	bulk := client.requestAsync("workspace/willRenameFiles", map[string]any{"files": []any{}})
	waitForConcurrencySignal(t, bulkStarted, "bulk workspace request did not start")
	lineComment := client.requestAsync("aspLsp/textDocument/lineCommentEdits", map[string]any{
		"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp", "version": 1},
		"selections":   []any{},
	})
	waitForConcurrencySignal(t, lineCommentStarted, "line-comment request waited behind bulk work")
	client.waitForResponse("aspLsp/textDocument/lineCommentEdits", lineComment)
	close(releaseBulk)
	client.waitForResponse("workspace/willRenameFiles", bulk)
}

func TestServeWorkspaceConfigurationRequestHasBoundedWait(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_WORKSPACE_CONFIGURATION_TIMEOUT_MS", "25")
	client := startStdioTestClient(t)
	defer client.close()
	client.request("initialize", map[string]any{
		"processId": nil,
		"rootUri":   nil,
		"capabilities": map[string]any{
			"workspace": map[string]any{"configuration": true},
		},
	})
	if err := client.notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	client.waitForServerRequest("workspace/configuration")
	hover := client.requestAsync("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	select {
	case response := <-hover:
		if response.Error != nil {
			t.Fatalf("hover after workspace configuration timeout failed: %#v", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("unanswered workspace/configuration blocked later interactive work")
	}
}

func TestServeCancelsOneConcurrentRequestWithoutCancellingAnother(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	var firstOnce sync.Once
	var secondOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			switch method {
			case "workspace/symbol":
				firstOnce.Do(func() { close(firstStarted) })
				<-ctx.Done()
			case "textDocument/hover":
				secondOnce.Do(func() { close(secondStarted) })
				select {
				case <-releaseSecond:
				case <-ctx.Done():
				}
			}
		}
	})
	defer client.close()

	first := client.requestAsync("workspace/symbol", map[string]any{"query": "cancel me"})
	second := client.requestAsync("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///concurrency/missing.asp"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	waitForConcurrencySignal(t, firstStarted, "first concurrent request did not start")
	waitForConcurrencySignal(t, secondStarted, "second concurrent request did not start")
	if err := client.notify("$/cancelRequest", map[string]any{"id": 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-first:
		if response.Error == nil || response.Error.Code != -32800 {
			t.Fatalf("cancelled request error = %#v, want code -32800", response.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled concurrent request did not return promptly")
	}
	select {
	case response := <-second:
		t.Fatalf("cancelling one request completed another request: %#v", response)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseSecond)
	client.waitForResponse("textDocument/hover", second)
}

func TestServeStopsSlowRequestsOnExitAndEOF(t *testing.T) {
	for _, test := range []struct {
		name string
		stop func(*stdioTestClient)
	}{
		{name: "exit", stop: func(client *stdioTestClient) { client.close() }},
		{name: "EOF", stop: stopStdioTestClientByEOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			var startedOnce sync.Once
			client := startStdioTestClientWithServer(t, func(server *Server) {
				server.requestDispatchTestHook = func(ctx context.Context, method string) {
					if method == "workspace/symbol" {
						startedOnce.Do(func() { close(started) })
						<-ctx.Done()
					}
				}
			})
			client.requestAsync("workspace/symbol", map[string]any{"query": "slow"})
			waitForConcurrencySignal(t, started, "slow request did not start")
			test.stop(client)
		})
	}
}

func stopStdioTestClientByEOF(client *stdioTestClient) {
	client.t.Helper()
	_ = client.serverInput.Close()
	select {
	case <-client.done:
	case <-time.After(time.Second):
		client.t.Fatal("stdio server did not stop after EOF")
	}
	_ = client.serverOutput.Close()
}

func waitForConcurrencySignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func TestServePipelinedLifecycleRequestsAreNotRejectedOrCancelled(t *testing.T) {
	releaseInitialize := make(chan struct{})
	initializeStarted := make(chan struct{})
	var initializeOnce sync.Once
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "initialize" {
				return
			}
			initializeOnce.Do(func() { close(initializeStarted) })
			select {
			case <-releaseInitialize:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	initialize := client.requestAsync("initialize", map[string]any{"capabilities": map[string]any{}})
	waitForConcurrencySignal(t, initializeStarted, "initialize did not start")
	if err := client.notify("initialized", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	// A revision-advancing notification must not cancel the in-flight handshake.
	if err := client.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": "file:///concurrency/pipelined.asp", "languageId": "classic-asp", "version": 1, "text": "<% Dim value %>",
	}}); err != nil {
		t.Fatal(err)
	}
	shutdown := client.requestAsync("shutdown", nil)
	select {
	case response := <-shutdown:
		t.Fatalf("shutdown answered before the pipelined initialize completed: %#v", response)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseInitialize)

	if response := client.waitForResponse("initialize", initialize); response.Result == nil {
		t.Fatalf("initialize returned no result: %#v", response)
	}
	client.waitForResponse("shutdown", shutdown)
}
