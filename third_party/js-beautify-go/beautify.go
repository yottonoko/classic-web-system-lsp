// Package beautify formats JavaScript, CSS, and HTML using the js-beautify
// option model.
package beautify

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/yottonoko/js-beautify-go/internal/css"
	"github.com/yottonoko/js-beautify-go/internal/html"
	"github.com/yottonoko/js-beautify-go/internal/javascript"
)

// Options is the public option map accepted by the beautifiers.
//
// Option names may use either underscores or dashes. Per-language child maps
// named "js", "css", and "html" override parent options for that language.
type Options map[string]any

// LangBeautifier formats an embedded language block with inherited options.
type LangBeautifier func(source string, opts Options) (string, error)

// JS formats JavaScript source with js-beautify-compatible options.
func JS(source string, opts Options) (string, error) {
	key := publicCacheKeyFor("js", source, opts)
	if output, ok := publicCacheGet(key); ok {
		return output, nil
	}
	output, err := javascript.Beautify(source, map[string]any(opts))
	if err != nil {
		return "", err
	}
	publicCachePut(key, output)
	return output, nil
}

// CSS formats CSS, SCSS, and Sass-like source accepted by js-beautify.
func CSS(source string, opts Options) (string, error) {
	key := publicCacheKeyFor("css", source, opts)
	if output, ok := publicCacheGet(key); ok {
		return output, nil
	}
	output, err := css.Beautify(source, map[string]any(opts))
	if err != nil {
		return "", err
	}
	publicCachePut(key, output)
	return output, nil
}

// HTML formats HTML source and delegates embedded script/style blocks to the
// Go JavaScript and CSS beautifiers.
func HTML(source string, opts Options) (string, error) {
	key := publicCacheKeyFor("html", source, opts)
	if output, ok := publicCacheGet(key); ok {
		return output, nil
	}
	output, err := HTMLWithBeautifiers(source, opts, JS, CSS)
	if err != nil {
		return "", err
	}
	publicCachePut(key, output)
	return output, nil
}

// HTMLWithBeautifiers formats HTML source with caller-provided embedded
// JavaScript and CSS beautifiers.
func HTMLWithBeautifiers(source string, opts Options, js LangBeautifier, css LangBeautifier) (string, error) {
	var jsFn html.LangBeautifier
	if js != nil {
		jsFn = func(source string, opts map[string]any) (string, error) {
			return js(source, Options(opts))
		}
	}
	var cssFn html.LangBeautifier
	if css != nil {
		cssFn = func(source string, opts map[string]any) (string, error) {
			return css(source, Options(opts))
		}
	}
	return html.Beautify(source, map[string]any(opts), jsFn, cssFn)
}

// DefaultJSOptions returns JavaScript formatter defaults.
func DefaultJSOptions() Options {
	return Options(javascript.DefaultOptions())
}

// DefaultCSSOptions returns CSS formatter defaults.
func DefaultCSSOptions() Options {
	return Options(css.DefaultOptions())
}

// DefaultHTMLOptions returns HTML formatter defaults.
func DefaultHTMLOptions() Options {
	return Options(html.DefaultOptions())
}

type publicCacheKey struct {
	lang    string
	source  string
	options string
}

var publicCache = struct {
	sync.Mutex
	values map[publicCacheKey]string
	order  []publicCacheKey
}{
	values: map[publicCacheKey]string{},
}

const publicCacheLimit = 16

func publicCacheKeyFor(lang string, source string, opts Options) publicCacheKey {
	return publicCacheKey{lang: lang, source: source, options: stableOptionString(map[string]any(opts))}
}

func publicCacheGet(key publicCacheKey) (string, bool) {
	publicCache.Lock()
	defer publicCache.Unlock()
	output, ok := publicCache.values[key]
	return output, ok
}

func publicCachePut(key publicCacheKey, output string) {
	publicCache.Lock()
	defer publicCache.Unlock()
	if _, ok := publicCache.values[key]; ok {
		publicCache.values[key] = output
		return
	}
	if len(publicCache.order) >= publicCacheLimit {
		evict := publicCache.order[0]
		copy(publicCache.order, publicCache.order[1:])
		publicCache.order = publicCache.order[:len(publicCache.order)-1]
		delete(publicCache.values, evict)
	}
	publicCache.values[key] = output
	publicCache.order = append(publicCache.order, key)
}

func stableOptionString(value any) string {
	switch value := value.(type) {
	case nil:
		return "<nil>"
	case Options:
		return stableOptionString(map[string]any(value))
	case map[string]any:
		return stableStringMap(value)
	case map[string]string:
		mapped := make(map[string]any, len(value))
		for key, item := range value {
			mapped[key] = item
		}
		return stableStringMap(mapped)
	case []string:
		return strings.Join(value, "\x00")
	case []any:
		parts := make([]string, len(value))
		for i, item := range value {
			parts[i] = stableOptionString(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		if mapped, ok := reflectStringMap(value); ok {
			return stableStringMap(mapped)
		}
		return fmt.Sprintf("%T:%v", value, value)
	}
}

func stableStringMap(value map[string]any) string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			out.WriteByte(',')
		}
		out.WriteString(key)
		out.WriteByte(':')
		out.WriteString(stableOptionString(value[key]))
	}
	out.WriteByte('}')
	return out.String()
}

func reflectStringMap(value any) (map[string]any, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out, true
}
