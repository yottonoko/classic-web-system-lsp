package lspserver

import (
	"context"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func (s *Server) flowchartIncludesContext(ctx context.Context, parsed *core.ParsedDocument) ([]map[string]any, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil, ctx.Err() == nil
	}
	includes := make([]map[string]any, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		if ctx.Err() != nil {
			return nil, false
		}
		item := map[string]any{
			"path":  include.Path,
			"mode":  include.Mode,
			"range": include.Range,
		}
		if details, ok := s.includeTargetDetailsForModeContext(ctx, parsed.URI, include.Path, include.Mode); ok {
			item["exists"] = details.Exists
			item["pathCaseMatches"] = !details.CaseMismatch
			if details.Path != "" {
				item["resolvedPath"] = details.Path
				item["resolvedUri"] = filePathURI(details.Path)
				if details.Exists {
					item["actualPath"] = s.graphDisplayFileName(details.Path)
				}
			}
		} else {
			item["exists"] = false
			item["pathCaseMatches"] = true
		}
		includes = append(includes, item)
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return includes, true
}

func flowchartMermaid(sections []map[string]any, nodes []map[string]any, edges []map[string]any, labelLineLength int) string {
	var out strings.Builder
	out.WriteString("flowchart TB\n")
	nodesByID := make(map[string]map[string]any, len(nodes))
	for _, node := range nodes {
		if id, _ := node["id"].(string); id != "" {
			nodesByID[id] = node
		}
	}
	written := map[string]struct{}{}
	for _, section := range sections {
		id, _ := section["id"].(string)
		label, _ := section["label"].(string)
		if id == "" || label == "" {
			continue
		}
		out.WriteString("  subgraph ")
		out.WriteString(id)
		out.WriteString("[\"")
		out.WriteString(flowchartMermaidLabel(label, labelLineLength))
		out.WriteString("\"]\n")
		for _, nodeID := range flowchartStringIDs(section["nodeIds"]) {
			if node := nodesByID[nodeID]; node != nil {
				flowchartWriteMermaidNode(&out, node, labelLineLength, "    ")
				written[nodeID] = struct{}{}
			}
		}
		out.WriteString("  end\n")
	}
	for _, node := range nodes {
		id, _ := node["id"].(string)
		if id == "" {
			continue
		}
		if _, ok := written[id]; ok {
			continue
		}
		flowchartWriteMermaidNode(&out, node, labelLineLength, "  ")
	}
	for _, edge := range edges {
		source, _ := edge["source"].(string)
		target, _ := edge["target"].(string)
		label, _ := edge["label"].(string)
		if source == "" || target == "" {
			continue
		}
		out.WriteString("  ")
		out.WriteString(source)
		if label == "" {
			out.WriteString(" --> ")
		} else {
			out.WriteString(" -- \"")
			out.WriteString(flowchartMermaidEscape(label))
			out.WriteString("\" --> ")
		}
		out.WriteString(target)
		out.WriteByte('\n')
	}
	return out.String()
}

func flowchartWriteMermaidNode(out *strings.Builder, node map[string]any, labelLineLength int, indent string) {
	id, _ := node["id"].(string)
	label, _ := node["label"].(string)
	if id == "" || label == "" {
		return
	}
	out.WriteString(indent)
	out.WriteString(id)
	kind, _ := node["kind"].(string)
	open, close := "[\"", "\"]"
	switch kind {
	case "if", "elseif", "select", "case", "for", "forEach", "do", "while":
		open, close = "{\"", "\"}"
	case "start", "end":
		open, close = "([\"", "\"])"
	case "merge":
		open, close = "((\"", "\"))"
	}
	out.WriteString(open)
	out.WriteString(flowchartMermaidLabel(label, labelLineLength))
	out.WriteString(close)
	out.WriteByte('\n')
}

func flowchartMermaidLabel(label string, lineLength int) string {
	parts := splitFlowchartLabel(label, lineLength)
	for index := range parts {
		parts[index] = flowchartMermaidEscape(parts[index])
	}
	return strings.Join(parts, "<br/>")
}

func flowchartMermaidEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"\"", "&quot;",
		"<", "&lt;",
		">", "&gt;",
		"|", "&#124;",
		"\r", " ",
		"\n", "<br/>",
	)
	return replacer.Replace(value)
}

func flowchartStringIDs(value any) []string {
	switch ids := value.(type) {
	case []string:
		return ids
	case []any:
		result := make([]string, 0, len(ids))
		for _, value := range ids {
			if id, ok := value.(string); ok {
				result = append(result, id)
			}
		}
		return result
	default:
		return nil
	}
}
