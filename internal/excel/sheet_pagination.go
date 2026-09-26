package excel

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/xuri/excelize/v2"
)

const (
	xlsxMaximumRows       = 1_048_576
	xlsxMaximumColumns    = 16_384
	xlsxMaximumCellUnits  = 32_767
	xlsxMaximumSheetRunes = 31
)

type workbookFormatLimits struct {
	rows      int
	columns   int
	cellUnits int
}

func defaultWorkbookFormatLimits() workbookFormatLimits {
	return workbookFormatLimits{
		rows:      xlsxMaximumRows,
		columns:   xlsxMaximumColumns,
		cellUnits: xlsxMaximumCellUnits,
	}
}

type physicalAnalysisSheet struct {
	name        string
	source      AnalysisSheet
	columnStart int
	columnEnd   int
	bodyStart   int
	bodyEnd     int
}

type overflowCellChunk struct {
	sheet  string
	row    int
	column int
	chunk  int
	text   string
}

func physicalAnalysisSheets(sheets []AnalysisSheet, limits workbookFormatLimits) []physicalAnalysisSheet {
	return physicalAnalysisSheetsWithNames(sheets, limits, map[string]struct{}{})
}

func physicalAnalysisSheetsWithNames(sheets []AnalysisSheet, limits workbookFormatLimits, usedNames map[string]struct{}) []physicalAnalysisSheet {
	if limits.rows <= 0 {
		limits.rows = xlsxMaximumRows
	}
	if limits.columns <= 0 {
		limits.columns = xlsxMaximumColumns
	}
	result := make([]physicalAnalysisSheet, 0, len(sheets))
	for _, sheet := range sheets {
		_, columns := worksheetDimensions(sheet.Data)
		for _, image := range sheet.Images {
			column, _, err := excelize.CellNameToCoordinates(image.Cell)
			if err == nil && column > columns {
				columns = column
			}
		}
		for _, shape := range sheet.Shapes {
			column, _, err := excelize.CellNameToCoordinates(shape.Cell)
			if err == nil && column > columns {
				columns = column
			}
		}
		if columns == 0 {
			columns = 1
		}
		sticky := min(max(sheet.StickyRowsCount, 0), len(sheet.Data))
		bodyRowsPerSheet := limits.rows - sticky
		if bodyRowsPerSheet <= 0 {
			bodyRowsPerSheet = 1
		}
		bodyStart := sticky
		bodyEnd := len(sheet.Data)
		if bodyStart == bodyEnd {
			bodyStart = 0
			bodyEnd = min(len(sheet.Data), limits.rows)
			sticky = 0
		}
		part := 0
		for columnStart := 0; columnStart < columns; columnStart += limits.columns {
			columnEnd := min(columnStart+limits.columns, columns)
			if bodyStart == bodyEnd {
				part++
				name := uniquePhysicalSheetName(sheet.Sheet, part, usedNames)
				result = append(result, physicalAnalysisSheet{name: name, source: sheet, columnStart: columnStart, columnEnd: columnEnd, bodyStart: bodyStart, bodyEnd: bodyEnd})
				continue
			}
			for start := bodyStart; start < bodyEnd; start += bodyRowsPerSheet {
				part++
				end := min(start+bodyRowsPerSheet, bodyEnd)
				name := uniquePhysicalSheetName(sheet.Sheet, part, usedNames)
				result = append(result, physicalAnalysisSheet{name: name, source: sheet, columnStart: columnStart, columnEnd: columnEnd, bodyStart: start, bodyEnd: end})
			}
		}
	}
	return result
}

func uniquePhysicalSheetName(base string, part int, used map[string]struct{}) string {
	base = sanitizeWorksheetName(base)
	for candidatePart := part; ; candidatePart++ {
		suffix := ""
		if candidatePart > 1 {
			suffix = fmt.Sprintf(" (%d)", candidatePart)
		}
		candidate := truncateRunes(base, xlsxMaximumSheetRunes-len([]rune(suffix))) + suffix
		key := strings.ToLower(candidate)
		if _, exists := used[key]; !exists {
			used[key] = struct{}{}
			return candidate
		}
	}
}

func sanitizeWorksheetName(name string) string {
	replacer := strings.NewReplacer(":", "_", "\\", "_", "/", "_", "?", "_", "*", "_", "[", "_", "]", "_")
	name = strings.TrimSpace(replacer.Replace(name))
	name = strings.Trim(name, "'")
	if name == "" {
		return "Sheet"
	}
	return truncateRunes(name, xlsxMaximumSheetRunes)
}

func truncateRunes(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

func physicalSheetRowCount(sheet physicalAnalysisSheet) int {
	sticky := min(max(sheet.source.StickyRowsCount, 0), len(sheet.source.Data))
	count := sheet.bodyEnd - sheet.bodyStart
	if sheet.bodyStart >= sticky {
		count += sticky
	}
	return max(count, 0)
}

func physicalSheetRow(sheet physicalAnalysisSheet, physicalIndex int) []Cell {
	sourceIndex := physicalSheetSourceRow(sheet, physicalIndex)
	if sourceIndex < 0 || sourceIndex >= sheet.bodyEnd || sourceIndex >= len(sheet.source.Data) {
		return nil
	}
	return physicalRowColumns(sheet.source.Data[sourceIndex], sheet.columnStart, sheet.columnEnd)
}

func physicalSheetSourceRow(sheet physicalAnalysisSheet, physicalIndex int) int {
	sticky := min(max(sheet.source.StickyRowsCount, 0), len(sheet.source.Data))
	sourceIndex := physicalIndex
	if sheet.bodyStart >= sticky {
		if physicalIndex < sticky {
			return physicalIndex
		}
		sourceIndex = sheet.bodyStart + physicalIndex - sticky
	} else {
		sourceIndex = sheet.bodyStart + physicalIndex
	}
	return sourceIndex
}

func physicalSheetAutoFilterRef(sheet physicalAnalysisSheet) string {
	parts := strings.Split(sheet.source.AutoFilterRef, ":")
	if len(parts) != 2 {
		return ""
	}
	startColumn, startRow, err := excelize.CellNameToCoordinates(parts[0])
	if err != nil {
		return ""
	}
	endColumn, endRow, err := excelize.CellNameToCoordinates(parts[1])
	if err != nil {
		return ""
	}
	columnStart := max(startColumn, sheet.columnStart+1)
	columnEnd := min(endColumn, sheet.columnEnd)
	if columnStart > columnEnd {
		return ""
	}
	physicalStart, physicalEnd := 0, 0
	for physicalRow := 0; physicalRow < physicalSheetRowCount(sheet); physicalRow++ {
		sourceRow := physicalSheetSourceRow(sheet, physicalRow) + 1
		if sourceRow < startRow || sourceRow > endRow {
			continue
		}
		if physicalStart == 0 {
			physicalStart = physicalRow + 1
		}
		physicalEnd = physicalRow + 1
	}
	if physicalStart == 0 || physicalEnd < physicalStart {
		return ""
	}
	start, err := excelize.CoordinatesToCellName(columnStart-sheet.columnStart, physicalStart)
	if err != nil {
		return ""
	}
	end, err := excelize.CoordinatesToCellName(columnEnd-sheet.columnStart, physicalEnd)
	if err != nil {
		return ""
	}
	return start + ":" + end
}

func physicalRowColumns(row []Cell, start, end int) []Cell {
	if start >= len(row) {
		return nil
	}
	return row[start:min(end, len(row))]
}

func physicalSheetImages(sheet physicalAnalysisSheet) []AnalysisImage {
	sticky := min(max(sheet.source.StickyRowsCount, 0), len(sheet.source.Data))
	result := make([]AnalysisImage, 0, len(sheet.source.Images))
	for _, image := range sheet.source.Images {
		column, row, err := excelize.CellNameToCoordinates(image.Cell)
		if err != nil {
			continue
		}
		sourceColumn := column - 1
		sourceRow := row - 1
		if sourceColumn < sheet.columnStart || sourceColumn >= sheet.columnEnd {
			continue
		}
		physicalRow := 0
		switch {
		case sourceRow < sticky && sheet.bodyStart >= sticky:
			physicalRow = sourceRow + 1
		case sourceRow >= sheet.bodyStart && sourceRow < sheet.bodyEnd:
			physicalRow = sticky + sourceRow - sheet.bodyStart + 1
		case sourceRow >= len(sheet.source.Data) && (sheet.bodyStart == sticky || sheet.bodyStart == 0):
			physicalRow = sourceRow + 1
		default:
			continue
		}
		mapped := image
		mapped.Cell, err = excelize.CoordinatesToCellName(sourceColumn-sheet.columnStart+1, physicalRow)
		if err == nil {
			result = append(result, mapped)
		}
	}
	return result
}

func physicalSheetShapes(sheet physicalAnalysisSheet) []AnalysisShape {
	sticky := min(max(sheet.source.StickyRowsCount, 0), len(sheet.source.Data))
	result := make([]AnalysisShape, 0, len(sheet.source.Shapes))
	for _, shape := range sheet.source.Shapes {
		column, row, err := excelize.CellNameToCoordinates(shape.Cell)
		if err != nil {
			continue
		}
		sourceColumn := column - 1
		sourceRow := row - 1
		if sourceColumn < sheet.columnStart || sourceColumn >= sheet.columnEnd {
			continue
		}
		physicalRow := 0
		switch {
		case sourceRow < sticky && sheet.bodyStart >= sticky:
			physicalRow = sourceRow + 1
		case sourceRow >= sheet.bodyStart && sourceRow < sheet.bodyEnd:
			physicalRow = sticky + sourceRow - sheet.bodyStart + 1
		case sourceRow >= len(sheet.source.Data) && (sheet.bodyStart == sticky || sheet.bodyStart == 0):
			physicalRow = sourceRow + 1
		default:
			continue
		}
		mapped := shape
		mapped.Cell, err = excelize.CoordinatesToCellName(sourceColumn-sheet.columnStart+1, physicalRow)
		if err == nil {
			result = append(result, mapped)
		}
	}
	return result
}

func workbookCellValue(value Cell, sheet string, row, column int, limit int, overflow *[]overflowCellChunk) Cell {
	value = presentedCellValue(value)
	text, ok := value.(string)
	if !ok || limit <= 0 || len(utf16.Encode([]rune(text))) <= limit {
		return value
	}
	chunks := splitUTF16Text(text, limit)
	for index, chunk := range chunks {
		*overflow = append(*overflow, overflowCellChunk{sheet: sheet, row: row, column: column, chunk: index + 1, text: chunk})
	}
	cell, _ := excelize.CoordinatesToCellName(column, row)
	return fmt.Sprintf("[Overflow: %s!%s, %d chunks]", sheet, cell, len(chunks))
}

func splitUTF16Text(text string, maximum int) []string {
	if maximum <= 0 {
		return []string{text}
	}
	parts := []string{}
	var current strings.Builder
	units := 0
	for _, value := range text {
		width := 1
		if value > 0xffff {
			width = 2
		}
		if units > 0 && units+width > maximum {
			parts = append(parts, current.String())
			current.Reset()
			units = 0
		}
		current.WriteRune(value)
		units += width
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func overflowAnalysisSheet(chunks []overflowCellChunk) AnalysisSheet {
	data := make([][]Cell, 1, len(chunks)+1)
	data[0] = []Cell{"Sheet", "Row", "Column", "Chunk", "Text"}
	for _, chunk := range chunks {
		data = append(data, []Cell{chunk.sheet, chunk.row, chunk.column, chunk.chunk, chunk.text})
	}
	return AnalysisSheet{Sheet: "Overflow", Data: data, StickyRowsCount: 1, AutoFilterRef: "A1:E1"}
}
