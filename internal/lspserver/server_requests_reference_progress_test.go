package lspserver

import (
	"context"
	"errors"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestWorkspaceReferencePartialLocationsStreamSingleSegmentWithCursor(t *testing.T) {
	const remainder = 17
	total := workspaceReferencePartialResultChunkSize*2049 + remainder
	locations := make([]lsp.Location, total)
	for index := range locations {
		locations[index].Range.Start.Line = index
	}

	var queue workspaceReferencePartialLocationQueue
	queue.append(locations)
	if len(queue.chunks) != 1 {
		t.Fatalf("single-segment queue chunks = %d, want one", len(queue.chunks))
	}
	expected := 0
	chunks := 0
	err := emitWorkspaceReferencePartialLocations(context.Background(), &queue, func(chunk []lsp.Location) error {
		chunks++
		if len(chunk) != workspaceReferencePartialResultChunkSize {
			t.Fatalf("partial chunk %d size = %d, want %d", chunks, len(chunk), workspaceReferencePartialResultChunkSize)
		}
		if &chunk[0] != &locations[expected] {
			t.Fatalf("partial chunk %d did not use the source cursor", chunks)
		}
		for offset, location := range chunk {
			if location.Range.Start.Line != expected+offset {
				t.Fatalf("partial location %d = %d, want %d", expected+offset, location.Range.Start.Line, expected+offset)
			}
		}
		expected += len(chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if chunks != 2049 || expected != total-remainder {
		t.Fatalf("streamed chunks/locations = %d/%d, want 2049/%d", chunks, expected, total-remainder)
	}
	if queue.len() != remainder || len(queue.chunks) != 1 {
		t.Fatalf("queue after full chunks = len %d, chunks %d; want %d, 1", queue.len(), len(queue.chunks), remainder)
	}

	tail := queue.take(queue.len())
	if len(tail) != remainder || &tail[0] != &locations[expected] {
		t.Fatalf("tail = len %d/source match %t, want %d/true", len(tail), len(tail) > 0 && &tail[0] == &locations[expected], remainder)
	}
	for offset, location := range tail {
		if location.Range.Start.Line != expected+offset {
			t.Fatalf("tail location %d = %d, want %d", expected+offset, location.Range.Start.Line, expected+offset)
		}
	}
	expected += len(tail)
	if expected != total || queue.len() != 0 || len(queue.chunks) != 0 {
		t.Fatalf("queue final state = expected %d/%d, len %d, chunks %d", expected, total, queue.len(), len(queue.chunks))
	}
}

func TestWorkspaceReferencePartialLocationsStreamAcrossSegmentsInOrder(t *testing.T) {
	segmentSizes := []int{7, 511, 1, 513, 29, 1024, 3}
	var queue workspaceReferencePartialLocationQueue
	next := 0
	for _, size := range segmentSizes {
		segment := make([]lsp.Location, size)
		for index := range segment {
			segment[index].Range.Start.Line = next + index
		}
		next += size
		queue.append(segment)
	}
	total := next
	expected := 0
	chunkCount := 0
	if err := emitWorkspaceReferencePartialLocations(context.Background(), &queue, func(chunk []lsp.Location) error {
		chunkCount++
		if len(chunk) != workspaceReferencePartialResultChunkSize {
			t.Fatalf("chunk %d size = %d, want %d", chunkCount, len(chunk), workspaceReferencePartialResultChunkSize)
		}
		for offset, location := range chunk {
			if location.Range.Start.Line != expected+offset {
				t.Fatalf("chunk %d location %d = %d, want %d", chunkCount, offset, location.Range.Start.Line, expected+offset)
			}
		}
		expected += len(chunk)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if queue.len() == 0 {
		t.Fatal("stream consumed the final partial segment")
	}
	tail := queue.take(queue.len())
	for offset, location := range tail {
		if location.Range.Start.Line != expected+offset {
			t.Fatalf("tail location %d = %d, want %d", offset, location.Range.Start.Line, expected+offset)
		}
	}
	expected += len(tail)
	if expected != total || queue.len() != 0 || len(queue.chunks) != 0 {
		t.Fatalf("stream order/final state = %d/%d, len %d, chunks %d", expected, total, queue.len(), len(queue.chunks))
	}
	if chunkCount != total/workspaceReferencePartialResultChunkSize {
		t.Fatalf("full chunk count = %d, want %d", chunkCount, total/workspaceReferencePartialResultChunkSize)
	}
}

func TestWorkspaceReferencePartialLocationQueueCompactsConsumedSegmentHeaders(t *testing.T) {
	const segments = 2048
	var queue workspaceReferencePartialLocationQueue
	for index := 0; index < segments; index++ {
		queue.append(make([]lsp.Location, workspaceReferencePartialResultChunkSize+1))
		if err := emitWorkspaceReferencePartialLocations(context.Background(), &queue, func([]lsp.Location) error {
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(queue.chunks) > 65 || cap(queue.chunks) > 128 {
			t.Fatalf("queue retained %d chunks with capacity %d after segment %d", len(queue.chunks), cap(queue.chunks), index)
		}
	}
	if queue.len() != segments%workspaceReferencePartialResultChunkSize {
		t.Fatalf("queue remainder = %d, want %d", queue.len(), segments%workspaceReferencePartialResultChunkSize)
	}
	queue.reset()
}

func TestWorkspaceReferencePartialResultStateDiscardsCallbacksAfterWriteError(t *testing.T) {
	writeErr := errors.New("partial result write failed")
	notifications := 0
	state := workspaceReferencePartialResultState{
		ctx: context.Background(),
		emit: func([]lsp.Location) error {
			notifications++
			return writeErr
		},
	}
	state.receive(make([]lsp.Location, workspaceReferencePartialResultChunkSize))
	if !errors.Is(state.writeErr, writeErr) || state.pending.len() != 0 {
		t.Fatalf("state after failed callback = err %v, pending %d; want write error and empty queue", state.writeErr, state.pending.len())
	}
	for index := 0; index < 1000; index++ {
		state.receive(make([]lsp.Location, workspaceReferencePartialResultChunkSize*2))
	}
	if notifications != 1 {
		t.Fatalf("notifications after repeated failed callbacks = %d, want one", notifications)
	}
	if state.pending.len() != 0 || len(state.pending.chunks) != 0 {
		t.Fatalf("queue after repeated failed callbacks = len %d, chunks %d; want empty", state.pending.len(), len(state.pending.chunks))
	}
	if !state.received {
		t.Fatal("failed callback was not recorded as received")
	}
}

func TestWorkspaceReferencePartialResultStateDiscardsCallbacksAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifications := 0
	state := workspaceReferencePartialResultState{
		ctx: ctx,
		emit: func([]lsp.Location) error {
			notifications++
			cancel()
			return nil
		},
	}
	state.receive(make([]lsp.Location, workspaceReferencePartialResultChunkSize*2))
	if notifications != 1 {
		t.Fatalf("notifications before cancellation = %d, want one", notifications)
	}
	for index := 0; index < 1000; index++ {
		state.receive(make([]lsp.Location, workspaceReferencePartialResultChunkSize*2))
	}
	if notifications != 1 {
		t.Fatalf("notifications after repeated cancelled callbacks = %d, want one", notifications)
	}
	if state.pending.len() != 0 || len(state.pending.chunks) != 0 {
		t.Fatalf("queue after repeated cancelled callbacks = len %d, chunks %d; want empty", state.pending.len(), len(state.pending.chunks))
	}
	if !state.received {
		t.Fatal("cancelled callback was not recorded as received")
	}
}

func TestEmitWorkspaceReferencePartialLocationsStopsOnCancellation(t *testing.T) {
	total := workspaceReferencePartialResultChunkSize*2 + 1
	locations := make([]lsp.Location, total)
	var queue workspaceReferencePartialLocationQueue
	queue.append(locations)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sent := 0
	if err := emitWorkspaceReferencePartialLocations(ctx, &queue, func(chunk []lsp.Location) error {
		sent++
		cancel()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("chunks sent after cancellation = %d, want one", sent)
	}
	if queue.len() != total-workspaceReferencePartialResultChunkSize {
		t.Fatalf("queued locations after cancellation = %d, want %d", queue.len(), total-workspaceReferencePartialResultChunkSize)
	}
}

func TestEmitWorkspaceReferencePartialLocationsStopsOnWriteError(t *testing.T) {
	total := workspaceReferencePartialResultChunkSize*2 + 1
	locations := make([]lsp.Location, total)
	var queue workspaceReferencePartialLocationQueue
	queue.append(locations)
	wantErr := errors.New("partial result write failed")
	sent := 0
	err := emitWorkspaceReferencePartialLocations(context.Background(), &queue, func(chunk []lsp.Location) error {
		sent++
		if len(chunk) != workspaceReferencePartialResultChunkSize {
			t.Fatalf("failed chunk size = %d, want %d", len(chunk), workspaceReferencePartialResultChunkSize)
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("write error = %v, want %v", err, wantErr)
	}
	if sent != 1 {
		t.Fatalf("chunks sent after write error = %d, want one", sent)
	}
	if queue.len() != total-workspaceReferencePartialResultChunkSize {
		t.Fatalf("queued locations after write error = %d, want %d", queue.len(), total-workspaceReferencePartialResultChunkSize)
	}
}
