export type Cell =
  | string
  | number
  | boolean
  | Date
  | null
  | undefined
  | {
      value: string | number | boolean | Date | null | undefined;
      type?: StringConstructor | NumberConstructor | BooleanConstructor | DateConstructor;
      fontWeight?: "bold";
      textColor?: string;
      backgroundColor?: string;
      wrap?: boolean;
      format?: string;
      columnSpan?: number;
    };

export interface AnalysisExcelSheet {
  sheet: string;
  data: Cell[][];
  columns?: Array<{ width: number }>;
  stickyRowsCount?: number;
  autoFilterRef?: string;
  hidden?: boolean;
  images?: AnalysisExcelImage[];
}

export interface AnalysisExcelImage {
  content: Buffer;
  contentType: string;
  width: number;
  height: number;
  dpi?: number;
  anchor: { row: number; column: number };
  title?: string;
  description?: string;
}
