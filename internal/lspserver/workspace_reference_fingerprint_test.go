package lspserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestWorkspaceReferenceNameFingerprintsHashPartsInDocumentKeyOrder(t *testing.T) {
	names := []string{"alpha", "beta", "gamma"}
	documents := make([]*core.ParsedDocument, 0, 9)
	for _, index := range []int{7, 2, 5, 0, 8, 3, 6, 1, 4} {
		var source strings.Builder
		source.WriteString("<%\n")
		for nameIndex, name := range names {
			for range (index + nameIndex) % 3 {
				source.WriteString("Response.Write " + name + "\n")
			}
		}
		source.WriteString("%>")
		documents = append(documents, core.ParseDocument(fmt.Sprintf("file:///site/fingerprint-%d.asp", index), source.String(), core.Settings{DefaultLanguage: "VBScript"}))
	}
	index := newWorkspaceReferenceIndex()
	got := index.semanticFingerprintsForNamesContext(context.Background(), names, documents)

	index.mu.RLock()
	defer index.mu.RUnlock()
	for _, name := range names {
		type part struct{ key, fingerprint string }
		parts := []part{}
		for _, segment := range index.names[name] {
			parts = append(parts, part{key: segment.documentKey, fingerprint: segment.countFingerprint})
		}
		if len(parts) == 0 {
			t.Fatalf("fixture produced no %s segments", name)
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].key < parts[j].key })
		hash := sha256.New()
		for _, item := range parts {
			hash.Write([]byte(item.key + "\x00" + item.fingerprint + "\x00"))
		}
		if want := hex.EncodeToString(hash.Sum(nil)); got[name] != want {
			t.Fatalf("%s fingerprint = %s, want persisted sorted-part hash %s", name, got[name], want)
		}
	}
}
