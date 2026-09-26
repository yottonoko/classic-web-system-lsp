package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

var errDiskCacheSymlink = errors.New("cache path contains a symlink")

func openDiskCacheDirectoryRoot(directory string, create bool) (*os.Root, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if create {
		if err := mkdirDiskCacheDirectorySecure(absolute, 0o755); err != nil {
			return nil, err
		}
	}
	if err := rejectSymlinkComponents(absolute, absolute); err != nil {
		return nil, err
	}
	ancestor, relative, err := existingDiskCacheAncestor(absolute)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(ancestor)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return root, nil
	}
	directoryRoot, err := root.OpenRoot(relative)
	_ = root.Close()
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkComponents(absolute, absolute); err != nil {
		_ = directoryRoot.Close()
		return nil, err
	}
	return directoryRoot, nil
}

func mkdirDiskCacheDirectorySecure(directory string, mode os.FileMode) error {
	ancestor, relative, err := existingDiskCacheAncestor(directory)
	if err != nil {
		return err
	}
	if relative == "." {
		return nil
	}
	root, err := os.OpenRoot(ancestor)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(relative, mode)
}

func existingDiskCacheAncestor(path string) (string, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	current := absolute
	var missing []string
	for {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", "", errDiskCacheSymlink
			}
			relative := "."
			for index := len(missing) - 1; index >= 0; index-- {
				relative = filepath.Join(relative, missing[index])
			}
			return current, filepath.Clean(relative), nil
		}
		if !os.IsNotExist(statErr) {
			return "", "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", "", os.ErrNotExist
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func openDiskCacheDatabase(path string, mode os.FileMode, options *bolt.Options, root *os.Root) (*bolt.DB, error) {
	cloned := bolt.Options{}
	if options != nil {
		cloned = *options
	}
	cloned.OpenFile = func(_ string, flags int, perm os.FileMode) (*os.File, error) {
		name := filepath.Base(path)
		if info, err := root.Lstat(name); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errDiskCacheSymlink
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		file, err := root.OpenFile(name, flags, perm)
		if err != nil {
			return nil, err
		}
		if info, err := root.Lstat(name); err != nil {
			_ = file.Close()
			return nil, err
		} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			_ = file.Close()
			return nil, errDiskCacheSymlink
		}
		return file, nil
	}
	return bolt.Open(path, mode, &cloned)
}

func NewDiskAnalysisCache(options DiskAnalysisCacheOptions) (*DiskAnalysisCache, error) {
	root := options.Directory
	if root == "" {
		root = filepath.Join(os.TempDir(), "asp-lsp-analysis-cache")
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	ttl := options.TTL
	if ttl <= 0 {
		ttl = defaultDiskCacheTTL
	}
	maxSize := options.MaxSize
	if maxSize <= 0 {
		maxSize = defaultDiskCacheMaxSize
	}
	namespace := options.Namespace
	if namespace == "" {
		namespace = "default"
	}
	databaseName := namespace
	if !safeDiskNamespacePattern.MatchString(databaseName) {
		databaseName = stableDiskHash(databaseName)
	}
	cache := &DiskAnalysisCache{
		root:                          root,
		databasePath:                  filepath.Join(root, "bbolt-v1", databaseName+".db"),
		ttl:                           ttl,
		maxSize:                       maxSize,
		enabled:                       options.Enabled,
		namespace:                     namespace,
		toolVersion:                   options.ToolVersion,
		gzip:                          options.Gzip,
		pending:                       make(map[string]pendingDiskWrite),
		inFlight:                      make(map[string]pendingDiskWrite),
		pendingReference:              make(map[string]pendingReferenceWrite),
		inFlightReference:             make(map[string]pendingReferenceWrite),
		pendingReferenceDocumentKeys:  make(map[string]map[string]struct{}),
		inFlightReferenceDocumentKeys: make(map[string]map[string]struct{}),
		notify:                        make(chan struct{}, 1),
		commands:                      make(chan diskCacheWriterCommand),
		writerDone:                    make(chan struct{}),
	}
	cache.nextSweepSize.Store(cache.cleanupTriggerSize())
	if !cache.enabled {
		close(cache.writerDone)
		return cache, nil
	}
	databaseRoot, err := openDiskCacheDirectoryRoot(filepath.Dir(cache.databasePath), true)
	if err != nil {
		return nil, err
	}
	_ = databaseRoot.Close()
	if err := cache.recoverInterruptedCompaction(); err != nil {
		return nil, err
	}
	if err := cache.openDatabase(true); err != nil {
		cache.closeBeforeWriter()
		return nil, err
	}
	databaseRoot, err = openDiskCacheDirectoryRoot(filepath.Dir(cache.databasePath), false)
	if err != nil {
		cache.closeBeforeWriter()
		return nil, err
	}
	backupErr := removeDiskCacheArtifact(databaseRoot, filepath.Base(cache.databasePath)+".backup")
	_ = databaseRoot.Close()
	if backupErr != nil {
		cache.closeBeforeWriter()
		return nil, backupErr
	}
	go cache.runWriter()
	return cache, nil
}

func (c *DiskAnalysisCache) closeBeforeWriter() {
	c.dbMu.Lock()
	if c.db != nil {
		_ = c.db.Close()
		c.db = nil
	}
	c.dbMu.Unlock()
	close(c.writerDone)
}

func rejectSymlinkComponents(root, path string) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("cache path is outside cache root: %s", path)
	}
	if symlink, err := findSymlinkAncestor(absolutePath); err != nil {
		return err
	} else if symlink != "" {
		return fmt.Errorf("cache path contains symlink: %s", symlink)
	}
	current := filepath.Clean(absoluteRoot)
	if err := rejectSymlinkComponent(current); err != nil {
		return err
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		if err := rejectSymlinkComponent(current); err != nil {
			return err
		}
	}
	return nil
}

func findSymlinkAncestor(path string) (string, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		info, statErr := os.Lstat(current)
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", statErr
		}
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 && !allowedSystemPathAlias(current) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}

func allowedSystemPathAlias(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	cleanPath := filepath.Clean(path)
	for _, alias := range []string{"/tmp", "/var", "/etc"} {
		if cleanPath != alias {
			continue
		}
		resolved, err := filepath.EvalSymlinks(cleanPath)
		return err == nil && filepath.Clean(resolved) == filepath.Join(string(filepath.Separator), "private", strings.TrimPrefix(alias, string(filepath.Separator)))
	}
	return false
}

func rejectSymlinkComponent(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cache path contains symlink: %s", path)
	}
	return nil
}

func (c *DiskAnalysisCache) recoverInterruptedCompaction() error {
	databaseRoot, err := openDiskCacheDirectoryRoot(filepath.Dir(c.databasePath), false)
	if err != nil {
		return err
	}
	defer databaseRoot.Close()
	databaseName := filepath.Base(c.databasePath)
	backupPath := c.databasePath + ".backup"
	compactPath := c.databasePath + ".compact"
	backupName := filepath.Base(backupPath)
	compactName := filepath.Base(compactPath)
	for _, name := range []string{databaseName, backupName, compactName} {
		if _, err := diskCacheArtifactInfo(databaseRoot, name); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	databaseInfo, err := diskCacheArtifactInfo(databaseRoot, databaseName)
	if os.IsNotExist(err) {
		if _, backupErr := diskCacheArtifactInfo(databaseRoot, backupName); backupErr == nil {
			if err := databaseRoot.Rename(backupName, databaseName); err != nil && !os.IsNotExist(err) {
				return err
			}
			if _, err := diskCacheArtifactInfo(databaseRoot, databaseName); err != nil && !os.IsNotExist(err) {
				return err
			}
		} else if !os.IsNotExist(backupErr) {
			return backupErr
		}
	} else if err != nil {
		return err
	} else if !databaseInfo.Mode().IsRegular() {
		return fmt.Errorf("cache database is not a regular file: %s", c.databasePath)
	}
	if _, err := diskCacheArtifactInfo(databaseRoot, compactName); err == nil {
		if err := databaseRoot.Remove(compactName); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func diskCacheArtifactInfo(root *os.Root, name string) (os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errDiskCacheSymlink
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cache artifact is not a regular file: %s", name)
	}
	return info, nil
}

func (c *DiskAnalysisCache) openDatabase(recreate bool) error {
	databaseRoot, err := openDiskCacheDirectoryRoot(filepath.Dir(c.databasePath), false)
	if err != nil {
		return err
	}
	db, err := openDiskCacheDatabase(c.databasePath, 0o600, &bolt.Options{Timeout: diskCacheOpenTimeout}, databaseRoot)
	_ = databaseRoot.Close()
	if err == nil {
		err = initializeDiskCacheDatabase(db, c.toolVersion)
	}
	if err == nil {
		c.db = db
		return nil
	}
	if db != nil {
		_ = db.Close()
	}
	if !recreate || errors.Is(err, bolterrors.ErrTimeout) {
		return err
	}
	if restored, restoreErr := c.restoreDiskCacheBackup(); restored || restoreErr != nil {
		return restoreErr
	}
	databaseRoot, rootErr := openDiskCacheDirectoryRoot(filepath.Dir(c.databasePath), false)
	if rootErr != nil {
		return errors.Join(err, rootErr)
	}
	removeErr := databaseRoot.Remove(filepath.Base(c.databasePath))
	_ = databaseRoot.Close()
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return errors.Join(err, removeErr)
	}
	return c.openDatabase(false)
}

func (c *DiskAnalysisCache) restoreDiskCacheBackup() (bool, error) {
	databaseRoot, err := openDiskCacheDirectoryRoot(filepath.Dir(c.databasePath), false)
	if err != nil {
		return false, err
	}
	defer databaseRoot.Close()
	databaseName := filepath.Base(c.databasePath)
	backupName := databaseName + ".backup"
	invalidName := databaseName + ".invalid"
	if _, err := diskCacheArtifactInfo(databaseRoot, backupName); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := removeDiskCacheArtifact(databaseRoot, invalidName); err != nil {
		return false, err
	}
	if err := databaseRoot.Rename(databaseName, invalidName); err != nil {
		return false, err
	}
	if err := databaseRoot.Rename(backupName, databaseName); err != nil {
		restoreErr := databaseRoot.Rename(invalidName, databaseName)
		return false, errors.Join(err, restoreErr)
	}
	if err := c.openDatabase(false); err == nil {
		return true, removeDiskCacheArtifact(databaseRoot, invalidName)
	}
	removeErr := removeDiskCacheArtifact(databaseRoot, databaseName)
	restoreErr := databaseRoot.Rename(invalidName, databaseName)
	if restoreErr != nil {
		return false, errors.Join(removeErr, restoreErr)
	}
	return false, removeErr
}

func initializeDiskCacheDatabase(db *bolt.DB, toolVersion string) error {
	needsUpdate := false
	if err := db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(diskCacheMetaBucket)
		if meta == nil {
			needsUpdate = true
			return nil
		}
		if schema := meta.Get(diskCacheSchemaKey); schema != nil && !bytes.Equal(schema, diskCacheSchemaValue) {
			return fmt.Errorf("unsupported analysis cache schema %q", schema)
		}
		if storedToolVersion := meta.Get(diskCacheToolVersionKey); storedToolVersion != nil && string(storedToolVersion) != toolVersion {
			return fmt.Errorf("analysis cache tool version %q does not match %q", storedToolVersion, toolVersion)
		}
		if meta.Get(diskCacheSchemaKey) == nil || meta.Get(diskCacheToolVersionKey) == nil || meta.Get(diskCacheLogicalSizeKey) == nil {
			needsUpdate = true
		}
		for _, bucketName := range [][]byte{diskCacheFilesBucket, diskCacheWorkspaceBucket, diskCacheExpiryBucket, diskReferenceMetaBucket, diskReferenceDocumentsBucket, diskReferencePostingsBucket, diskReferenceQueriesBucket, diskWorkspaceManifestBucket, diskDocumentHeadsBucket, diskDocumentArtifactsBucket, diskIncludeEdgesBucket} {
			if tx.Bucket(bucketName) == nil {
				needsUpdate = true
			}
		}
		return nil
	}); err != nil || !needsUpdate {
		return err
	}
	return db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(diskCacheMetaBucket)
		if err != nil {
			return err
		}
		for _, bucketName := range [][]byte{diskCacheFilesBucket, diskCacheWorkspaceBucket, diskCacheExpiryBucket, diskWorkspaceManifestBucket, diskDocumentHeadsBucket, diskDocumentArtifactsBucket, diskIncludeEdgesBucket} {
			if _, err := tx.CreateBucketIfNotExists(bucketName); err != nil {
				return err
			}
		}
		for _, bucketName := range [][]byte{diskReferenceMetaBucket, diskReferenceDocumentsBucket, diskReferencePostingsBucket, diskReferenceQueriesBucket} {
			if err := ensureReferenceBucket(tx, bucketName); err != nil {
				return err
			}
		}
		if schema := meta.Get(diskCacheSchemaKey); schema != nil && !bytes.Equal(schema, diskCacheSchemaValue) {
			return fmt.Errorf("unsupported analysis cache schema %q", schema)
		}
		if storedToolVersion := meta.Get(diskCacheToolVersionKey); storedToolVersion != nil && string(storedToolVersion) != toolVersion {
			return fmt.Errorf("analysis cache tool version %q does not match %q", storedToolVersion, toolVersion)
		}
		if err := meta.Put(diskCacheSchemaKey, diskCacheSchemaValue); err != nil {
			return err
		}
		if err := meta.Put(diskCacheToolVersionKey, []byte(toolVersion)); err != nil {
			return err
		}
		if meta.Get(diskCacheLogicalSizeKey) == nil {
			return meta.Put(diskCacheLogicalSizeKey, make([]byte, 8))
		}
		return nil
	})
}

func (c *DiskAnalysisCache) Enabled() bool { return c.enabled && !c.isClosed() }

func (c *DiskAnalysisCache) Directory() string { return c.root }

// ReadWorkspaceMembershipManifest restores one normalized workspace manifest.
func (c *DiskAnalysisCache) isClosed() bool {
	c.lifecycleMu.RLock()
	defer c.lifecycleMu.RUnlock()
	return c.closed
}

// ReadFileBundle restores the complete persisted analysis bundle for a source.
