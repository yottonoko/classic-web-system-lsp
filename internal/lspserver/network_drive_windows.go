//go:build windows

package lspserver

import (
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const windowsDriveRemote = 4

var (
	getDriveTypeProc = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDriveTypeW")
	remoteDriveKinds sync.Map
)

// isRemoteDrivePath reports whether path is on a mapped network drive such as
// Z:\. Those paths do not look like UNC paths but pay the same round trip for
// every filesystem call.
func isRemoteDrivePath(path string) bool {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' {
		return false
	}
	root := strings.ToUpper(volume) + `\`
	if remote, ok := remoteDriveKinds.Load(root); ok {
		return remote.(bool)
	}
	name, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return false
	}
	kind, _, _ := getDriveTypeProc.Call(uintptr(unsafe.Pointer(name)))
	remote := kind == windowsDriveRemote
	remoteDriveKinds.Store(root, remote)
	return remote
}
