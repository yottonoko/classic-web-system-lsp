package lspserver

import (
	"strings"
	"testing"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestContentHashMemoMatchesDiskContentHash(t *testing.T) {
	var memo contentHashMemo
	small := "small"
	large := strings.Repeat("Function Library\n", contentHashMemoMinBytes)
	edited := strings.Repeat("Function Library\n", contentHashMemoMinBytes-1) + "Function Changed\n"
	for _, text := range []string{small, large, large, edited, string([]byte(large))} {
		if got, want := memo.hash(text), workspacepkg.DiskContentHash(text); got != want {
			t.Fatalf("memoized hash of %d bytes = %s, want %s", len(text), got, want)
		}
	}
	memo.mu.Lock()
	defer memo.mu.Unlock()
	if memo.bytes > contentHashMemoMaxBytes {
		t.Fatalf("memo holds %d bytes, want at most %d", memo.bytes, contentHashMemoMaxBytes)
	}
}

func TestContentHashMemoBoundsRetainedBytes(t *testing.T) {
	var memo contentHashMemo
	text := strings.Repeat("x", contentHashMemoMaxBytes/4)
	for index := 0; index < 8; index++ {
		memo.hash(text[:len(text)-index])
	}
	memo.mu.Lock()
	defer memo.mu.Unlock()
	if memo.bytes > contentHashMemoMaxBytes {
		t.Fatalf("memo holds %d bytes, want at most %d", memo.bytes, contentHashMemoMaxBytes)
	}
	total := 0
	for _, entry := range memo.entries {
		total += len(entry.text)
	}
	if total != memo.bytes {
		t.Fatalf("memo counts %d bytes but holds %d", memo.bytes, total)
	}
}
