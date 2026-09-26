package graph

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type Payload struct {
	Scope            string         `json:"scope"`
	URI              string         `json:"uri,omitempty"`
	RootURI          string         `json:"rootUri,omitempty"`
	Nodes            []Node         `json:"nodes"`
	Edges            []Edge         `json:"edges"`
	Links            []Edge         `json:"links,omitempty"`
	Stats            map[string]int `json:"stats,omitempty"`
	Settings         map[string]any `json:"settings,omitempty"`
	Pending          *bool          `json:"pending,omitempty"`
	CorrelationID    string         `json:"correlationId,omitempty"`
	BackgroundTaskID string         `json:"backgroundTaskId,omitempty"`
}

type Node struct {
	ID                      string      `json:"id"`
	Label                   string      `json:"label"`
	Kind                    string      `json:"kind"`
	URI                     string      `json:"uri,omitempty"`
	Range                   *lsp.Range  `json:"range,omitempty"`
	SourceRange             *lsp.Range  `json:"sourceRange,omitempty"`
	FileName                string      `json:"fileName,omitempty"`
	Exists                  *bool       `json:"exists,omitempty"`
	DeclarationKind         string      `json:"declarationKind,omitempty"`
	Group                   string      `json:"group,omitempty"`
	ExternalKind            string      `json:"externalKind,omitempty"`
	Role                    string      `json:"role,omitempty"`
	ReceiverName            string      `json:"receiverName,omitempty"`
	MemberName              string      `json:"memberName,omitempty"`
	FullPath                string      `json:"fullPath,omitempty"`
	BindingScope            string      `json:"bindingScope,omitempty"`
	Implicit                bool        `json:"implicit,omitempty"`
	ImplicitGlobal          bool        `json:"implicitGlobal,omitempty"`
	ImplicitGlobalCandidate bool        `json:"implicitGlobalCandidate,omitempty"`
	TypeName                string      `json:"typeName,omitempty"`
	ArrayKind               string      `json:"arrayKind,omitempty"`
	ArrayDimensions         *[]string   `json:"arrayDimensions,omitempty"`
	Parameters              []Parameter `json:"parameters,omitempty"`
	Origin                  string      `json:"origin,omitempty"`
	MemberOf                string      `json:"memberOf,omitempty"`
	ProcedureKind           string      `json:"procedureKind,omitempty"`
	IsRoot                  bool        `json:"isRoot,omitempty"`
}

type Parameter struct {
	Name     string `json:"name"`
	Mode     string `json:"mode,omitempty"`
	TypeName string `json:"typeName,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

type Edge struct {
	ID      string         `json:"id"`
	Source  string         `json:"source"`
	Target  string         `json:"target"`
	Kind    string         `json:"kind"`
	Label   string         `json:"label,omitempty"`
	Role    string         `json:"role,omitempty"`
	Count   int            `json:"count,omitempty"`
	Ranges  []lsp.Location `json:"ranges,omitempty"`
	Include *IncludeInfo   `json:"include,omitempty"`
}

type IncludeInfo struct {
	Path            string `json:"path,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Exists          bool   `json:"exists"`
	ResolvedURI     string `json:"resolvedUri,omitempty"`
	ResolvedPath    string `json:"resolvedPath,omitempty"`
	ActualPath      string `json:"actualPath,omitempty"`
	PathCaseMatches bool   `json:"pathCaseMatches"`
}

func BuildDocumentGraph(parsed *core.ParsedDocument) Payload {
	payload := Payload{
		Scope: "document",
		URI:   parsed.URI,
		Nodes: []Node{{ID: parsed.URI, Label: fileLabel(parsed.URI), Kind: "file", URI: parsed.URI, FileName: fileLabel(parsed.URI), IsRoot: true}},
		Stats: map[string]int{"files": 1},
	}
	for _, include := range parsed.Includes {
		id := parsed.URI + "#include:" + include.Path
		payload.Nodes = append(payload.Nodes, Node{ID: id, Label: include.Path, Kind: "include"})
		payload.addEdge(Edge{
			ID:      id,
			Source:  parsed.URI,
			Target:  id,
			Kind:    "include",
			Ranges:  []lsp.Location{{URI: parsed.URI, Range: include.Range}},
			Include: &IncludeInfo{Path: include.Path, Mode: include.Mode, PathCaseMatches: true},
		})
	}
	declarationCount := 0
	for _, declaration := range vbscript.DeclarationSymbols(parsed) {
		id := parsed.URI + "#symbol:" + strings.ToLower(declaration.Name)
		payload.Nodes = append(payload.Nodes, Node{ID: id, Label: declaration.Name, Kind: "vbDeclaration", URI: parsed.URI})
		payload.addEdge(Edge{ID: id, Source: parsed.URI, Target: id, Kind: "contains", Ranges: []lsp.Location{{URI: parsed.URI, Range: declaration.Range}}})
		declarationCount++
	}
	payload.Stats["declarations"] = declarationCount
	payload.Stats["includes"] = len(parsed.Includes)
	payload.Stats["links"] = len(payload.Links)
	return payload
}

func fileLabel(uri string) string {
	if parsed, err := url.Parse(uri); err == nil && parsed.Path != "" {
		if unescaped, err := url.PathUnescape(parsed.Path); err == nil {
			return filepath.Base(unescaped)
		}
	}
	return filepath.Base(uri)
}

func (p *Payload) AddEdge(edge Edge) {
	p.addEdge(edge)
}

func (p *Payload) addEdge(edge Edge) {
	if edgeSlicesShareBacking(p.Edges, p.Links) {
		p.Edges = append(p.Edges, edge)
		p.Links = p.Edges
		return
	}
	edges := append([]Edge(nil), p.Edges...)
	links := append([]Edge(nil), p.Links...)
	p.Edges = append(edges, edge)
	p.Links = append(links, edge)
}

func edgeSlicesShareBacking(edges, links []Edge) bool {
	if len(edges) != len(links) {
		return false
	}
	if len(edges) == 0 {
		return true
	}
	return &edges[0] == &links[0]
}
