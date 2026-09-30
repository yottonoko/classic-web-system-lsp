package core

import "testing"

func TestSanitizeFormattingOptionsBoundsSizesAndEnums(t *testing.T) {
	preserved := 1 << 20
	options := SanitizeFormattingOptions(FormattingOptions{
		TabSize:                            -4,
		HTMLTabSize:                        1 << 20,
		VBScriptLineContinuationIndentSize: 1 << 20,
		HTMLWrapAttributesIndentSize:       -1,
		PrintWidth:                         -80,
		MaxPreserveNewLines:                &preserved,
		HTMLWrapAttributes:                 "Force-Aligned ",
		CSSBraceStyle:                      "bogus",
	})
	if options.TabSize != 0 || options.HTMLTabSize != maxFormatIndentSize || options.VBScriptLineContinuationIndentSize != maxFormatIndentSize {
		t.Fatalf("indent sizes = %d, %d, %d", options.TabSize, options.HTMLTabSize, options.VBScriptLineContinuationIndentSize)
	}
	if options.HTMLWrapAttributesIndentSize != 0 || options.PrintWidth != 0 {
		t.Fatalf("negative sizes kept: %d, %d", options.HTMLWrapAttributesIndentSize, options.PrintWidth)
	}
	if *options.MaxPreserveNewLines != maxFormatPreservedNewLine || preserved != 1<<20 {
		t.Fatalf("max preserved newlines = %d, caller value = %d", *options.MaxPreserveNewLines, preserved)
	}
	if options.HTMLWrapAttributes != "force-aligned" || options.CSSBraceStyle != "" {
		t.Fatalf("enums = %q, %q", options.HTMLWrapAttributes, options.CSSBraceStyle)
	}
}
