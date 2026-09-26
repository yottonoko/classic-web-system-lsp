package graph

import (
	"encoding/json"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestPayloadAddEdgeSharesEdgesAndLinksBacking(t *testing.T) {
	payload := Payload{Edges: []Edge{}, Links: []Edge{}}
	payload.AddEdge(Edge{ID: "first", Kind: "include"})
	payload.AddEdge(Edge{ID: "second", Kind: "references"})

	if len(payload.Edges) != 2 || len(payload.Links) != 2 {
		t.Fatalf("edge/link lengths = %d/%d, want 2/2", len(payload.Edges), len(payload.Links))
	}
	for index := range payload.Edges {
		if &payload.Edges[index] != &payload.Links[index] {
			t.Fatalf("edge %d is not backed by the same storage as its link", index)
		}
	}

	payload.Edges[0].Label = "updated"
	if got := payload.Links[0].Label; got != "updated" {
		t.Fatalf("linked edge label = %q, want updated", got)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var wire struct {
		Edges []Edge `json:"edges"`
		Links []Edge `json:"links"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(wire.Edges) != 2 || len(wire.Links) != 2 {
		t.Fatalf("JSON edge/link lengths = %d/%d, want 2/2", len(wire.Edges), len(wire.Links))
	}
	for index := range wire.Edges {
		if wire.Edges[index].ID != wire.Links[index].ID {
			t.Fatalf("JSON edge/link %d IDs = %q/%q", index, wire.Edges[index].ID, wire.Links[index].ID)
		}
	}
}

func TestPayloadAddEdgePreservesDivergedEdgeViews(t *testing.T) {
	payload := Payload{
		Edges: []Edge{{ID: "existing-edge"}},
		Links: []Edge{{ID: "existing-link"}},
	}
	payload.AddEdge(Edge{ID: "new"})

	if got := []string{payload.Edges[0].ID, payload.Edges[1].ID}; got[0] != "existing-edge" || got[1] != "new" {
		t.Fatalf("edges = %#v, want existing-edge/new", got)
	}
	if got := []string{payload.Links[0].ID, payload.Links[1].ID}; got[0] != "existing-link" || got[1] != "new" {
		t.Fatalf("links = %#v, want existing-link/new", got)
	}
}

func TestPayloadAddEdgePreservesDivergedAliasedEdgeViews(t *testing.T) {
	payload := Payload{Edges: make([]Edge, 0, 4), Links: make([]Edge, 0, 4)}
	payload.AddEdge(Edge{ID: "existing"})
	payload.Links = append(payload.Links, Edge{ID: "manual"})
	payload.AddEdge(Edge{ID: "new"})

	if got := []string{payload.Edges[0].ID, payload.Edges[1].ID}; got[0] != "existing" || got[1] != "new" {
		t.Fatalf("edges = %#v, want existing/new", got)
	}
	if got := []string{payload.Links[0].ID, payload.Links[1].ID, payload.Links[2].ID}; got[0] != "existing" || got[1] != "manual" || got[2] != "new" {
		t.Fatalf("links = %#v, want existing/manual/new", got)
	}
}

func TestBuildDocumentGraphStatsMatchSharedEdgeViews(t *testing.T) {
	payload := BuildDocumentGraph(&core.ParsedDocument{
		URI: "file:///site/default.asp",
		Includes: []core.Include{{
			Path:  "shared.inc",
			Mode:  "file",
			Range: lsp.Range{},
		}},
	})

	if got := payload.Stats["links"]; got != len(payload.Links) {
		t.Fatalf("links stat = %d, want %d", got, len(payload.Links))
	}
	if len(payload.Edges) != len(payload.Links) {
		t.Fatalf("edge/link lengths = %d/%d, want equal", len(payload.Edges), len(payload.Links))
	}
	if len(payload.Edges) > 0 && &payload.Edges[0] != &payload.Links[0] {
		t.Fatal("document graph edge and link views do not share backing storage")
	}
}
