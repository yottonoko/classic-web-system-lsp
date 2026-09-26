package workspace

import "path/filepath"

func Match(pattern, name string) bool {
	matched, err := filepath.Match(pattern, name)
	return err == nil && matched
}
