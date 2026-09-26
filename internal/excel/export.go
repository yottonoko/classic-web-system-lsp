package excel

import (
	"encoding/csv"
	"os"
	"strconv"

	"github.com/xuri/excelize/v2"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func WriteCSV(path string, payload graph.Payload) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"kind", "id", "label", "source", "target"}); err != nil {
		return err
	}
	for _, node := range payload.Nodes {
		if err := writer.Write([]string{"node", node.ID, node.Label, "", ""}); err != nil {
			return err
		}
	}
	for _, edge := range payload.Edges {
		if err := writer.Write([]string{"edge", edge.ID, edge.Kind, edge.Source, edge.Target}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func WriteXLSX(path string, payload graph.Payload) error {
	file := excelize.NewFile()
	defer file.Close()
	if err := file.SetSheetName("Sheet1", "概要"); err != nil {
		return err
	}
	if _, err := file.NewSheet("宣言"); err != nil {
		return err
	}
	if _, err := file.NewSheet("チャート元データ"); err != nil {
		return err
	}
	if err := file.SetSheetVisible("チャート元データ", false); err != nil {
		return err
	}
	styles, err := newWorkbookPresentationStyles(file)
	if err != nil {
		return err
	}
	if err := writeSummarySheet(file, payload, styles); err != nil {
		return err
	}
	if err := writeDeclarationsSheet(file, payload, styles); err != nil {
		return err
	}
	if err := writeChartDataSheet(file, payload, styles); err != nil {
		return err
	}
	index, err := file.GetSheetIndex("概要")
	if err != nil {
		return err
	}
	file.SetActiveSheet(index)
	return file.SaveAs(path)
}

func writeSummarySheet(file *excelize.File, payload graph.Payload, styles workbookPresentationStyles) error {
	rows := [][]Cell{
		{"項目", "値"},
		{"scope", payload.Scope},
		{"uri", payload.URI},
		{"nodes", len(payload.Nodes)},
		{"edges", len(payload.Edges)},
	}
	for rowIndex, row := range rows {
		for columnIndex, value := range row {
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err != nil {
				return err
			}
			if err := file.SetCellValue("概要", cell, value); err != nil {
				return err
			}
		}
	}
	return applyWorksheetPresentation(file, "概要", rows, 0, styles)
}

func writeDeclarationsSheet(file *excelize.File, payload graph.Payload, styles workbookPresentationStyles) error {
	rows := make([][]Cell, 0, len(payload.Nodes)+1)
	headers := []Cell{"kind", "id", "label", "detail"}
	rows = append(rows, headers)
	for columnIndex, value := range headers {
		cell, err := excelize.CoordinatesToCellName(columnIndex+1, 1)
		if err != nil {
			return err
		}
		if err := file.SetCellValue("宣言", cell, value); err != nil {
			return err
		}
	}
	for rowIndex, node := range payload.Nodes {
		values := []Cell{"node", node.ID, node.Label, node.Kind}
		rows = append(rows, values)
		for columnIndex, value := range values {
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+2)
			if err != nil {
				return err
			}
			if err := file.SetCellValue("宣言", cell, value); err != nil {
				return err
			}
		}
	}
	lastRow := len(payload.Nodes) + 1
	if lastRow < 1 {
		lastRow = 1
	}
	if err := applyWorksheetPresentation(file, "宣言", rows, 0, styles); err != nil {
		return err
	}
	return file.AutoFilter("宣言", "A1:D"+excelRow(lastRow), nil)
}

func writeChartDataSheet(file *excelize.File, payload graph.Payload, styles workbookPresentationStyles) error {
	rows := make([][]Cell, 0, len(payload.Edges)+1)
	headers := []Cell{"kind", "id", "source", "target"}
	rows = append(rows, headers)
	for columnIndex, value := range headers {
		cell, err := excelize.CoordinatesToCellName(columnIndex+1, 1)
		if err != nil {
			return err
		}
		if err := file.SetCellValue("チャート元データ", cell, value); err != nil {
			return err
		}
	}
	for rowIndex, edge := range payload.Edges {
		values := []Cell{edge.Kind, edge.ID, edge.Source, edge.Target}
		rows = append(rows, values)
		for columnIndex, value := range values {
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+2)
			if err != nil {
				return err
			}
			if err := file.SetCellValue("チャート元データ", cell, value); err != nil {
				return err
			}
		}
	}
	return applyWorksheetPresentation(file, "チャート元データ", rows, 0, styles)
}

func excelRow(row int) string {
	return strconv.Itoa(row)
}
