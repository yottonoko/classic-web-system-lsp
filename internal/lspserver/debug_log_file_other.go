//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package lspserver

func debugLogFileOpenFlags(flags int) int {
	return flags
}
