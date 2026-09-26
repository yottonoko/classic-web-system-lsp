package lspserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityRebuildsEmbeddedFeaturesImmediatelyForTypedBoundaryTags(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	javascriptURI := pathToFileURI(filepath.Join(root, "typed-script-boundary.asp"))
	javascriptSource := `<script
const boundaryValue = 1;
boundaryValue;
boundaryV
</script>`
	openClassicASPDocument(t, client, javascriptURI, javascriptSource)
	javascript := notifyTypedInsertion(t, client, javascriptURI, javascriptSource, 1, len("<script"), ">")
	assertEmbeddedBoundaryJavaScriptFeatures(t, client, javascriptURI, javascript.Text, true)

	cssURI := pathToFileURI(filepath.Join(root, "typed-style-boundary.asp"))
	cssSource := `<style
.boundary { color: red; colo }
</style>`
	openClassicASPDocument(t, client, cssURI, cssSource)
	css := notifyTypedInsertion(t, client, cssURI, cssSource, 1, len("<style"), ">")
	assertEmbeddedBoundaryCSSFeatures(t, client, cssURI, css.Text, true)
}

func TestStdioParityInvalidatesAndRestoresEmbeddedFeaturesForRepeatedBoundaryEdits(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	client.request("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      pathToFileURI(root),
		"capabilities": map[string]any{},
	})

	javascriptURI := pathToFileURI(filepath.Join(root, "repeated-script-boundary.asp"))
	javascriptSource := `<script>
const stableValue = 1;
stableValue;
</script>
<script id="boundary-script">
const boundaryValue = 1;
boundaryValue;
boundaryV
</script>`
	openClassicASPDocument(t, client, javascriptURI, javascriptSource)
	assertEmbeddedBoundaryJavaScriptFeatures(t, client, javascriptURI, javascriptSource, true)
	javascriptVersion := 1
	for range 3 {
		javascriptVersion++
		javascriptSource = notifyNeedleReplacement(t, client, javascriptURI, javascriptSource, javascriptVersion, `<script id="boundary-script"`, `<scrip id="boundary-script"`)
		assertEmbeddedBoundaryJavaScriptFeatures(t, client, javascriptURI, javascriptSource, false)
		javascriptVersion++
		javascriptSource = notifyNeedleReplacement(t, client, javascriptURI, javascriptSource, javascriptVersion, `<scrip id="boundary-script"`, `<script id="boundary-script"`)
		assertEmbeddedBoundaryJavaScriptFeatures(t, client, javascriptURI, javascriptSource, true)
	}

	cssURI := pathToFileURI(filepath.Join(root, "repeated-style-boundary.asp"))
	cssSource := `<style>
.stable { display: block; }
</style>
<style id="boundary-style">
.boundary { color: red; colo }
</style>`
	openClassicASPDocument(t, client, cssURI, cssSource)
	assertEmbeddedBoundaryCSSFeatures(t, client, cssURI, cssSource, true)
	cssVersion := 1
	for range 3 {
		cssVersion++
		cssSource = notifyNeedleReplacement(t, client, cssURI, cssSource, cssVersion, `<style id="boundary-style"`, `<styl id="boundary-style"`)
		assertEmbeddedBoundaryCSSFeatures(t, client, cssURI, cssSource, false)
		cssVersion++
		cssSource = notifyNeedleReplacement(t, client, cssURI, cssSource, cssVersion, `<styl id="boundary-style"`, `<style id="boundary-style"`)
		assertEmbeddedBoundaryCSSFeatures(t, client, cssURI, cssSource, true)
	}
}

func assertEmbeddedBoundaryJavaScriptFeatures(t *testing.T, client *stdioTestClient, uri, source string, available bool) {
	t.Helper()
	completion := requestCompletionAtSuffix(t, client, uri, source, "boundaryV")
	hasCompletion := strings.Contains(mustJSONText(t, completion.Result), "boundaryValue")
	hoverOffset := strings.Index(source, "boundaryValue;")
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, hoverOffset+len("boundary")),
	})
	hasHover := strings.Contains(mustJSONText(t, hover.Result), "boundaryValue")
	if hasCompletion != available || hasHover != available {
		t.Fatalf("JavaScript boundary features: completion=%v hover=%v want available=%v; completion=%s hover=%s", hasCompletion, hasHover, available, mustJSONText(t, completion.Result), mustJSONText(t, hover.Result))
	}
}

func assertEmbeddedBoundaryCSSFeatures(t *testing.T, client *stdioTestClient, uri, source string, available bool) {
	t.Helper()
	completion := requestCompletionAtSuffix(t, client, uri, source, "colo")
	hasCompletion := completionLabels(completion.Result).contains("color")
	hoverOffset := strings.Index(source, "color: red")
	hover := client.request("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     positionAt(source, hoverOffset+len("color")),
	})
	hasHover := hover.Result != nil && mustJSONText(t, hover.Result) != "null"
	if hasCompletion != available || hasHover != available {
		t.Fatalf("CSS boundary features: completion=%v hover=%v want available=%v; completion=%s hover=%s", hasCompletion, hasHover, available, mustJSONText(t, completion.Result), mustJSONText(t, hover.Result))
	}
}
