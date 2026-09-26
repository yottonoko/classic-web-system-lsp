// Package cli implements js-beautify-compatible command-line behavior.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	beautify "github.com/yottonoko/js-beautify-go"
)

// Version is the CLI version string printed by --version.
const Version = "2.0.1-go"

// Config contains parsed command-line and configuration-file settings.
type Config struct {
	// Options stores formatter options collected from CLI and config files.
	Options map[string]any
	// Files stores input paths or "-" for stdin.
	Files []string
	// Outfile stores the output path when -o is used.
	Outfile string
	// Replace writes output back over the input file.
	Replace bool
	// Quiet suppresses status output.
	Quiet bool
	// Type selects js, css, or html formatting.
	Type string
}

var boolOptions = map[string]bool{
	"indent_with_tabs": true, "preserve_newlines": true, "space_in_paren": true,
	"space_in_empty_paren": true, "jslint_happy": true, "space_after_anon_function": true,
	"space_after_named_function": true, "unindent_chained_methods": true,
	"break_chained_methods": true, "keep_array_indentation": true, "unescape_strings": true,
	"e4x": true, "end_with_newline": true, "comma_first": true, "indent_empty_lines": true,
	"selector_separator_newline": true, "newline_between_rules": true, "space_around_combinator": true,
	"space_around_selector_separator": true, "inline_custom_elements": true, "indent_inner_html": true,
	"indent_handlebars": true, "editorconfig": true,
}

var numberOptions = map[string]bool{
	"indent_size": true, "indent_level": true, "max_preserve_newlines": true,
	"wrap_line_length": true, "wrap_attributes_min_attrs": true, "wrap_attributes_indent_size": true,
	"max_char": true,
}

var shortOptions = map[string]string{
	"s": "indent_size", "c": "indent_char", "e": "eol", "l": "indent_level",
	"t": "indent_with_tabs", "p": "preserve_newlines", "m": "max_preserve_newlines",
	"P": "space_in_paren", "Q": "space_in_empty_paren", "j": "jslint_happy",
	"a": "space_after_anon_function", "b": "brace_style", "u": "unindent_chained_methods",
	"B": "break_chained_methods", "k": "keep_array_indentation", "x": "unescape_strings",
	"w": "wrap_line_length", "X": "e4x", "n": "end_with_newline", "C": "comma_first",
	"O": "operator_position", "L": "selector_separator_newline", "N": "newline_between_rules",
	"A": "wrap_attributes", "M": "wrap_attributes_min_attrs", "i": "wrap_attributes_indent_size",
	"W": "max_char", "d": "inline", "U": "unformatted", "T": "content_unformatted",
	"I": "indent_inner_html", "H": "indent_handlebars", "S": "indent_scripts",
	"E": "extra_liners", "v": "version", "h": "help", "f": "files", "o": "outfile",
	"r": "replace", "q": "quiet",
}

// Run executes a js-beautify compatible command.
func Run(argv []string, scriptName string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	cfg, help, version, err := parseArgs(argv, scriptName)
	if err != nil {
		fmt.Fprintln(stderr, err)
		usage(scriptName, stderr)
		return 1
	}
	if version {
		fmt.Fprintln(stdout, Version)
		return 0
	}
	if help {
		usage(scriptName, stdout)
		return 0
	}
	if err := cfg.loadConfigChain(); err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintln(stderr, "Error while loading beautifier configuration.")
		return 1
	}
	if err := cfg.checkType(scriptName); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := cfg.checkFiles(stdin); err != nil {
		fmt.Fprintln(stderr, err)
		usage(scriptName, stderr)
		return 1
	}
	if err := processInputs(cfg, stdin, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func parseArgs(argv []string, scriptName string) (*Config, bool, bool, error) {
	cfg := &Config{Options: map[string]any{}, Type: ""}
	var configPath string
	var positional []string
	help := false
	version := false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			positional = append(positional, argv[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		name := strings.TrimLeft(arg, "-")
		value := ""
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			value = name[idx+1:]
			name = name[:idx]
		}
		if len(name) == 1 {
			if mapped, ok := shortOptions[name]; ok {
				name = mapped
			} else {
				return nil, false, false, fmt.Errorf("unknown option: %s", arg)
			}
		}
		name = strings.ReplaceAll(name, "-", "_")
		switch name {
		case "help":
			help = true
			continue
		case "version":
			version = true
			continue
		case "replace":
			cfg.Replace = true
			continue
		case "quiet":
			cfg.Quiet = true
			continue
		case "js":
			cfg.Type = "js"
			continue
		case "css":
			cfg.Type = "css"
			continue
		case "html":
			cfg.Type = "html"
			continue
		}
		needsValue := !boolOptions[name]
		if needsValue && value == "" {
			if i+1 >= len(argv) {
				return nil, false, false, fmt.Errorf("option %s requires a value", arg)
			}
			i++
			value = argv[i]
		} else if !needsValue && value == "" {
			value = "true"
		}
		switch name {
		case "files":
			cfg.Files = append(cfg.Files, value)
		case "outfile":
			cfg.Outfile = value
		case "type":
			cfg.Type = value
		case "config":
			configPath = value
		default:
			cfg.Options[name] = parseOptionValue(name, value)
		}
	}
	cfg.Files = append(cfg.Files, positional...)
	if configPath != "" {
		cfg.Options["config"] = configPath
	}
	return cfg, help, version, nil
}

func parseOptionValue(name, value string) any {
	if boolOptions[name] {
		return value != "false" && value != "0"
	}
	if numberOptions[name] {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	if strings.Contains(value, ",") && (name == "templating" || name == "inline" || name == "unformatted" || name == "content_unformatted" || name == "extra_liners") {
		return strings.Split(value, ",")
	}
	return value
}

func (c *Config) loadConfigChain() error {
	merged := defaultOptions()
	if home := userHome(); home != "" {
		_ = mergeConfigFile(merged, filepath.Join(home, ".jsbeautifyrc"))
	}
	if recursive := findRecursive(mustGetwd(), ".jsbeautifyrc"); recursive != "" {
		if err := mergeConfigFile(merged, recursive); err != nil {
			return err
		}
	}
	if config, _ := c.Options["config"].(string); config != "" {
		if err := mergeConfigFile(merged, config); err != nil {
			return err
		}
	}
	for key, value := range envOptions() {
		merged[key] = value
	}
	for key, value := range c.Options {
		if key != "config" {
			merged[key] = value
		}
	}
	c.Options = merged
	return nil
}

func defaultOptions() map[string]any {
	return map[string]any{
		"indent_size": 4, "indent_char": " ", "indent_level": 0,
		"indent_with_tabs": false, "preserve_newlines": true, "max_preserve_newlines": 10,
		"jslint_happy": false, "space_after_named_function": false, "space_after_anon_function": false,
		"brace_style": "collapse", "keep_array_indentation": false, "keep_function_indentation": false,
		"space_before_conditional": true, "break_chained_methods": false, "eval_code": false,
		"unescape_strings": false, "wrap_line_length": 0, "indent_empty_lines": false,
		"templating": []string{"auto"},
	}
}

func mergeConfigFile(options map[string]any, path string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	for key, value := range parsed {
		options[strings.ReplaceAll(key, "-", "_")] = value
	}
	return nil
}

func envOptions() map[string]any {
	out := map[string]any{}
	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if !ok || !strings.HasPrefix(strings.ToLower(key), "jsbeautify_") {
			continue
		}
		name := strings.TrimPrefix(strings.ToLower(key), "jsbeautify_")
		out[name] = parseOptionValue(name, value)
	}
	return out
}

func (c *Config) checkType(scriptName string) error {
	if c.Type == "" {
		switch {
		case strings.HasPrefix(scriptName, "css-"):
			c.Type = "css"
		case strings.HasPrefix(scriptName, "html-"):
			c.Type = "html"
		default:
			c.Type = "js"
		}
	}
	if c.Type != "js" && c.Type != "css" && c.Type != "html" {
		return fmt.Errorf("invalid type: %s", c.Type)
	}
	return nil
}

func (c *Config) checkFiles(stdin io.Reader) error {
	expanded := []string{}
	hadGlob := false
	for _, file := range c.Files {
		if file == "-" {
			expanded = append(expanded, "-")
			continue
		}
		if hasGlob(file) {
			hadGlob = true
			matches, err := expandGlob(file)
			if err != nil {
				return err
			}
			expanded = append(expanded, matches...)
			continue
		}
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("Unable to open path %q", file)
		}
		abs, _ := filepath.Abs(file)
		expanded = append(expanded, abs)
	}
	if c.Outfile != "" && len(expanded) == 0 {
		if _, err := os.Stat(c.Outfile); err == nil {
			expanded = append(expanded, c.Outfile)
			c.Replace = true
		}
	}
	if hadGlob || len(expanded) > 1 {
		c.Replace = true
	}
	if len(expanded) == 0 {
		expanded = append(expanded, "-")
	}
	seen := map[string]bool{}
	c.Files = c.Files[:0]
	for _, file := range expanded {
		key := file
		if file != "-" {
			key, _ = filepath.Abs(file)
		}
		if !seen[key] {
			seen[key] = true
			c.Files = append(c.Files, file)
		}
	}
	return nil
}

type inputResult struct {
	output string
	err    error
}

func processInputs(cfg *Config, stdin io.Reader, stdout io.Writer) error {
	if !shouldProcessInputsParallel(cfg) {
		for _, file := range cfg.Files {
			if err := processInput(file, cfg, stdin, stdout); err != nil {
				return err
			}
		}
		return nil
	}
	return processInputsParallel(cfg, stdout)
}

func shouldProcessInputsParallel(cfg *Config) bool {
	if len(cfg.Files) <= 1 || !cfg.Replace {
		return false
	}
	for _, file := range cfg.Files {
		if file == "-" {
			return false
		}
	}
	return true
}

func processInputsParallel(cfg *Config, stdout io.Writer) error {
	files := cfg.Files
	results := make([]inputResult, len(files))
	workers := min(runtime.GOMAXPROCS(0), 8, len(files))
	if workers < 2 {
		for _, file := range files {
			if err := processInput(file, cfg, nil, stdout); err != nil {
				return err
			}
		}
		return nil
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = makeInputResult(files[index], cfg, nil)
			}
		}()
	}
	for index := range files {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	for _, result := range results {
		if result.err != nil {
			return result.err
		}
		if result.output != "" {
			if _, err := io.WriteString(stdout, result.output); err != nil {
				return err
			}
		}
	}
	return nil
}

func processInput(file string, cfg *Config, stdin io.Reader, stdout io.Writer) error {
	result := makeInputResult(file, cfg, stdin)
	if result.err != nil {
		return result.err
	}
	if result.output != "" {
		_, err := io.WriteString(stdout, result.output)
		return err
	}
	return nil
}

func makeInputResult(file string, cfg *Config, stdin io.Reader) inputResult {
	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(stdin)
		if err != nil {
			return inputResult{err: err}
		}
	} else {
		data, err = os.ReadFile(file)
		if err != nil {
			return inputResult{err: err}
		}
	}
	options := cloneMap(cfg.Options)
	if enabled, _ := options["editorconfig"].(bool); enabled {
		applyEditorConfig(options, editorConfigPath(file, cfg))
	}
	pretty, err := makePretty(cfg.Type, string(data), options)
	if err != nil {
		return inputResult{err: err}
	}
	outfile := cfg.Outfile
	if cfg.Replace || outfile == "true" {
		outfile = file
	}
	if outfile == "" {
		return inputResult{output: pretty}
	}
	if file == "-" && outfile == "-" {
		return inputResult{output: pretty}
	}
	if err := os.MkdirAll(filepath.Dir(outfile), 0o755); err != nil {
		return inputResult{err: err}
	}
	if old, err := os.ReadFile(outfile); err == nil && string(old) == pretty {
		if !cfg.Quiet {
			return inputResult{output: fmt.Sprintf("beautified %s - unchanged\n", rel(outfile))}
		}
		return inputResult{}
	}
	if err := os.WriteFile(outfile, []byte(pretty), 0o644); err != nil {
		return inputResult{err: err}
	}
	if !cfg.Quiet {
		return inputResult{output: fmt.Sprintf("beautified %s\n", rel(outfile))}
	}
	return inputResult{}
}

func makePretty(fileType, code string, options map[string]any) (string, error) {
	switch fileType {
	case "js":
		return beautify.JS(code, beautify.Options(options))
	case "css":
		return beautify.CSS(code, beautify.Options(options))
	case "html":
		return beautify.HTML(code, beautify.Options(options))
	default:
		return "", fmt.Errorf("unknown type %q", fileType)
	}
}

func applyEditorConfig(options map[string]any, path string) {
	dir := filepath.Dir(path)
	for {
		ec := filepath.Join(dir, ".editorconfig")
		data, err := os.ReadFile(ec)
		if err == nil {
			parseEditorConfig(options, string(data))
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
}

func parseEditorConfig(options map[string]any, data string) {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "indent_style":
			options["indent_with_tabs"] = value == "tab"
		case "indent_size":
			options["indent_size"] = parseOptionValue("indent_size", value)
		case "max_line_length":
			if value == "off" {
				options["wrap_line_length"] = 0
			} else {
				options["wrap_line_length"] = parseOptionValue("wrap_line_length", value)
			}
		case "insert_final_newline":
			options["end_with_newline"] = value == "true"
		case "end_of_line":
			switch value {
			case "cr":
				options["eol"] = "\r"
			case "lf":
				options["eol"] = "\n"
			case "crlf":
				options["eol"] = "\r\n"
			}
		}
	}
}

func editorConfigPath(file string, cfg *Config) string {
	if file != "-" {
		return file
	}
	if cfg.Outfile != "" {
		return cfg.Outfile
	}
	return "stdin." + cfg.Type
}

func usage(scriptName string, out io.Writer) {
	fmt.Fprintf(out, `%s@%s

CLI Options:
  -f, --file       Input file(s) (Pass '-' for stdin)
  -r, --replace    Write output in-place, replacing input
  -o, --outfile    Write output to file (default stdout)
  --config         Path to config file
  --type           [js|css|html] ["js"]
  -q, --quiet      Suppress logging to stdout
  -h, --help       Show this help
  -v, --version    Show the version

Beautifier Options:
  -s, --indent-size                 Indentation size [4]
  -c, --indent-char                 Indentation character [" "]
  -t, --indent-with-tabs            Indent with tabs, overrides -s and -c
  -e, --eol                         Character(s) to use as line terminators.
  -n, --end-with-newline            End output with newline
  --editorconfig                    Use EditorConfig to set up the options
`, scriptName, Version)
}

func userHome() string {
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home
	}
	return os.Getenv("HOME")
}

func findRecursive(dir, name string) string {
	for {
		full := filepath.Join(dir, name)
		if _, err := os.Stat(full); err == nil {
			return full
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func hasGlob(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func expandGlob(pattern string) ([]string, error) {
	if !strings.Contains(pattern, "**") {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		return absSorted(matches), nil
	}
	root := "."
	if idx := strings.Index(pattern, "**"); idx > 0 {
		root = filepath.Clean(pattern[:idx])
	}
	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		ok, _ := doublestarMatch(pattern, path)
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	return absSorted(matches), err
}

func doublestarMatch(pattern, path string) (bool, error) {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return filepath.Match(pattern, path)
	}
	return strings.HasPrefix(path, strings.TrimSuffix(parts[0], "/")) &&
		strings.HasSuffix(path, strings.TrimPrefix(parts[1], "/")), nil
}

func absSorted(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		abs, _ := filepath.Abs(path)
		out = append(out, abs)
	}
	sort.Strings(out)
	return out
}

func cloneMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func rel(path string) string {
	if value, err := filepath.Rel(mustGetwd(), path); err == nil {
		return value
	}
	return path
}
