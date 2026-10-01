package lspserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultDebugLogMaxQueuedEntries = 10_000
	defaultDebugLogMaxQueuedBytes   = 1024 * 1024
	// debugLogFileMaxBatchBytes caps one append so a burst of lines costs one
	// open and write instead of one per line.
	debugLogFileMaxBatchBytes = 64 * 1024
	debugLogFileMode          = 0o600
)

var errDebugLogFileNotRegular = errors.New("debug log path is not a regular file")
var errDebugLogFileTargetInvalid = errors.New("debug log target is invalid")

type debugLogFileTarget struct {
	filePath string
	root     *os.Root
	relative string
}

type debugLogFileEntry struct {
	filePath string
	root     *os.Root
	relative string
	level    string
	category string
	message  string
	metadata map[string]any
}

type queuedDebugLogFileEntry struct {
	debugLogFileEntry
	line       string
	byteLength int
}

// debugLogFileWriter mirrors the TypeScript writer's bounded, asynchronous
// queue so diagnostic work never waits on a slow log filesystem.
type debugLogFileWriter struct {
	warn             func(string)
	maxQueuedEntries int
	maxQueuedBytes   int

	mu           sync.Mutex
	queue        []queuedDebugLogFileEntry
	currentSizes map[string]int64
	failedPaths  map[string]struct{}
	flushing     bool
	queuedBytes  int
	queuedTrace  int
	droppedTrace int
	droppedLog   int
	flushDone    chan struct{}
	targets      map[string]debugLogFileTarget
}

func newDebugLogFileWriter(warn func(string)) *debugLogFileWriter {
	return &debugLogFileWriter{
		warn:             warn,
		maxQueuedEntries: defaultDebugLogMaxQueuedEntries,
		maxQueuedBytes:   defaultDebugLogMaxQueuedBytes,
		currentSizes:     map[string]int64{},
		failedPaths:      map[string]struct{}{},
		flushDone:        closedDebugLogChannel(),
		targets:          map[string]debugLogFileTarget{},
	}
}

func (w *debugLogFileWriter) enqueue(entry debugLogFileEntry) {
	if w == nil || entry.filePath == "" {
		return
	}
	w.adoptTarget(&entry)
	line := formatDebugLogFileLine(entry)
	queued := queuedDebugLogFileEntry{debugLogFileEntry: entry, line: line, byteLength: len([]byte(line))}
	startFlush := false
	w.mu.Lock()
	if _, failed := w.failedPaths[entry.filePath]; failed {
		w.mu.Unlock()
		return
	}
	if !w.canQueueLocked(queued) {
		if strings.EqualFold(entry.level, "trace") {
			w.droppedTrace++
			w.mu.Unlock()
			return
		}
		if !w.dropOldestTraceLocked() || !w.canQueueLocked(queued) {
			w.droppedLog++
			w.mu.Unlock()
			return
		}
	}
	w.queue = append(w.queue, queued)
	w.queuedBytes += queued.byteLength
	if strings.EqualFold(entry.level, "trace") {
		w.queuedTrace++
	}
	if !w.flushing {
		w.flushing = true
		w.flushDone = make(chan struct{})
		startFlush = true
	}
	w.mu.Unlock()
	if startFlush {
		go w.flushAsync()
	}
}

func (w *debugLogFileWriter) canQueueLocked(entry queuedDebugLogFileEntry) bool {
	return len(w.queue) < w.maxQueuedEntries && w.queuedBytes+entry.byteLength <= w.maxQueuedBytes
}

func (w *debugLogFileWriter) dropOldestTraceLocked() bool {
	if w.queuedTrace == 0 {
		return false
	}
	for index, entry := range w.queue {
		if !strings.EqualFold(entry.level, "trace") {
			continue
		}
		w.queue = append(w.queue[:index], w.queue[index+1:]...)
		w.queuedBytes -= entry.byteLength
		w.queuedTrace--
		w.droppedTrace++
		return true
	}
	return false
}

func (w *debugLogFileWriter) flushAsync() {
	for {
		w.mu.Lock()
		if len(w.queue) == 0 {
			w.flushing = false
			done := w.flushDone
			w.flushDone = closedDebugLogChannel()
			w.mu.Unlock()
			close(done)
			return
		}
		entry := w.queue[0]
		var lines strings.Builder
		count := 0
		for count < len(w.queue) {
			next := w.queue[count]
			if next.filePath != entry.filePath || next.root != entry.root || next.relative != entry.relative {
				break
			}
			if count > 0 && lines.Len()+next.byteLength > debugLogFileMaxBatchBytes {
				break
			}
			lines.WriteString(next.line)
			w.queuedBytes -= next.byteLength
			if strings.EqualFold(next.level, "trace") {
				w.queuedTrace--
			}
			count++
		}
		w.queue = w.queue[count:]
		notice := w.droppedNoticeLocked(entry.debugLogFileEntry)
		w.mu.Unlock()
		if notice != "" {
			if err := w.writeLineEntry(entry.debugLogFileEntry, notice); err != nil {
				w.markFailed(entry.filePath, err)
				continue
			}
		}
		if err := w.writeLineEntry(entry.debugLogFileEntry, lines.String()); err != nil {
			w.markFailed(entry.filePath, err)
		}
	}
}

func (w *debugLogFileWriter) wait() {
	if w == nil {
		return
	}
	w.mu.Lock()
	done := w.flushDone
	w.mu.Unlock()
	<-done
}

func (w *debugLogFileWriter) adoptTarget(entry *debugLogFileEntry) {
	if entry == nil || entry.root == nil {
		return
	}
	w.mu.Lock()
	if w.targets == nil {
		w.targets = make(map[string]debugLogFileTarget)
	}
	cached, ok := w.targets[entry.filePath]
	if !ok {
		w.targets[entry.filePath] = debugLogFileTarget{
			filePath: entry.filePath,
			root:     entry.root,
			relative: entry.relative,
		}
		w.mu.Unlock()
		return
	}
	incoming := entry.root
	entry.root = cached.root
	entry.relative = cached.relative
	keepIncoming := incoming == cached.root || w.rootUsedLocked(incoming)
	w.mu.Unlock()
	if !keepIncoming {
		_ = incoming.Close()
	}
}

func (w *debugLogFileWriter) adoptedTarget(filePath string) (debugLogFileTarget, bool) {
	if w == nil {
		return debugLogFileTarget{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	target, ok := w.targets[filePath]
	return target, ok && target.root != nil
}

func (w *debugLogFileWriter) rootUsedLocked(root *os.Root) bool {
	for _, target := range w.targets {
		if target.root == root {
			return true
		}
	}
	return false
}

func (w *debugLogFileWriter) closeRoots() {
	if w == nil {
		return
	}
	w.mu.Lock()
	targets := w.targets
	w.targets = map[string]debugLogFileTarget{}
	w.mu.Unlock()
	closed := make(map[*os.Root]struct{}, len(targets))
	for _, target := range targets {
		root := target.root
		if root == nil {
			continue
		}
		if _, ok := closed[root]; ok {
			continue
		}
		closed[root] = struct{}{}
		_ = root.Close()
	}
}

func closedDebugLogChannel() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func (w *debugLogFileWriter) droppedNoticeLocked(entry debugLogFileEntry) string {
	if w.droppedTrace == 0 && w.droppedLog == 0 {
		return ""
	}
	trace, other := w.droppedTrace, w.droppedLog
	w.droppedTrace, w.droppedLog = 0, 0
	return formatDebugLogFileLine(debugLogFileEntry{
		filePath: entry.filePath,
		root:     entry.root,
		relative: entry.relative,
		level:    "warn",
		category: "debugLogFile.queue",
		message:  "[asp-lsp] debugLogFile.queue.dropped",
		metadata: map[string]any{"trace": trace, "other": other},
	})
}

func (w *debugLogFileWriter) writeLine(filePath, line string) error {
	target, err := openDebugLogFilePathTarget(filePath)
	if err != nil {
		return err
	}
	defer target.root.Close()
	return w.writeLineEntry(debugLogFileEntry{filePath: target.filePath, root: target.root, relative: target.relative}, line)
}

func (w *debugLogFileWriter) writeLineEntry(entry debugLogFileEntry, line string) error {
	target := debugLogFileTarget{filePath: entry.filePath, root: entry.root, relative: entry.relative}
	if target.root != nil && target.relative == "" {
		return errDebugLogFileTargetInvalid
	}
	if err := mkdirDebugLogFileTarget(target); err != nil {
		return err
	}
	incoming := int64(len([]byte(line)))
	if err := w.rotateIfNeededTarget(target, incoming); err != nil {
		return err
	}
	file, err := openDebugLogFileTarget(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, debugLogFileMode)
	if err != nil {
		return err
	}
	if err := file.Chmod(debugLogFileMode); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := file.WriteString(line)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	w.mu.Lock()
	w.currentSizes[target.filePath] += incoming
	w.mu.Unlock()
	return nil
}

func (w *debugLogFileWriter) rotateIfNeededTarget(target debugLogFileTarget, incoming int64) error {
	maxBytes := debugLogFileMaxBytes()
	if maxBytes <= 0 {
		return nil
	}
	w.mu.Lock()
	size, cached := w.currentSizes[target.filePath]
	w.mu.Unlock()
	if !cached {
		info, err := statDebugLogFileTarget(target)
		if err == nil {
			size = info.Size()
		} else if !os.IsNotExist(err) {
			return err
		}
		w.mu.Lock()
		w.currentSizes[target.filePath] = size
		w.mu.Unlock()
	}
	if size+incoming <= maxBytes {
		return nil
	}
	maxBackups := debugLogFileMaxBackups()
	if maxBackups <= 0 {
		if _, err := statDebugLogFileTarget(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := removeDebugLogFileTarget(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		w.mu.Lock()
		w.currentSizes[target.filePath] = 0
		w.mu.Unlock()
		return nil
	}
	for index := 0; index <= maxBackups; index++ {
		path := debugLogFileBackupTarget(target, index)
		if err := chmodDebugLogFileIfExistsTarget(path); err != nil {
			return err
		}
	}
	if err := removeDebugLogFileTarget(debugLogFileBackupTarget(target, maxBackups)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for index := maxBackups - 1; index >= 1; index-- {
		if err := renameDebugLogFileTargetIfExists(debugLogFileBackupTarget(target, index), debugLogFileBackupTarget(target, index+1)); err != nil {
			return err
		}
	}
	if err := renameDebugLogFileTargetIfExists(target, debugLogFileBackupTarget(target, 1)); err != nil {
		return err
	}
	w.mu.Lock()
	w.currentSizes[target.filePath] = 0
	w.mu.Unlock()
	return nil
}

func (w *debugLogFileWriter) markFailed(filePath string, err error) {
	w.mu.Lock()
	w.failedPaths[filePath] = struct{}{}
	w.mu.Unlock()
	if w.warn != nil {
		w.warn("[asp-lsp] debugLogFile.write.failed: " + filePath + ": " + err.Error())
	}
}

func formatDebugLogFileLine(entry debugLogFileEntry) string {
	metadata := ""
	if len(entry.metadata) > 0 {
		encoded, err := json.Marshal(entry.metadata)
		if err != nil {
			encoded = []byte(`{"serialization":"failed"}`)
		}
		metadata = " " + escapeDebugLogFileField(string(encoded))
	}
	return time.Now().UTC().Format(time.RFC3339Nano) + " " +
		escapeDebugLogFileField(strings.ToUpper(entry.level)) + " " +
		escapeDebugLogFileField(entry.category) + " " +
		escapeDebugLogFileField(entry.message) + metadata + "\n"
}

func escapeDebugLogFileField(value string) string {
	value = strings.ReplaceAll(value, "\r", `\r`)
	return strings.ReplaceAll(value, "\n", `\n`)
}

func renameIfExists(source, target string) error {
	err := os.Rename(source, target)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func openDebugLogFilePathTarget(path string) (debugLogFileTarget, error) {
	if path == "" {
		return debugLogFileTarget{}, errDebugLogFileTargetInvalid
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return debugLogFileTarget{}, err
	}
	existing := filepath.Dir(absolute)
	missing := []string{}
	var existingInfo os.FileInfo
	for {
		existingInfo, err = os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return debugLogFileTarget{}, err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return debugLogFileTarget{}, err
		}
		missing = append(missing, filepath.Base(existing))
		existing = parent
	}
	if !existingInfo.IsDir() || existingInfo.Mode()&os.ModeSymlink != 0 {
		return debugLogFileTarget{}, &os.PathError{Op: "open", Path: path, Err: errDebugLogFileTargetInvalid}
	}
	root, err := os.OpenRoot(existing)
	if err != nil {
		return debugLogFileTarget{}, err
	}
	openedRoot, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return debugLogFileTarget{}, err
	}
	openedInfo, statErr := openedRoot.Stat()
	closeErr := openedRoot.Close()
	if statErr != nil || closeErr != nil || openedInfo == nil || !os.SameFile(existingInfo, openedInfo) {
		_ = root.Close()
		if statErr != nil {
			return debugLogFileTarget{}, statErr
		}
		if closeErr != nil {
			return debugLogFileTarget{}, closeErr
		}
		return debugLogFileTarget{}, &os.PathError{Op: "open", Path: path, Err: errDebugLogFileTargetInvalid}
	}
	parts := make([]string, 0, len(missing)+1)
	for index := len(missing) - 1; index >= 0; index-- {
		parts = append(parts, missing[index])
	}
	parts = append(parts, filepath.Base(absolute))
	return debugLogFileTarget{
		filePath: absolute,
		root:     root,
		relative: filepath.ToSlash(filepath.Join(parts...)),
	}, nil
}

func mkdirDebugLogFileTarget(target debugLogFileTarget) error {
	if target.root == nil {
		return os.MkdirAll(filepath.Dir(target.filePath), 0o755)
	}
	directory := filepath.Dir(filepath.FromSlash(target.relative))
	if directory == "." {
		return nil
	}
	return target.root.MkdirAll(filepath.ToSlash(directory), 0o755)
}

func openDebugLogFileTarget(target debugLogFileTarget, flags int, perm os.FileMode) (*os.File, error) {
	var (
		file *os.File
		err  error
	)
	if target.root == nil {
		file, err = os.OpenFile(target.filePath, debugLogFileOpenFlags(flags), perm)
	} else {
		file, err = target.root.OpenFile(target.relative, debugLogFileOpenFlags(flags), perm)
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, &os.PathError{Op: "open", Path: target.filePath, Err: errDebugLogFileNotRegular}
	}
	return file, nil
}

func statDebugLogFileTarget(target debugLogFileTarget) (os.FileInfo, error) {
	var (
		info os.FileInfo
		err  error
	)
	if target.root == nil {
		info, err = os.Lstat(target.filePath)
	} else {
		info, err = target.root.Lstat(target.relative)
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &os.PathError{Op: "lstat", Path: target.filePath, Err: errDebugLogFileNotRegular}
	}
	return info, nil
}

func removeDebugLogFileTarget(target debugLogFileTarget) error {
	if target.root == nil {
		return os.Remove(target.filePath)
	}
	return target.root.Remove(target.relative)
}

func renameDebugLogFileTargetIfExists(source, target debugLogFileTarget) error {
	if source.root == nil && target.root == nil {
		return renameIfExists(source.filePath, target.filePath)
	}
	if source.root == nil || source.root != target.root {
		return errDebugLogFileTargetInvalid
	}
	err := source.root.Rename(source.relative, target.relative)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func debugLogFileBackupTarget(target debugLogFileTarget, index int) debugLogFileTarget {
	if index <= 0 {
		return target
	}
	return debugLogFileTarget{
		filePath: debugLogBackupPath(target.filePath, index),
		root:     target.root,
		relative: debugLogBackupPath(target.relative, index),
	}
}

func chmodDebugLogFileIfExistsTarget(target debugLogFileTarget) error {
	file, err := openDebugLogFileTarget(target, os.O_RDONLY, 0)
	if os.IsNotExist(err) {
		info, statErr := statDebugLogFileTarget(target)
		if statErr == nil {
			if !info.Mode().IsRegular() {
				return &os.PathError{Op: "open", Path: target.filePath, Err: errDebugLogFileNotRegular}
			}
			return err
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Chmod(debugLogFileMode)
}

func debugLogBackupPath(path string, index int) string {
	return path + "." + strconv.Itoa(index)
}
