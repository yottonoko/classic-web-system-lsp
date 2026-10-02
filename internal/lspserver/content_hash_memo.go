package lspserver

import (
	"sync"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const (
	contentHashMemoEntries  = 32
	contentHashMemoMinBytes = 16 * 1024
	// The memo keeps its texts alive, so bound what it can hold on to.
	contentHashMemoMaxBytes = 32 * 1024 * 1024
)

// contentHashMemo remembers the hashes of recent large texts. A shared
// library include is hashed again for every page that includes it while
// diagnostics fingerprint their include closures, and its text is the same
// string each time, so comparing it with a remembered text is cheap.
type contentHashMemo struct {
	mu      sync.Mutex
	entries [contentHashMemoEntries]contentHashMemoEntry
	next    int
	bytes   int
}

type contentHashMemoEntry struct {
	text string
	hash string
}

var largeTextContentHashes contentHashMemo

// textContentHash returns workspacepkg.DiskContentHash(text), reusing the
// hash of a recently hashed large text with the same contents.
func textContentHash(text string) string {
	return largeTextContentHashes.hash(text)
}

func (m *contentHashMemo) hash(text string) string {
	if len(text) < contentHashMemoMinBytes {
		return workspacepkg.DiskContentHash(text)
	}
	m.mu.Lock()
	entries := m.entries
	m.mu.Unlock()
	for _, entry := range entries {
		// Equal strings that share their bytes compare in constant time.
		if len(entry.text) == len(text) && entry.text == text {
			return entry.hash
		}
	}
	hash := workspacepkg.DiskContentHash(text)
	if len(text) > contentHashMemoMaxBytes/4 {
		return hash
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bytes += len(text) - len(m.entries[m.next].text)
	m.entries[m.next] = contentHashMemoEntry{text: text, hash: hash}
	m.next = (m.next + 1) % contentHashMemoEntries
	for index := m.next; m.bytes > contentHashMemoMaxBytes; index = (index + 1) % contentHashMemoEntries {
		m.bytes -= len(m.entries[index].text)
		m.entries[index] = contentHashMemoEntry{}
	}
	return hash
}
