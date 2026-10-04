package lspserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type rpcMessage struct {
	logSpan runtimeLogSpan
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`

	// These fields preserve the distinction between an omitted member and a
	// member whose value happens to be null.  That distinction is required to
	// classify requests, notifications, and client responses correctly.
	idPresent     bool
	methodPresent bool
	resultPresent bool
	errorPresent  bool
	invalidFields bool

	// Revision-advancing notifications are prepared once when they enter the
	// server. Their params are immutable after UnmarshalJSON, so routing and
	// dispatch can share the validation result instead of decoding didChange
	// repeatedly on the reader and worker paths.
	revisionNotificationChecked bool
	revisionNotificationValid   bool
	revisionNotificationErr     error
	didChangeParams             *didChangeParams
	didOpenParams               *didOpenParams
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMessageKind uint8

const (
	rpcRequest rpcMessageKind = iota + 1
	rpcNotification
	rpcResponse
)

const maxRPCMessageBytes = 64 * 1024 * 1024
const maxRPCHeaderBytes = 64 * 1024

var errRPCHeaderTooLarge = fmt.Errorf("JSON-RPC header exceeds %d bytes", maxRPCHeaderBytes)

type rpcBodyParseError struct {
	err error
}

type rpcInvalidRequestBodyError struct{}

func (e *rpcInvalidRequestBodyError) Error() string {
	return "JSON-RPC message must be an object"
}

type rpcBodyTooLargeError struct {
	length int
}

func (e *rpcBodyTooLargeError) Error() string {
	return fmt.Sprintf("JSON-RPC message exceeds %d bytes", maxRPCMessageBytes)
}

func (e *rpcBodyParseError) Error() string {
	if e == nil || e.err == nil {
		return "parse error"
	}
	return "parse error: " + e.err.Error()
}

func (e *rpcBodyParseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

type malformedNotificationError struct {
	err error
}

func (e *malformedNotificationError) Error() string {
	if e == nil || e.err == nil {
		return "malformed notification"
	}
	return e.err.Error()
}

func (e *malformedNotificationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func malformedNotification(err error) error {
	if err == nil {
		return nil
	}
	return &malformedNotificationError{err: err}
}

func decodeNotificationParams(params json.RawMessage, target any) error {
	if err := json.Unmarshal(params, target); err != nil {
		return malformedNotification(err)
	}
	return nil
}

func requireObjectParams(params json.RawMessage) error {
	if len(bytes.TrimSpace(params)) == 0 || bytes.Equal(bytes.TrimSpace(params), []byte("null")) {
		return fmt.Errorf("params must be an object")
	}
	// Equivalent to decoding into a non-nil map, without copying every member.
	if trimmed := bytes.TrimSpace(params); trimmed[0] != '{' || !json.Valid(trimmed) {
		return fmt.Errorf("params must be an object")
	}
	return nil
}

func validateRequestParams(method string, params json.RawMessage) *rpcError {
	if !requestParamsObjectRequired(method) {
		return nil
	}
	if err := requireObjectParams(params); err != nil {
		return invalidParams(err)
	}
	return nil
}

func requestParamsObjectRequired(method string) bool {
	switch method {
	case "initialize",
		"aspLsp/textDocument/lineCommentEdits",
		"textDocument/completion", "completionItem/resolve", "codeLens/resolve",
		"codeAction/resolve", "documentLink/resolve", "inlayHint/resolve",
		"textDocument/hover", "textDocument/definition", "textDocument/declaration",
		"textDocument/typeDefinition", "textDocument/implementation", "textDocument/references",
		"textDocument/documentHighlight", "textDocument/selectionRange", "textDocument/documentColor",
		"textDocument/inlayHint", "textDocument/codeLens", "textDocument/codeAction",
		"textDocument/inlineValue", "textDocument/signatureHelp", "textDocument/linkedEditingRange",
		"textDocument/prepareRename", "textDocument/rename", "workspace/willRenameFiles",
		"textDocument/onTypeFormatting", "textDocument/willSaveWaitUntil", "workspace/symbol",
		"textDocument/colorPresentation", "callHierarchy/incomingCalls", "callHierarchy/outgoingCalls",
		"typeHierarchy/supertypes", "typeHierarchy/subtypes", "textDocument/prepareCallHierarchy",
		"textDocument/moniker", "textDocument/prepareTypeHierarchy", "textDocument/formatting",
		"textDocument/rangeFormatting", "textDocument/documentSymbol", "textDocument/foldingRange",
		"textDocument/documentLink", "textDocument/semanticTokens/full", "textDocument/semanticTokens/range",
		"textDocument/semanticTokens/full/delta", "textDocument/diagnostic", "workspace/diagnostic",
		"workspace/executeCommand":
		return true
	default:
		return false
	}
}

func validateNotificationParams(method string, params json.RawMessage) error {
	switch method {
	case "workspace/didChangeWorkspaceFolders", "workspace/didRenameFiles",
		"workspace/didCreateFiles", "workspace/didDeleteFiles", "workspace/didChangeWatchedFiles",
		"workspace/didChangeConfiguration", "textDocument/willSave", "textDocument/didOpen",
		"textDocument/didChange", "textDocument/didSave", "textDocument/didClose":
		return requireObjectParams(params)
	default:
		return nil
	}
}

func (m *rpcMessage) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("JSON-RPC message must be an object")
	}
	*m = rpcMessage{}
	if raw, ok := fields["jsonrpc"]; ok {
		if err := json.Unmarshal(raw, &m.JSONRPC); err != nil {
			m.invalidFields = true
		}
	}
	if raw, ok := fields["id"]; ok {
		m.idPresent = true
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if err := json.Unmarshal(raw, &m.ID); err != nil {
				m.invalidFields = true
			}
		}
	}
	if raw, ok := fields["method"]; ok {
		m.methodPresent = true
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &m.Method) != nil {
			m.invalidFields = true
		}
	}
	if raw, ok := fields["params"]; ok {
		m.Params = append(m.Params[:0], raw...)
	}
	if raw, ok := fields["result"]; ok {
		m.resultPresent = true
		if err := json.Unmarshal(raw, &m.Result); err != nil {
			m.invalidFields = true
		}
	}
	if raw, ok := fields["error"]; ok {
		m.errorPresent = true
		if string(raw) == "null" || json.Unmarshal(raw, &m.Error) != nil || m.Error == nil {
			m.invalidFields = true
		}
	}
	return nil
}

func classifyRPCMessage(message *rpcMessage) (rpcMessageKind, *rpcError) {
	if message == nil || message.invalidFields || message.JSONRPC != "2.0" {
		return 0, invalidRPCRequestError()
	}
	if message.methodPresent {
		if message.resultPresent || message.errorPresent {
			return 0, invalidRPCRequestError()
		}
		if message.idPresent {
			if !validRPCRequestID(message.ID) {
				return 0, invalidRPCRequestError()
			}
			return rpcRequest, nil
		}
		return rpcNotification, nil
	}
	if !message.idPresent || message.resultPresent == message.errorPresent {
		return 0, invalidRPCRequestError()
	}
	if !validRPCResponseID(message.ID) {
		return 0, invalidRPCRequestError()
	}
	return rpcResponse, nil
}

func validRPCRequestID(id any) bool {
	if id == nil {
		return true
	}
	switch id.(type) {
	case string, float64, json.Number, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}

func validRPCResponseID(id any) bool {
	return id == nil || validRPCRequestID(id)
}

func rpcMessageIDForWire(message *rpcMessage) any {
	if message == nil {
		return json.RawMessage("null")
	}
	if message.idPresent && message.ID == nil {
		return json.RawMessage("null")
	}
	return message.ID
}

func invalidRPCRequestError() *rpcError {
	return &rpcError{Code: -32600, Message: "invalid request"}
}

func parseError() *rpcError {
	return &rpcError{Code: -32700, Message: "parse error"}
}

func readMessage(reader *bufio.Reader) (*rpcMessage, error) {
	contentLength := -1
	headerBytes := 0
	for {
		lineBytes, err := readRPCHeaderLine(reader)
		if err != nil {
			return nil, err
		}
		headerBytes += len(lineBytes)
		if headerBytes > maxRPCHeaderBytes {
			return nil, errRPCHeaderTooLarge
		}
		line := string(lineBytes)
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid header %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	if contentLength > maxRPCMessageBytes {
		return nil, &rpcBodyTooLargeError{length: contentLength}
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, &rpcInvalidRequestBodyError{}
	}
	var message rpcMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, &rpcBodyParseError{err: err}
	}
	return &message, nil
}

func readRPCHeaderLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 128)
	for {
		fragment, prefix, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(line)+len(fragment) > maxRPCHeaderBytes {
			return nil, errRPCHeaderTooLarge
		}
		line = append(line, fragment...)
		if !prefix {
			return line, nil
		}
	}
}

func writeMessage(writer io.Writer, message rpcMessage) error {
	encoded, err := encodeMessage(message)
	if err != nil {
		return err
	}
	_, err = writer.Write(encoded)
	return err
}

func encodeMessage(message rpcMessage) ([]byte, error) {
	if message.JSONRPC == "" {
		message.JSONRPC = "2.0"
	}
	body, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "Content-Length: %d\r\n\r\n", len(body))
	buffer.Write(body)
	return buffer.Bytes(), nil
}
