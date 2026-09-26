package workspace

import (
	"net/url"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
)

func SourceURIIdentityKey(uri string) string {
	if !strings.HasPrefix(strings.ToLower(uri), "file:") {
		return uri
	}
	parsed, err := url.Parse(uri)
	if err != nil || strings.ToLower(parsed.Scheme) != "file" {
		return uri
	}
	host := ""
	driveHost := isWindowsDrivePath(parsed.Host)
	if parsed.Hostname() != "" && !driveHost {
		host = "//" + strings.ToLower(parsed.Hostname())
	}
	pathname, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return uri
	}
	pathname = strings.ReplaceAll(pathname, "\\", "/")
	if driveHost {
		pathname = parsed.Host + pathname
	}
	uncPath := parsed.Host == "" && strings.HasPrefix(pathname, "//")
	withoutDriveSlash := stripLeadingDriveSlash(pathname)
	suffix := ""
	if parsed.RawQuery != "" {
		suffix += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		suffix += "#" + parsed.Fragment
	}
	key := host + withoutDriveSlash + suffix
	if host != "" || uncPath || isWindowsDrivePath(withoutDriveSlash) || isWindowsDrivePath(pathname) {
		return strings.ToLower(key)
	}
	return key
}

func SameSourceURI(left, right string) bool {
	if left == right {
		return true
	}
	return SourceURIIdentityKey(left) == SourceURIIdentityKey(right)
}

func FileIdentityKeyFromFileName(fileName string) string {
	slashFileName := strings.ReplaceAll(fileName, "\\", "/")
	withoutDriveSlash := stripLeadingDriveSlash(slashFileName)
	if isWindowsDrivePath(withoutDriveSlash) {
		return strings.ToLower(pathpkg.Clean(withoutDriveSlash))
	}
	if strings.HasPrefix(slashFileName, "//") {
		cleaned := pathpkg.Clean("/" + strings.TrimLeft(slashFileName, "/"))
		return "//" + strings.ToLower(strings.TrimPrefix(cleaned, "/"))
	}
	absolute, err := filepath.Abs(fileName)
	if err != nil {
		absolute = fileName
	}
	slashPath := strings.ReplaceAll(absolute, "\\", "/")
	withoutDriveSlash = stripLeadingDriveSlash(slashPath)
	windowsLike := runtime.GOOS == "windows" || isWindowsDrivePath(withoutDriveSlash) || isWindowsDrivePath(slashPath) || strings.HasPrefix(slashPath, "//")
	if windowsLike {
		return strings.ToLower(withoutDriveSlash)
	}
	return absolute
}

func FileIdentityKeyFromURI(uri string) string {
	if len(uri) < len("file://") || !strings.EqualFold(uri[:len("file://")], "file://") {
		return SourceURIIdentityKey(uri)
	}
	fileName, ok := filePathFromURI(uri)
	if !ok {
		return SourceURIIdentityKey(uri)
	}
	return FileIdentityKeyFromFileName(fileName)
}

func SameFileIdentityURI(left, right string) bool {
	if left == right {
		return true
	}
	return FileIdentityKeyFromURI(left) == FileIdentityKeyFromURI(right)
}

// filePathFromSimpleURI resolves file URIs with an empty authority and no
// escape, query, or fragment sequences without URL parsing. Inputs outside
// this subset fall back to filePathFromURI unchanged.
func filePathFromSimpleURI(raw string) (string, bool) {
	const prefix = "file://"
	if len(raw) <= len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return "", false
	}
	rest := raw[len(prefix):]
	if rest == "" || rest[0] != '/' {
		return "", false
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '%', '?', '#':
			return "", false
		}
	}
	pathname := strings.ReplaceAll(rest, "\\", "/")
	return stripLeadingDriveSlash(pathname), true
}

func filePathFromURI(raw string) (string, bool) {
	if path, ok := filePathFromSimpleURI(raw); ok {
		return path, true
	}
	parsed, err := url.Parse(raw)
	if err != nil || strings.ToLower(parsed.Scheme) != "file" {
		return "", false
	}
	pathname, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", false
	}
	if isWindowsDrivePath(parsed.Host) {
		return parsed.Host + pathname, true
	}
	if parsed.Host != "" {
		return "//" + parsed.Host + pathname, true
	}
	pathname = strings.ReplaceAll(pathname, "\\", "/")
	return stripLeadingDriveSlash(pathname), true
}

func stripLeadingDriveSlash(pathname string) string {
	if len(pathname) >= 3 && pathname[0] == '/' && isASCIIAlpha(pathname[1]) && pathname[2] == ':' {
		if len(pathname) == 3 || pathname[3] == '/' {
			return pathname[1:]
		}
	}
	return pathname
}

func isWindowsDrivePath(pathname string) bool {
	if len(pathname) < 2 {
		return false
	}
	if pathname[0] == '/' {
		return len(pathname) >= 3 && isASCIIAlpha(pathname[1]) && pathname[2] == ':' && (len(pathname) == 3 || pathname[3] == '/')
	}
	return isASCIIAlpha(pathname[0]) && pathname[1] == ':' && (len(pathname) == 2 || pathname[2] == '/')
}

func isASCIIAlpha(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}
