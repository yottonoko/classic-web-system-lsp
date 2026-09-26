package lspserver

import (
	"context"
	"sync/atomic"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type graphDocumentAnalysis struct {
	uri               string
	referenceShard    *vbscript.ReferenceShard
	declarationRanges map[string]map[lsp.Range]struct{}
	procedureRanges   []graphVBProcedureRange
	sourceDocument    *core.TextDocument
}

func (s *Server) graphDocumentAnalysesWithProgress(ctx context.Context, documents []*core.ParsedDocument, report graphProgressReporter, labelPrefix string) map[string]graphDocumentAnalysis {
	analyses := make([]graphDocumentAnalysis, len(documents))
	var completed atomic.Int64
	s.analysisWorkers.parallelForBulk(ctx, len(documents), func(workerCtx context.Context, index int) {
		if workerCtx.Err() != nil {
			return
		}
		document := documents[index]
		if document != nil {
			analyses[index] = graphDocumentAnalysis{
				uri:               document.URI,
				referenceShard:    vbscript.BuildReferenceShard(document),
				declarationRanges: graphDeclarationRanges(document),
				procedureRanges:   graphVBProcedureRanges(document),
				sourceDocument:    core.NewTextDocument(document.URI, "classic-asp", 0, document.Text),
			}
		}
		if report != nil {
			detail := ""
			if document != nil {
				detail = progressDetailForURI(document.URI)
			}
			report(labelPrefix+".indexReferences", detail, int(completed.Add(1)), len(documents))
		}
	})
	byURI := make(map[string]graphDocumentAnalysis, len(analyses))
	for _, analysis := range analyses {
		if analysis.uri == "" {
			continue
		}
		byURI[analysis.uri] = analysis
	}
	return byURI
}
