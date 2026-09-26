// Package core contains shared option, scanner, token, and output primitives.
package core

import (
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	parallelRenderMinLines       = 1024
	parallelRenderMinBytes       = 256 * 1024
	parallelRenderMinItems       = 80 * 1024
	parallelRenderMaxWorkers     = 8
	parallelRenderLinesPerWorker = 256
)

// OutputLine holds a single logical output line while formatting.
type OutputLine struct {
	parent                  *Output
	characterCount          int
	indentCount             int
	alignmentCount          int
	wrapPointIndex          int
	wrapPointCharacterCount int
	wrapPointIndentCount    int
	wrapPointAlignmentCount int
	items                   []string
}

type outputLineRenderSnapshot struct {
	indent string
	items  []string
}

func newOutputLine(parent *Output) *OutputLine {
	return &OutputLine{parent: parent, indentCount: -1}
}

func (l *OutputLine) cloneEmpty() *OutputLine {
	line := newOutputLine(l.parent)
	line.SetIndent(l.indentCount, l.alignmentCount)
	return line
}

// Item returns the item at index, accepting negative indexes from the end.
func (l *OutputLine) Item(index int) string {
	if index < 0 {
		index = len(l.items) + index
	}
	if index < 0 || index >= len(l.items) {
		return ""
	}
	return l.items[index]
}

// HasMatch reports whether any item contains substr.
func (l *OutputLine) HasMatch(substr string) bool {
	for i := len(l.items) - 1; i >= 0; i-- {
		if strings.Contains(l.items[i], substr) {
			return true
		}
	}
	return false
}

// SetIndent sets indentation for an empty line.
func (l *OutputLine) SetIndent(indent, alignment int) {
	if l.IsEmpty() {
		l.indentCount = indent
		l.alignmentCount = alignment
		l.characterCount = l.parent.GetIndentSize(indent, alignment)
	}
}

func (l *OutputLine) setWrapPoint() {
	if l.parent.WrapLineLength > 0 {
		l.wrapPointIndex = len(l.items)
		l.wrapPointCharacterCount = l.characterCount
		l.wrapPointIndentCount = l.parent.nextLine.indentCount
		l.wrapPointAlignmentCount = l.parent.nextLine.alignmentCount
	}
}

func (l *OutputLine) shouldWrap() bool {
	return l.wrapPointIndex != 0 &&
		l.characterCount > l.parent.WrapLineLength &&
		l.wrapPointCharacterCount > l.parent.nextLine.characterCount
}

func (l *OutputLine) allowWrap() bool {
	if !l.shouldWrap() {
		return false
	}
	l.parent.AddNewLine(false)
	next := l.parent.CurrentLine
	next.SetIndent(l.wrapPointIndentCount, l.wrapPointAlignmentCount)
	next.items = append([]string{}, l.items[l.wrapPointIndex:]...)
	l.items = append([]string{}, l.items[:l.wrapPointIndex]...)
	next.characterCount += l.characterCount - l.wrapPointCharacterCount
	l.characterCount = l.wrapPointCharacterCount
	if len(next.items) > 0 && next.items[0] == " " {
		next.items = next.items[1:]
		next.characterCount--
	}
	return true
}

// IsEmpty reports whether the line has no printable items.
func (l *OutputLine) IsEmpty() bool {
	return len(l.items) == 0
}

// Last returns the final item on the line.
func (l *OutputLine) Last() string {
	if l.IsEmpty() {
		return ""
	}
	return l.items[len(l.items)-1]
}

// CharacterCount returns the current rendered width of the line.
func (l *OutputLine) CharacterCount() int {
	return l.characterCount
}

// Push appends an item and updates the display width.
func (l *OutputLine) Push(item string) {
	l.items = append(l.items, item)
	if idx := strings.LastIndex(item, "\n"); idx != -1 {
		l.characterCount = displayWidth(item[idx+1:])
	} else {
		l.characterCount += displayWidth(item)
	}
}

// Pop removes and returns the final item.
func (l *OutputLine) Pop() string {
	if l.IsEmpty() {
		return ""
	}
	item := l.items[len(l.items)-1]
	l.items = l.items[:len(l.items)-1]
	l.characterCount -= displayWidth(item)
	return item
}

func (l *OutputLine) removeIndent() {
	if l.indentCount > 0 {
		l.indentCount--
		l.characterCount -= l.parent.IndentSize
	}
}

func (l *OutputLine) removeWrapIndent() {
	if l.wrapPointIndentCount > 0 {
		l.wrapPointIndentCount--
	}
}

// Trim removes trailing single-space items.
func (l *OutputLine) Trim() {
	for l.Last() == " " {
		l.items = l.items[:len(l.items)-1]
		l.characterCount--
	}
}

// String renders the line with indentation.
func (l *OutputLine) String() string {
	if l.IsEmpty() {
		if l.parent.IndentEmptyLines {
			return l.parent.GetIndentString(l.indentCount, 0)
		}
		return ""
	}
	return l.parent.GetIndentString(l.indentCount, l.alignmentCount) + strings.Join(l.items, "")
}

func displayWidth(value string) int {
	for i := 0; i < len(value); i++ {
		if value[i] >= utf8.RuneSelf {
			return i + utf8.RuneCountInString(value[i:])
		}
	}
	return len(value)
}

type indentStringCache struct {
	cache            []string
	indentSize       int
	indentString     string
	baseString       string
	baseStringLength int
}

func newIndentStringCache(options OutputOptions, baseIndentString string) *indentStringCache {
	indentString := options.IndentChar
	if !options.IndentWithTabs {
		indentString = strings.Repeat(options.IndentChar, options.IndentSize)
	}
	if options.IndentLevel > 0 {
		baseIndentString = strings.Repeat(indentString, options.IndentLevel)
	}
	return &indentStringCache{
		cache:            []string{""},
		indentSize:       options.IndentSize,
		indentString:     indentString,
		baseString:       baseIndentString,
		baseStringLength: len(baseIndentString),
	}
}

func (c *indentStringCache) getIndentSize(indent, column int) int {
	result := c.baseStringLength
	if indent < 0 {
		result = 0
	}
	return result + indent*c.indentSize + column
}

func (c *indentStringCache) getIndentString(indentLevel, column int) string {
	result := c.baseString
	if indentLevel < 0 {
		indentLevel = 0
		result = ""
	}
	column += indentLevel * c.indentSize
	c.ensureCache(column)
	return result + c.cache[column]
}

func (c *indentStringCache) ensureCache(column int) {
	for column >= len(c.cache) {
		c.addColumn()
	}
}

func (c *indentStringCache) addColumn() {
	column := len(c.cache)
	result := ""
	if c.indentSize > 0 && column >= c.indentSize {
		indent := column / c.indentSize
		column -= indent * c.indentSize
		result = strings.Repeat(c.indentString, indent)
	}
	if column > 0 {
		result += strings.Repeat(" ", column)
	}
	c.cache = append(c.cache, result)
}

// OutputOptions configures output rendering.
type OutputOptions struct {
	// EndWithNewline appends a final line terminator.
	EndWithNewline bool
	// IndentSize is the number of columns in one indentation level.
	IndentSize int
	// IndentChar is the character used to build indentation.
	IndentChar string
	// IndentLevel is the initial indentation level.
	IndentLevel int
	// IndentWithTabs uses tab indentation.
	IndentWithTabs bool
	// WrapLineLength is the preferred maximum output line length.
	WrapLineLength int
	// IndentEmptyLines emits indentation on otherwise empty lines.
	IndentEmptyLines bool
}

// OutputOptionsFromBase converts BaseOptions to output-specific options.
func OutputOptionsFromBase(o *BaseOptions) OutputOptions {
	return OutputOptions{
		EndWithNewline:   o.EndWithNewline,
		IndentSize:       o.IndentSize,
		IndentChar:       o.IndentChar,
		IndentLevel:      o.IndentLevel,
		IndentWithTabs:   o.IndentWithTabs,
		WrapLineLength:   o.WrapLineLength,
		IndentEmptyLines: o.IndentEmptyLines,
	}
}

// Output accumulates formatted text, indentation, wrapping, and spacing state.
type Output struct {
	indentCache *indentStringCache
	// Raw preserves source layout when adding raw tokens.
	Raw            bool
	endWithNewline bool
	// IndentSize is the number of columns in one indentation level.
	IndentSize int
	// WrapLineLength is the preferred maximum output line length.
	WrapLineLength int
	// IndentEmptyLines emits indentation on otherwise empty lines.
	IndentEmptyLines bool
	lines            []*OutputLine
	// PreviousLine points to the rendered line before CurrentLine.
	PreviousLine *OutputLine
	// CurrentLine points to the line currently receiving tokens.
	CurrentLine *OutputLine
	nextLine    *OutputLine
	// SpaceBeforeToken requests a space before the next printable token.
	SpaceBeforeToken bool
	// NonBreakingSpace keeps the next requested space from becoming a wrap point.
	NonBreakingSpace bool
	// PreviousTokenWrapped reports whether the last token caused wrapping.
	PreviousTokenWrapped bool
}

// NewOutput returns an output accumulator initialized with base indentation.
func NewOutput(options OutputOptions, baseIndentString string) *Output {
	out := &Output{
		indentCache:      newIndentStringCache(options, baseIndentString),
		endWithNewline:   options.EndWithNewline,
		IndentSize:       options.IndentSize,
		WrapLineLength:   options.WrapLineLength,
		IndentEmptyLines: options.IndentEmptyLines,
		nextLine:         newOutputLine(nil),
	}
	out.nextLine.parent = out
	out.addOutputLine()
	return out
}

func (o *Output) addOutputLine() {
	o.PreviousLine = o.CurrentLine
	o.CurrentLine = o.nextLine.cloneEmpty()
	o.lines = append(o.lines, o.CurrentLine)
}

// GetLineNumber returns the number of accumulated output lines.
func (o *Output) GetLineNumber() int {
	return len(o.lines)
}

// GetIndentString returns the cached indentation string for indent and column.
func (o *Output) GetIndentString(indent, column int) string {
	return o.indentCache.getIndentString(indent, column)
}

// GetIndentSize returns the display width for indent and column.
func (o *Output) GetIndentSize(indent, column int) int {
	return o.indentCache.getIndentSize(indent, column)
}

// IsEmpty reports whether no printable output has been accumulated.
func (o *Output) IsEmpty() bool {
	return o.PreviousLine == nil && o.CurrentLine.IsEmpty()
}

// AddNewLine starts a new line unless suppressed by state and force is false.
func (o *Output) AddNewLine(force bool) bool {
	if o.IsEmpty() || (!force && o.JustAddedNewline()) {
		return false
	}
	if !o.Raw {
		o.addOutputLine()
	}
	return true
}

// GetCode renders accumulated output using eol as the line terminator.
func (o *Output) GetCode(eol string) string {
	o.Trim(true)
	last := o.CurrentLine.Pop()
	if last != "" {
		last = strings.TrimRight(last, "\n")
		o.CurrentLine.Push(last)
	}
	if o.endWithNewline {
		o.addOutputLine()
	}
	code := ""
	byteCount, itemCount := o.estimateRenderWork()
	if lineRenderWorkerCount(len(o.lines), byteCount, itemCount) >= 2 {
		lines, exactByteCount := o.snapshotRenderLines()
		code = strings.Join(renderLineSnapshots(lines, exactByteCount, itemCount), "\n")
	} else {
		code = o.getCodeSequential()
	}
	if eol != "\n" {
		code = strings.ReplaceAll(code, "\n", eol)
	}
	return code
}

func (o *Output) getCodeSequential() string {
	parts := make([]string, len(o.lines))
	for i, line := range o.lines {
		parts[i] = line.String()
	}
	return strings.Join(parts, "\n")
}

func (o *Output) estimateRenderWork() (int, int) {
	if len(o.lines) < parallelRenderMinLines {
		return 0, 0
	}
	byteCount := 0
	itemCount := 0
	for _, line := range o.lines {
		for _, item := range line.items {
			byteCount += len(item)
		}
		itemCount += len(line.items)
		if byteCount >= parallelRenderMinBytes && itemCount >= parallelRenderMinItems {
			return byteCount, itemCount
		}
	}
	if len(o.lines) > 1 {
		byteCount += len(o.lines) - 1
	}
	return byteCount, itemCount
}

func (o *Output) snapshotRenderLines() ([]outputLineRenderSnapshot, int) {
	lines := make([]outputLineRenderSnapshot, len(o.lines))
	byteCount := 0
	for i, line := range o.lines {
		snapshot := outputLineRenderSnapshot{}
		if line.IsEmpty() {
			if o.IndentEmptyLines {
				snapshot.indent = o.GetIndentString(line.indentCount, 0)
			}
		} else {
			snapshot.indent = o.GetIndentString(line.indentCount, line.alignmentCount)
			snapshot.items = line.items
		}
		lineBytes := len(snapshot.indent)
		for _, item := range snapshot.items {
			lineBytes += len(item)
		}
		byteCount += lineBytes
		lines[i] = snapshot
	}
	if len(lines) > 1 {
		byteCount += len(lines) - 1
	}
	return lines, byteCount
}

func renderLineSnapshots(lines []outputLineRenderSnapshot, byteCount int, itemCount int) []string {
	parts := make([]string, len(lines))
	workers := lineRenderWorkerCount(len(lines), byteCount, itemCount)
	if workers < 2 {
		renderLineSnapshotsRange(parts, lines)
		return parts
	}
	chunkSize := (len(lines) + workers - 1) / workers
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start := worker * chunkSize
		end := start + chunkSize
		if end > len(lines) {
			end = len(lines)
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(start int, end int) {
			defer wg.Done()
			renderLineSnapshotsRange(parts[start:end], lines[start:end])
		}(start, end)
	}
	wg.Wait()
	return parts
}

func renderLineSnapshotsRange(parts []string, lines []outputLineRenderSnapshot) {
	for index, line := range lines {
		if len(line.items) == 0 {
			parts[index] = line.indent
			continue
		}
		parts[index] = line.indent + strings.Join(line.items, "")
	}
}

func lineRenderWorkerCount(lineCount int, byteCount int, itemCount int) int {
	if lineCount < parallelRenderMinLines || byteCount < parallelRenderMinBytes || itemCount < parallelRenderMinItems {
		return 1
	}
	workers := (lineCount + parallelRenderLinesPerWorker - 1) / parallelRenderLinesPerWorker
	if maxProcs := runtime.GOMAXPROCS(0); workers > maxProcs {
		workers = maxProcs
	}
	if workers > parallelRenderMaxWorkers {
		workers = parallelRenderMaxWorkers
	}
	return workers
}

// SetWrapPoint marks the current position as a legal wrap point.
func (o *Output) SetWrapPoint() {
	o.CurrentLine.setWrapPoint()
}

// SetIndent sets indentation for the current and next line.
func (o *Output) SetIndent(indent, alignment int) bool {
	o.nextLine.SetIndent(indent, alignment)
	if len(o.lines) > 1 {
		o.CurrentLine.SetIndent(indent, alignment)
		return true
	}
	o.CurrentLine.SetIndent(0, 0)
	return false
}

// AddRawToken appends a token without normal spacing or indentation changes.
func (o *Output) AddRawToken(token *Token) {
	for i := 0; i < token.Newlines; i++ {
		o.addOutputLine()
	}
	o.CurrentLine.SetIndent(-1, 0)
	o.CurrentLine.Push(token.WhitespaceBefore)
	o.CurrentLine.Push(token.Text)
	o.SpaceBeforeToken = false
	o.NonBreakingSpace = false
	o.PreviousTokenWrapped = false
}

// AddRawText appends text without normal spacing or indentation changes.
func (o *Output) AddRawText(text string) {
	for index, line := range strings.Split(text, "\n") {
		if index > 0 {
			o.addOutputLine()
		}
		o.CurrentLine.SetIndent(-1, 0)
		o.CurrentLine.Push(line)
	}
	o.SpaceBeforeToken = false
	o.NonBreakingSpace = false
	o.PreviousTokenWrapped = false
}

// AddToken appends a printable token with pending spacing and wrapping applied.
func (o *Output) AddToken(printableToken string) {
	o.addSpaceBeforeToken()
	o.CurrentLine.Push(printableToken)
	o.SpaceBeforeToken = false
	o.NonBreakingSpace = false
	o.PreviousTokenWrapped = o.CurrentLine.allowWrap()
}

func (o *Output) addSpaceBeforeToken() {
	if o.SpaceBeforeToken && !o.JustAddedNewline() {
		if !o.NonBreakingSpace {
			o.SetWrapPoint()
		}
		o.CurrentLine.Push(" ")
	}
}

// RemoveIndent removes one indentation level from lines starting at index.
func (o *Output) RemoveIndent(index int) {
	for index < len(o.lines) {
		o.lines[index].removeIndent()
		index++
	}
	o.CurrentLine.removeWrapIndent()
}

// Trim removes trailing spaces and optionally trailing empty lines.
func (o *Output) Trim(eatNewlines bool) {
	o.CurrentLine.Trim()
	for eatNewlines && len(o.lines) > 1 && o.CurrentLine.IsEmpty() {
		o.lines = o.lines[:len(o.lines)-1]
		o.CurrentLine = o.lines[len(o.lines)-1]
		o.CurrentLine.Trim()
	}
	if len(o.lines) > 1 {
		o.PreviousLine = o.lines[len(o.lines)-2]
	} else {
		o.PreviousLine = nil
	}
}

// JustAddedNewline reports whether the current line is empty.
func (o *Output) JustAddedNewline() bool {
	return o.CurrentLine.IsEmpty()
}

// JustAddedBlankline reports whether the output just added a blank line.
func (o *Output) JustAddedBlankline() bool {
	return o.IsEmpty() || (o.CurrentLine.IsEmpty() && o.PreviousLine != nil && o.PreviousLine.IsEmpty())
}

// EnsureEmptyLineAbove inserts an empty line above matching structural output.
func (o *Output) EnsureEmptyLineAbove(startsWith, endsWith string) {
	index := len(o.lines) - 2
	for index >= 0 {
		line := o.lines[index]
		if line.IsEmpty() {
			break
		}
		if !strings.HasPrefix(line.Item(0), startsWith) && line.Item(-1) != endsWith {
			newLine := newOutputLine(o)
			o.lines = append(o.lines[:index+1], append([]*OutputLine{newLine}, o.lines[index+1:]...)...)
			if len(o.lines) > 1 {
				o.PreviousLine = o.lines[len(o.lines)-2]
			}
			break
		}
		index--
	}
}
