package lspserver

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type workspaceReferenceLocationPlan struct {
	originURI            string
	originDocumentKey    string
	symbolKind           string
	includeDeclaration   bool
	implicitTarget       *vbUsageDeclaration
	implicitDocumentKeys map[string]struct{}
	unqualifiedTarget    bool
}

type workspaceReferenceLocationGroup struct {
	index     int
	locations []lsp.Location
}

func (s *Server) materializeWorkspaceReferenceSegments(
	ctx context.Context,
	segments []*workspaceReferenceDocumentSegment,
	plan workspaceReferenceLocationPlan,
	progress workspaceReferenceProgressReporter,
) ([]lsp.Location, int64) {
	if len(segments) == 0 {
		return nil, 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	bufferSize := max(1, s.analysisWorkers.workerCount()*2)
	groups := make(chan workspaceReferenceLocationGroup, bufferSize)
	var completedDocuments atomic.Int64
	var shadowedDocuments atomic.Int64
	go func() {
		s.analysisWorkers.parallelForBulk(ctx, len(segments), func(workerCtx context.Context, index int) {
			segment := segments[index]
			detailURI := ""
			var local []lsp.Location
			if segment != nil && segment.parsed != nil && workerCtx.Err() == nil {
				document := segment.parsed
				detailURI = document.URI
				if plan.implicitTarget == nil && segment.documentKey != plan.originDocumentKey && segment.counts.Declarations > 0 {
					shadowedDocuments.Add(1)
				} else {
					local = materializeWorkspaceReferenceSegment(segment, plan)
				}
			}
			if progress != nil {
				progress(int(completedDocuments.Add(1)), len(segments), detailURI)
			}
			select {
			case groups <- workspaceReferenceLocationGroup{index: index, locations: local}:
			case <-ctx.Done():
			}
		})
		close(groups)
	}()

	pending := make(map[int][]lsp.Location, bufferSize)
	next := 0
	locations := make([]lsp.Location, 0)
	for group := range groups {
		pending[group.index] = group.locations
		for {
			ordered, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			locations = append(locations, ordered...)
			emitWorkspaceReferenceLocations(ctx, ordered)
			next++
		}
	}
	return locations, shadowedDocuments.Load()
}

func materializeWorkspaceReferenceSegment(segment *workspaceReferenceDocumentSegment, plan workspaceReferenceLocationPlan) []lsp.Location {
	postings := segment.postings
	globalResolutions := segment.globalResolutions
	if len(globalResolutions) != len(postings) {
		globalResolutions = vbscript.GlobalReferenceResolutions(postings)
	}
	locations := make([]lsp.Location, 0, len(postings))
	callOnly := vbReferenceUsesCallRanges(plan.symbolKind)
	for index, posting := range postings {
		if plan.unqualifiedTarget && !globalResolutions[index] {
			continue
		}
		if posting.HasRole(vbscript.ReferenceRoleCref) && !strings.HasPrefix(plan.symbolKind, "reference:") {
			continue
		}
		if callOnly && !posting.HasRole(vbscript.ReferenceRoleCall) && !posting.HasRole(vbscript.ReferenceRoleDeclaration) {
			continue
		}
		isDeclaration := posting.HasRole(vbscript.ReferenceRoleDeclaration)
		if !isDeclaration {
			_, isDeclaration = segment.declarationRanges[posting.Range]
		}
		if !plan.includeDeclaration && (isDeclaration || plan.symbolKind == "variable" && posting.HasRole(vbscript.ReferenceRoleObjectInitialization)) {
			continue
		}
		location := lsp.Location{URI: segment.parsed.URI, Range: posting.Range}
		if plan.implicitTarget != nil {
			if !plan.includeDeclaration && workspacepkg.SameFileIdentityURI(location.URI, plan.originURI) && location.Range == plan.implicitTarget.Range {
				continue
			}
			if _, ok := plan.implicitDocumentKeys[segment.documentKey]; !ok {
				continue
			}
		}
		locations = append(locations, location)
	}
	return locations
}
