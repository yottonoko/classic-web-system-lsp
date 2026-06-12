import ExcelJS from "exceljs";
import type { AnalysisExcelSheet, Cell } from "./sheets";

export interface WriteAnalysisExcelWorkbookOptions {
  filename: string;
}

export async function writeAnalysisExcelWorkbookFile(
  sheets: readonly AnalysisExcelSheet[],
  options: WriteAnalysisExcelWorkbookOptions,
): Promise<void> {
  const workbook = new ExcelJS.stream.xlsx.WorkbookWriter({
    filename: options.filename,
    useStyles: true,
    useSharedStrings: false,
  });
  for (const sheet of sheets) {
    const worksheet = workbook.addWorksheet(sheet.sheet, {
      autoFilter: sheet.autoFilterRef,
      state: sheet.hidden === true ? "hidden" : "visible",
      views:
        sheet.stickyRowsCount && sheet.stickyRowsCount > 0
          ? [{ state: "frozen", ySplit: sheet.stickyRowsCount }]
          : undefined,
    } as Partial<ExcelJS.AddWorksheetOptions> & { autoFilter?: string });
    if (sheet.columns?.length) {
      worksheet.columns = sheet.columns.map((column) => ({ width: column.width }));
    }
    for (const sourceRow of sheet.data) {
      const row = worksheet.addRow(sourceRow.map(cellValue));
      sourceRow.forEach((cell, index) => {
        applyCellStyle(row.getCell(index + 1), cell);
        const span = objectCell(cell)?.columnSpan;
        if (span && span > 1) {
          worksheet.mergeCells(row.number, index + 1, row.number, index + span);
        }
      });
      row.commit();
    }
    worksheet.commit();
  }
  await workbook.commit();
}

function cellValue(cell: Cell): string | number | boolean | Date | null {
  if (cell === null || cell === undefined) {
    return null;
  }
  if (cell instanceof Date || typeof cell !== "object") {
    return cell;
  }
  return "value" in cell ? (cell.value ?? null) : null;
}

function applyCellStyle(cell: ExcelJS.Cell, source: Cell): void {
  const object = objectCell(source);
  if (!object) {
    return;
  }
  if (object.fontWeight || object.textColor) {
    cell.font = {
      bold: object.fontWeight === "bold",
      color: object.textColor ? { argb: argbColor(object.textColor) } : undefined,
    };
  }
  if (object.backgroundColor) {
    cell.fill = {
      type: "pattern",
      pattern: "solid",
      fgColor: { argb: argbColor(object.backgroundColor) },
    };
  }
  if (object.wrap) {
    cell.alignment = { wrapText: true, vertical: "top" };
  }
  if (object.format) {
    cell.numFmt = object.format;
  }
}

function objectCell(cell: Cell):
  | Extract<
      Cell,
      {
        value: string | number | boolean | Date | null | undefined;
      }
    >
  | undefined {
  return cell && typeof cell === "object" && "value" in cell ? cell : undefined;
}

function argbColor(value: string): string {
  const hex = value.replace(/^#/, "").toUpperCase();
  return hex.length === 6 ? `FF${hex}` : hex.padStart(8, "F");
}
