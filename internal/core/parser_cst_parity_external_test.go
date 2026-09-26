package core_test

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestBuildsLosslessASPCSTWithEmbeddedVBScriptNodes(t *testing.T) {
	source := `<%@ LANGUAGE="VBScript" %>
<!-- #include file="inc/common.inc" -->
<script runat="server">
Sub Save(value)
End Sub
</script>
<style>.x { color: red; }</style>
<div style="display: block"><%= title %></div>`
	parsed := core.ParseDocument("file:///site/default.asp", source, core.Settings{})

	if parsed.Text != source {
		t.Fatalf("parsed text changed:\n%s", parsed.Text)
	}
	if parsed.DefaultLanguage != core.LanguageVBScript {
		t.Fatalf("default language = %q", parsed.DefaultLanguage)
	}
	if len(parsed.Includes) != 1 || parsed.Includes[0].Path != "inc/common.inc" {
		t.Fatalf("includes = %#v", parsed.Includes)
	}
	if firstRegionOfKind(parsed, core.RegionASPDirective) == nil {
		t.Fatalf("missing ASP directive region: %#v", parsed.Regions)
	}
	if firstRegionOfKind(parsed, core.RegionStyleAttribute) == nil {
		t.Fatalf("missing style attribute region: %#v", parsed.Regions)
	}
	server := firstRegionOfKind(parsed, core.RegionServerScript)
	if server == nil || server.Language != core.LanguageVBScript {
		t.Fatalf("server script region = %#v in %#v", server, parsed.Regions)
	}
	if !strings.Contains(source[server.ContentStart:server.ContentEnd], "Sub Save(value)") {
		t.Fatalf("server script content = %q", source[server.ContentStart:server.ContentEnd])
	}
	if !hasDeclaration(vbscript.DeclarationSymbols(parsed), "Save", "sub") {
		t.Fatalf("missing embedded VBScript Save procedure: %#v", vbscript.DeclarationSymbols(parsed))
	}
}

func firstRegionOfKind(parsed *core.ParsedDocument, kind core.RegionKind) *core.Region {
	for i := range parsed.Regions {
		if parsed.Regions[i].Kind == kind {
			return &parsed.Regions[i]
		}
	}
	return nil
}

func hasDeclaration(symbols []vbscript.Symbol, name string, kind string) bool {
	for _, symbol := range symbols {
		if strings.EqualFold(symbol.Name, name) && symbol.Kind == kind {
			return true
		}
	}
	return false
}
