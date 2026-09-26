package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStdioParityNavigationGraphProtocolSupportsAllScopes(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	folder := filepath.Join(root, "admin")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(folder, "default.asp")
	other := filepath.Join(root, "outside.asp")
	writeFlowchartFixture(t, page, `<a href="next.asp">Next</a>
<a href="/root.asp?name=Ada+Lovelace" target="_blank">Root</a>
<a href="https://example.com/help">Help</a>
<script>location.href = unresolvedTarget;</script>`)
	writeFlowchartFixture(t, filepath.Join(folder, "next.asp"), "")
	writeFlowchartFixture(t, filepath.Join(root, "root.asp"), "")
	writeFlowchartFixture(t, other, `<a href="outside-next.asp">Outside</a>`)
	writeFlowchartFixture(t, filepath.Join(root, "outside-next.asp"), "")
	pageURI := pathToFileURI(page)
	folderURI := pathToFileURI(folder)
	initializeAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})

	document := buildNavigationGraph(t, client, map[string]any{"scope": "document", "uri": pageURI})
	assertNavigationProtocolPayload(t, document)
	if document["rootUri"] != pageURI {
		t.Fatalf("document rootUri = %v, want %s", document["rootUri"], pageURI)
	}
	if !navigationNodeMatching(document, func(node map[string]any) bool { return node["isRoot"] == true && node["kind"] == "page" }) {
		t.Fatalf("document navigation root missing: %s", mustJSONText(t, document))
	}
	if !navigationEdgeMatching(document, func(edge map[string]any) bool {
		target := navigationNodeByID(document, asString(edge["target"]))
		return target["uri"] == pathToFileURI(filepath.Join(root, "root.asp")) &&
			edge["targetFrame"] == "_blank" &&
			navigationEdgeHasParameterValue(edge, "name", "queryString", "Ada Lovelace")
	}) {
		t.Fatalf("root-relative navigation/query payload mismatch: %s", mustJSONText(t, document))
	}
	documentStats := document["stats"].(map[string]any)
	if documentStats["external"] != float64(1) {
		t.Fatalf("external navigation stats = %v, want 1", documentStats["external"])
	}
	if documentStats["unknown"] != float64(1) || !navigationNodeMatching(document, func(node map[string]any) bool {
		return node["kind"] == "unknown" && node["label"] == "unresolvedTarget"
	}) {
		t.Fatalf("dynamic navigation confidence/node mismatch: %s", mustJSONText(t, document))
	}

	folderPayload := buildNavigationGraph(t, client, map[string]any{"scope": "folder", "uri": folderURI})
	assertNavigationProtocolPayload(t, folderPayload)
	if navigationNodeMatching(folderPayload, func(node map[string]any) bool { return node["label"] == "outside.asp" }) {
		t.Fatalf("folder graph leaked outside file: %s", mustJSONText(t, folderPayload))
	}

	workspace := buildNavigationGraph(t, client, map[string]any{"scope": "workspace", "uri": pageURI})
	if _, ok := workspace["rootUri"]; ok {
		t.Fatalf("project graph unexpectedly selected an open file as its root: %s", mustJSONText(t, workspace))
	}
	assertNavigationProtocolPayload(t, workspace)
	if !navigationNodeMatching(workspace, func(node map[string]any) bool { return node["label"] == "outside.asp" }) {
		t.Fatalf("workspace graph missing outside file: %s", mustJSONText(t, workspace))
	}

	notifications := client.drainNotifications("aspLsp/navigationGraphUpdated")
	if len(notifications) != 0 {
		t.Fatalf("navigation graph update notifications = %d, want 0 synchronous duplicate payloads", len(notifications))
	}
}

func TestStdioParityDocumentNavigationGraphIncludesIncomingQueryTargetsAndHonorsProjectGlobs(t *testing.T) {
	client := startStdioTestClient(t)
	defer client.close()

	root := t.TempDir()
	target := filepath.Join(root, "target.asp")
	referrer := filepath.Join(root, "referrer.asp")
	rootReferrer := filepath.Join(root, "nested", "root-referrer.asp")
	includeReferrer := filepath.Join(root, "include-referrer.asp")
	redirectInclude := filepath.Join(root, "redirect.inc")
	firstLinkInclude := filepath.Join(root, "first-link.inc")
	secondLinkInclude := filepath.Join(root, "second-link.inc")
	unrelated := filepath.Join(root, "unrelated.asp")
	excluded := filepath.Join(root, "excluded", "hidden.asp")
	if err := os.MkdirAll(filepath.Dir(excluded), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(rootReferrer), 0o755); err != nil {
		t.Fatal(err)
	}
	targetSource := `<a href="next.asp">Next</a>
<a href="?tab=self">Self query</a>
<a href="#details">Self fragment</a>
<!-- #include file="first-link.inc" -->
<!-- #include file="second-link.inc" -->`
	writeFlowchartFixture(t, target, targetSource)
	writeFlowchartFixture(t, filepath.Join(root, "next.asp"), "")
	writeFlowchartFixture(t, filepath.Join(root, "duplicate.asp"), "")
	writeFlowchartFixture(t, referrer, `<a href="target.asp?mode=edit#details">Edit</a>`)
	writeFlowchartFixture(t, rootReferrer, `<a href="/target.asp?from=root#details">Root edit</a>`)
	writeFlowchartFixture(t, includeReferrer, `<% redirectTarget = "target.asp" %>
<!-- #include file="redirect.inc" -->`)
	writeFlowchartFixture(t, redirectInclude, `<% Response.Redirect redirectTarget %>`)
	writeFlowchartFixture(t, firstLinkInclude, `<a href="duplicate.asp">Duplicate</a>`)
	writeFlowchartFixture(t, secondLinkInclude, `<a href="duplicate.asp">Duplicate</a>`)
	writeFlowchartFixture(t, unrelated, `<a href="other.asp">Other</a>`)
	writeFlowchartFixture(t, filepath.Join(root, "other.asp"), "")
	writeFlowchartFixture(t, excluded, `<a href="../target.asp?mode=hidden">Hidden</a>`)

	initializeWithConfigurationAndWaitForWorkspaceIndex(t, client, map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	}, map[string]any{"aspLsp": map[string]any{
		"workspace": map[string]any{
			"includes": []string{"**/*.asp", "**/*.inc"},
			"excludes": []string{"excluded/**"},
		},
	}})
	openClassicASPDocument(t, client, pathToFileURI(excluded), `<a href="../target.asp?mode=hidden">Hidden</a>`)
	openClassicASPDocument(t, client, pathToFileURI(target), targetSource)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	document := buildNavigationGraph(t, client, map[string]any{
		"scope": "document",
		"uri":   pathToFileURI(target),
	})
	assertNavigationProtocolPayload(t, document)
	if !navigationEdgeMatching(document, func(edge map[string]any) bool {
		source := navigationNodeByID(document, asString(edge["source"]))
		targetNode := navigationNodeByID(document, asString(edge["target"]))
		return source["uri"] == pathToFileURI(referrer) &&
			targetNode["uri"] == pathToFileURI(target) &&
			navigationEdgeHasParameterValue(edge, "mode", "queryString", "edit")
	}) {
		t.Fatalf("document graph missing incoming query target: %s", mustJSONText(t, document))
	}
	if !navigationNodeMatching(document, func(node map[string]any) bool {
		return node["uri"] == pathToFileURI(target) && node["isRoot"] == true && node["exists"] == true && node["label"] == "target.asp"
	}) {
		t.Fatalf("document graph did not mark the selected incoming target as root: %s", mustJSONText(t, document))
	}
	if !navigationEdgeMatching(document, func(edge map[string]any) bool {
		source := navigationNodeByID(document, asString(edge["source"]))
		targetNode := navigationNodeByID(document, asString(edge["target"]))
		return source["uri"] == pathToFileURI(rootReferrer) &&
			targetNode["uri"] == pathToFileURI(target) &&
			navigationEdgeHasParameterValue(edge, "from", "queryString", "root")
	}) {
		t.Fatalf("document graph missing incoming root-relative query target: %s", mustJSONText(t, document))
	}
	if !navigationEdgeMatching(document, func(edge map[string]any) bool {
		source := navigationNodeByID(document, asString(edge["source"]))
		targetNode := navigationNodeByID(document, asString(edge["target"]))
		return edge["kind"] == "serverRedirect" &&
			source["uri"] == pathToFileURI(includeReferrer) &&
			targetNode["uri"] == pathToFileURI(target) &&
			edge["declaredInUri"] == pathToFileURI(redirectInclude)
	}) {
		t.Fatalf("document graph missing incoming include-state redirect: %s", mustJSONText(t, document))
	}
	if !navigationEdgeMatching(document, func(edge map[string]any) bool {
		source := navigationNodeByID(document, asString(edge["source"]))
		targetNode := navigationNodeByID(document, asString(edge["target"]))
		return source["uri"] == pathToFileURI(target) &&
			targetNode["uri"] == pathToFileURI(target) &&
			navigationEdgeHasParameterValue(edge, "tab", "queryString", "self")
	}) {
		t.Fatalf("document graph missing same-file query target: %s", mustJSONText(t, document))
	}
	if !navigationEdgeMatching(document, func(edge map[string]any) bool {
		source := navigationNodeByID(document, asString(edge["source"]))
		targetNode := navigationNodeByID(document, asString(edge["target"]))
		return source["uri"] == pathToFileURI(target) &&
			targetNode["uri"] == pathToFileURI(target) &&
			strings.Contains(mustJSONText(t, edge["evidence"]), "#details")
	}) {
		t.Fatalf("document graph missing same-file fragment target: %s", mustJSONText(t, document))
	}
	if !navigationEdgeMatching(document, func(edge map[string]any) bool {
		source := navigationNodeByID(document, asString(edge["source"]))
		targetNode := navigationNodeByID(document, asString(edge["target"]))
		if source["uri"] != pathToFileURI(target) || targetNode["uri"] != pathToFileURI(filepath.Join(root, "duplicate.asp")) {
			return false
		}
		evidence, _ := edge["evidence"].([]any)
		seen := map[string]bool{}
		for _, item := range evidence {
			value, _ := item.(map[string]any)
			seen[asString(value["uri"])] = true
		}
		return seen[pathToFileURI(firstLinkInclude)] && seen[pathToFileURI(secondLinkInclude)]
	}) {
		t.Fatalf("document graph did not merge evidence from both included files: %s", mustJSONText(t, document))
	}
	if stats, _ := document["stats"].(map[string]any); stats["documents"] != float64(9) {
		t.Fatalf("document graph document count = %v, want six endpoint files and three include evidence files", stats["documents"])
	}
	for _, hiddenLabel := range []string{"hidden.asp", "unrelated.asp", "other.asp"} {
		if navigationNodeMatching(document, func(node map[string]any) bool { return node["label"] == hiddenLabel }) {
			t.Fatalf("document graph leaked unrelated or excluded node %s: %s", hiddenLabel, mustJSONText(t, document))
		}
	}

	workspace := buildNavigationGraph(t, client, map[string]any{"scope": "workspace"})
	if navigationNodeMatching(workspace, func(node map[string]any) bool { return node["label"] == "hidden.asp" }) {
		t.Fatalf("project graph included an explicitly opened excluded file: %s", mustJSONText(t, workspace))
	}
	if !navigationNodeMatching(workspace, func(node map[string]any) bool { return node["label"] == "referrer.asp" }) {
		t.Fatalf("project graph omitted an included file: %s", mustJSONText(t, workspace))
	}
}

func assertNavigationProtocolPayload(t *testing.T, payload map[string]any) {
	t.Helper()
	stats, ok := payload["stats"].(map[string]any)
	if !ok {
		t.Fatalf("navigation stats missing: %s", mustJSONText(t, payload))
	}
	for _, key := range []string{"documents", "nodes", "edges", "certain", "probable", "possible", "unknown", "external"} {
		if _, ok := stats[key]; !ok {
			t.Fatalf("navigation stats missing %s: %s", key, mustJSONText(t, payload))
		}
	}
	edges, _ := payload["edges"].([]any)
	for _, value := range edges {
		edge, _ := value.(map[string]any)
		for _, key := range []string{"confidence", "ranges", "evidence", "declaredInUri"} {
			if _, ok := edge[key]; !ok {
				t.Fatalf("navigation edge missing %s: %s", key, mustJSONText(t, edge))
			}
		}
		evidence, _ := edge["evidence"].([]any)
		if len(evidence) == 0 {
			t.Fatalf("navigation edge has no evidence: %s", mustJSONText(t, edge))
		}
	}
}
