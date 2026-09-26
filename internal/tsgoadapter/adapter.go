package tsgoadapter

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/microsoft/typescript-go/aspadapter"
)

const PinnedCommit = "79fe60a804acfa49d81e67c499bd7509d70aa8ce"

type Status struct {
	SubmodulePath string
	Available     bool
	Reason        string
}

func Check(root string) Status {
	path := filepath.Join(root, "third_party", "typescript-go")
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err != nil {
		return Status{SubmodulePath: path, Available: false, Reason: err.Error()}
	}
	return Status{SubmodulePath: path, Available: true}
}

type Identifier = aspadapter.Identifier

type Analysis = aspadapter.Analysis
type JavaScriptLiteralContext = aspadapter.JavaScriptLiteralContext
type FormattingOptions = aspadapter.FormattingOptions
type TextChange = aspadapter.TextChange
type NavigationValueKind = aspadapter.NavigationValueKind
type SourceRange = aspadapter.SourceRange
type NavigationValue = aspadapter.NavigationValue
type NavigationExpression = aspadapter.NavigationExpression
type NavigationSink = aspadapter.NavigationSink
type NavigationAnalysis = aspadapter.NavigationAnalysis
type JavaScriptNavigationAnalysis = aspadapter.JavaScriptNavigationAnalysis
type JavaScriptNavigationSink = aspadapter.JavaScriptNavigationSink
type JavaScriptNavigationExpression = aspadapter.JavaScriptNavigationExpression
type JavaScriptNavigationValue = aspadapter.JavaScriptNavigationValue

const (
	JavaScriptLiteralRaw          = aspadapter.JavaScriptLiteralRaw
	JavaScriptLiteralSingleQuoted = aspadapter.JavaScriptLiteralSingleQuoted
	JavaScriptLiteralDoubleQuoted = aspadapter.JavaScriptLiteralDoubleQuoted
	JavaScriptLiteralTemplate     = aspadapter.JavaScriptLiteralTemplate
	JavaScriptLiteralRegex        = aspadapter.JavaScriptLiteralRegex

	NavigationValueLimit    = aspadapter.NavigationValueLimit
	NavigationValueLiteral  = aspadapter.NavigationValueLiteral
	NavigationValueTemplate = aspadapter.NavigationValueTemplate
	NavigationValueUnknown  = aspadapter.NavigationValueUnknown
)

// ClassifyJavaScriptLiteralContexts classifies byte offsets with the pinned
// TypeScript parser's lexical-goal handling.
func ClassifyJavaScriptLiteralContexts(source string, offsets []int) []JavaScriptLiteralContext {
	return aspadapter.ClassifyJavaScriptLiteralContexts(source, offsets)
}

// ClassifyJavaScriptLiteralContextsContext classifies byte offsets with
// cancellation support.
func ClassifyJavaScriptLiteralContextsContext(ctx context.Context, source string, offsets []int) ([]JavaScriptLiteralContext, error) {
	return aspadapter.ClassifyJavaScriptLiteralContextsContext(ctx, source, offsets)
}

func AnalyzeJavaScript(source string) Analysis {
	return aspadapter.AnalyzeJavaScript(source)
}

func AnalyzeJavaScriptAST(source string) Analysis {
	return aspadapter.AnalyzeJavaScriptAST(source)
}

// AnalyzeJavaScriptNavigation evaluates navigation sinks in virtual
// JavaScript text without requiring a workspace project.
func AnalyzeJavaScriptNavigation(ctx context.Context, source string) (NavigationAnalysis, error) {
	return aspadapter.AnalyzeJavaScriptNavigation(ctx, source)
}

// AnalyzeJavaScriptNavigationContext is an explicit context-taking alias for
// generic adapter call sites.
func AnalyzeJavaScriptNavigationContext(ctx context.Context, source string) (NavigationAnalysis, error) {
	return aspadapter.AnalyzeJavaScriptNavigationContext(ctx, source)
}

// AnalyzeJavaScriptNavigationNoContext evaluates navigation without
// cancellation.
func AnalyzeJavaScriptNavigationNoContext(source string) NavigationAnalysis {
	return aspadapter.AnalyzeJavaScriptNavigationNoContext(source)
}

func FormatJavaScript(source string, options FormattingOptions) []TextChange {
	return aspadapter.FormatJavaScript(source, options)
}

func FormatJavaScriptAfterKeystroke(source string, position int, key string, options FormattingOptions) []TextChange {
	return aspadapter.FormatJavaScriptAfterKeystroke(source, position, key, options)
}

// Project is a reusable, real TypeScript compiler and language-service project.
type Project struct {
	inner *aspadapter.Project
	mu    sync.Mutex
}

type ProjectState = aspadapter.ProjectState
type ProjectOptions = aspadapter.ProjectOptions

// NewProject creates a JavaScript project rooted at root.
func NewProject(root string) *Project {
	return &Project{inner: aspadapter.NewProject(root)}
}

// UpdateFiles atomically replaces the virtual JavaScript roots in the project.
func (p *Project) UpdateFiles(files map[string]string) (ProjectState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner.UpdateFiles(files)
}

// Update atomically replaces project files and compiler options.
func (p *Project) Update(files map[string]string, options ProjectOptions) (ProjectState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner.Update(files, options)
}

// UpdateDelta applies changed and removed project files without replacing the
// caller-side file map. The underlying project still reuses its single-file
// incremental compiler update when exactly one existing file changed.
func (p *Project) UpdateDelta(upserts map[string]string, deletes []string, options ProjectOptions) (ProjectState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner.UpdateDelta(upserts, deletes, options)
}

// State reports the actual compiler project lifecycle counters.
func (p *Project) State() ProjectState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner.State()
}

// Request forwards a standard LSP request to the TypeScript language service.
func (p *Project) Request(ctx context.Context, method string, params []byte) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inner.Request(ctx, method, params)
}
