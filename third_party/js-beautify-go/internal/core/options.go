// Package core contains shared option, scanner, token, and output primitives.
package core

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// BaseOptions contains options shared by all beautifiers.
type BaseOptions struct {
	// RawOptions stores normalized js-beautify option values.
	RawOptions map[string]any
	// Disabled leaves the source unchanged when set.
	Disabled bool
	// EOL is the output line terminator or "auto".
	EOL string
	// EndWithNewline appends a final line terminator.
	EndWithNewline bool
	// IndentSize is the number of columns in one indentation level.
	IndentSize int
	// IndentChar is the character used to build indentation.
	IndentChar string
	// IndentLevel is the initial indentation level.
	IndentLevel int
	// PreserveNewlines keeps source newlines where the formatter supports it.
	PreserveNewlines bool
	// MaxPreserveNewlines limits consecutive preserved newlines.
	MaxPreserveNewlines int
	// IndentWithTabs uses tab indentation.
	IndentWithTabs bool
	// WrapLineLength is the preferred maximum output line length.
	WrapLineLength int
	// IndentEmptyLines emits indentation on otherwise empty lines.
	IndentEmptyLines bool
	// Templating lists enabled template language handlers.
	Templating []string
}

// NewBaseOptions applies js-beautify's common option normalization and default
// semantics.
func NewBaseOptions(options map[string]any, mergeChildField string) (*BaseOptions, error) {
	raw := MergeOpts(options, mergeChildField)
	o := &BaseOptions{RawOptions: raw}
	o.Disabled = o.GetBoolean("disabled", false)
	o.EOL = o.GetCharacters("eol", "auto")
	o.EndWithNewline = o.GetBoolean("end_with_newline", false)
	o.IndentSize = o.GetNumber("indent_size", 4)
	o.IndentChar = o.GetCharacters("indent_char", " ")
	o.IndentLevel = o.GetNumber("indent_level", 0)
	o.PreserveNewlines = o.GetBoolean("preserve_newlines", true)
	o.MaxPreserveNewlines = o.GetNumber("max_preserve_newlines", 32786)
	if !o.PreserveNewlines {
		o.MaxPreserveNewlines = 0
	}
	o.IndentWithTabs = o.GetBoolean("indent_with_tabs", o.IndentChar == "\t")
	if o.IndentWithTabs {
		o.IndentChar = "\t"
		if o.IndentSize == 1 {
			o.IndentSize = 4
		}
	}
	o.WrapLineLength = o.GetNumber("wrap_line_length", o.GetNumber("max_char", 0))
	o.IndentEmptyLines = o.GetBoolean("indent_empty_lines", false)
	var err error
	o.Templating, err = o.GetSelectionList("templating",
		[]string{"auto", "none", "angular", "django", "erb", "handlebars", "php", "smarty"},
		[]string{"auto"})
	if err != nil {
		return nil, err
	}
	return o, nil
}

// NormalizeOpts replaces dashes with underscores in option keys.
func NormalizeOpts(options map[string]any) map[string]any {
	converted := map[string]any{}
	for key, value := range options {
		converted[strings.ReplaceAll(key, "-", "_")] = value
	}
	return converted
}

// MergeOpts merges per-language child options over parent options.
func MergeOpts(options map[string]any, childFieldName string) map[string]any {
	finalOpts := map[string]any{}
	if options == nil {
		options = map[string]any{}
	}
	allOptions := NormalizeOpts(options)
	for name, value := range allOptions {
		if name != childFieldName {
			finalOpts[name] = value
		}
	}
	if childFieldName != "" {
		if child, ok := asMap(allOptions[childFieldName]); ok {
			for name, value := range child {
				finalOpts[strings.ReplaceAll(name, "-", "_")] = value
			}
		}
	}
	return finalOpts
}

func asMap(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case map[string]string:
		out := map[string]any{}
		for k, item := range v {
			out[k] = item
		}
		return out, true
	default:
		rv := reflect.ValueOf(value)
		if rv.IsValid() && rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
			out := map[string]any{}
			iter := rv.MapRange()
			for iter.Next() {
				out[iter.Key().String()] = iter.Value().Interface()
			}
			return out, true
		}
	}
	return nil, false
}

// GetArray follows js-beautify's permissive string/array option conversion.
func (o *BaseOptions) GetArray(name string, defaultValue []string) []string {
	result := append([]string{}, defaultValue...)
	value, ok := o.RawOptions[name]
	if !ok || value == nil {
		return result
	}
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...)
	case []any:
		result = result[:0]
		for _, item := range v {
			result = append(result, fmt.Sprint(item))
		}
		return result
	case string:
		if v == "" {
			return []string{}
		}
		fields := strings.FieldsFunc(v, func(r rune) bool {
			return !(r == '_' || r == '/' || r == '-' ||
				(r >= '0' && r <= '9') ||
				(r >= 'A' && r <= 'Z') ||
				(r >= 'a' && r <= 'z'))
		})
		return fields
	}
	return result
}

// GetBoolean converts option values with JavaScript truthiness where practical.
func (o *BaseOptions) GetBoolean(name string, defaultValue bool) bool {
	value, ok := o.RawOptions[name]
	if !ok {
		return defaultValue
	}
	switch v := value.(type) {
	case bool:
		return v
	case nil:
		return false
	case string:
		return v != ""
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	default:
		return true
	}
}

// GetCharacters expands escaped CR/LF/TAB sequences.
func (o *BaseOptions) GetCharacters(name, defaultValue string) string {
	value, ok := o.RawOptions[name]
	if !ok {
		return defaultValue
	}
	v, ok := value.(string)
	if !ok {
		return defaultValue
	}
	v = strings.Replace(v, `\r`, "\r", 1)
	v = strings.Replace(v, `\n`, "\n", 1)
	v = strings.Replace(v, `\t`, "\t", 1)
	return v
}

// GetNumber parses option values with JavaScript parseInt-like behavior.
func (o *BaseOptions) GetNumber(name string, defaultValue int) int {
	value, ok := o.RawOptions[name]
	if !ok {
		return defaultValue
	}
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		return parseLeadingInt(v, defaultValue)
	default:
		return defaultValue
	}
}

func parseLeadingInt(value string, fallback int) int {
	value = strings.TrimLeft(value, " \t\r\n")
	if value == "" {
		return fallback
	}
	sign := 1
	if value[0] == '-' {
		sign = -1
		value = value[1:]
	} else if value[0] == '+' {
		value = value[1:]
	}
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i == 0 {
		return fallback
	}
	n, err := strconv.Atoi(value[:i])
	if err != nil {
		return fallback
	}
	return sign * n
}

// GetSelection returns a single valid selection.
func (o *BaseOptions) GetSelection(name string, selectionList []string, defaultValue []string) (string, error) {
	result, err := o.GetSelectionList(name, selectionList, defaultValue)
	if err != nil {
		return "", err
	}
	if len(result) != 1 {
		return "", fmt.Errorf("Invalid Option Value: The option '%s' can only be one of the following values:\n%s\nYou passed in: '%s'",
			name, strings.Join(selectionList, ","), jsOptionString(o.RawOptions[name]))
	}
	return result[0], nil
}

// GetSelectionList returns a validated list option.
func (o *BaseOptions) GetSelectionList(name string, selectionList []string, defaultValue []string) ([]string, error) {
	if len(selectionList) == 0 {
		return nil, fmt.Errorf("Selection list cannot be empty.")
	}
	if len(defaultValue) == 0 {
		defaultValue = []string{selectionList[0]}
	}
	if !IsValidSelection(defaultValue, selectionList) {
		return nil, fmt.Errorf("Invalid Default Value!")
	}
	result := o.GetArray(name, defaultValue)
	if !IsValidSelection(result, selectionList) {
		return nil, fmt.Errorf("Invalid Option Value: The option '%s' can contain only the following values:\n%s\nYou passed in: '%s'",
			name, strings.Join(selectionList, ","), jsOptionString(o.RawOptions[name]))
	}
	return result, nil
}

func jsOptionString(value any) string {
	switch v := value.(type) {
	case []string:
		return strings.Join(v, ",")
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = fmt.Sprint(item)
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(v)
	}
}

// IsValidSelection checks that all result items are in selectionList.
func IsValidSelection(result, selectionList []string) bool {
	if len(result) == 0 || len(selectionList) == 0 {
		return false
	}
	allowed := map[string]bool{}
	for _, item := range selectionList {
		allowed[item] = true
	}
	for _, item := range result {
		if !allowed[item] {
			return false
		}
	}
	return true
}
