package lspserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRPCWriteFatalIsStickyAndRejectsConcurrentWriters(t *testing.T) {
	writeErr := errors.New("connection reset")
	writer := &fatalTransportWriter{err: writeErr}
	server := New(strings.NewReader(""), writer, io.Discard)

	first := server.writeRPCMessage(rpcMessage{Method: "fatal/first", Params: mustRaw(map[string]any{})})
	var firstFatal *rpcTransportFatalError
	if !errors.As(first, &firstFatal) {
		t.Fatalf("first write error = %v, want fatal transport error", first)
	}

	const concurrentWriters = 16
	errorsSeen := make(chan error, concurrentWriters)
	var group sync.WaitGroup
	group.Add(concurrentWriters)
	for index := 0; index < concurrentWriters; index++ {
		go func(index int) {
			defer group.Done()
			errorsSeen <- server.writeRPCMessage(rpcMessage{Method: "fatal/later", Params: mustRaw(index)})
		}(index)
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		var fatal *rpcTransportFatalError
		if !errors.As(err, &fatal) {
			t.Fatalf("later write error = %v, want fatal transport error", err)
		}
		if fatal != firstFatal {
			t.Fatalf("later fatal pointer = %p, want first fatal %p", fatal, firstFatal)
		}
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls after sticky fatal = %d, want one", writer.calls)
	}
}

func TestServeStopsOnAsyncFatalWhileInputIsIdle(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	defer inputReader.Close()
	writer := &fatalTransportWriter{err: errors.New("async connection reset")}
	server := New(inputReader, writer, io.Discard)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(context.Background())
	}()

	var notificationFatal *rpcTransportFatalError
	select {
	case <-time.After(10 * time.Millisecond):
	case <-serveDone:
		t.Fatal("Serve stopped before the idle-input fatal write")
	}
	writeErr := server.sendNotification("window/logMessage", map[string]any{"message": "fatal"})
	if !errors.As(writeErr, &notificationFatal) {
		t.Fatalf("notification write error = %v, want fatal transport error", writeErr)
	}

	select {
	case err := <-serveDone:
		if err != notificationFatal {
			t.Fatalf("Serve error = %v, want original fatal %v", err, notificationFatal)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after an asynchronous fatal write")
	}
}

func TestServeRejectsNonClosableBlockingInputBeforeRead(t *testing.T) {
	reader := &blockingNonClosableReader{}
	server := New(reader, &fatalTransportWriter{err: errors.New("connection reset")}, io.Discard)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(context.Background())
	}()

	select {
	case err := <-serveDone:
		if !errors.Is(err, errServerInputNotInterruptible) {
			t.Fatalf("Serve error = %v, want unsupported input error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve blocked on a non-closable input reader")
	}
	if reader.reads != 0 {
		t.Fatalf("unsupported input was read %d times, want no read goroutine", reader.reads)
	}
	var fatal *rpcTransportFatalError
	if err := server.sendNotification("window/logMessage", map[string]any{"message": "after rejection"}); !errors.As(err, &fatal) {
		t.Fatalf("post-rejection notification error = %v, want sticky fatal transport error", err)
	}
}

func TestServeAcceptsFiniteNonClosableStringReader(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"exit"}`
	input := strings.NewReader("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body)
	server := New(input, io.Discard, io.Discard)
	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("Serve finite non-closable reader = %v", err)
	}
}

func TestServeCancellationStopsIdlePipeAndReturnsContextError(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	defer inputReader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := New(inputReader, io.Discard, io.Discard)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(ctx)
	}()

	select {
	case <-time.After(10 * time.Millisecond):
	case err := <-serveDone:
		t.Fatalf("Serve stopped before cancellation: %v", err)
	}
	cancel()
	select {
	case err := <-serveDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after parent cancellation")
	}
}

func TestServeFatalWinsCancellationRace(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	defer inputReader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := newBlockingFatalTransportWriter(errors.New("fatal race connection reset"))
	server := New(inputReader, writer, io.Discard)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(ctx)
	}()
	select {
	case <-time.After(10 * time.Millisecond):
	case err := <-serveDone:
		t.Fatalf("Serve stopped before the fatal/cancel race: %v", err)
	}

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- server.sendNotification("window/logMessage", map[string]any{"message": "fatal race"})
	}()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("fatal writer was not entered")
	}
	cancel()
	close(writer.release)
	writeErr := <-writeDone
	var fatal *rpcTransportFatalError
	if !errors.As(writeErr, &fatal) {
		t.Fatalf("fatal race write error = %v, want fatal transport error", writeErr)
	}
	select {
	case serveErr := <-serveDone:
		if serveErr != fatal {
			t.Fatalf("fatal/cancel Serve error = %v, want original fatal %v", serveErr, fatal)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after fatal/cancel race")
	}
}

func TestServeCancellationClosesInputOnce(t *testing.T) {
	reader := newCountingClosableReader()
	ctx, cancel := context.WithCancel(context.Background())
	server := New(reader, io.Discard, io.Discard)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(ctx)
	}()
	select {
	case <-reader.readStarted:
	case err := <-serveDone:
		t.Fatalf("Serve stopped before reading idle input: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Serve did not start the idle input read")
	}
	cancel()
	select {
	case err := <-serveDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve repeated-close error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop on counting reader cancellation")
	}
	if got := reader.closeCount.Load(); got != 1 {
		t.Fatalf("input Close count = %d, want one", got)
	}
}

type fatalTransportWriter struct {
	mu     sync.Mutex
	output bytes.Buffer
	err    error
	calls  int
}

type blockingNonClosableReader struct {
	reads int
}

func (r *blockingNonClosableReader) Read([]byte) (int, error) {
	r.reads++
	select {}
}

type blockingFatalTransportWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	err     error
}

func newBlockingFatalTransportWriter(err error) *blockingFatalTransportWriter {
	return &blockingFatalTransportWriter{entered: make(chan struct{}), release: make(chan struct{}), err: err}
}

func (w *blockingFatalTransportWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	n := len(data) / 2
	if n == 0 && len(data) > 0 {
		n = 1
	}
	return n, w.err
}

type countingClosableReader struct {
	closed      chan struct{}
	readStarted chan struct{}
	readOnce    sync.Once
	closeOnce   sync.Once
	closeCount  atomic.Int32
}

func newCountingClosableReader() *countingClosableReader {
	return &countingClosableReader{closed: make(chan struct{}), readStarted: make(chan struct{})}
}

func (r *countingClosableReader) Read([]byte) (int, error) {
	r.readOnce.Do(func() { close(r.readStarted) })
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *countingClosableReader) Close() error {
	r.closeCount.Add(1)
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func (w *fatalTransportWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.calls > 1 {
		return 0, errors.New("writer used after fatal transport error")
	}
	n := len(data) / 2
	if n == 0 && len(data) > 0 {
		n = 1
	}
	_, _ = w.output.Write(data[:n])
	return n, w.err
}
