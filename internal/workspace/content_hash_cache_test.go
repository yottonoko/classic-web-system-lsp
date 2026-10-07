package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestDiskContentHashCacheMatchesSHA256(t *testing.T) {
	large := strings.Repeat("<% Response.Write 1 %>\n", 512)
	sameBytes := strings.Clone(large)
	changed := large[:len(large)-1] + "x"
	for _, text := range []string{"", "short", large, large, sameBytes, changed, large} {
		sum := sha256.Sum256([]byte(text))
		if got, want := DiskContentHash(text), hex.EncodeToString(sum[:]); got != want {
			t.Fatalf("DiskContentHash(len %d) = %s, want %s", len(text), got, want)
		}
	}
}
