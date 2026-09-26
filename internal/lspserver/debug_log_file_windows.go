//go:build windows

package lspserver

import "syscall"

func debugLogFileOpenFlags(flags int) int {
	// OpenFile passes the high flag bits through to CreateFile. Opening the
	// reparse point itself lets the caller reject it from the file handle
	// without ever following a symbolic link to its target.
	return flags | syscall.FILE_FLAG_OPEN_REPARSE_POINT
}
