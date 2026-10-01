//go:build !windows

package lspserver

func isRemoteDrivePath(string) bool {
	return false
}
