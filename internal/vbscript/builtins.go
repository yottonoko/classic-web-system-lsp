package vbscript

import "strings"

type builtinFunctionSpec struct {
	Label      string
	Signature  string
	ReturnType string
	Summary    string
}

type builtinConstantSpec struct {
	Label string
	Type  string
}

var builtinFunctionCatalog = []builtinFunctionSpec{
	{"CStr", "CStr(value)", "String", "Converts a value to String."},
	{"CByte", "CByte(value)", "Number", "Converts a value to Byte."},
	{"CInt", "CInt(value)", "Number", "Converts a value to Integer."},
	{"CLng", "CLng(value)", "Number", "Converts a value to Long."},
	{"CSng", "CSng(value)", "Number", "Converts a value to Single."},
	{"CDbl", "CDbl(value)", "Number", "Converts a value to Double."},
	{"CCur", "CCur(value)", "Currency", "Converts a value to Currency."},
	{"CDec", "CDec(value)", "Decimal", "Converts a value to Decimal."},
	{"CBool", "CBool(value)", "Boolean", "Converts a value to Boolean."},
	{"CDate", "CDate(value)", "Date", "Converts a value to Date."},
	{"CVar", "CVar(value)", "Variant", "Converts a value to Variant."},
	{"CVErr", "CVErr(errorNumber)", "Error", "Converts an error number to an Error subtype."},
	{"Asc", "Asc(string)", "Number", "Returns the ANSI character code for a string."},
	{"AscB", "AscB(string)", "Number", "Returns the first byte of a string."},
	{"AscW", "AscW(string)", "Number", "Returns the Unicode character code for a string."},
	{"Chr", "Chr(charCode)", "String", "Returns the character for an ANSI code."},
	{"ChrB", "ChrB(charCode)", "String", "Returns a single-byte character for a code."},
	{"ChrW", "ChrW(charCode)", "String", "Returns the Unicode character for a code."},
	{"Hex", "Hex(number)", "String", "Returns the hexadecimal value of a number."},
	{"Oct", "Oct(number)", "String", "Returns the octal value of a number."},
	{"Array", "Array(values)", "Array", "Creates a Variant array."},
	{"Filter", "Filter(inputStrings, value, include, compare)", "Array", "Returns matching entries from a string array."},
	{"Join", "Join(list, delimiter)", "String", "Joins array entries into a string."},
	{"LBound", "LBound(array, dimension)", "Number", "Returns the smallest available subscript for an array dimension."},
	{"Split", "Split(expression, delimiter, count, compare)", "Array", "Splits a string into an array."},
	{"UBound", "UBound(array, dimension)", "Number", "Returns the largest available subscript for an array dimension."},
	{"LCase", "LCase(value)", "String", "Converts a string to lowercase."},
	{"UCase", "UCase(value)", "String", "Converts a string to uppercase."},
	{"Trim", "Trim(value)", "String", "Removes leading and trailing spaces."},
	{"LTrim", "LTrim(value)", "String", "Removes leading spaces."},
	{"RTrim", "RTrim(value)", "String", "Removes trailing spaces."},
	{"Len", "Len(value)", "Number", "Returns the number of characters in a string."},
	{"LenB", "LenB(value)", "Number", "Returns the number of bytes in a string."},
	{"InStr", "InStr(start, string1, string2, compare)", "Number", "Returns the position of one string within another."},
	{"InStrB", "InStrB(start, string1, string2, compare)", "Number", "Returns the byte position of one string within another."},
	{"InStrRev", "InStrRev(string1, string2, start, compare)", "Number", "Returns the position of one string within another from the end."},
	{"Replace", "Replace(expression, find, replaceWith, start, count, compare)", "String", "Returns a string with replacements applied."},
	{"Left", "Left(value, length)", "String", "Returns the left part of a string."},
	{"LeftB", "LeftB(value, length)", "String", "Returns the left bytes of a string."},
	{"Right", "Right(value, length)", "String", "Returns the right part of a string."},
	{"RightB", "RightB(value, length)", "String", "Returns the right bytes of a string."},
	{"Mid", "Mid(value, start, length)", "String", "Returns part of a string."},
	{"MidB", "MidB(value, start, length)", "String", "Returns bytes from a string."},
	{"Space", "Space(number)", "String", "Returns a string of spaces."},
	{"StrComp", "StrComp(string1, string2, compare)", "Number", "Compares two strings."},
	{"String", "String(number, character)", "String", "Returns a repeated character string."},
	{"StrReverse", "StrReverse(value)", "String", "Reverses a string."},
	{"Date", "Date()", "Date", "Returns the current system date."},
	{"Now", "Now()", "Date", "Returns the current date and time."},
	{"Time", "Time()", "Date", "Returns the current system time."},
	{"Timer", "Timer()", "Number", "Returns the number of seconds since midnight."},
	{"DateAdd", "DateAdd(interval, number, date)", "Date", "Returns a date with an interval added."},
	{"DateDiff", "DateDiff(interval, date1, date2, firstDayOfWeek, firstWeekOfYear)", "Number", "Returns the number of intervals between two dates."},
	{"DatePart", "DatePart(interval, date, firstDayOfWeek, firstWeekOfYear)", "Number", "Returns part of a date."},
	{"DateSerial", "DateSerial(year, month, day)", "Date", "Returns a date from year, month, and day values."},
	{"DateValue", "DateValue(date)", "Date", "Returns a date value."},
	{"Day", "Day(date)", "Number", "Returns the day of the month."},
	{"FormatDateTime", "FormatDateTime(date, namedFormat)", "String", "Formats a date or time expression."},
	{"Hour", "Hour(time)", "Number", "Returns the hour of the day."},
	{"IsDate", "IsDate(value)", "Boolean", "Returns whether a value can be converted to a date."},
	{"Minute", "Minute(time)", "Number", "Returns the minute of the hour."},
	{"Month", "Month(date)", "Number", "Returns the month of the year."},
	{"MonthName", "MonthName(month, abbreviate)", "String", "Returns the name of a month."},
	{"Second", "Second(time)", "Number", "Returns the second of the minute."},
	{"TimeSerial", "TimeSerial(hour, minute, second)", "Date", "Returns a time from hour, minute, and second values."},
	{"TimeValue", "TimeValue(time)", "Date", "Returns a time value."},
	{"Weekday", "Weekday(date, firstDayOfWeek)", "Number", "Returns the weekday number."},
	{"WeekdayName", "WeekdayName(weekday, abbreviate, firstDayOfWeek)", "String", "Returns the name of a weekday."},
	{"Year", "Year(date)", "Number", "Returns the year."},
	{"FormatCurrency", "FormatCurrency(expression, digitsAfterDecimal, includeLeadingDigit, useParensForNegativeNumbers, groupDigits)", "String", "Formats an expression as currency."},
	{"FormatNumber", "FormatNumber(expression, digitsAfterDecimal, includeLeadingDigit, useParensForNegativeNumbers, groupDigits)", "String", "Formats an expression as a number."},
	{"FormatPercent", "FormatPercent(expression, digitsAfterDecimal, includeLeadingDigit, useParensForNegativeNumbers, groupDigits)", "String", "Formats an expression as a percentage."},
	{"GetLocale", "GetLocale()", "Number", "Returns the current locale identifier."},
	{"SetLocale", "SetLocale(locale)", "Number", "Sets the current locale and returns the previous locale."},
	{"Abs", "Abs(number)", "Number", "Returns the absolute value of a number."},
	{"Atn", "Atn(number)", "Number", "Returns the arctangent of a number."},
	{"Cos", "Cos(number)", "Number", "Returns the cosine of an angle."},
	{"Exp", "Exp(number)", "Number", "Returns e raised to a power."},
	{"Fix", "Fix(number)", "Number", "Returns the integer part of a number."},
	{"Int", "Int(number)", "Number", "Returns the integer part of a number."},
	{"Log", "Log(number)", "Number", "Returns the natural logarithm of a number."},
	{"Rnd", "Rnd(number)", "Number", "Returns a random number."},
	{"Randomize", "Randomize(number)", "Variant", "Initializes the random-number generator."},
	{"Round", "Round(number, decimalPlaces)", "Number", "Rounds a number."},
	{"Sgn", "Sgn(number)", "Number", "Returns the sign of a number."},
	{"Sin", "Sin(number)", "Number", "Returns the sine of an angle."},
	{"Sqr", "Sqr(number)", "Number", "Returns the square root of a number."},
	{"Tan", "Tan(number)", "Number", "Returns the tangent of an angle."},
	{"CreateObject", "CreateObject(progId)", "Object", "Creates an automation object."},
	{"GetObject", "GetObject(pathName, class)", "Object", "Returns an existing automation object."},
	{"GetRef", "GetRef(procedureName)", "Object", "Returns a reference to a procedure."},
	{"InputBox", "InputBox(prompt, title, default, xpos, ypos, helpfile, context)", "String", "Displays an input dialog box."},
	{"LoadPicture", "LoadPicture(pictureName)", "Object", "Returns a picture object."},
	{"MsgBox", "MsgBox(prompt, buttons, title, helpfile, context)", "Number", "Displays a message dialog box."},
	{"Eval", "Eval(expression)", "Variant", "Evaluates an expression."},
	{"IsArray", "IsArray(value)", "Boolean", "Returns whether a value is an array."},
	{"IsNull", "IsNull(value)", "Boolean", "Returns whether a value is Null."},
	{"IsEmpty", "IsEmpty(value)", "Boolean", "Returns whether a variable is Empty."},
	{"IsNumeric", "IsNumeric(value)", "Boolean", "Returns whether a value can be evaluated as a number."},
	{"IsObject", "IsObject(value)", "Boolean", "Returns whether a value is an automation object."},
	{"RGB", "RGB(red, green, blue)", "Number", "Returns an RGB color value."},
	{"ScriptEngine", "ScriptEngine()", "String", "Returns the script engine name."},
	{"ScriptEngineBuildVersion", "ScriptEngineBuildVersion()", "Number", "Returns the script engine build version."},
	{"ScriptEngineMajorVersion", "ScriptEngineMajorVersion()", "Number", "Returns the script engine major version."},
	{"ScriptEngineMinorVersion", "ScriptEngineMinorVersion()", "Number", "Returns the script engine minor version."},
	{"TypeName", "TypeName(value)", "String", "Returns the subtype name for a variable."},
	{"VarType", "VarType(value)", "Number", "Returns the subtype code for a variable."},
}

var builtinConstantCatalog = []builtinConstantSpec{
	{"adBigInt", "Number"}, {"adBinary", "Number"}, {"adBoolean", "Number"}, {"adChar", "Number"},
	{"adCurrency", "Number"}, {"adDate", "Number"}, {"adDBTimeStamp", "Number"}, {"adDecimal", "Number"},
	{"adDouble", "Number"}, {"adGUID", "Number"}, {"adIDispatch", "Number"}, {"adInteger", "Number"},
	{"adLongVarBinary", "Number"}, {"adLongVarChar", "Number"}, {"adLongVarWChar", "Number"}, {"adNumeric", "Number"},
	{"adSingle", "Number"}, {"adSmallInt", "Number"}, {"adUnsignedTinyInt", "Number"}, {"adVarBinary", "Number"},
	{"adVarChar", "Number"}, {"adVariant", "Number"}, {"adVarWChar", "Number"}, {"adWChar", "Number"},
	{"vbBlack", "Number"}, {"vbRed", "Number"}, {"vbGreen", "Number"}, {"vbYellow", "Number"},
	{"vbBlue", "Number"}, {"vbMagenta", "Number"}, {"vbCyan", "Number"}, {"vbWhite", "Number"},
	{"vbBinaryCompare", "Number"}, {"vbTextCompare", "Number"}, {"vbSunday", "Number"}, {"vbMonday", "Number"},
	{"vbTuesday", "Number"}, {"vbWednesday", "Number"}, {"vbThursday", "Number"}, {"vbFriday", "Number"},
	{"vbSaturday", "Number"}, {"vbUseSystem", "Number"}, {"vbUseSystemDayOfWeek", "Number"}, {"vbFirstJan1", "Number"},
	{"vbFirstFourDays", "Number"}, {"vbFirstFullWeek", "Number"}, {"vbGeneralDate", "Number"}, {"vbLongDate", "Number"},
	{"vbShortDate", "Number"}, {"vbLongTime", "Number"}, {"vbShortTime", "Number"}, {"vbObjectError", "Number"},
	{"vbOKOnly", "Number"}, {"vbOKCancel", "Number"}, {"vbAbortRetryIgnore", "Number"}, {"vbYesNoCancel", "Number"},
	{"vbYesNo", "Number"}, {"vbRetryCancel", "Number"}, {"vbCritical", "Number"}, {"vbQuestion", "Number"},
	{"vbExclamation", "Number"}, {"vbInformation", "Number"}, {"vbDefaultButton1", "Number"}, {"vbDefaultButton2", "Number"},
	{"vbDefaultButton3", "Number"}, {"vbDefaultButton4", "Number"}, {"vbApplicationModal", "Number"}, {"vbSystemModal", "Number"},
	{"vbCr", "String"}, {"vbCrLf", "String"}, {"vbFormFeed", "String"}, {"vbLf", "String"},
	{"vbNewLine", "String"}, {"vbNullChar", "String"}, {"vbNullString", "String"}, {"vbTab", "String"},
	{"vbVerticalTab", "String"}, {"vbUseDefault", "Number"}, {"vbTrue", "Number"}, {"vbFalse", "Number"},
}

var builtinFunctionSpecs = mapBuiltinFunctions()
var builtinConstantSpecs = mapBuiltinConstants()

func mapBuiltinFunctions() map[string]builtinFunctionSpec {
	mapped := make(map[string]builtinFunctionSpec, len(builtinFunctionCatalog))
	for _, spec := range builtinFunctionCatalog {
		mapped[strings.ToLower(spec.Label)] = spec
	}
	return mapped
}

func mapBuiltinConstants() map[string]builtinConstantSpec {
	mapped := make(map[string]builtinConstantSpec, len(builtinConstantCatalog))
	for _, spec := range builtinConstantCatalog {
		mapped[strings.ToLower(spec.Label)] = spec
	}
	return mapped
}

func builtinFunctionSpecForKey(key string) (builtinFunctionSpec, bool) {
	if strings.Contains(key, ".") {
		return builtinFunctionSpec{}, false
	}
	spec, ok := builtinFunctionSpecs[strings.ToLower(key)]
	return spec, ok
}

func builtinConstantSpecForKey(key string) (builtinConstantSpec, bool) {
	if strings.Contains(key, ".") {
		return builtinConstantSpec{}, false
	}
	spec, ok := builtinConstantSpecs[strings.ToLower(key)]
	return spec, ok
}

func builtinFunctionHoverText(spec builtinFunctionSpec) string {
	return "Function " + spec.Signature + " As " + spec.ReturnType + "\n\n" + spec.Summary
}

func builtinConstantHoverText(spec builtinConstantSpec) string {
	return "Const " + spec.Label + " As " + spec.Type + "\n\n" + builtinConstantSummary(spec)
}

func builtinConstantSummary(spec builtinConstantSpec) string {
	lower := strings.ToLower(spec.Label)
	switch lower {
	case "adinteger":
		return "ADO data type constant for a 32-bit signed integer value."
	case "advarchar":
		return "ADO data type constant for a variable-length non-Unicode string."
	case "advarwchar":
		return "ADO data type constant for a variable-length Unicode string."
	case "adboolean":
		return "ADO data type constant for a Boolean value."
	case "addate":
		return "ADO data type constant for a date value."
	}
	if strings.HasPrefix(lower, "ad") {
		return "ADO data type constant used when declaring fields or parameters as " + spec.Label + "."
	}
	return "VBScript constant available without an explicit Const declaration."
}

// BuiltinFunctionDocumentation returns documentation metadata for a top-level VBScript built-in function.
func BuiltinFunctionDocumentation(label string) (signature string, returnType string, summary string, ok bool) {
	spec, ok := builtinFunctionSpecForKey(label)
	if !ok {
		return "", "", "", false
	}
	return spec.Signature, spec.ReturnType, spec.Summary, true
}

// BuiltinConstantDocumentation returns documentation metadata for a top-level VBScript built-in constant.
func BuiltinConstantDocumentation(label string) (typeName string, summary string, ok bool) {
	spec, ok := builtinConstantSpecForKey(label)
	if !ok {
		return "", "", false
	}
	return spec.Type, builtinConstantSummary(spec), true
}

// BuiltinIdentifierKeys returns lower-case built-in function and constant names.
func BuiltinIdentifierKeys() []string {
	keys := make([]string, 0, len(builtinFunctionCatalog)+len(builtinConstantCatalog))
	for _, spec := range builtinFunctionCatalog {
		keys = append(keys, strings.ToLower(spec.Label))
	}
	for _, spec := range builtinConstantCatalog {
		keys = append(keys, strings.ToLower(spec.Label))
	}
	return keys
}
