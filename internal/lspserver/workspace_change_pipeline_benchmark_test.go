package lspserver

import (
	"fmt"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

var workspaceArtifactBenchmarkSink workspaceDocumentArtifactDelta

func BenchmarkWorkspaceArtifactDeltaTwoThousandFiles(b *testing.B) {
	const familyCount = 100
	const filesPerFamily = 20
	manifests := make([]*workspaceDocumentArtifactManifest, 0, familyCount*filesPerFamily)
	for family := 0; family < familyCount; family++ {
		for file := 0; file < filesPerFamily; file++ {
			name := fmt.Sprintf("value_%d_%d", family, file)
			manifests = append(manifests, &workspaceDocumentArtifactManifest{
				DocumentID:     workspaceDocumentID(fmt.Sprintf("/workspace/family-%03d/file-%02d.inc", family, file)),
				PublicSymbols:  map[string]workspaceArtifactFingerprint{name: "before"},
				ExternalUsages: map[string]workspaceArtifactFingerprint{}, ImplicitGlobalCandidates: map[string]workspaceArtifactFingerprint{},
				ObjectTagVariables: map[string]workspaceArtifactFingerprint{}, References: map[string]workspaceReferenceArtifactSegment{},
				VirtualDocuments: map[core.EmbeddedLanguage]workspaceArtifactFingerprint{}, LocalDiagnostics: map[string]workspaceArtifactFingerprint{},
			})
		}
	}
	changed := *manifests[len(manifests)/2]
	changed.PublicSymbols = map[string]workspaceArtifactFingerprint{}
	for name := range manifests[len(manifests)/2].PublicSymbols {
		changed.PublicSymbols[name] = "after"
	}

	b.Run("one-name-warm", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			workspaceArtifactBenchmarkSink = compareWorkspaceDocumentArtifacts(manifests[len(manifests)/2], &changed)
		}
	})
	b.Run("forced-full-rebuild", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			for _, manifest := range manifests {
				workspaceArtifactBenchmarkSink = workspaceAllArtifactDelta(manifest)
			}
		}
	})
}
