package core

import (
	"runtime"
	"strings"
	"testing"
)

func TestLineRenderWorkerCountThresholds(t *testing.T) {
	oldMaxProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(oldMaxProcs)

	if got := lineRenderWorkerCount(parallelRenderMinLines-1, parallelRenderMinBytes*2, parallelRenderMinItems*2); got != 1 {
		t.Fatalf("lineRenderWorkerCount below line threshold = %d, want 1", got)
	}
	if got := lineRenderWorkerCount(parallelRenderMinLines, parallelRenderMinBytes-1, parallelRenderMinItems*2); got != 1 {
		t.Fatalf("lineRenderWorkerCount below byte threshold = %d, want 1", got)
	}
	if got := lineRenderWorkerCount(parallelRenderMinLines, parallelRenderMinBytes, parallelRenderMinItems-1); got != 1 {
		t.Fatalf("lineRenderWorkerCount below item threshold = %d, want 1", got)
	}
	if got := lineRenderWorkerCount(parallelRenderMinLines, parallelRenderMinBytes, parallelRenderMinItems); got != 4 {
		t.Fatalf("lineRenderWorkerCount at threshold = %d, want 4", got)
	}
}

func TestParallelRenderLineSnapshotsMatchesSequential(t *testing.T) {
	oldMaxProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(oldMaxProcs)

	lines := make([]outputLineRenderSnapshot, parallelRenderMinLines+257)
	byteCount := 0
	itemCount := 0
	for i := range lines {
		items := []string{"line-", strings.Repeat("x", 260+i%11), "-tail", strings.Repeat("y", 5)}
		if i%17 == 0 {
			items = nil
		}
		lines[i] = outputLineRenderSnapshot{
			indent: strings.Repeat(" ", i%5),
			items:  items,
		}
		byteCount += len(lines[i].indent)
		for _, item := range lines[i].items {
			byteCount += len(item)
		}
		itemCount += len(lines[i].items)
	}
	byteCount += len(lines) - 1
	for itemCount < parallelRenderMinItems {
		for i := range lines {
			lines[i].items = append(lines[i].items, "z")
			byteCount++
			itemCount++
			if itemCount >= parallelRenderMinItems {
				break
			}
		}
	}
	if workers := lineRenderWorkerCount(len(lines), byteCount, itemCount); workers < 2 {
		t.Fatalf("lineRenderWorkerCount = %d, want parallel render", workers)
	}
	got := renderLineSnapshots(lines, byteCount, itemCount)
	want := make([]string, len(lines))
	renderLineSnapshotsRange(want, lines)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatal("parallel render output differs from sequential render")
	}
}

func TestOutputGetCodePreservesEOLBlankLinesAndRawText(t *testing.T) {
	out := NewOutput(OutputOptions{
		EndWithNewline:   true,
		IndentSize:       2,
		IndentChar:       " ",
		IndentEmptyLines: true,
	}, "")
	out.SetIndent(1, 0)
	out.AddToken("alpha")
	out.AddNewLine(false)
	out.AddNewLine(true)
	out.SetIndent(2, 0)
	out.AddToken("beta\n")
	out.AddRawText("raw\n  text")

	got := out.GetCode("\r\n")
	want := "alpha\r\n  \r\n    beta\r\nraw\r\n  text\r\n    "
	if got != want {
		t.Fatalf("GetCode() = %q, want %q", got, want)
	}
}
