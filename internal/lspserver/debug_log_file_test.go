package lspserver

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFormatDebugLogFileLineEscapesNewlines(t *testing.T) {
	line := formatDebugLogFileLine(debugLogFileEntry{
		level:    "debug\n",
		category: "category\r\n",
		message:  "uri=file:///tmp/client\r\nmessage",
		metadata: map[string]any{"detail": "metadata\n"},
	})

	if strings.Count(line, "\n") != 1 {
		t.Fatalf("formatted debug log line has multiple physical lines: %q", line)
	}
	if strings.Contains(strings.TrimSuffix(line, "\n"), "\r") {
		t.Fatalf("formatted debug log line contains a carriage return: %q", line)
	}
	for _, expected := range []string{`DEBUG\n`, `category\r\n`, `uri=file:///tmp/client\r\nmessage`, `"detail":"metadata\n"`} {
		if !strings.Contains(line, expected) {
			t.Fatalf("formatted debug log line missing escaped field %q: %q", expected, line)
		}
	}
}

func TestDebugLogFileWriteUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not portable to Windows")
	}
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "0")

	path := filepath.Join(t.TempDir(), "debug.log")
	writer := newDebugLogFileWriter(nil)
	if err := writer.writeLine(path, "first\n"); err != nil {
		t.Fatal(err)
	}
	assertDebugLogFilePermissions(t, path)

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writer.writeLine(path, "second\n"); err != nil {
		t.Fatal(err)
	}
	assertDebugLogFilePermissions(t, path)
}

func TestDebugLogFileRotationUsesPrivatePermissionsForLegacyFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not portable to Windows")
	}
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "1")
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BACKUPS", "2")

	path := filepath.Join(t.TempDir(), "debug.log")
	for _, filePath := range []string{path, path + ".1", path + ".2"} {
		if err := os.WriteFile(filePath, []byte("legacy\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filePath, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writer := newDebugLogFileWriter(nil)
	if err := writer.writeLine(path, "current\n"); err != nil {
		t.Fatal(err)
	}

	for _, filePath := range []string{path, path + ".1", path + ".2"} {
		assertDebugLogFilePermissions(t, filePath)
	}
}

func TestDebugLogFileRotationRejectsBackupSymlinkWithoutTouchingTarget(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "1")
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BACKUPS", "2")

	root := t.TempDir()
	externalRoot := t.TempDir()
	path := filepath.Join(root, "debug.log")
	target := filepath.Join(externalRoot, "target.log")
	link := path + ".1"
	if err := os.WriteFile(target, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	writer := newDebugLogFileWriter(nil)
	if err := writer.writeLine(path, "current\n"); err == nil {
		t.Fatal("rotation unexpectedly followed backup symlink")
	}

	gotTarget, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotTarget) != "external\n" {
		t.Fatalf("external target content changed: %q", gotTarget)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("external target permissions = %04o, want %04o", got, 0o644)
		}
	}
	if info, err := os.Lstat(link); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("backup symlink was replaced during rejected rotation: %s", link)
	}

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := writer.writeLine(path, "current\n"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path + ".1"); err != nil {
		t.Fatal(err)
	} else if string(got) != "legacy\n" {
		t.Fatalf("rotated current content = %q, want %q", got, "legacy\n")
	}
}

func TestDebugLogFileWriteRejectsCurrentSymlinkWithoutTouchingTarget(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "0")

	root := t.TempDir()
	externalRoot := t.TempDir()
	path := filepath.Join(root, "debug.log")
	target := filepath.Join(externalRoot, "target.log")
	if err := os.WriteFile(target, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	writer := newDebugLogFileWriter(nil)
	if err := writer.writeLine(path, "current\n"); err == nil {
		t.Fatal("write unexpectedly followed current symlink")
	}
	gotTarget, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotTarget) != "external\n" {
		t.Fatalf("external target content changed: %q", gotTarget)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("external target permissions = %04o, want %04o", got, 0o644)
		}
	}
}

func TestDebugLogFileWriteRejectsInRootCurrentSymlinkWithCachedSize(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "100")

	root := t.TempDir()
	path := filepath.Join(root, "debug.log")
	target := filepath.Join(root, "target.log")
	if err := os.WriteFile(target, []byte("inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	writer := newDebugLogFileWriter(nil)
	writer.mu.Lock()
	writer.currentSizes[path] = 0
	writer.mu.Unlock()
	if err := writer.writeLine(path, "blocked\n"); err == nil {
		t.Fatal("write unexpectedly followed an in-root current symlink")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "inside\n" {
		t.Fatalf("in-root target content changed: %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("in-root target permissions = %04o, want %04o", got, 0o644)
		}
	}
	if info, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("current symlink was replaced during rejected write: %s", path)
	}
}

func TestDebugLogFileRotationRejectsInRootBackupSymlinkWithCachedSize(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "1")
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BACKUPS", "2")

	root := t.TempDir()
	path := filepath.Join(root, "debug.log")
	backup := path + ".1"
	target := filepath.Join(root, "target.log")
	if err := os.WriteFile(path, []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for _, filePath := range []string{path, target} {
			if err := os.Chmod(filePath, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Symlink(target, backup); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	writer := newDebugLogFileWriter(nil)
	writer.mu.Lock()
	writer.currentSizes[path] = int64(len("legacy\n"))
	writer.mu.Unlock()
	if err := writer.writeLine(path, "blocked\n"); err == nil {
		t.Fatal("rotation unexpectedly followed an in-root backup symlink")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "inside\n" {
		t.Fatalf("in-root backup target content changed: %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("in-root backup target permissions = %04o, want %04o", got, 0o644)
		}
	}
	if info, err := os.Lstat(backup); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("backup symlink was replaced during rejected rotation: %s", backup)
	}
}

func TestDebugLogFileWriterCachesAnchoredTargetTuple(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "0")

	root := t.TempDir()
	logDirectory := filepath.Join(root, "logs")
	if err := os.Mkdir(logDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logDirectory, "debug.log")
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.DebugLogFileEnabled = true
	server.settings.DebugLogFilePath = path
	defer server.debugLogWriter.closeRoots()

	server.logDebugFile("DEBUG", "tuple", "first")
	server.debugLogWriter.wait()
	server.settings.CacheDirectory = logDirectory
	server.logDebugFile("DEBUG", "tuple", "second")
	server.debugLogWriter.wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, message := range []string{"first", "second"} {
		if !strings.Contains(text, message) {
			t.Fatalf("debug log is missing %q: %q", message, text)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "debug.log")); !os.IsNotExist(err) {
		t.Fatalf("debug log was written outside its intended directory: err=%v", err)
	}
}

func TestDebugLogFileRootRejectsParentSymlinkReplacement(t *testing.T) {
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "0")

	root := t.TempDir()
	logDirectory := filepath.Join(root, "logs")
	if err := os.Mkdir(logDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	externalRoot := t.TempDir()
	externalLog := filepath.Join(externalRoot, "debug.log")
	if err := os.WriteFile(externalLog, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(externalLog, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.DebugLogFileEnabled = true
	server.settings.DebugLogFilePath = filepath.Join(logDirectory, "debug.log")
	target, ok := server.debugLogFileTarget()
	if !ok {
		t.Fatal("debug log target was not authorized")
	}
	defer target.root.Close()

	movedDirectory := filepath.Join(root, "logs-moved")
	if err := os.Rename(logDirectory, movedDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalRoot, logDirectory); err != nil {
		_ = os.Rename(movedDirectory, logDirectory)
		t.Skipf("symbolic links unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(logDirectory)
		_ = os.Rename(movedDirectory, logDirectory)
	})

	writer := newDebugLogFileWriter(nil)
	err := writer.writeLineEntry(debugLogFileEntry{filePath: target.filePath, root: target.root, relative: target.relative}, "blocked\n")
	if err == nil {
		t.Fatal("parent symlink replacement was followed")
	}
	gotExternal, err := os.ReadFile(externalLog)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotExternal) != "external\n" {
		t.Fatalf("external target content changed: %q", gotExternal)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(externalLog)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("external target permissions = %04o, want %04o", got, 0o644)
		}
	}
}

func assertDebugLogFilePermissions(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != debugLogFileMode {
		t.Fatalf("debug log permissions = %04o, want %04o", got, debugLogFileMode)
	}
}
