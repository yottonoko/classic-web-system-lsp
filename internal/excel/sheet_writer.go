package excel

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// AnalysisWorkbookWriteOptions controls XLSX workbook writing metadata.
type AnalysisWorkbookWriteOptions struct {
	Progress  func(AnalysisProgressEvent)
	Cancelled func() bool
	// Commit replaces the target path with the completed temporary workbook.
	// The callback owns the commit linearization boundary and must leave the
	// temporary path in place when it returns an error.
	Commit   func(tempPath string) error
	limits   *workbookFormatLimits
	zipLimit int64
}

// WriteAnalysisWorkbookFile writes generated analysis sheets to an XLSX workbook.
func WriteAnalysisWorkbookFile(path string, sheets []AnalysisSheet) error {
	return WriteAnalysisWorkbookFileWithOptions(path, sheets, AnalysisWorkbookWriteOptions{})
}

// WriteAnalysisWorkbookFileWithOptions writes generated analysis sheets to an XLSX workbook.
func WriteAnalysisWorkbookFileWithOptions(path string, sheets []AnalysisSheet, options AnalysisWorkbookWriteOptions) error {
	if workbookWriteCancelled(options) {
		return context.Canceled
	}
	tempPath, err := createWorkbookTemp(path)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	commit := options.Commit
	if commit == nil {
		commit = func(tempPath string) error {
			return os.Rename(tempPath, path)
		}
	}
	file := excelize.NewFile()
	defer file.Close()
	reportWorkbookProgress(options, "excel.file", 1, 1, path, nil)
	if len(sheets) == 0 {
		reportWorkbookProgress(options, "excel.fileCommit", 0, 1, path, []string{"commit"})
		if err := writeWorkbookArchiveFile(file, tempPath, options.zipLimit); err != nil {
			return err
		}
		if workbookWriteCancelled(options) {
			return context.Canceled
		}
		if err := commit(tempPath); err != nil {
			return err
		}
		reportWorkbookProgress(options, "excel.fileCommit", 1, 1, path, nil)
		return nil
	}
	styles, err := newWorkbookPresentationStyles(file)
	if err != nil {
		return err
	}
	limits := defaultWorkbookFormatLimits()
	if options.limits != nil {
		limits = *options.limits
	}
	physicalSheets := physicalAnalysisSheets(sheets, limits)
	usedSheetNames := make(map[string]struct{}, len(physicalSheets))
	for _, sheet := range physicalSheets {
		usedSheetNames[strings.ToLower(sheet.name)] = struct{}{}
	}
	overflow := []overflowCellChunk{}
	for index, sheet := range physicalSheets {
		if workbookWriteCancelled(options) {
			return context.Canceled
		}
		if index == 0 {
			// Renaming Sheet1 preserves its active state. Calling SetActiveSheet
			// after streaming would make excelize parse every worksheet XML again.
			if err := file.SetSheetName("Sheet1", sheet.name); err != nil {
				return err
			}
		} else if _, err := file.NewSheet(sheet.name); err != nil {
			return err
		}
		if err := writePhysicalAnalysisSheet(file, sheet, options, styles, limits, &overflow); err != nil {
			return err
		}
		if sheet.source.Hidden {
			if err := file.SetSheetVisible(sheet.name, false); err != nil {
				return err
			}
		}
		reportWorkbookProgress(options, "excel.fileSheet", index+1, len(physicalSheets), sheet.name, []string{sheet.name})
	}
	if len(overflow) > 0 {
		overflowSheets := physicalAnalysisSheetsWithNames([]AnalysisSheet{overflowAnalysisSheet(overflow)}, limits, usedSheetNames)
		for _, sheet := range overflowSheets {
			if _, err := file.NewSheet(sheet.name); err != nil {
				return err
			}
			if err := writePhysicalAnalysisSheet(file, sheet, options, styles, limits, &[]overflowCellChunk{}); err != nil {
				return err
			}
		}
	}
	reportWorkbookProgress(options, "excel.fileCommit", 0, 1, path, []string{"commit"})
	if err := writeWorkbookArchiveFile(file, tempPath, options.zipLimit); err != nil {
		return err
	}
	if workbookWriteCancelled(options) {
		return context.Canceled
	}
	if err := commit(tempPath); err != nil {
		return err
	}
	reportWorkbookProgress(options, "excel.fileCommit", 1, 1, path, nil)
	return nil
}

func createWorkbookTemp(path string) (string, error) {
	directory := filepath.Dir(path)
	base := filepath.Base(path)
	pattern := "." + base + ".tmp-*"
	if extension := filepath.Ext(path); extension != "" {
		pattern += extension
	}
	temp, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	return tempPath, nil
}

func writePhysicalAnalysisSheet(file *excelize.File, sheet physicalAnalysisSheet, options AnalysisWorkbookWriteOptions, styles workbookPresentationStyles, limits workbookFormatLimits, overflow *[]overflowCellChunk) error {
	rowCount := physicalSheetRowCount(sheet)
	images := physicalSheetImages(sheet)
	shapes := physicalSheetShapes(sheet)
	stream, err := file.NewStreamWriter(sheet.name)
	if err != nil {
		return err
	}
	rows := make([][]Cell, rowCount)
	columnCount := 0
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		rows[rowIndex] = physicalSheetRow(sheet, rowIndex)
		columnCount = max(columnCount, len(rows[rowIndex]))
	}
	stickyRows := min(sheet.source.StickyRowsCount, rowCount)
	headerRows := stickyRows
	if headerRows <= 0 && rowCount > 0 {
		headerRows = 1
	}
	for column := 1; column <= columnCount; column++ {
		width := analysisColumnWidth(rows, column-1, headerRows)
		if err := stream.SetColWidth(column, column, width); err != nil {
			return err
		}
	}
	if stickyRows > 0 {
		panes := &excelize.Panes{
			Freeze:      true,
			Split:       false,
			XSplit:      0,
			YSplit:      stickyRows,
			TopLeftCell: "A" + excelRow(stickyRows+1),
			ActivePane:  "bottomLeft",
		}
		if err := stream.SetPanes(panes); err != nil {
			return err
		}
	}
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		if workbookWriteCancelled(options) {
			return context.Canceled
		}
		row := rows[rowIndex]
		values := make([]any, len(row))
		styleID := 0
		if rowIndex < headerRows {
			styleID = styles.header
		}
		for columnIndex, value := range row {
			values[columnIndex] = excelize.Cell{
				StyleID: analysisCellStyle(value, styleID, styles),
				Value:   workbookCellValue(value, sheet.name, rowIndex+1, columnIndex+1, limits.cellUnits, overflow),
			}
		}
		cell, err := excelize.CoordinatesToCellName(1, rowIndex+1)
		if err != nil {
			return err
		}
		rowOptions := []excelize.RowOpts{}
		if rowIndex < headerRows {
			rowOptions = append(rowOptions, excelize.RowOpts{Height: 20})
		}
		if err := stream.SetRow(cell, values, rowOptions...); err != nil {
			return err
		}
		reportWorkbookRowProgress(options, rowIndex+1, rowCount, sheet.name)
	}
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		row := rows[rowIndex]
		for columnIndex, value := range row {
			styled, ok := value.(StyledCell)
			if !ok || styled.ColumnSpan <= 1 {
				continue
			}
			span := min(styled.ColumnSpan, columnCount-columnIndex)
			if span <= 1 {
				continue
			}
			start, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err != nil {
				return err
			}
			end, err := excelize.CoordinatesToCellName(columnIndex+span, rowIndex+1)
			if err != nil {
				return err
			}
			if err := stream.MergeCell(start, end); err != nil {
				return err
			}
		}
	}
	if filterRef := physicalSheetAutoFilterRef(sheet); filterRef != "" {
		if err := stream.AddTable(&excelize.Table{Range: filterRef}); err != nil {
			return err
		}
	}
	if err := stream.Flush(); err != nil {
		return err
	}
	for _, image := range images {
		if len(image.File) == 0 || image.Cell == "" {
			continue
		}
		extension := image.Extension
		if extension == "" {
			extension = ".png"
		}
		if err := file.AddPictureFromBytes(sheet.name, image.Cell, &excelize.Picture{
			Extension: extension,
			File:      image.File,
			Format: &excelize.GraphicOptions{
				AltText: image.AltText,
				Name:    image.Name,
			},
		}); err != nil {
			return err
		}
	}
	for _, shape := range shapes {
		options := &excelize.Shape{
			Cell: shape.Cell, Type: shape.Type, Width: shape.Width, Height: shape.Height,
			Format: excelize.GraphicOptions{
				AltText: shape.AltText, Name: shape.Name, OffsetX: shape.OffsetX, OffsetY: shape.OffsetY,
				Positioning: "oneCell",
			},
			Line: excelize.LineOptions{Fill: excelize.Fill{Color: []string{shape.LineColor}}, Width: 1.5},
		}
		if shape.FillColor != "" {
			options.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{shape.FillColor}}
		}
		if shape.Text != "" {
			options.Paragraph = []excelize.RichTextRun{{
				Text: shape.Text,
				Font: &excelize.Font{Bold: shape.Bold, Color: shape.TextColor},
			}}
		}
		if err := file.AddShape(sheet.name, options); err != nil {
			return err
		}
	}
	return nil
}

func reportWorkbookRowProgress(options AnalysisWorkbookWriteOptions, current, total int, sheet string) {
	const progressBatchRows = 256
	if total > progressBatchRows && current != 1 && current != total && current%progressBatchRows != 0 {
		return
	}
	reportWorkbookProgress(options, "excel.fileRows", current, total, sheet, []string{sheet})
}

func workbookWriteCancelled(options AnalysisWorkbookWriteOptions) bool {
	return options.Cancelled != nil && options.Cancelled()
}

func reportWorkbookProgress(options AnalysisWorkbookWriteOptions, label string, current int, total int, detail string, activeItems []string) {
	if options.Progress == nil {
		return
	}
	options.Progress(AnalysisProgressEvent{
		Label:       label,
		Current:     current,
		Total:       total,
		Detail:      detail,
		ActiveItems: append([]string(nil), activeItems...),
	})
}
