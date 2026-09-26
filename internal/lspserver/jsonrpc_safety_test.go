package lspserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
)

func TestReadMessageRejectsNonObjectBodies(t *testing.T) {
	for _, body := range []string{"null", "[]", `"text"`} {
		t.Run(body, func(t *testing.T) {
			wire := []byte("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body)
			_, err := readMessage(bufio.NewReader(bytes.NewReader(wire)))
			var invalidBodyErr *rpcInvalidRequestBodyError
			if !errors.As(err, &invalidBodyErr) {
				t.Fatalf("readMessage() error = %v, want invalid body", err)
			}
		})
	}
}

func TestReadMessageRejectsOversizedBodiesBeforeAllocation(t *testing.T) {
	wire := []byte("Content-Length: " + strconv.Itoa(maxRPCMessageBytes+1) + "\r\n\r\n")
	_, err := readMessage(bufio.NewReader(bytes.NewReader(wire)))
	var tooLargeErr *rpcBodyTooLargeError
	if !errors.As(err, &tooLargeErr) {
		t.Fatalf("readMessage() error = %v, want oversized body", err)
	}
	if tooLargeErr.length != maxRPCMessageBytes+1 {
		t.Fatalf("oversized body length = %d, want %d", tooLargeErr.length, maxRPCMessageBytes+1)
	}
}

func TestReadMessageRejectsOversizedHeadersBeforeUnboundedBufferGrowth(t *testing.T) {
	wire := []byte("X-Header: " + strings.Repeat("x", maxRPCHeaderBytes+1))
	_, err := readMessage(bufio.NewReader(bytes.NewReader(wire)))
	if !errors.Is(err, errRPCHeaderTooLarge) {
		t.Fatalf("readMessage() error = %v, want oversized header", err)
	}
}

func TestRPCMessageRejectsNullMethod(t *testing.T) {
	var message rpcMessage
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"method":null}`), &message); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, rpcErr := classifyRPCMessage(&message); rpcErr == nil || rpcErr.Code != -32600 {
		t.Fatalf("classifyRPCMessage() error = %#v, want Invalid Request", rpcErr)
	}
}

func TestRPCMessageAcceptsNullRequestID(t *testing.T) {
	var message rpcMessage
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":null,"method":"shutdown"}`), &message); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	kind, rpcErr := classifyRPCMessage(&message)
	if rpcErr != nil || kind != rpcRequest {
		t.Fatalf("classifyRPCMessage() = kind %d, error %#v, want request", kind, rpcErr)
	}
	if !message.idPresent || message.ID != nil {
		t.Fatalf("null request ID state = present=%t id=%#v", message.idPresent, message.ID)
	}
}

func TestServeRespondsToNullRequestID(t *testing.T) {
	var input bytes.Buffer
	writeTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "id": nil, "method": "shutdown"})
	writeTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	server := New(&input, &output, io.Discard)
	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	message, err := readMessage(bufio.NewReader(&output))
	if err != nil {
		t.Fatalf("read null-ID response: %v", err)
	}
	if !message.idPresent || message.ID != nil {
		t.Fatalf("response ID state = present=%t id=%#v", message.idPresent, message.ID)
	}
	if message.Error != nil || !message.resultPresent || message.Result != nil {
		t.Fatalf("null-ID response = %#v, want successful null result", message)
	}
}

func TestServeRespondsToMalformedNoIDEnvelopes(t *testing.T) {
	for _, malformed := range []map[string]any{
		{"jsonrpc": "1.0", "method": "notify"},
		{"jsonrpc": "2.0", "method": nil},
	} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			var input bytes.Buffer
			writeTestMessage(t, &input, malformed)
			writeTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

			var output bytes.Buffer
			server := New(&input, &output, io.Discard)
			if err := server.Serve(context.Background()); err != nil {
				t.Fatalf("Serve() error = %v", err)
			}
			message, err := readMessage(bufio.NewReader(&output))
			if err != nil {
				t.Fatalf("read malformed-envelope response: %v", err)
			}
			if message.Error == nil || message.Error.Code != -32600 {
				t.Fatalf("malformed-envelope response = %#v, want Invalid Request", message)
			}
		})
	}
}

func TestServeRespondsToInvalidBodyAndContinues(t *testing.T) {
	var input bytes.Buffer
	invalidBody := "null"
	input.WriteString("Content-Length: ")
	input.WriteString(strconv.Itoa(len(invalidBody)))
	input.WriteString("\r\n\r\n")
	input.WriteString(invalidBody)
	writeTestMessage(t, &input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	server := New(&input, &output, io.Discard)
	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	message, err := readMessage(bufio.NewReader(&output))
	if err != nil {
		t.Fatalf("read invalid-body response: %v", err)
	}
	if message.Error == nil || message.Error.Code != -32600 {
		t.Fatalf("invalid-body response = %#v, want Invalid Request", message)
	}
}
