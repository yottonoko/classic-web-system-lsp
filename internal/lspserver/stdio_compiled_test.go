package lspserver

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func TestCompiledServerStdioSmoke(t *testing.T) {
	root := repositoryRoot(t)
	binary := filepath.Join(t.TempDir(), "asp-lsp-go")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -buildid=", "-o", binary, "./cmd/asp-lsp-go")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build compiled server: %v\n%s", err, output)
	}

	command := exec.Command(binary, "--stdio")
	var stderr lockedBuffer
	command.Stderr = &stderr
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("open compiled server stdin: %v", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("open compiled server stdout: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start compiled server: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	finished := false
	t.Cleanup(func() {
		if finished {
			return
		}
		select {
		case <-waitDone:
			return
		default:
		}
		_ = command.Process.Kill()
		select {
		case <-waitDone:
		case <-time.After(time.Second):
			t.Errorf("compiled server did not stop during cleanup; stderr=%s", stderr.String())
		}
	})

	reader := bufio.NewReader(output)
	type readResult struct {
		message *rpcMessage
		err     error
	}
	messages := make(chan readResult)
	go func() {
		for {
			message, err := readMessage(reader)
			messages <- readResult{message: message, err: err}
			if err != nil {
				return
			}
		}
	}()
	write := func(message rpcMessage) {
		t.Helper()
		if err := writeMessage(input, message); err != nil {
			t.Fatalf("write compiled server message %q: %v", message.Method, err)
		}
	}
	read := func(id int, method string) *rpcMessage {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			var result readResult
			select {
			case result = <-messages:
			case <-deadline:
				t.Fatalf("timed out waiting for compiled server %s response; stderr=%s", method, stderr.String())
				return nil
			}
			if result.err != nil {
				t.Fatalf("read compiled server %s response: %v; stderr=%s", method, result.err, stderr.String())
				return nil
			}
			if result.message.ID == nil || result.message.Method != "" || fmt.Sprint(result.message.ID) != fmt.Sprint(id) {
				continue
			}
			return result.message
		}
	}

	write(rpcMessage{
		ID:     1,
		Method: "initialize",
		Params: mustRaw(map[string]any{"processId": nil, "capabilities": map[string]any{}}),
	})
	initialize := read(1, "initialize")
	if initialize.ID == nil || initialize.Result == nil || initialize.Error != nil {
		t.Fatalf("compiled server initialize response = %#v", initialize)
	}

	write(rpcMessage{Method: "initialized", Params: mustRaw(map[string]any{})})
	write(rpcMessage{ID: 2, Method: "shutdown", Params: mustRaw(map[string]any{})})
	shutdown := read(2, "shutdown")
	if shutdown.ID == nil || shutdown.Error != nil {
		t.Fatalf("compiled server shutdown response = %#v", shutdown)
	}
	write(rpcMessage{Method: "exit", Params: mustRaw(map[string]any{})})
	select {
	case err := <-waitDone:
		finished = true
		if err != nil {
			t.Fatalf("compiled server exit: %v; stderr=%s", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("compiled server did not exit; stderr=%s", stderr.String())
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
