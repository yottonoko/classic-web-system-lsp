package lspserver

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

// trustedPathCache remembers, for the network stat TTL, the lstat kind of
// paths and their ancestors, what directories resolve to, and root stats.
// Trust checks lstat every ancestor of a path up to its workspace root and
// resolve the path again; on a network share each call is a round trip, and
// thousands of pages share the same ancestors. The FsGateway already serves
// stats that are this old on network profiles, and file reads still go through
// os.Root, which refuses to leave the root. A zero TTL disables the cache.
type trustedPathCache struct {
	ttl        atomic.Int64
	symlinks   sync.Map // path -> trustedPathFlag
	canonicals sync.Map // directory -> trustedPathCanonical
	stats      sync.Map // directory -> trustedPathStat

	rootsMu sync.Mutex
	roots   map[string]*trustedRootHandle // canonical root -> shared handle
}

// trustedRootHandleLifetime bounds how long an opened root is shared while the
// cache is enabled. Opening a root costs an open and a stat of the root
// directory, a round trip each on a network share, and a workspace index stats
// thousands of files under the same root in one burst.
const trustedRootHandleLifetime = 2 * time.Second

type trustedRootHandle struct {
	key     string
	root    *os.Root
	info    os.FileInfo
	refs    int
	retired bool
}

type trustedPathFlag struct {
	expires int64
	value   bool
}

type trustedPathCanonical struct {
	expires int64
	path    string
}

type trustedPathStat struct {
	expires int64
	info    os.FileInfo
}

func (c *trustedPathCache) setTTL(ttl time.Duration) {
	if c == nil {
		return
	}
	if previous := time.Duration(c.ttl.Swap(int64(ttl))); previous != ttl {
		c.clear()
	}
}

func (c *trustedPathCache) clear() {
	if c == nil {
		return
	}
	c.symlinks.Clear()
	c.canonicals.Clear()
	c.stats.Clear()
	c.closeRoots()
}

// openRoot opens root like openTrustedFilesystemRoot. While the cache is
// enabled, callers share one handle per root for trustedRootHandleLifetime;
// each use still checks that the handle is the directory root.info names.
// The returned release must be called once the handle is no longer used.
func (c *trustedPathCache) openRoot(root trustedFilesystemRoot) (*os.Root, func(), bool) {
	if !c.enabled() {
		handle, ok := openTrustedFilesystemRoot(root)
		if !ok {
			return nil, nil, false
		}
		return handle, func() { _ = handle.Close() }, true
	}
	if root.info == nil {
		return nil, nil, false
	}
	c.rootsMu.Lock()
	entry := c.roots[root.canonical]
	if entry != nil {
		entry.refs++
	}
	c.rootsMu.Unlock()
	if entry != nil {
		if os.SameFile(entry.info, root.info) {
			return entry.root, func() { c.releaseRoot(entry) }, true
		}
		c.releaseRoot(entry)
	}
	handle, ok := openTrustedFilesystemRoot(root)
	if !ok {
		return nil, nil, false
	}
	entry = &trustedRootHandle{key: root.canonical, root: handle, info: root.info, refs: 1}
	c.rootsMu.Lock()
	if previous := c.roots[entry.key]; previous != nil {
		c.retireRootLocked(previous)
	}
	if c.roots == nil {
		c.roots = map[string]*trustedRootHandle{}
	}
	c.roots[entry.key] = entry
	c.rootsMu.Unlock()
	time.AfterFunc(trustedRootHandleLifetime, func() {
		c.rootsMu.Lock()
		defer c.rootsMu.Unlock()
		c.retireRootLocked(entry)
	})
	return handle, func() { c.releaseRoot(entry) }, true
}

func (c *trustedPathCache) releaseRoot(entry *trustedRootHandle) {
	c.rootsMu.Lock()
	defer c.rootsMu.Unlock()
	entry.refs--
	if entry.retired && entry.refs == 0 {
		_ = entry.root.Close()
	}
}

// retireRootLocked stops sharing entry and closes it once no caller uses it.
func (c *trustedPathCache) retireRootLocked(entry *trustedRootHandle) {
	if entry.retired {
		return
	}
	entry.retired = true
	if c.roots[entry.key] == entry {
		delete(c.roots, entry.key)
	}
	if entry.refs == 0 {
		_ = entry.root.Close()
	}
}

// closeRoots retires every shared root handle.
func (c *trustedPathCache) closeRoots() {
	if c == nil {
		return
	}
	c.rootsMu.Lock()
	defer c.rootsMu.Unlock()
	for _, entry := range c.roots {
		c.retireRootLocked(entry)
	}
}

// forget drops what the cache knows about path, so a watched change is seen
// before the TTL expires.
func (c *trustedPathCache) forget(path string) {
	if c == nil {
		return
	}
	// A file event may come from a replaced parent directory, and watchers do
	// not always report directory events, so drop the whole ancestor chain.
	path = filepath.Clean(path)
	for {
		c.symlinks.Delete(path)
		c.canonicals.Delete(path)
		c.stats.Delete(path)
		parent := filepath.Dir(path)
		if parent == path {
			return
		}
		path = parent
	}
}

// expiry returns when an entry stored now expires, or false when caching is
// off.
func (c *trustedPathCache) expiry() (int64, bool) {
	if c == nil {
		return 0, false
	}
	ttl := c.ttl.Load()
	if ttl <= 0 {
		return 0, false
	}
	return time.Now().UnixNano() + ttl, true
}

func (c *trustedPathCache) enabled() bool {
	return c != nil && c.ttl.Load() > 0
}

// lstatKind reports whether path exists and whether it is a symlink, as seen
// by lstat. Only existing paths are cached.
func (c *trustedPathCache) lstatKind(path string) (exists bool, symlink bool) {
	if c.enabled() {
		if value, ok := c.symlinks.Load(path); ok {
			if entry := value.(trustedPathFlag); time.Now().UnixNano() < entry.expires {
				return true, entry.value
			}
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, false
	}
	symlink = info.Mode()&os.ModeSymlink != 0
	if expires, ok := c.expiry(); ok {
		c.symlinks.Store(path, trustedPathFlag{expires: expires, value: symlink})
	}
	return true, symlink
}

// rememberRegularFile records that path was just read as a regular file
// through its root, which refuses symlinked files, so trust checks of the same
// file need no lstat until the TTL expires.
func (c *trustedPathCache) rememberRegularFile(path string) {
	if expires, ok := c.expiry(); ok {
		c.symlinks.Store(filepath.Clean(path), trustedPathFlag{expires: expires, value: false})
	}
}

// canonicalDirectory returns the symlink-resolved form of an existing
// directory.
func (c *trustedPathCache) canonicalDirectory(directory string) (string, bool) {
	if c.enabled() {
		if value, ok := c.canonicals.Load(directory); ok {
			if entry := value.(trustedPathCanonical); time.Now().UnixNano() < entry.expires {
				return entry.path, true
			}
		}
		// Build on the parent's cached form when this directory is not a
		// symlink. EvalSymlinks would lstat every ancestor again for each
		// directory, and sibling directories share those ancestors.
		if parent := filepath.Dir(directory); parent != directory {
			if exists, symlink := c.lstatKind(directory); exists && !symlink {
				if canonicalParent, ok := c.canonicalDirectory(parent); ok {
					resolved := filepath.Join(canonicalParent, filepath.Base(directory))
					if expires, ok := c.expiry(); ok {
						c.canonicals.Store(directory, trustedPathCanonical{expires: expires, path: resolved})
					}
					return resolved, true
				}
			}
		}
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		if runtime.GOOS != "windows" || !errors.Is(err, fs.ErrPermission) {
			return "", false
		}
		resolved = directory
	}
	resolved = filepath.Clean(resolved)
	if expires, ok := c.expiry(); ok {
		c.canonicals.Store(directory, trustedPathCanonical{expires: expires, path: resolved})
	}
	return resolved, true
}

// statDirectory stats a trusted root. Only successful results are cached.
func (c *trustedPathCache) statDirectory(directory string) (os.FileInfo, error) {
	if c.enabled() {
		if value, ok := c.stats.Load(directory); ok {
			if entry := value.(trustedPathStat); time.Now().UnixNano() < entry.expires {
				return entry.info, nil
			}
		}
	}
	info, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	if expires, ok := c.expiry(); ok {
		c.stats.Store(directory, trustedPathStat{expires: expires, info: info})
	}
	return info, nil
}

// rootPathCurrent reports whether root.path still is the directory root was
// authorized as.
func (c *trustedPathCache) rootPathCurrent(root trustedFilesystemRoot) bool {
	if root.info == nil || root.path == "" {
		return false
	}
	info, err := c.statDirectory(root.path)
	return err == nil && info.IsDir() && os.SameFile(root.info, info)
}

// pathContainsSymlinkWithinRoot reports whether path or any of its ancestors
// below root is a symlink.
func (c *trustedPathCache) pathContainsSymlinkWithinRoot(path, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || relative == ".." || filepath.IsAbs(relative) ||
		len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return true
	}
	rootKey := workspacepkg.FileIdentityKeyFromFileName(cleanRoot)
	for current := cleanPath; ; current = filepath.Dir(current) {
		if workspacepkg.FileIdentityKeyFromFileName(current) == rootKey {
			// A configured workspace root may itself be a symlink. The root is
			// the trust boundary; only symlinks below it can escape that boundary.
			return false
		}
		if _, symlink := c.lstatKind(current); symlink {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return true
		}
	}
}

// resolvePathForTrust resolves path like resolvePathForTrustContext. When the
// path exists and is not a symlink, it resolves only the parent directory,
// which sibling files share.
func (c *trustedPathCache) resolvePathForTrust(ctx context.Context, path string) (string, bool) {
	if c.enabled() {
		cleaned := filepath.Clean(path)
		if exists, symlink := c.lstatKind(cleaned); exists && !symlink {
			if parent := filepath.Dir(cleaned); parent != cleaned {
				if canonicalParent, ok := c.canonicalDirectory(parent); ok {
					return filepath.Join(canonicalParent, filepath.Base(cleaned)), true
				}
			}
		}
	}
	return resolvePathForTrustContext(ctx, path)
}
