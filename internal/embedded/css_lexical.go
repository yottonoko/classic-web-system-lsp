package embedded

// maskCSSNonCode replaces CSS comments and string contents with spaces while
// preserving line endings and byte offsets. Embedded CSS indexes use the
// resulting text so that source-map ranges can still refer to the original
// document.
func maskCSSNonCode(text string) string {
	masked := []byte(text)
	for index := 0; index < len(masked); {
		if index+1 < len(masked) && masked[index] == '/' && masked[index+1] == '*' {
			masked[index] = ' '
			masked[index+1] = ' '
			index += 2
			for index < len(masked) {
				if index+1 < len(masked) && masked[index] == '*' && masked[index+1] == '/' {
					masked[index] = ' '
					masked[index+1] = ' '
					index += 2
					break
				}
				if masked[index] != '\n' && masked[index] != '\r' {
					masked[index] = ' '
				}
				index++
			}
			continue
		}
		if masked[index] != '\'' && masked[index] != '"' {
			index++
			continue
		}
		quote := masked[index]
		masked[index] = ' '
		index++
		for index < len(masked) {
			if masked[index] == '\\' {
				masked[index] = ' '
				index++
				if index < len(masked) {
					if masked[index] != '\n' && masked[index] != '\r' {
						masked[index] = ' '
					}
					index++
				}
				continue
			}
			if masked[index] == quote {
				masked[index] = ' '
				index++
				break
			}
			if masked[index] != '\n' && masked[index] != '\r' {
				masked[index] = ' '
			}
			index++
		}
	}
	return string(masked)
}

func cssPositionIsCode(text string, offset int) bool {
	if offset < 0 {
		return false
	}
	if offset > len(text) {
		offset = len(text)
	}
	for index := 0; index < offset; {
		if index+1 < len(text) && text[index] == '/' && text[index+1] == '*' {
			index += 2
			closed := false
			for index < offset {
				if index+1 < len(text) && text[index] == '*' && text[index+1] == '/' {
					index += 2
					closed = true
					break
				}
				index++
			}
			if !closed {
				return false
			}
			continue
		}
		if text[index] != '\'' && text[index] != '"' {
			index++
			continue
		}
		quote := text[index]
		index++
		closed := false
		for index < offset {
			if text[index] == '\\' {
				index += 2
				continue
			}
			index++
			if index-1 < len(text) && text[index-1] == quote {
				closed = true
				break
			}
		}
		if !closed {
			return false
		}
	}
	return true
}
