package lspserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestNamingDeclarationsCachePreservesCallerOwnership(t *testing.T) {
	parsed := core.ParseDocument("file:///naming.asp", "<% Dim value\nvalue = 1 %>", core.Settings{DefaultLanguage: "VBScript"})
	expected := collectVBNamingDeclarations(parsed)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			actual := collectVBNamingDeclarations(parsed)
			if !reflect.DeepEqual(actual, expected) {
				t.Error("cached declarations differ")
			}
			actual[0].Name = "changed"
			actual = append(actual, vbUsageDeclaration{Name: "extra"})
		})
	}
	workers.Wait()
	if actual := collectVBNamingDeclarations(parsed); !reflect.DeepEqual(actual, expected) {
		t.Fatal("caller mutated cached declarations")
	}
	restored := core.ParseDocument(parsed.URI, parsed.Text, core.Settings{DefaultLanguage: "VBScript"})
	restored.StoreAnalysis("lspserver.vb-naming-declarations.v1", expected)
	if actual := collectVBNamingDeclarations(restored); !reflect.DeepEqual(actual, expected) {
		t.Fatal("persisted declarations differ")
	}
}

func TestLiteralAssignmentIndexPreservesScopeAndRevision(t *testing.T) {
	for _, literal := range []string{"1", "2"} {
		parsed := core.ParseDocument("file:///literal.asp", "<% Dim Value\nVALUE = "+literal+"\nSub Example()\nDim value\nvalue = \"local\"\nEnd Sub %>", core.Settings{DefaultLanguage: "VBScript"})
		assignments := vbscriptAssignments(parsed)
		for _, declaration := range collectVBNamingDeclarations(parsed) {
			var expected []int
			for index, assignment := range assignments {
				if strings.EqualFold(assignment.Name, declaration.Name) && strings.EqualFold(assignment.Scope, declaration.Scope) {
					expected = append(expected, index)
				}
			}
			if actual := vbscriptLiteralAssignmentIndices(parsed, assignments, declaration); !reflect.DeepEqual(actual, expected) {
				t.Fatalf("%s/%s: got %v, want %v", declaration.Scope, declaration.Name, actual, expected)
			}
		}
		if actual := vbscriptLiteralUnionTypeForDeclaration(parsed, vbUsageDeclaration{Name: "value"}); actual != literal {
			t.Fatalf("global literal = %q, want %q", actual, literal)
		}
	}
}

func TestRuntimeLogSpansKeepParallelParents(t *testing.T) {
	ctx, root := newRuntimeLogSpan(context.Background())
	var workers sync.WaitGroup
	ids := sync.Map{}
	for range 32 {
		workers.Go(func() {
			childCtx, child := newRuntimeLogSpan(ctx)
			_, grandchild := newRuntimeLogSpan(childCtx)
			if child.parentID != root.id || grandchild.parentID != child.id || grandchild.traceID != root.traceID {
				t.Error("span parent chain differs")
			}
			for _, span := range []runtimeLogSpan{child, grandchild} {
				if _, duplicate := ids.LoadOrStore(span.id, true); duplicate {
					t.Error("duplicate span ID")
				}
			}
		})
	}
	workers.Wait()
}

func BenchmarkInitialDeclarationAnalysis(b *testing.B) {
	source := benchmarkClassicASPLSPDocument(500)
	for b.Loop() {
		parsed := core.ParseDocument("file:///cold.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		lspPerformanceBenchmarkSink = summarizeVBScriptFileAnalysis(parsed)
		lspPerformanceBenchmarkSink = collectVBNamingDeclarations(parsed)
	}
}

func TestDiagnosticLogsEmitParentSpansToOutput(t *testing.T) {
	var output bytes.Buffer
	server := New(nil, &output, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.settings.DebugOutput = "verbose"
	uri := "file:///cold-log.asp"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<% Dim value\nvalue = 1 %>")
	ctx, request := newRuntimeLogSpan(context.Background())
	if err := server.publishFinalDiagnosticsContext(ctx, uri, false, false, 1); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	pattern := regexp.MustCompile(`(spanId|parentSpanId|traceId)=([^ ]+)`)
	records := map[string]map[string]string{}
	for {
		message, err := readMessage(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if message.Method != "window/logMessage" {
			continue
		}
		var params struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		for _, match := range pattern.FindAllStringSubmatch(params.Message, -1) {
			fields[match[1]] = match[2]
		}
		for _, event := range []string{"LSP check started:", "check.diagnostics.started", "check.parser.started"} {
			if strings.Contains(params.Message, event) {
				records[event] = fields
			}
		}
	}
	total := records["LSP check started:"]
	diagnostics := records["check.diagnostics.started"]
	parser := records["check.parser.started"]
	if total["parentSpanId"] != request.id || diagnostics["parentSpanId"] != total["spanId"] || parser["parentSpanId"] != diagnostics["spanId"] || parser["traceId"] != request.traceID {
		t.Fatalf("incorrect log hierarchy: %#v", records)
	}
}

func TestLiteralAssignmentFoldMatchesEqualFold(t *testing.T) {
	names := []string{"Key", "key", "Key", "Scope", "ſcope", "I", "ı", "Σ", "σ", "ς"}
	for _, a := range names {
		for _, b := range names {
			if (literalAssignmentFoldKey(a) == literalAssignmentFoldKey(b)) != strings.EqualFold(a, b) {
				t.Fatalf("fold mismatch for %q and %q", a, b)
			}
		}
	}
}
