package utils

import "regexp"

// Trim removes the text matched by re once, matching the upstream helper.
func Trim(str string, re *regexp.Regexp) string {
	loc := re.FindStringIndex(str)
	if loc == nil || loc[1] == loc[0] {
		return str
	}
	return str[:len(str)-(loc[1]-loc[0])]
}
