package lspserver

import "testing"

func TestDiagnosticsPendingProgressLabelReportsRemainingProjectWork(t *testing.T) {
	tasks := []string{
		"parser", "declarations", "calls", "vbscript.syntax", "vbscript.deadCode", "vbscript.unused",
		"vbscript.naming", "includes", "vbscript.types", "html", "css", "javascript",
	}
	var completed uint32
	for index := range tasks {
		if tasks[index] != "javascript" {
			completed |= 1 << index
		}
	}
	if got := diagnosticsPendingProgressLabel(tasks, completed); got != "diagnostics.project" {
		t.Fatalf("13/15 remaining JavaScript label = %q, want diagnostics.project", got)
	}
}

func TestDiagnosticsPendingProgressLabelUsesHighestPriorityRemainingPhase(t *testing.T) {
	tasks := []string{"parser", "includes", "vbscript.deadCode", "javascript"}
	if got := diagnosticsPendingProgressLabel(tasks, 0); got != "diagnostics.project" {
		t.Fatalf("initial pending label = %q", got)
	}
	if got := diagnosticsPendingProgressLabel(tasks, 1<<3); got != "diagnostics.include" {
		t.Fatalf("pending include label = %q", got)
	}
	if got := diagnosticsPendingProgressLabel(tasks, 1<<3|1<<1); got != "diagnostics.projectFast" {
		t.Fatalf("pending project-fast label = %q", got)
	}
}
