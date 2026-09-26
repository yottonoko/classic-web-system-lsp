package lspserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptTypeDefinitionFollowsIncludeExecutionOrder(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
		want   string
	}{
		{name: "a then b", first: "a.inc", second: "b.inc", want: "B"},
		{name: "b then a", first: "b.inc", second: "a.inc", want: "A"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTypeDefinitionInclude(t, root, "a.inc", "A")
			writeTypeDefinitionInclude(t, root, "b.inc", "B")
			ownerPath := filepath.Join(root, "default.asp")
			ownerSource := fmt.Sprintf("<%%\nDim value\n%%>\n<!-- #include file=\"%s\" -->\n<!-- #include file=\"%s\" -->\n<%% value %%>", test.first, test.second)
			writeTypeDefinitionFile(t, ownerPath, ownerSource)

			server, ownerURI := typeDefinitionFixtureServer(t, root, ownerPath, ownerSource)
			position := core.NewTextDocument(ownerURI, "classic-asp", 0, ownerSource).PositionAt(strings.LastIndex(ownerSource, "value"))
			locations := server.typeDefinitionContext(context.Background(), ownerURI, position)
			if len(locations) != 1 {
				t.Fatalf("typeDefinition locations = %#v, want one %s class", locations, test.want)
			}
			if locations[0].URI != filePathURI(filepath.Join(root, test.second)) {
				t.Fatalf("typeDefinition URI = %q, want %s include", locations[0].URI, test.want)
			}
			if locations[0].Range.Start.Line != 1 {
				t.Fatalf("typeDefinition range = %#v, want class declaration on line 1", locations[0].Range)
			}
		})
	}
}

func TestVBScriptTypeDefinitionUsesOwnerUnionContract(t *testing.T) {
	root := t.TempDir()
	writeTypeDefinitionInclude(t, root, "shared.inc", "Child")
	writeTypeDefinitionFile(t, filepath.Join(root, "OwnerA.inc"), "<%\nClass OwnerA\nEnd Class\n%>")
	writeTypeDefinitionFile(t, filepath.Join(root, "OwnerB.inc"), "<%\nClass OwnerB\nEnd Class\n%>")
	ownerPath := filepath.Join(root, "default.asp")
	ownerSource := "<%\n' @type value As OwnerA | OwnerB\nDim value\n%>\n<!-- #include file=\"OwnerA.inc\" -->\n<!-- #include file=\"shared.inc\" -->\n<!-- #include file=\"OwnerB.inc\" -->\n<% value %>"
	writeTypeDefinitionFile(t, ownerPath, ownerSource)

	server, ownerURI := typeDefinitionFixtureServer(t, root, ownerPath, ownerSource)
	position := core.NewTextDocument(ownerURI, "classic-asp", 0, ownerSource).PositionAt(strings.LastIndex(ownerSource, "value"))
	locations := server.typeDefinitionContext(context.Background(), ownerURI, position)
	if len(locations) != 2 {
		t.Fatalf("owner-contract typeDefinition locations = %#v, want two union arms", locations)
	}
	want := []string{filePathURI(filepath.Join(root, "OwnerA.inc")), filePathURI(filepath.Join(root, "OwnerB.inc"))}
	for index, location := range locations {
		if location.URI != want[index] {
			t.Fatalf("owner-contract location %d URI = %q, want %q", index, location.URI, want[index])
		}
		if location.Range.Start.Line != 1 {
			t.Fatalf("owner-contract location %d range = %#v, want class declaration on line 1", index, location.Range)
		}
	}
}

func TestVBScriptTypeDefinitionKeepsAssignmentsAcrossLaterDim(t *testing.T) {
	root := t.TempDir()
	writeTypeDefinitionInclude(t, root, "a.inc", "A")
	writeTypeDefinitionInclude(t, root, "b.inc", "B")
	ownerPath := filepath.Join(root, "default.asp")
	ownerSource := "<!-- #include file=\"a.inc\" -->\n<!-- #include file=\"b.inc\" -->\n<% Dim value\nvalue %>"
	writeTypeDefinitionFile(t, ownerPath, ownerSource)
	server, ownerURI := typeDefinitionFixtureServer(t, root, ownerPath, ownerSource)
	position := core.NewTextDocument(ownerURI, "classic-asp", 0, ownerSource).PositionAt(strings.LastIndex(ownerSource, "value"))
	locations := server.typeDefinitionContext(context.Background(), ownerURI, position)
	if len(locations) != 1 || locations[0].URI != filePathURI(filepath.Join(root, "b.inc")) {
		t.Fatalf("typeDefinition after later Dim = %#v, want B include", locations)
	}
}

func TestVBScriptTypeDefinitionDoesNotPublishIncompleteOrCancelledIncludes(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	for index := 1; index <= 14; index++ {
		next := ""
		if index < 14 {
			next = fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", index+1, index+1)
		}
		source := next + fmt.Sprintf("<%% Set value = New Level%d %%>", index)
		writeTypeDefinitionFile(t, filepath.Join(root, fmt.Sprintf("level-%d.inc", index)), source)
	}
	ownerPath := filepath.Join(root, "default.asp")
	ownerSource := "<!-- #include file=\"level-1.inc\" -->\n<!-- #include file=\"level-1.inc\" -->\n<% value %>"
	writeTypeDefinitionFile(t, ownerPath, ownerSource)
	ownerURI := filePathURI(ownerPath)
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	position := core.NewTextDocument(ownerURI, "classic-asp", 0, ownerSource).PositionAt(strings.LastIndex(ownerSource, "value"))

	locations := server.typeDefinitionContext(context.Background(), ownerURI, position)
	if locations != nil {
		t.Fatalf("incomplete include typeDefinition locations = %#v, want no publication", locations)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if locations := server.typeDefinitionContext(cancelled, ownerURI, position); locations != nil {
		t.Fatalf("cancelled include typeDefinition locations = %#v, want no publication", locations)
	}
}

func writeTypeDefinitionInclude(t *testing.T, root, name, className string) {
	t.Helper()
	writeTypeDefinitionFile(t, filepath.Join(root, name), fmt.Sprintf("<%%\nClass %s\nEnd Class\nSet value = New %s\n%%>", className, className))
}

func writeTypeDefinitionFile(t *testing.T, path, source string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func typeDefinitionFixtureServer(t *testing.T, root, ownerPath, ownerSource string) (*Server, string) {
	t.Helper()
	ownerURI := filePathURI(ownerPath)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	return server, ownerURI
}
