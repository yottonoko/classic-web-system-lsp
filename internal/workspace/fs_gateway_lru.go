package workspace

import (
	"container/list"
	"time"
)

// fsLRU keeps cache entries in least-recently-used order with constant-time
// lookups, updates, and evictions. Gateway lookups run under the gateway
// mutex for every stat and directory read, so a linear order scan would
// serialize workers on large workspaces.
type fsLRU[T any] struct {
	items map[string]*list.Element
	order list.List // front is the most recently used entry
}

type fsLRUItem[T any] struct {
	key   string
	entry fsCacheEntry[T]
}

func (c *fsLRU[T]) get(key string, now time.Time) (fsCacheEntry[T], bool) {
	element, ok := c.items[key]
	if !ok {
		return fsCacheEntry[T]{}, false
	}
	item := element.Value.(*fsLRUItem[T])
	if now.After(item.entry.expiresAt) {
		c.removeElement(element)
		return item.entry, false
	}
	c.order.MoveToFront(element)
	return item.entry, true
}

func (c *fsLRU[T]) set(key string, entry fsCacheEntry[T], maxEntries int) {
	if c.items == nil {
		c.items = map[string]*list.Element{}
	}
	if element, ok := c.items[key]; ok {
		element.Value.(*fsLRUItem[T]).entry = entry
		c.order.MoveToFront(element)
	} else {
		c.items[key] = c.order.PushFront(&fsLRUItem[T]{key: key, entry: entry})
	}
	for c.order.Len() > maxEntries {
		c.removeElement(c.order.Back())
	}
}

func (c *fsLRU[T]) delete(key string) {
	if element, ok := c.items[key]; ok {
		c.removeElement(element)
	}
}

func (c *fsLRU[T]) clear() {
	c.items = map[string]*list.Element{}
	c.order.Init()
}

func (c *fsLRU[T]) removeElement(element *list.Element) {
	delete(c.items, element.Value.(*fsLRUItem[T]).key)
	c.order.Remove(element)
}
