//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package lspserver

import "syscall"

func debugLogFileOpenFlags(flags int) int {
	return flags | syscall.O_NOFOLLOW
}
