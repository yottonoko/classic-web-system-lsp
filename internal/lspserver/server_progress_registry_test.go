package lspserver

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDocumentProgressTaskPublishesReferenceOwnerIdentity(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/default.asp"
	taskID, _ := server.beginDocumentProgressTask("references.batch", "analyzing", "references", "references.workspace", "default.asp", uri, 7, true, 3, false)
	server.mu.Lock()
	status := server.progressTasksStatusLocked("references")
	server.mu.Unlock()
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"id":"`+taskID+`"`) || !strings.Contains(text, `"documentUri":"`+uri+`"`) || !strings.Contains(text, `"documentVersion":7`) {
		t.Fatalf("reference progress owner metadata is incomplete: %s", text)
	}
	server.finishProgressTask(taskID, "references", "completed")
}

func TestReferenceProgressPublishesIncrementBeforeCompletion(t *testing.T) {
	var output synchronizedBuffer
	server := New(nil, &output, io.Discard)
	uri := "file:///workspace/default.asp"
	taskID, _ := server.beginDocumentProgressTask("references.batch", "analyzing", "references", "references.workspace", "default.asp", uri, 7, true, 96, false)
	done := server.updateProgressTaskImmediate(taskID, "references", "references.workspace", "include.inc", 32, 96, []string{"include.inc"}, "running")
	if done != nil {
		<-done
	}
	server.finishProgressTask(taskID, "references", "completed")
	server.progressPublishWG.Wait()
	server.stopProgressPublisher()

	encoded := output.String()
	if !strings.Contains(encoded, `"label":"references.workspace"`) || !strings.Contains(encoded, `"current":32,"total":96`) {
		t.Fatalf("incremental reference progress was not published: %s", encoded)
	}
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

type blockingStatusWriter struct {
	output  synchronizedBuffer
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newBlockingStatusWriter() *blockingStatusWriter {
	return &blockingStatusWriter{entered: make(chan struct{}), release: make(chan struct{})}
}

func (w *blockingStatusWriter) Write(data []byte) (int, error) {
	blocked := false
	w.once.Do(func() {
		blocked = true
		close(w.entered)
	})
	if blocked {
		<-w.release
	}
	return w.output.Write(data)
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func TestProgressTaskKeepsStableStartAndMonotonicPhaseProgress(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	taskID, startedAt := server.beginProgressTask("test", "analyzing", "test", "test.scan", "first.asp", 5, true)
	server.updateProgressTask(taskID, "test", "test.scan", "second.asp", 2, 5, []string{"second.asp"}, "running")
	server.updateProgressTask(taskID, "test", "test.scan", "first.asp", 1, 5, []string{"first.asp"}, "running")

	server.mu.Lock()
	task := *server.progressTasks[taskID]
	server.mu.Unlock()
	if task.StartedAt != startedAt {
		t.Fatalf("progress start changed from %d to %d", startedAt, task.StartedAt)
	}
	if task.Current != 2 || task.Total != 5 {
		t.Fatalf("progress regressed to %d/%d, want 2/5", task.Current, task.Total)
	}
	if task.Detail != "first.asp" || len(task.ActiveItems) != 1 || task.ActiveItems[0] != "first.asp" {
		t.Fatalf("progress detail was not refreshed: %#v", task)
	}

	server.updateProgressTask(taskID, "test", "test.write", "analysis database", 0, 2, nil, "running")
	server.mu.Lock()
	task = *server.progressTasks[taskID]
	server.mu.Unlock()
	if task.Label != "test.write" || task.Current != 0 || task.Total != 2 {
		t.Fatalf("new progress phase did not reset honestly: %#v", task)
	}

	server.finishProgressTask(taskID, "test", "completed")
	server.mu.Lock()
	_, exists := server.progressTasks[taskID]
	server.mu.Unlock()
	if exists {
		t.Fatal("completed progress task remained registered")
	}
}

func TestProgressTaskClampsCurrentToTotal(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	taskID, _ := server.beginProgressTask("test", "analyzing", "test", "test.files", "", 10, false)
	server.updateProgressTask(taskID, "test", "test.files", "file.asp", 12, 10, nil, "running")
	server.mu.Lock()
	task := *server.progressTasks[taskID]
	server.mu.Unlock()
	if task.Current != 10 || task.Total != 10 {
		t.Fatalf("over-complete progress = %d/%d, want 10/10", task.Current, task.Total)
	}
}

func TestProgressAggregateRemainsIndeterminateWhileAnyTaskHasUnknownTotal(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.beginProgressTask("unknown", "loading", "test", "test.scan", "", 0, false)
	knownID, _ := server.beginProgressTask("known", "analyzing", "test", "test.files", "", 2, false)
	server.updateProgressTask(knownID, "test", "test.files", "file.asp", 1, 2, nil, "running")
	server.mu.Lock()
	status := server.progressTasksStatusLocked("test")
	server.mu.Unlock()
	progress, ok := status["progress"].(map[string]int)
	if !ok {
		t.Fatalf("aggregate progress type = %T", status["progress"])
	}
	if progress["current"] != 0 || progress["total"] != 0 {
		t.Fatalf("mixed known/unknown aggregate = %#v, want indeterminate 0/0", progress)
	}
}

func TestProgressTaskCompletionPublishesOnlyFinalRegistryState(t *testing.T) {
	var output synchronizedBuffer
	server := New(nil, &output, io.Discard)
	taskID, _ := server.beginProgressTask("test", "analyzing", "test", "test.files", "file.asp", 1, false)
	server.finishProgressTask(taskID, "test", "completed")
	server.progressPublishWG.Wait()
	server.stopProgressPublisher()

	encoded := output.String()
	if strings.Contains(encoded, `"state":"completed"`) {
		t.Fatalf("completion published a transient 100%% task before idle: %s", encoded)
	}
	if lastIdle := strings.LastIndex(encoded, `"status":"idle"`); lastIdle < strings.LastIndex(encoded, `"status":"analyzing"`) {
		t.Fatalf("final registry state was not idle: %s", encoded)
	}
}

func TestProgressTasksSupportConcurrentLifecycle(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	const taskCount = 32
	var group sync.WaitGroup
	group.Add(taskCount)
	for index := 0; index < taskCount; index++ {
		go func() {
			defer group.Done()
			taskID, _ := server.beginProgressTask("concurrent", "loading", "test", "test.files", "", 3, false)
			for current := 1; current <= 3; current++ {
				server.updateProgressTask(taskID, "test", "test.files", "file.asp", current, 3, []string{"file.asp"}, "running")
			}
			server.finishProgressTask(taskID, "test", "completed")
		}()
	}
	group.Wait()

	server.mu.Lock()
	remaining := len(server.progressTasks)
	server.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("progress task registry retained %d completed tasks", remaining)
	}
}

func TestProgressTaskUpdatesCoalesceStatusNotifications(t *testing.T) {
	var output synchronizedBuffer
	server := New(nil, &output, io.Discard)
	taskID, _ := server.beginProgressTask("test", "analyzing", "test", "test.files", "", 1_000, false)
	for current := 1; current <= 1_000; current++ {
		server.updateProgressTask(taskID, "test", "test.files", "file.asp", current, 1_000, nil, "running")
	}
	server.finishProgressTask(taskID, "test", "completed")
	server.progressPublishWG.Wait()
	server.stopProgressPublisher()

	count := strings.Count(output.String(), `"method":"aspLsp/status"`)
	if count > 3 {
		t.Fatalf("status notifications = %d, want at most 3 for 1000 same-phase updates", count)
	}
}

func TestProgressPublisherReplacesPendingStatusBehindSlowWrite(t *testing.T) {
	output := newBlockingStatusWriter()
	server := New(nil, output, io.Discard)
	taskID, _ := server.beginProgressTask("test", "analyzing", "test", "test.files", "", 1_000, false)
	select {
	case <-output.entered:
	case <-time.After(time.Second):
		t.Fatal("initial progress status did not reach the writer")
	}
	updatesDone := make(chan struct{})
	finishDone := make(chan struct{})
	go func() {
		for current := 1; current <= 1_000; current++ {
			server.updateProgressTask(taskID, "test", "test.files", "file.asp", current, 1_000, nil, "running")
		}
		close(updatesDone)
		server.finishProgressTask(taskID, "test", "completed")
		close(finishDone)
	}()
	select {
	case <-updatesDone:
	case <-time.After(time.Second):
		t.Fatal("progress updates blocked behind a slow status write")
	}
	close(output.release)
	select {
	case <-finishDone:
	case <-time.After(time.Second):
		t.Fatal("final idle status did not flush after the writer resumed")
	}
	server.progressPublishWG.Wait()
	server.stopProgressPublisher()

	encoded := output.output.String()
	if count := strings.Count(encoded, `"method":"aspLsp/status"`); count > 2 {
		t.Fatalf("status notifications = %d, want initial and latest only", count)
	}
	if lastIdle := strings.LastIndex(encoded, `"status":"idle"`); lastIdle < strings.LastIndex(encoded, `"status":"analyzing"`) {
		t.Fatalf("stale analyzing status followed final idle status: %s", encoded)
	}
}
