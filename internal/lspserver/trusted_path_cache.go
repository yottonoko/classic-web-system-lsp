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

// canonicalDirectory returns the symlink-resolved form of an existing
// directory.
func (c *trustedPathCache) canonicalDirectory(directory string) (string, bool) {
	if c.enabled() {
		if value, ok := c.canonicals.Load(directory); ok {
			if entry := value.(trustedPathCanonical); time.Now().UnixNano() < entry.expires {
				return entry.path, true
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

// rootPathCurrent reports whether root still is the directory it was
// authorized as, like trustedFilesystemRootPathCurrent.
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
