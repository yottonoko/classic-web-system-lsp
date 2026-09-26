package lspserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type stdioTestClient struct {
	t              *testing.T
	serverInput    *io.PipeWriter
	serverOutput   *io.PipeReader
	notifications  chan *rpcMessage
	serverRequests chan *rpcMessage
	responses      map[string]chan *rpcMessage
	nextID         int
	mu             sync.Mutex
	done           chan error
}

func startStdioTestClient(t *testing.T) *stdioTestClient {
	return startStdioTestClientWithServer(t, nil)
}

func startStdioTestClientWithServer(t *testing.T, configure func(*Server)) *stdioTestClient {
	t.Helper()
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()
	client := &stdioTestClient{
		t:              t,
		serverInput:    clientWriter,
		serverOutput:   clientReader,
		notifications:  make(chan *rpcMessage, 1024),
		serverRequests: make(chan *rpcMessage, 32),
		responses:      map[string]chan *rpcMessage{},
		done:           make(chan error, 1),
	}
	server := New(serverReader, serverWriter, nil)
	if configure != nil {
		configure(server)
	}
	go func() {
		err := server.Serve(context.Background())
		_ = serverReader.Close()
		_ = serverWriter.Close()
		client.done <- err
	}()
	go client.readLoop()
	return client
}

func (c *stdioTestClient) close() {
	c.t.Helper()
	_ = c.notify("exit", nil)
	_ = c.serverInput.Close()
	_ = c.serverOutput.Close()
	select {
	case <-c.done:
	case <-time.After(time.Second):
		c.t.Fatalf("stdio server did not stop")
	}
}

func (c *stdioTestClient) request(method string, params any) *rpcMessage {
	c.t.Helper()
	response := c.requestAsync(method, params)
	select {
	case message := <-response:
		if message.Error != nil {
			c.t.Fatalf("%s returned error: %#v", method, message.Error)
		}
		return message
	case <-time.After(10 * time.Second):
		c.t.Fatalf("timed out waiting for %s response", method)
		return nil
	}
}

func (c *stdioTestClient) requestAsync(method string, params any) <-chan *rpcMessage {
	c.t.Helper()
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	key := fmt.Sprint(id)
	response := make(chan *rpcMessage, 1)
	c.responses[key] = response
	c.mu.Unlock()
	if err := writeMessage(c.serverInput, rpcMessage{ID: id, Method: method, Params: mustRawMessage(c.t, params)}); err != nil {
		c.t.Fatalf("write request %s: %v", method, err)
	}
	return response
}

func (c *stdioTestClient) waitForResponse(method string, response <-chan *rpcMessage) *rpcMessage {
	c.t.Helper()
	select {
	case message := <-response:
		if message.Error != nil {
			c.t.Fatalf("%s returned error: %#v", method, message.Error)
		}
		return message
	case <-time.After(10 * time.Second):
		c.t.Fatalf("timed out waiting for %s response", method)
		return nil
	}
}

func (c *stdioTestClient) notify(method string, params any) error {
	return writeMessage(c.serverInput, rpcMessage{Method: method, Params: mustRawMessage(c.t, params)})
}

func (c *stdioTestClient) waitForServerRequest(method string) *rpcMessage {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case message := <-c.serverRequests:
			if message.Method == method {
				return message
			}
		case <-deadline:
			c.t.Fatalf("timed out waiting for server request %s", method)
			return nil
		}
	}
}

func (c *stdioTestClient) respondToServerRequest(request *rpcMessage, result any) {
	c.t.Helper()
	if err := writeMessage(c.serverInput, rpcMessage{ID: request.ID, Result: result}); err != nil {
		c.t.Fatalf("write server response %s: %v", request.Method, err)
	}
}

func (c *stdioTestClient) waitForNotification(method string, contains string) *rpcMessage {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	seen := []string{}
	for {
		select {
		case message := <-c.notifications:
			seen = append(seen, message.Method+" "+string(message.Params))
			if message.Method != method {
				continue
			}
			if contains == "" || strings.Contains(string(message.Params), contains) {
				return message
			}
		case <-deadline:
			c.t.Fatalf("timed out waiting for %s containing %q; seen: %s", method, contains, strings.Join(seen, "\n"))
			return nil
		}
	}
}

func (c *stdioTestClient) waitForNotificationWithSeen(method string, contains string) (*rpcMessage, []*rpcMessage) {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	seen := []*rpcMessage{}
	seenText := []string{}
	for {
		select {
		case message := <-c.notifications:
			seen = append(seen, message)
			seenText = append(seenText, message.Method+" "+string(message.Params))
			if message.Method != method {
				continue
			}
			if contains == "" || strings.Contains(string(message.Params), contains) {
				return message, seen
			}
		case <-deadline:
			c.t.Fatalf("timed out waiting for %s containing %q; seen: %s", method, contains, strings.Join(seenText, "\n"))
			return nil, nil
		}
	}
}

func (c *stdioTestClient) waitForLogContaining(contains string) *rpcMessage {
	c.t.Helper()
	return c.waitForNotification("window/logMessage", contains)
}

func (c *stdioTestClient) drainNotifications(method string) []*rpcMessage {
	c.t.Helper()
	drained := []*rpcMessage{}
	for {
		select {
		case message := <-c.notifications:
			if method == "" || message.Method == method {
				drained = append(drained, message)
			}
		default:
			return drained
		}
	}
}

func (c *stdioTestClient) readLoop() {
	reader := bufio.NewReader(c.serverOutput)
	for {
		message, err := readMessage(reader)
		if err != nil {
			return
		}
		if message.ID != nil && message.Method != "" {
			c.serverRequests <- message
			continue
		}
		if message.ID != nil {
			key := fmt.Sprint(message.ID)
			c.mu.Lock()
			response := c.responses[key]
			delete(c.responses, key)
			c.mu.Unlock()
			if response != nil {
				response <- message
			}
			continue
		}
		c.notifications <- message
	}
}

func mustRawMessage(t *testing.T, value any) json.RawMessage {
	t.Helper()
	if value == nil {
		return nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func mustJSONText(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

type completionItem struct {
	Label            string        `json:"label"`
	Kind             int           `json:"kind"`
	Detail           string        `json:"detail"`
	Documentation    any           `json:"documentation"`
	InsertText       string        `json:"insertText"`
	InsertTextFormat int           `json:"insertTextFormat"`
	TextEdit         *lsp.TextEdit `json:"textEdit"`
	FilterText       string        `json:"filterText"`
	Data             any           `json:"data"`
}

type completionItemList []completionItem

type labelList []string

type markedText struct {
	Text     string         `json:"text"`
	Position map[string]int `json:"position"`
}

type decodedSemanticToken struct {
	Line           int
	Character      int
	Length         int
	TokenType      int
	TokenModifiers int
}

type diagnosticResult struct {
	Source  string    `json:"source"`
	Message string    `json:"message"`
	Range   lsp.Range `json:"range"`
	Code    any       `json:"code,omitempty"`
}

type typedInsertionResult struct {
	Text    string
	Version int
}

const (
	semanticTokenParameter = 2
	semanticTokenFunction  = 3
	semanticTokenClass     = 4
	semanticTokenVariable  = 1
	semanticTokenMethod    = 5
	semanticTokenProperty  = 6
	semanticTokenOperator  = 9
	semanticModifierByRef  = 1 << 4
	semanticModifierByVal  = 1 << 5
)

func completionItems(value any) completionItemList {
	var envelope struct {
		Items []completionItem `json:"items"`
	}
	body, _ := json.Marshal(value)
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Items != nil {
		return envelope.Items
	}
	var items []completionItem
	_ = json.Unmarshal(body, &items)
	return items
}

func completionLabels(value any) labelList {
	items := completionItems(value)
	return completionItemLabels(items)
}

func completionItemLabels(items completionItemList) labelList {
	labels := make(labelList, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func completionEditNewText(item completionItem) string {
	if item.TextEdit != nil {
		return item.TextEdit.NewText
	}
	return item.InsertText
}

func (items completionItemList) find(label string) (completionItem, bool) {
	for _, item := range items {
		if item.Label == label {
			return item, true
		}
	}
	return completionItem{}, false
}

func (items completionItemList) hasLabel(label string) bool {
	_, ok := items.find(label)
	return ok
}

func (items completionItemList) hasItem(label string, kind int, detail string) bool {
	for _, item := range items {
		if item.Label == label && item.Kind == kind && item.Detail == detail {
			return true
		}
	}
	return false
}

func (labels labelList) contains(label string) bool {
	for _, candidate := range labels {
		if candidate == label {
			return true
		}
	}
	return false
}

func stringSliceContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func mustDecodeResult(t *testing.T, value any, target any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatal(err)
	}
}

func mustReadText(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func pathToFileURI(path string) string {
	return filePathURI(path)
}

func markedDocument(source string) markedText {
	const marker = "<<<caret>>>"
	offset := strings.Index(source, marker)
	text := strings.Replace(source, marker, "", 1)
	return markedText{Text: text, Position: map[string]int{"line": lineAt(text, offset), "character": characterAt(text, offset)}}
}

func positionAt(text string, offset int) map[string]int {
	return map[string]int{"line": lineAt(text, offset), "character": characterAt(text, offset)}
}

func mapPosition(position map[string]int) lsp.Position {
	return lsp.Position{Line: position["line"], Character: position["character"]}
}

func mapPositionRange(start map[string]int, end map[string]int) lsp.Range {
	return lsp.Range{
		Start: mapPosition(start),
		End:   mapPosition(end),
	}
}

func lineAt(text string, offset int) int {
	line := 0
	for i := 0; i < offset && i < len(text); i++ {
		if text[i] == '\n' {
			line++
		}
	}
	return line
}

func characterAt(text string, offset int) int {
	offset = max(0, min(offset, len(text)))
	lastNewline := strings.LastIndex(text[:offset], "\n")
	if lastNewline < 0 {
		return utf16CharacterCount(text[:offset])
	}
	return utf16CharacterCount(text[lastNewline+1 : offset])
}

func utf16CharacterCount(text string) int {
	count := 0
	for _, r := range text {
		if r > 0xFFFF {
			count += 2
		} else {
			count++
		}
	}
	return count
}

func resultArrayLength(t *testing.T, value any) int {
	t.Helper()
	var result []any
	mustDecodeResult(t, value, &result)
	return len(result)
}

func decodeSemanticTokens(t *testing.T, value any) []decodedSemanticToken {
	t.Helper()
	var result struct {
		Data []int `json:"data"`
	}
	mustDecodeResult(t, value, &result)
	return decodeSemanticTokenData(result.Data)
}

func decodeSemanticTokenData(data []int) []decodedSemanticToken {
	tokens := make([]decodedSemanticToken, 0, len(data)/5)
	line := 0
	character := 0
	for i := 0; i+4 < len(data); i += 5 {
		line += data[i]
		if data[i] == 0 {
			character += data[i+1]
		} else {
			character = data[i+1]
		}
		tokens = append(tokens, decodedSemanticToken{
			Line:           line,
			Character:      character,
			Length:         data[i+2],
			TokenType:      data[i+3],
			TokenModifiers: data[i+4],
		})
	}
	return tokens
}

func semanticTokenData(t *testing.T, value any) []int {
	t.Helper()
	var result struct {
		Data []int `json:"data"`
	}
	mustDecodeResult(t, value, &result)
	return append([]int(nil), result.Data...)
}

func applySemanticTokenDeltaEdits(t *testing.T, previous []int, delta any) []int {
	t.Helper()
	var result struct {
		Edits []struct {
			Start       int   `json:"start"`
			DeleteCount int   `json:"deleteCount"`
			Data        []int `json:"data"`
		} `json:"edits"`
	}
	mustDecodeResult(t, delta, &result)
	applied := append([]int(nil), previous...)
	for _, edit := range result.Edits {
		if edit.Start < 0 || edit.Start > len(applied) || edit.DeleteCount < 0 || edit.Start+edit.DeleteCount > len(applied) {
			t.Fatalf("invalid semantic token delta edit: %#v for %d tokens", edit, len(applied))
		}
		next := make([]int, 0, len(applied)-edit.DeleteCount+len(edit.Data))
		next = append(next, applied[:edit.Start]...)
		next = append(next, edit.Data...)
		next = append(next, applied[edit.Start+edit.DeleteCount:]...)
		applied = next
	}
	return applied
}

func hasSemanticToken(tokens []decodedSemanticToken, line int, character int, tokenType int, modifiers int) bool {
	for _, token := range tokens {
		if token.Line == line && token.Character == character && token.TokenType == tokenType && token.TokenModifiers == modifiers {
			return true
		}
	}
	return false
}

func hasSemanticTokenType(tokens []decodedSemanticToken, tokenType int) bool {
	for _, token := range tokens {
		if token.TokenType == tokenType {
			return true
		}
	}
	return false
}

func hasTokenMatchingText(source string, tokens []decodedSemanticToken, text string, tokenType int) bool {
	for _, token := range tokens {
		if token.TokenType != tokenType {
			continue
		}
		offset := offsetAtPosition(source, token.Line, token.Character)
		if offset < 0 || offset+token.Length > len(source) {
			continue
		}
		if source[offset:offset+token.Length] == text {
			return true
		}
	}
	return false
}

func semanticResultID(t *testing.T, value any) string {
	t.Helper()
	var result struct {
		ResultID string `json:"resultId"`
	}
	mustDecodeResult(t, value, &result)
	if result.ResultID == "" {
		t.Fatalf("semantic token result missing resultId: %s", mustJSONText(t, value))
	}
	return result.ResultID
}

func notifyRangedReplacement(t *testing.T, client *stdioTestClient, uri string, source string, line int, oldText string, newText string) string {
	t.Helper()
	lines := strings.Split(source, "\n")
	if line < 0 || line >= len(lines) {
		t.Fatalf("line %d out of range", line)
	}
	lineOffset := strings.Index(lines[line], oldText)
	if lineOffset < 0 {
		t.Fatalf("line %d does not contain %q: %q", line, oldText, lines[line])
	}
	start := lineOffset
	for _, previousLine := range lines[:line] {
		start += len(previousLine) + 1
	}
	end := start + len(oldText)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"range": map[string]any{"start": positionAt(source, start), "end": positionAt(source, end)},
			"text":  newText,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	lines[line] = strings.Replace(lines[line], oldText, newText, 1)
	return strings.Join(lines, "\n")
}

func notifyNeedleReplacement(t *testing.T, client *stdioTestClient, uri string, source string, version int, oldText string, newText string) string {
	t.Helper()
	start := strings.Index(source, oldText)
	if start < 0 {
		t.Fatalf("missing text %q", oldText)
	}
	end := start + len(oldText)
	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": version},
		"contentChanges": []map[string]any{{
			"range": map[string]any{
				"start": positionAt(source, start),
				"end":   positionAt(source, end),
			},
			"text": newText,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	return source[:start] + newText + source[end:]
}

func notifyTypedInsertion(t *testing.T, client *stdioTestClient, uri string, source string, version int, insertOffset int, insertedText string) typedInsertionResult {
	t.Helper()
	text := source
	nextVersion := version
	offset := insertOffset
	for _, char := range insertedText {
		position := positionAt(text, offset)
		nextVersion++
		inserted := string(char)
		if err := client.notify("textDocument/didChange", map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": nextVersion},
			"contentChanges": []map[string]any{{
				"range":       map[string]any{"start": position, "end": position},
				"rangeLength": 0,
				"text":        inserted,
			}},
		}); err != nil {
			t.Fatal(err)
		}
		text = text[:offset] + inserted + text[offset:]
		offset += len(inserted)
	}
	return typedInsertionResult{Text: text, Version: nextVersion}
}

func offsetAtPosition(text string, line int, character int) int {
	if line < 0 {
		line = 0
	}
	lineStart := 0
	for currentLine := 0; currentLine < line; currentLine++ {
		newline := strings.IndexByte(text[lineStart:], '\n')
		if newline < 0 {
			return len(text)
		}
		lineStart += newline + 1
	}
	lineEnd := strings.IndexByte(text[lineStart:], '\n')
	if lineEnd < 0 {
		lineEnd = len(text)
	} else {
		lineEnd += lineStart
	}
	for lineEnd > lineStart && (text[lineEnd-1] == '\r' || text[lineEnd-1] == '\n') {
		lineEnd--
	}
	return lineStart + byteOffsetForUTF16Character(text[lineStart:lineEnd], character)
}

func byteOffsetForUTF16Character(text string, character int) int {
	if character <= 0 {
		return 0
	}
	units := 0
	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units+width >= character {
			return offset + size
		}
		units += width
		offset += size
	}
	return len(text)
}

func offsetAt(text string, position lsp.Position) int {
	return offsetAtPosition(text, position.Line, position.Character)
}

func applyTextEdit(text string, editRange lsp.Range, newText string) string {
	start := offsetAt(text, editRange.Start)
	end := offsetAt(text, editRange.End)
	if start < 0 || end < 0 {
		return text
	}
	if end < start {
		start, end = end, start
	}
	return text[:start] + newText + text[end:]
}

func runRenameScopeCase(t *testing.T, root string, ownerURI string, source string, workspaceSymbolRename bool) any {
	t.Helper()
	client := startStdioTestClient(t)
	defer client.close()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})
	if workspaceSymbolRename {
		if err := client.notify("workspace/didChangeConfiguration", map[string]any{
			"settings": map[string]any{
				"aspLsp": map[string]any{
					"rename": map[string]any{"workspaceSymbolRename": true},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        ownerURI,
			"languageId": "classic-asp",
			"version":    1,
			"text":       source,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", ownerURI)
	return client.request("textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": ownerURI},
		"position":     positionAt(source, strings.Index(source, "SharedValue")+1),
		"newName":      "RenamedValue",
	}).Result
}

func openClassicASPDocument(t *testing.T, client *stdioTestClient, uri string, text string) {
	t.Helper()
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       text,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client.waitForNotification("textDocument/publishDiagnostics", uri)
}

func assertRequestContains(t *testing.T, client *stdioTestClient, method string, params map[string]any, expected string) {
	t.Helper()
	result := mustJSONText(t, client.request(method, params).Result)
	if !strings.Contains(result, expected) {
		t.Fatalf("%s result missing %q: %s", method, expected, result)
	}
}

func vbscriptHoverCodeBlockJSON(lines ...string) string {
	return "```vbscript\\n" + strings.Join(lines, "\\n") + "\\n```"
}

func hoverIdentifierFromVBScriptCodeBlockExpectation(expected string) string {
	for _, line := range strings.Split(expected, "\\n") {
		fields := strings.Fields(strings.Trim(line, "`"))
		if len(fields) >= 3 && strings.HasPrefix(fields[0], "(") && (fields[1] == "Dim" || fields[1] == "Const") {
			return fields[2]
		}
		if len(fields) >= 2 && (fields[0] == "Dim" || fields[0] == "Const") {
			return fields[1]
		}
	}
	return ""
}

func waitForDiagnosticsContaining(t *testing.T, client *stdioTestClient, expected string) *rpcMessage {
	t.Helper()
	return client.waitForNotification("textDocument/publishDiagnostics", expected)
}

func waitForDiagnosticsCleared(t *testing.T, client *stdioTestClient, uri string) *rpcMessage {
	t.Helper()
	deadline := time.After(10 * time.Second)
	seen := []string{}
	for {
		select {
		case message := <-client.notifications:
			seen = append(seen, message.Method+" "+string(message.Params))
			if message.Method != "textDocument/publishDiagnostics" {
				continue
			}
			if diagnosticsAreCleared(t, message, uri) {
				return message
			}
		case <-deadline:
			t.Fatalf("timed out waiting for cleared diagnostics for %s; seen: %s", uri, strings.Join(seen, "\n"))
			return nil
		}
	}
}

func countOccurrences(text string, needle string) int {
	return strings.Count(text, needle)
}

func verboseImmediateDiagnosticsSettings() map[string]any {
	return map[string]any{"aspLsp": map[string]any{
		"debug":       map[string]any{"output": "verbose"},
		"diagnostics": map[string]any{"debounceMs": 0},
	}}
}

func findNotificationContaining(messages []*rpcMessage, method string, expected string) *rpcMessage {
	for _, message := range messages {
		if message.Method == method && strings.Contains(string(message.Params), expected) {
			return message
		}
	}
	return nil
}

func diagnosticFromSource(t *testing.T, message *rpcMessage, source string) *diagnosticResult {
	t.Helper()
	for _, diagnostic := range diagnosticsFromMessage(t, message) {
		if diagnostic.Source == source {
			return &diagnostic
		}
	}
	return nil
}

func diagnosticContaining(t *testing.T, message *rpcMessage, expected string) *diagnosticResult {
	t.Helper()
	for _, diagnostic := range diagnosticsFromMessage(t, message) {
		if strings.Contains(diagnostic.Message, expected) {
			return &diagnostic
		}
	}
	return nil
}

func diagnosticsFromMessage(t *testing.T, message *rpcMessage) []diagnosticResult {
	t.Helper()
	if message == nil {
		return nil
	}
	var params struct {
		Diagnostics []diagnosticResult `json:"diagnostics"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		t.Fatal(err)
	}
	return params.Diagnostics
}

func diagnosticsURI(t *testing.T, message *rpcMessage) string {
	t.Helper()
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		t.Fatal(err)
	}
	return params.URI
}

func diagnosticVersion(t *testing.T, message *rpcMessage) int {
	t.Helper()
	var params struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		t.Fatal(err)
	}
	return params.Version
}

func diagnosticsAreCleared(t *testing.T, message *rpcMessage, uri string) bool {
	t.Helper()
	return diagnosticsURI(t, message) == uri && len(diagnosticsFromMessage(t, message)) == 0
}

func documentColors(t *testing.T, value any) []lsp.ColorInformation {
	t.Helper()
	var colors []lsp.ColorInformation
	mustDecodeResult(t, value, &colors)
	return colors
}

func colorStartsContain(colors []lsp.ColorInformation, want lsp.Position) bool {
	for _, color := range colors {
		if color.Range.Start == want {
			return true
		}
	}
	return false
}

func colorPresentations(t *testing.T, value any) []lsp.ColorPresentation {
	t.Helper()
	var presentations []lsp.ColorPresentation
	mustDecodeResult(t, value, &presentations)
	return presentations
}

func colorPresentationLabels(presentations []lsp.ColorPresentation) labelList {
	labels := make(labelList, 0, len(presentations))
	for _, presentation := range presentations {
		labels = append(labels, presentation.Label)
	}
	return labels
}

var debugTimingLogPattern = regexp.MustCompile(`LSP analysis|LSP check|Formatting conversion|analysis\.|check\.|format\.|heat=duration-`)
var elapsedLogPattern = regexp.MustCompile(`in \d+\.\d ms`)

func expectElapsedLogWithoutHeat(t *testing.T, message *rpcMessage) {
	t.Helper()
	text := string(message.Params)
	if !elapsedLogPattern.MatchString(text) {
		t.Fatalf("log missing elapsed duration: %s", text)
	}
	if strings.Contains(text, "heat=duration-") {
		t.Fatalf("log contains duration heat marker: %s", text)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = parent
	}
}

func waitForFileContaining(t *testing.T, path string, expected string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil {
			text := string(body)
			if strings.Contains(text, expected) {
				return text
			}
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("timed out waiting for %s containing %q: %v", path, expected, lastErr)
	}
	t.Fatalf("timed out waiting for %s containing %q", path, expected)
	return ""
}

func waitForPathExists(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %v", path, lastErr)
}

func notifyConfiguration(t *testing.T, client *stdioTestClient, settings map[string]any) {
	t.Helper()
	if err := client.notify("workspace/didChangeConfiguration", map[string]any{"settings": settings}); err != nil {
		t.Fatal(err)
	}
}

func notifyOpenClassicASPDocument(t *testing.T, client *stdioTestClient, uri string, text string) {
	t.Helper()
	if err := client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       text,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func requestInlayHintsText(t *testing.T, client *stdioTestClient, uri string, startLine int, endLine int) string {
	t.Helper()
	return mustJSONText(t, client.request("textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": startLine, "character": 0},
			"end":   map[string]any{"line": endLine, "character": 0},
		},
	}).Result)
}

func workspaceEditChangeURIs(t *testing.T, value any) []string {
	t.Helper()
	var edit struct {
		Changes map[string]any `json:"changes"`
	}
	mustDecodeResult(t, value, &edit)
	keys := make([]string, 0, len(edit.Changes))
	for key := range edit.Changes {
		keys = append(keys, key)
	}
	return keys
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := map[string]int{}
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		if seen[value] == 0 {
			return false
		}
		seen[value]--
	}
	return true
}
