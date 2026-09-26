package excel

import (
	"fmt"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

type workbookPresentationStyles struct {
	header      int
	body        int
	bodyAlt     int
	section     int
	sideLabel   int
	sideValue   int
	toneDanger  int
	toneGood    int
	toneInfo    int
	toneNeutral int
	toneWarning int
	percent     int
}

func newWorkbookPresentationStyles(file *excelize.File) (workbookPresentationStyles, error) {
	borders := []excelize.Border{
		{Type: "left", Color: "B7C9D6", Style: 1},
		{Type: "right", Color: "B7C9D6", Style: 1},
		{Type: "top", Color: "B7C9D6", Style: 1},
		{Type: "bottom", Color: "B7C9D6", Style: 1},
	}
	header, err := file.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"1F4E79"}, Pattern: 1},
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Alignment: &excelize.Alignment{
			Horizontal: "left",
			Vertical:   "center",
			WrapText:   false,
		},
	})
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	body, err := file.NewStyle(&excelize.Style{
		Fill:   excelize.Fill{Type: "pattern", Color: []string{"F8FBFF"}, Pattern: 1},
		Font:   &excelize.Font{Color: "111827"},
		Border: borders,
		Alignment: &excelize.Alignment{
			Vertical: "top",
		},
	})
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	bodyAlt, err := file.NewStyle(&excelize.Style{
		Fill:   excelize.Fill{Type: "pattern", Color: []string{"E6F2FA"}, Pattern: 1},
		Font:   &excelize.Font{Color: "111827"},
		Border: borders,
		Alignment: &excelize.Alignment{
			Vertical: "top",
		},
	})
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	section, err := file.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"EAF2F8"}, Pattern: 1},
		Font: &excelize.Font{Bold: true, Color: "17365D"},
	})
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	sideLabel, err := file.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F9FAFB"}, Pattern: 1},
		Font:      &excelize.Font{Bold: true, Color: "374151"},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
	})
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	sideValue, err := file.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F9FAFB"}, Pattern: 1},
		Font:      &excelize.Font{Color: "374151"},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
	})
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	newTone := func(background, foreground string) (int, error) {
		return file.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Color: []string{background}, Pattern: 1},
			Font: &excelize.Font{Bold: true, Color: foreground},
		})
	}
	toneDanger, err := newTone("F4CCCC", "990000")
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	toneGood, err := newTone("D9EAD3", "274E13")
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	toneInfo, err := newTone("CFE2F3", "0B5394")
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	toneNeutral, err := newTone("E7E6E6", "404040")
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	toneWarning, err := newTone("FCE5CD", "B45F06")
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	format := "0.0%"
	percent, err := file.NewStyle(&excelize.Style{CustomNumFmt: &format})
	if err != nil {
		return workbookPresentationStyles{}, err
	}
	return workbookPresentationStyles{
		header: header, body: body, bodyAlt: bodyAlt, section: section,
		sideLabel: sideLabel, sideValue: sideValue,
		toneDanger: toneDanger, toneGood: toneGood, toneInfo: toneInfo,
		toneNeutral: toneNeutral, toneWarning: toneWarning, percent: percent,
	}, nil
}

func analysisCellStyle(cell Cell, fallback int, styles workbookPresentationStyles) int {
	styled, ok := cell.(StyledCell)
	if !ok {
		return fallback
	}
	switch styled.Style {
	case cellPresentationHeader:
		return styles.header
	case cellPresentationSection:
		return styles.section
	case cellPresentationSideLabel:
		return styles.sideLabel
	case cellPresentationSideValue:
		return styles.sideValue
	case cellPresentationToneDanger:
		return styles.toneDanger
	case cellPresentationToneGood:
		return styles.toneGood
	case cellPresentationToneInfo:
		return styles.toneInfo
	case cellPresentationToneNeutral:
		return styles.toneNeutral
	case cellPresentationToneWarning:
		return styles.toneWarning
	case cellPresentationPercent:
		return styles.percent
	default:
		return fallback
	}
}

func applyWorksheetPresentation(file *excelize.File, sheet string, rows [][]Cell, stickyRows int, styles workbookPresentationStyles) error {
	rowCount, columnCount := worksheetDimensions(rows)
	if rowCount == 0 || columnCount == 0 {
		return nil
	}
	lastCell, err := excelize.CoordinatesToCellName(columnCount, rowCount)
	if err != nil {
		return err
	}
	if err := file.SetCellStyle(sheet, "A1", lastCell, styles.body); err != nil {
		return err
	}
	headerRows := stickyRows
	if headerRows <= 0 {
		headerRows = 1
	}
	if headerRows > rowCount {
		headerRows = rowCount
	}
	headerEnd, err := excelize.CoordinatesToCellName(columnCount, headerRows)
	if err != nil {
		return err
	}
	if err := file.SetCellStyle(sheet, "A1", headerEnd, styles.header); err != nil {
		return err
	}
	for row := headerRows + 1; row <= rowCount; row++ {
		if (row-headerRows)%2 == 0 {
			rowStart, err := excelize.CoordinatesToCellName(1, row)
			if err != nil {
				return err
			}
			rowEnd, err := excelize.CoordinatesToCellName(columnCount, row)
			if err != nil {
				return err
			}
			if err := file.SetCellStyle(sheet, rowStart, rowEnd, styles.bodyAlt); err != nil {
				return err
			}
		}
	}
	for row := 1; row <= headerRows; row++ {
		if err := file.SetRowHeight(sheet, row, 20); err != nil {
			return err
		}
	}
	return setWorksheetColumnWidths(file, sheet, rows, columnCount, headerRows)
}

func worksheetDimensions(rows [][]Cell) (int, int) {
	columnCount := 0
	for _, row := range rows {
		if len(row) > columnCount {
			columnCount = len(row)
		}
	}
	return len(rows), columnCount
}

func setWorksheetColumnWidths(file *excelize.File, sheet string, rows [][]Cell, columnCount, headerRows int) error {
	for column := 1; column <= columnCount; column++ {
		width := analysisColumnWidth(rows, column-1, headerRows)
		name, err := excelize.ColumnNumberToName(column)
		if err != nil {
			return err
		}
		if err := file.SetColWidth(sheet, name, name, width); err != nil {
			return err
		}
	}
	return nil
}

func analysisColumnWidth(rows [][]Cell, column, headerRows int) float64 {
	bodyWidth := 10.0
	headerWidth := 0.0
	for rowIndex, row := range rows {
		if column >= len(row) {
			continue
		}
		width := displayWidth(row[column])
		styled, styledHeader := row[column].(StyledCell)
		if rowIndex < headerRows || styledHeader && styled.Style == cellPresentationHeader {
			headerWidth = max(headerWidth, width+4)
			continue
		}
		bodyWidth = max(bodyWidth, min(width+2, 48))
	}
	return min(max(bodyWidth, headerWidth), 255)
}

func displayWidth(value Cell) float64 {
	text := fmt.Sprint(presentedCellValue(value))
	width := 0
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		if r == utf8.RuneError && size == 1 {
			width++
			continue
		}
		if r < 0x80 {
			width++
		} else {
			width += 2
		}
	}
	return float64(width)
}
