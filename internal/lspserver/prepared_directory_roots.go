package lspserver

import (
	"container/list"
	"os"
	"sync"
)

// maxPreparedDirectoryRoots bounds the directory handles one prepared root
// keeps open between reads.
const maxPreparedDirectoryRoots = 32

// preparedDirectoryRoots shares directory handles below a prepared root for
// one workspace index run. os.Root resolves a nested name by opening every
// directory on the way, a round trip each on a network share, while index
// reads arrive grouped by directory.
type preparedDirectoryRoots struct {
	mu      sync.Mutex
	entries map[string]*preparedDirectoryRoot
	order   list.List // front is the most recently used directory
	closed  bool
}

type preparedDirectoryRoot struct {
	directory string
	root      *os.Root
	refs      int
	element   *list.Element
	retired   bool
}

// acquire returns the handle for directory, a slash-separated name relative
// to parent. The returned release must be called once the handle is unused.
func (d *preparedDirectoryRoots) acquire(parent *os.Root, directory string) (*os.Root, func(), bool) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, nil, false
	}
	if entry := d.entries[directory]; entry != nil {
		entry.refs++
		d.order.MoveToFront(entry.element)
		d.mu.Unlock()
		return entry.root, func() { d.release(entry) }, true
	}
	d.mu.Unlock()
	opened, err := parent.OpenRoot(directory)
	if err != nil {
		return nil, nil, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		_ = opened.Close()
		return nil, nil, false
	}
	if entry := d.entries[directory]; entry != nil {
		_ = opened.Close()
		entry.refs++
		d.order.MoveToFront(entry.element)
		return entry.root, func() { d.release(entry) }, true
	}
	if d.entries == nil {
		d.entries = map[string]*preparedDirectoryRoot{}
	}
	entry := &preparedDirectoryRoot{directory: directory, root: opened, refs: 1}
	entry.element = d.order.PushFront(entry)
	d.entries[directory] = entry
	for element := d.order.Back(); element != nil && d.order.Len() > maxPreparedDirectoryRoots; {
		previous := element.Prev()
		if candidate := element.Value.(*preparedDirectoryRoot); candidate.refs == 0 {
			d.retireLocked(candidate)
		}
		element = previous
	}
	return opened, func() { d.release(entry) }, true
}

func (d *preparedDirectoryRoots) release(entry *preparedDirectoryRoot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry.refs--
	if entry.retired && entry.refs == 0 {
		_ = entry.root.Close()
	}
}

func (d *preparedDirectoryRoots) retireLocked(entry *preparedDirectoryRoot) {
	if entry.retired {
		return
	}
	entry.retired = true
	delete(d.entries, entry.directory)
	d.order.Remove(entry.element)
	if entry.refs == 0 {
		_ = entry.root.Close()
	}
}

// close retires every handle; handles still in use close on release.
func (d *preparedDirectoryRoots) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	for _, entry := range d.entries {
		d.retireLocked(entry)
	}
}
