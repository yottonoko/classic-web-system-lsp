package graph

import (
	"path/filepath"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type BulkSourceMetadata struct {
	FileName string `json:"fileName"`
	MtimeMS  int64  `json:"mtimeMs"`
	Size     int64  `json:"size"`
}

type BulkDocument struct {
	URI        string             `json:"uri"`
	FileName   string             `json:"fileName"`
	Text       string             `json:"text"`
	Source     BulkSourceMetadata `json:"source"`
	DiskBacked bool               `json:"diskBacked"`
}

type BulkSource struct {
	URI        string
	FileName   string
	TextLength int
	Load       func() (BulkDocument, error)
}

type BulkIncludeRef struct {
	Path  string    `json:"path"`
	Mode  string    `json:"mode"`
	Range lsp.Range `json:"range"`
}

type BulkVBDeclaration struct {
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	NormalizedName          string    `json:"normalizedName"`
	Kind                    string    `json:"kind"`
	Range                   lsp.Range `json:"range"`
	NameRange               lsp.Range `json:"nameRange"`
	BindingScope            string    `json:"bindingScope,omitempty"`
	Implicit                bool      `json:"implicit,omitempty"`
	ImplicitGlobal          bool      `json:"implicitGlobal,omitempty"`
	ImplicitGlobalCandidate bool      `json:"implicitGlobalCandidate,omitempty"`
}

type BulkVBReference struct {
	Name           string    `json:"name"`
	NormalizedName string    `json:"normalizedName"`
	Range          lsp.Range `json:"range"`
	Role           string    `json:"role"`
	ResolvedID     string    `json:"resolvedId,omitempty"`
}

type BulkVBSymbolIndex struct {
	URI          string              `json:"uri"`
	Declarations []BulkVBDeclaration `json:"declarations"`
	References   []BulkVBReference   `json:"references"`
	IncludeRefs  []BulkIncludeRef    `json:"includeRefs"`
}

type BulkGraphFileIndex struct {
	Key           string             `json:"key"`
	URI           string             `json:"uri"`
	FileName      string             `json:"fileName"`
	Source        BulkSourceMetadata `json:"source"`
	IncludeRefs   []BulkIncludeRef   `json:"includeRefs"`
	VBSymbolIndex BulkVBSymbolIndex  `json:"vbSymbolIndex"`
	Fingerprint   string             `json:"fingerprint"`
	LastUsed      int64              `json:"lastUsed"`
}

type BulkIndexedDocument struct {
	Document   BulkDocument       `json:"document"`
	GraphIndex BulkGraphFileIndex `json:"graphIndex"`
}

type BulkProgressEvent struct {
	Stage   string
	Phase   string
	Source  *BulkSource
	Detail  string
	Current int
	Total   int
}

type BulkPipelineOptions struct {
	Sources            []BulkSource
	SpillDirectory     string
	IndexDocument      func(BulkDocument) (BulkIndexedDocument, error)
	ResolveIncludePath func(ownerURI string, includePath string) (string, error)
	GraphFileKey       func(string) string
	NormalizeFileName  func(string) string
	Fingerprint        func(BulkVBSymbolIndex) string
	OnProgress         func(BulkProgressEvent)
}

type SpilledGraphIndexPipeline struct {
	store    *workspace.SpillStore
	refs     []workspace.SpillRecordRef
	options  BulkPipelineOptions
	disposed bool
}

func RunSpilledGraphIndexPipeline(options BulkPipelineOptions) (*SpilledGraphIndexPipeline, error) {
	store := workspace.NewSpillStore(options.SpillDirectory, "graph-index", 24)
	pipeline := &SpilledGraphIndexPipeline{store: store, options: options}
	total := len(options.Sources)
	for i := range options.Sources {
		source := options.Sources[i]
		pipeline.progress("load", "start", &source, i, total)
		document, err := source.Load()
		if err != nil {
			_ = store.Clear()
			return nil, err
		}
		pipeline.progress("load", "done", &source, i+1, total)
		pipeline.progress("index", "start", &source, i, total)
		indexed, err := options.IndexDocument(document)
		if err != nil {
			_ = store.Clear()
			return nil, err
		}
		pipeline.progress("index", "done", &source, i+1, total)
		pipeline.progress("spill", "start", &source, i, total)
		ref, err := store.WriteRecord("graph-index", indexed)
		if err != nil {
			_ = store.Clear()
			return nil, err
		}
		pipeline.refs = append(pipeline.refs, ref)
		pipeline.progress("spill", "done", &source, i+1, total)
	}
	return pipeline, nil
}

func (p *SpilledGraphIndexPipeline) ScanCanonicalized() ([]BulkIndexedDocument, error) {
	total := len(p.refs) + 1
	p.progress("canonicalize", "start", nil, 0, total)
	documents := make([]BulkIndexedDocument, 0, len(p.refs))
	for index, ref := range p.refs {
		var document BulkIndexedDocument
		if err := p.store.ReadRecord(ref, &document); err != nil {
			return nil, err
		}
		documents = append(documents, document)
		source := bulkSourceForProgress(document.Document)
		p.progress("canonicalize", "progress", &source, index+1, total)
	}
	canonicalizeBulkImplicitGlobals(documents, p.options)
	p.progress("canonicalize", "done", nil, total, total)
	return documents, nil
}

func (p *SpilledGraphIndexPipeline) Dispose() error {
	if p.disposed {
		return nil
	}
	p.disposed = true
	return p.store.Clear()
}

func (p *SpilledGraphIndexPipeline) progress(stage string, phase string, source *BulkSource, current, total int) {
	if p.options.OnProgress != nil {
		detail := ""
		if source != nil {
			detail = source.FileName
			if detail == "" {
				detail = source.URI
			}
		}
		p.options.OnProgress(BulkProgressEvent{
			Stage: stage, Phase: phase, Source: source, Detail: detail, Current: current, Total: total,
		})
	}
}

func bulkSourceForProgress(document BulkDocument) BulkSource {
	return BulkSource{URI: document.URI, FileName: document.FileName, TextLength: len(document.Text)}
}

func canonicalizeBulkImplicitGlobals(documents []BulkIndexedDocument, options BulkPipelineOptions) {
	keyFor := options.GraphFileKey
	if keyFor == nil {
		keyFor = func(value string) string { return strings.ToLower(filepath.Clean(value)) }
	}
	normalize := options.NormalizeFileName
	if normalize == nil {
		normalize = filepath.Clean
	}
	byFileKey := map[string]*BulkIndexedDocument{}
	for i := range documents {
		key := keyFor(normalize(documents[i].Document.FileName))
		byFileKey[key] = &documents[i]
	}
	rewriteIDs := map[string]string{}
	for i := range documents {
		index := &documents[i].GraphIndex
		for _, include := range index.IncludeRefs {
			if options.ResolveIncludePath == nil {
				continue
			}
			includeFileName, err := options.ResolveIncludePath(index.URI, include.Path)
			if err != nil {
				continue
			}
			target := byFileKey[keyFor(normalize(includeFileName))]
			if target == nil {
				continue
			}
			targetDeclarations := declarationsByName(target.GraphIndex.VBSymbolIndex.Declarations)
			filtered := index.VBSymbolIndex.Declarations[:0]
			for _, declaration := range index.VBSymbolIndex.Declarations {
				targetDeclaration, ok := targetDeclarations[declaration.NormalizedName]
				if ok && declaration.Implicit && declaration.ImplicitGlobalCandidate {
					rewriteIDs[declaration.ID] = targetDeclaration.ID
					continue
				}
				filtered = append(filtered, declaration)
			}
			index.VBSymbolIndex.Declarations = filtered
		}
	}
	for i := range documents {
		for refIndex := range documents[i].GraphIndex.VBSymbolIndex.References {
			if replacement, ok := rewriteIDs[documents[i].GraphIndex.VBSymbolIndex.References[refIndex].ResolvedID]; ok {
				documents[i].GraphIndex.VBSymbolIndex.References[refIndex].ResolvedID = replacement
			}
		}
		if options.Fingerprint != nil {
			documents[i].GraphIndex.Fingerprint = options.Fingerprint(documents[i].GraphIndex.VBSymbolIndex)
		}
	}
}

func declarationsByName(declarations []BulkVBDeclaration) map[string]BulkVBDeclaration {
	byName := map[string]BulkVBDeclaration{}
	for _, declaration := range declarations {
		byName[declaration.NormalizedName] = declaration
	}
	return byName
}
