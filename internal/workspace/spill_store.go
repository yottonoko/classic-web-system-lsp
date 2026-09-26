package workspace

import (
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const recordLengthBytes = 4
const spillRootPrefix = "asp-lsp-bulk-"
const maxSpillRecordBytes = 64 * 1024 * 1024

var (
	errSpillRecordReferenceOutsideRoot = errors.New("spill record reference is outside store root")
	errSpillRecordReferenceInvalid     = errors.New("invalid spill record reference")
	errSpillRecordTooLarge             = errors.New("spill record exceeds size limit")
	errSpillStoreSymlink               = errors.New("spill store path contains a symlink")
)

var (
	spillStoreFilesystemMu sync.RWMutex
	spillStorePathLocks    sync.Map
)

func spillStorePathLock(path string) *sync.Mutex {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	lock, _ := spillStorePathLocks.LoadOrStore(absolute, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

type SpillRecordRef struct {
	Kind     string `json:"kind"`
	FileName string `json:"fileName"`
	Offset   int64  `json:"offset"`
	Bytes    uint32 `json:"bytes"`
}

type SpillStore struct {
	root       string
	ttl        time.Duration
	initErr    error
	mu         sync.Mutex
	rootHandle *os.Root
}

func NewSpillStore(directory, namespace string, ttlHours float64) *SpillStore {
	if namespace == "" {
		namespace = "default"
	}
	if ttlHours <= 0 {
		ttlHours = 24
	}
	root := directory
	var initErr error
	if root == "" {
		root, initErr = os.MkdirTemp(os.TempDir(), spillRootPrefix+safePathSegment(namespace)+"-")
	}
	if root != "" {
		absolute, err := filepath.Abs(root)
		if err == nil {
			root = absolute
		}
	}
	return &SpillStore{root: root, ttl: time.Duration(ttlHours * float64(time.Hour)), initErr: initErr}
}

func (s *SpillStore) Directory() string {
	return s.root
}

func (s *SpillStore) WriteRecord(kind string, value any) (SpillRecordRef, error) {
	if s == nil {
		return SpillRecordRef{}, errSpillRecordReferenceInvalid
	}
	if s.initErr != nil {
		return SpillRecordRef{}, s.initErr
	}
	safeKind := safePathSegment(kind)
	payload, err := cbor.Marshal(value)
	if err != nil {
		return SpillRecordRef{}, err
	}
	if len(payload) > maxSpillRecordBytes {
		return SpillRecordRef{}, errSpillRecordTooLarge
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	spillStoreFilesystemMu.RLock()
	defer spillStoreFilesystemMu.RUnlock()
	pathLock := spillStorePathLock(s.root)
	pathLock.Lock()
	defer pathLock.Unlock()
	root, err := s.openRoot(true)
	if err != nil {
		return SpillRecordRef{}, err
	}
	fileName := filepath.Join(s.root, safeKind+".cborl")
	if err := rejectSpillSymlinkComponents(s.root, fileName); err != nil {
		return SpillRecordRef{}, err
	}
	relativeName, err := filepath.Rel(s.root, fileName)
	if err != nil || relativeName == "." || filepath.IsAbs(relativeName) || stringsHasPrefix(relativeName, ".."+string(os.PathSeparator)) {
		return SpillRecordRef{}, errSpillRecordReferenceOutsideRoot
	}
	file, err := root.OpenFile(relativeName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if !os.IsExist(err) {
			return SpillRecordRef{}, err
		}
		file, err = root.OpenFile(relativeName, os.O_RDWR, 0o600)
		if err != nil {
			return SpillRecordRef{}, err
		}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return SpillRecordRef{}, err
	}
	pathInfo, err := root.Lstat(relativeName)
	if err != nil {
		return SpillRecordRef{}, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return SpillRecordRef{}, errSpillStoreSymlink
	}
	if !info.Mode().IsRegular() || !pathInfo.Mode().IsRegular() || !os.SameFile(info, pathInfo) {
		return SpillRecordRef{}, errSpillRecordReferenceInvalid
	}
	offset, err := repairSpillRecordTail(file)
	if err != nil {
		return SpillRecordRef{}, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return SpillRecordRef{}, err
	}
	record := make([]byte, recordLengthBytes+len(payload))
	binary.BigEndian.PutUint32(record, uint32(len(payload)))
	copy(record[recordLengthBytes:], payload)
	if written, err := file.Write(record); err != nil {
		truncateErr := file.Truncate(offset)
		return SpillRecordRef{}, errors.Join(err, truncateErr)
	} else if written != len(record) {
		truncateErr := file.Truncate(offset)
		return SpillRecordRef{}, errors.Join(io.ErrShortWrite, truncateErr)
	}
	return SpillRecordRef{Kind: safeKind, FileName: fileName, Offset: offset, Bytes: uint32(len(payload))}, nil
}

func (s *SpillStore) openRoot(create bool) (*os.Root, error) {
	if s.rootHandle != nil {
		return s.rootHandle, nil
	}
	if s.root == "" {
		return nil, errSpillRecordReferenceInvalid
	}
	root, err := openSpillDirectoryRoot(s.root, create)
	if err != nil {
		return nil, err
	}
	s.rootHandle = root
	return root, nil
}

func openSpillDirectoryRoot(directory string, create bool) (*os.Root, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := rejectSpillSymlinkComponents(absolute, absolute); err != nil {
		return nil, err
	}
	if create {
		if err := mkdirSpillDirectorySecure(absolute, 0o700); err != nil {
			return nil, err
		}
	}
	if err := rejectSpillSymlinkComponents(absolute, absolute); err != nil {
		return nil, err
	}
	ancestor, relative, err := existingSpillDirectoryAncestor(absolute)
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
	if err := rejectSpillSymlinkComponents(absolute, absolute); err != nil {
		_ = directoryRoot.Close()
		return nil, err
	}
	return directoryRoot, nil
}

func mkdirSpillDirectorySecure(directory string, mode os.FileMode) error {
	ancestor, relative, err := existingSpillDirectoryAncestor(directory)
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

func existingSpillDirectoryAncestor(path string) (string, string, error) {
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
				return "", "", errSpillStoreSymlink
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

func repairSpillRecordTail(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	offset := int64(0)
	header := make([]byte, recordLengthBytes)
	for offset < info.Size() {
		remaining := info.Size() - offset
		if remaining < recordLengthBytes {
			return offset, file.Truncate(offset)
		}
		if _, err := file.ReadAt(header, offset); err != nil {
			return 0, err
		}
		payloadBytes := int64(binary.BigEndian.Uint32(header))
		if payloadBytes <= 0 || payloadBytes > maxSpillRecordBytes || payloadBytes > info.Size()-offset-recordLengthBytes {
			return offset, file.Truncate(offset)
		}
		offset += recordLengthBytes + payloadBytes
	}
	return offset, nil
}

func rejectSpillSymlinkComponents(root, path string) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil || relative == ".." || stringsHasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return errSpillRecordReferenceOutsideRoot
	}
	if symlink, err := findSymlinkAncestor(absolutePath); err != nil {
		return err
	} else if symlink != "" {
		return errSpillStoreSymlink
	}
	current := filepath.Clean(absoluteRoot)
	if err := rejectSpillSymlinkComponent(current); err != nil {
		return err
	}
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		if err := rejectSpillSymlinkComponent(current); err != nil {
			return err
		}
	}
	return nil
}

func rejectSpillSymlinkComponent(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errSpillStoreSymlink
	}
	return nil
}

func (s *SpillStore) ReadRecord(ref SpillRecordRef, target any) error {
	if s == nil {
		return errSpillRecordReferenceInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	spillStoreFilesystemMu.RLock()
	defer spillStoreFilesystemMu.RUnlock()
	pathLock := spillStorePathLock(s.root)
	pathLock.Lock()
	defer pathLock.Unlock()
	fileName, err := s.validateRecordReference(ref)
	if err != nil {
		return err
	}
	root, err := s.openRoot(false)
	if err != nil {
		return err
	}
	relativeName, err := filepath.Rel(s.root, fileName)
	if err != nil || relativeName == "." || filepath.IsAbs(relativeName) || stringsHasPrefix(relativeName, ".."+string(os.PathSeparator)) {
		return errSpillRecordReferenceOutsideRoot
	}
	file, err := root.OpenFile(relativeName, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	pathInfo, err := root.Lstat(relativeName)
	if err != nil {
		return err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return errSpillStoreSymlink
	}
	if !info.Mode().IsRegular() || !pathInfo.Mode().IsRegular() || !os.SameFile(info, pathInfo) {
		return errSpillRecordReferenceInvalid
	}
	if ref.Offset < 0 || ref.Bytes == 0 || ref.Bytes > maxSpillRecordBytes || ref.Offset > info.Size()-recordLengthBytes {
		return errSpillRecordReferenceInvalid
	}
	header := make([]byte, recordLengthBytes)
	if _, err := file.ReadAt(header, ref.Offset); err != nil {
		return err
	}
	bytes := binary.BigEndian.Uint32(header)
	if bytes == 0 || bytes > maxSpillRecordBytes || bytes != ref.Bytes {
		return errSpillRecordReferenceInvalid
	}
	payloadEnd := ref.Offset + recordLengthBytes + int64(bytes)
	if payloadEnd < ref.Offset || payloadEnd > info.Size() {
		return errSpillRecordReferenceInvalid
	}
	payload := make([]byte, int(bytes))
	if _, err := file.ReadAt(payload, ref.Offset+recordLengthBytes); err != nil {
		return err
	}
	return cbor.Unmarshal(payload, target)
}

func (s *SpillStore) validateRecordReference(ref SpillRecordRef) (string, error) {
	if s == nil || s.initErr != nil || s.root == "" || ref.FileName == "" {
		return "", errSpillRecordReferenceInvalid
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	fileName, err := filepath.Abs(ref.FileName)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, fileName)
	if err != nil || relative == "." || relative == ".." || stringsHasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", errSpillRecordReferenceOutsideRoot
	}
	if filepath.Dir(relative) != "." || filepath.Ext(relative) != ".cborl" {
		return "", errSpillRecordReferenceOutsideRoot
	}
	if ref.Kind != "" && filepath.Base(relative) != safePathSegment(ref.Kind)+".cborl" {
		return "", errSpillRecordReferenceInvalid
	}
	if err := rejectSpillSymlinkComponents(root, fileName); err != nil {
		return "", err
	}
	rootReal, rootErr := filepath.EvalSymlinks(root)
	fileReal, fileErr := filepath.EvalSymlinks(fileName)
	if rootErr == nil && fileErr == nil {
		realRelative, relErr := filepath.Rel(rootReal, fileReal)
		if relErr != nil || realRelative == "." || realRelative == ".." || stringsHasPrefix(realRelative, ".."+string(os.PathSeparator)) || filepath.IsAbs(realRelative) {
			return "", errSpillRecordReferenceOutsideRoot
		}
	}
	return fileName, nil
}

func (s *SpillStore) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	spillStoreFilesystemMu.RLock()
	defer spillStoreFilesystemMu.RUnlock()
	pathLock := spillStorePathLock(s.root)
	pathLock.Lock()
	defer pathLock.Unlock()
	if s.rootHandle != nil {
		_ = s.rootHandle.Close()
		s.rootHandle = nil
	}
	parent, err := openSpillDirectoryRoot(filepath.Dir(s.root), false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Lstat(filepath.Base(s.root))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errSpillStoreSymlink
	}
	return parent.RemoveAll(filepath.Base(s.root))
}

// Close releases filesystem handles without deleting spill records.
func (s *SpillStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rootHandle == nil {
		return nil
	}
	err := s.rootHandle.Close()
	s.rootHandle = nil
	return err
}

func (s *SpillStore) Sweep() (int, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	spillStoreFilesystemMu.RLock()
	defer spillStoreFilesystemMu.RUnlock()
	pathLock := spillStorePathLock(s.root)
	pathLock.Lock()
	defer pathLock.Unlock()
	cutoff := time.Now().Add(-s.ttl)
	removed := 0
	root, err := s.openRoot(false)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if removeErr := root.Remove(path); removeErr != nil {
			return removeErr
		}
		removed++
		return nil
	})
	return removed, err
}

func SweepStaleTemporaryRoots(directory string, ttlHours float64) (int, error) {
	if directory == "" {
		directory = os.TempDir()
	}
	if ttlHours <= 0 {
		ttlHours = 24
	}
	cutoff := time.Now().Add(-time.Duration(ttlHours * float64(time.Hour)))
	spillStoreFilesystemMu.Lock()
	defer spillStoreFilesystemMu.Unlock()
	root, err := openSpillDirectoryRoot(directory, false)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !stringsHasPrefix(entry.Name(), spillRootPrefix) {
			continue
		}
		info, err := root.Lstat(entry.Name())
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		info, err = root.Stat(entry.Name())
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := root.RemoveAll(entry.Name()); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

var unsafePathSegment = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func safePathSegment(value string) string {
	safe := unsafePathSegment.ReplaceAllString(value, "-")
	safe = regexp.MustCompile(`^-+|-+$`).ReplaceAllString(safe, "")
	if safe == "" {
		return "default"
	}
	return safe
}

func stringsHasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
