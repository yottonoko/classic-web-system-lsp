package lspserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestStdioParityPublishesStagedDiagnosticsFromParserToFinalLayers(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-staged-diagnostics.asp"
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text": `<!-- #include file="missing.inc" -->
<style>.broken { color: }</style>
<%
Option Explicit
Response.Write missingName`,
		},
	}); err != nil {
		t.Fatal(err)
	}

	diagnostics, logs := waitForStagedDiagnosticsComplete(t, client)
	fast := firstDiagnosticsContaining(t, diagnostics, "closing %>")
	if fast == nil {
		t.Fatalf("fast diagnostics missing closing %%>: %s", mustJSONText(t, diagnostics))
	}
	if strings.Contains(string(fast.Params), "missing.inc") {
		t.Fatalf("fast diagnostics included include diagnostics too early: %s", fast.Params)
	}
	if firstDiagnosticsContaining(t, diagnostics, "Include file 'missing.inc' could not be resolved.") == nil {
		t.Fatalf("include diagnostics missing: %s", mustJSONText(t, diagnostics))
	}
	if firstDiagnosticsContaining(t, diagnostics, "asp-lsp-css") == nil {
		t.Fatalf("CSS diagnostics missing: %s", mustJSONText(t, diagnostics))
	}
	final := lastDiagnosticsContaining(t, diagnostics, "missingName")
	if final == nil ||
		!strings.Contains(string(final.Params), "Include file 'missing.inc' could not be resolved.") ||
		!strings.Contains(string(final.Params), "missingName") {
		t.Fatalf("final diagnostics missing include or project diagnostics: %s", mustJSONText(t, diagnostics))
	}
	assertLogOrder(t, logs, []string{
		"diagnostics.fast.published",
		"diagnostics.include.published",
		"diagnostics.syntax.published",
		"diagnostics.projectFast.published",
		"diagnostics.project.published",
		"diagnostics.final.published",
		"LSP check completed",
	})
}

func TestStdioParityDoesNotClearVisibleDiagnosticsWhileChangeDiagnosticsAreDebounced(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-diagnostics-preserve-on-change.asp"
	initial := `<%
Option Explicit
Response.Write missingName
%>
<div>hello</div>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 120},
	}})
	notifyOpenClassicASPDocument(t, client, uri, initial)
	waitForDiagnosticsContaining(t, client, "missingName")
	client.waitForLogContaining("LSP check completed")
	client.drainNotifications("textDocument/publishDiagnostics")
	client.drainNotifications("window/logMessage")

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": strings.Replace(initial, "hello", "hello!", 1)}},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if pending := client.drainNotifications("textDocument/publishDiagnostics"); len(pending) != 0 {
		t.Fatalf("debounced change cleared visible diagnostics: %s", mustJSONText(t, pending))
	}
	final := waitForDiagnosticsContaining(t, client, "missingName")
	if !strings.Contains(string(final.Params), "missingName") {
		t.Fatalf("final debounced diagnostics missing missingName: %s", final.Params)
	}
}

func TestStdioParityReusesInFlightStagedDiagnosticsForDuplicateValidations(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "go-staged-diagnostics-reuse.asp"))
	includes := make([]string, 0, 64)
	for index := 0; index < 64; index++ {
		includes = append(includes, fmt.Sprintf(`<!-- #include file="missing-%d.inc" -->`, index))
	}
	source := strings.Join(includes, "\n") + `
<%
Option Explicit
Response.Write missingName
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	notifyConfiguration(t, client, verboseImmediateDiagnosticsSettings())
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		if err := client.notify("textDocument/didSave", map[string]any{
			"textDocument": map[string]any{"uri": uri},
		}); err != nil {
			t.Fatal(err)
		}
	}

	waitForStagedDiagnosticsReuseComplete(t, client)
	time.Sleep(100 * time.Millisecond)
	logText := mustJSONText(t, client.drainNotifications("window/logMessage"))
	if strings.Contains(logText, "LSP check started") {
		t.Fatalf("duplicate staged diagnostics validation started again: %s", logText)
	}
}

func TestStdioParityDeduplicatesPublishedDiagnosticsAndIgnoresLineContinuationUnderscores(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-vb-diagnostic-dedupe.asp"
	source := `<%
Option Explicit
Dim message
message = "hello" & _
  missingName
Response.Write missingName
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}

	diagnosticsMessage := client.waitForNotification("textDocument/publishDiagnostics", "missingName")
	vbDiagnostics := diagnosticsForSourceWithIdentity(t, diagnosticsMessage, "asp-lsp-vbscript")
	serialized := mustJSONText(t, vbDiagnostics)
	if !strings.Contains(serialized, "missingName") {
		t.Fatalf("VBScript diagnostics missing missingName: %s", serialized)
	}
	if strings.Contains(serialized, "'_'") {
		t.Fatalf("VBScript diagnostics reported line continuation underscore: %s", serialized)
	}
	seen := map[string]bool{}
	for _, diagnostic := range vbDiagnostics {
		key := mustJSONText(t, map[string]any{
			"source":   diagnostic.Source,
			"code":     diagnostic.Code,
			"severity": diagnostic.Severity,
			"range":    diagnostic.Range,
			"message":  diagnostic.Message,
		})
		if seen[key] {
			t.Fatalf("duplicate VBScript diagnostic published: %s in %s", key, serialized)
		}
		seen[key] = true
	}
}

func TestStdioParityDoesNotReportVBScriptWordOperatorsAsUndeclared(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	uri := "file:///tmp/go-vb-word-operators.asp"
	source := `<%
Option Explicit
Dim leftValue, rightValue, result, value
Set value = Nothing
result = leftValue Xor rightValue
result = result Eqv (leftValue Imp rightValue)
result = result And Not (leftValue Or rightValue)
result = result Mod 2
If value Is Nothing Then
  Response.Write "empty"
End If
%>`
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}

	diagnosticsMessage := client.waitForNotification("textDocument/publishDiagnostics", uri)
	vbDiagnostics := diagnosticsForSourceWithIdentity(t, diagnosticsMessage, "asp-lsp-vbscript")
	serialized := mustJSONText(t, vbDiagnostics)
	for _, unexpected := range []string{"Xor", "Eqv", "Imp", "And", "Not", "Or", "Mod", "Is"} {
		if strings.Contains(serialized, unexpected) {
			t.Fatalf("VBScript diagnostics reported word operator %s as undeclared: %s", unexpected, serialized)
		}
	}
}

func waitForStagedDiagnosticsReuseComplete(t *testing.T, client *stdioTestClient) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	seen := []string{}
	sawStarted := false
	sawReuse := false
	sawMissingName := false
	sawCompleted := false
	for {
		select {
		case message := <-client.notifications:
			text := message.Method + " " + string(message.Params)
			seen = append(seen, text)
			if message.Method == "window/logMessage" {
				sawStarted = sawStarted || strings.Contains(text, "LSP check started")
				sawReuse = sawReuse || strings.Contains(text, "diagnostics.reuse")
				sawCompleted = sawCompleted || strings.Contains(text, "LSP check completed")
			}
			if message.Method == "textDocument/publishDiagnostics" {
				sawMissingName = sawMissingName || diagnosticsContainMessage(t, message, "missingName")
			}
			if sawStarted && sawReuse && sawMissingName && sawCompleted {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for staged diagnostics reuse completion; started=%t reuse=%t missingName=%t completed=%t seen: %s", sawStarted, sawReuse, sawMissingName, sawCompleted, strings.Join(seen, "\n"))
		}
	}
}

func waitForStagedDiagnosticsComplete(t *testing.T, client *stdioTestClient) ([]*rpcMessage, []*rpcMessage) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	diagnostics := []*rpcMessage{}
	logs := []*rpcMessage{}
	seen := []string{}
	for {
		select {
		case message := <-client.notifications:
			seen = append(seen, message.Method+" "+string(message.Params))
			switch message.Method {
			case "textDocument/publishDiagnostics":
				diagnostics = append(diagnostics, message)
			case "window/logMessage":
				logs = append(logs, message)
				if strings.Contains(string(message.Params), "LSP check completed") {
					return diagnostics, logs
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for staged diagnostics completion; seen: %s", strings.Join(seen, "\n"))
			return nil, nil
		}
	}
}

func firstDiagnosticsContaining(t *testing.T, messages []*rpcMessage, expected string) *rpcMessage {
	t.Helper()
	for _, message := range messages {
		if diagnosticsContainMessage(t, message, expected) || strings.Contains(string(message.Params), expected) {
			return message
		}
	}
	return nil
}

func lastDiagnosticsContaining(t *testing.T, messages []*rpcMessage, expected string) *rpcMessage {
	t.Helper()
	for index := len(messages) - 1; index >= 0; index-- {
		if diagnosticsContainMessage(t, messages[index], expected) || strings.Contains(string(messages[index].Params), expected) {
			return messages[index]
		}
	}
	return nil
}

func diagnosticsContainMessage(t *testing.T, message *rpcMessage, expected string) bool {
	t.Helper()
	for _, diagnostic := range diagnosticsFromMessage(t, message) {
		if strings.Contains(diagnostic.Message, expected) {
			return true
		}
	}
	return false
}

func diagnosticsForSourceWithIdentity(t *testing.T, message *rpcMessage, source string) []diagnosticIdentity {
	t.Helper()
	var params struct {
		Diagnostics []diagnosticIdentity `json:"diagnostics"`
	}
	mustDecodeResult(t, message.Params, &params)
	result := make([]diagnosticIdentity, 0, len(params.Diagnostics))
	for _, diagnostic := range params.Diagnostics {
		if diagnostic.Source == source {
			result = append(result, diagnostic)
		}
	}
	return result
}

type diagnosticIdentity struct {
	Source   string    `json:"source"`
	Code     any       `json:"code"`
	Severity any       `json:"severity"`
	Range    lsp.Range `json:"range"`
	Message  string    `json:"message"`
}

func assertLogOrder(t *testing.T, logs []*rpcMessage, expected []string) {
	t.Helper()
	logText := mustJSONText(t, logs)
	lastIndex := -1
	for _, needle := range expected {
		index := strings.Index(logText, needle)
		if index < 0 {
			t.Fatalf("staged diagnostics log missing %q: %s", needle, logText)
		}
		if index <= lastIndex {
			t.Fatalf("staged diagnostics log %q out of order: %s", needle, logText)
		}
		lastIndex = index
	}
}
