package lspserver

// maskEmbeddedHTMLComments hides HTML comments without changing byte offsets.
// HTML virtual documents already mask ASP and embedded-language bodies, so the
// remaining text can be safely searched by the lightweight HTML indexes.
func maskEmbeddedHTMLComments(text string) string {
	masked := []byte(text)
	for index := 0; index < len(masked); {
		start := index
		if index+3 >= len(masked) || string(masked[index:index+4]) != "<!--" {
			index++
			continue
		}
		index += 4
		for index < len(masked) {
			if index+2 < len(masked) && string(masked[index:index+3]) == "-->" {
				index += 3
				break
			}
			if masked[index] != '\n' && masked[index] != '\r' {
				masked[index] = ' '
			}
			index++
		}
		for cursor := start; cursor < index && cursor < len(masked); cursor++ {
			if masked[cursor] != '\n' && masked[cursor] != '\r' {
				masked[cursor] = ' '
			}
		}
	}
	return string(masked)
}

// maskEmbeddedCSSNonCode hides CSS comments and strings while keeping source
// offsets stable for completion and rename indexes.
func maskEmbeddedCSSNonCode(text string) string {
	masked := []byte(text)
	for index := 0; index < len(masked); {
		if index+1 < len(masked) && masked[index] == '/' && masked[index+1] == '*' {
			masked[index], masked[index+1] = ' ', ' '
			index += 2
			for index < len(masked) {
				if index+1 < len(masked) && masked[index] == '*' && masked[index+1] == '/' {
					masked[index], masked[index+1] = ' ', ' '
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

// maskEmbeddedJavaScriptComments removes JavaScript comments but preserves
// string literals because navigation extraction intentionally reads URL and
// form values from those literals.
func maskEmbeddedJavaScriptComments(text string) string {
	masked := []byte(text)
	for index := 0; index < len(masked); {
		if masked[index] == '\'' || masked[index] == '"' || masked[index] == '`' {
			quote := masked[index]
			index++
			for index < len(masked) {
				if masked[index] == '\\' {
					index += 2
					continue
				}
				if masked[index] == quote {
					index++
					break
				}
				index++
			}
			continue
		}
		if index+1 >= len(masked) || masked[index] != '/' {
			index++
			continue
		}
		switch masked[index+1] {
		case '/':
			masked[index], masked[index+1] = ' ', ' '
			index += 2
			for index < len(masked) && masked[index] != '\n' && masked[index] != '\r' {
				masked[index] = ' '
				index++
			}
		case '*':
			masked[index], masked[index+1] = ' ', ' '
			index += 2
			for index < len(masked) {
				if index+1 < len(masked) && masked[index] == '*' && masked[index+1] == '/' {
					masked[index], masked[index+1] = ' ', ' '
					index += 2
					break
				}
				if masked[index] != '\n' && masked[index] != '\r' {
					masked[index] = ' '
				}
				index++
			}
		default:
			index++
		}
	}
	return string(masked)
}
