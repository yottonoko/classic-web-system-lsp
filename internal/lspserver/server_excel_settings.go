package lspserver

import "github.com/yottonoko/classic-web-system-lsp/internal/excel"

type analysisExcelExportArg struct {
	Scope                                   string   `json:"scope"`
	URI                                     string   `json:"uri"`
	FileURIs                                []string `json:"fileUris"`
	TargetPath                              string   `json:"targetPath"`
	IncludeGlobs                            []string `json:"includeGlobs"`
	ExcludeGlobs                            []string `json:"excludeGlobs"`
	RespectGitIgnore                        *bool    `json:"respectGitIgnore"`
	IncludeRelatedIncludeTreesForUnresolved *bool    `json:"includeRelatedIncludeTreesForUnresolved"`
	SkipTypeInference                       *bool    `json:"skipTypeInference"`
}

func (s *Server) analysisExcelWorkbookSettings(arg analysisExcelExportArg) *excel.AnalysisWorkbookSettings {
	includeRelatedIncludeTrees := s.settings.ExcelIncludeRelatedIncludeTrees
	if arg.IncludeRelatedIncludeTreesForUnresolved != nil {
		includeRelatedIncludeTrees = *arg.IncludeRelatedIncludeTreesForUnresolved
	}
	skipTypeInference := s.settings.ExcelSkipTypeInference
	if arg.SkipTypeInference != nil {
		skipTypeInference = *arg.SkipTypeInference
	}
	// The workbook metadata records only the command override. Workspace
	// defaults still control filtering in exportAnalysisPayload, but TS leaves
	// this optional field unset when the command omitted it.
	respectGitIgnore := false
	if arg.RespectGitIgnore != nil {
		respectGitIgnore = *arg.RespectGitIgnore
	}
	return &excel.AnalysisWorkbookSettings{
		ExcelLocale:                             s.analysisExcelLocaleSetting(),
		IncludeRelatedIncludeTreesForUnresolved: includeRelatedIncludeTrees,
		ForceRelatedIncludeTreeAnalysis:         includeRelatedIncludeTrees,
		SkipTypeInference:                       skipTypeInference,
		IncludeAnalysisTypeDetails:              !skipTypeInference,
		AnalysisFileCount:                       len(arg.FileURIs),
		IncludeGlobs:                            append([]string(nil), arg.IncludeGlobs...),
		ExcludeGlobs:                            append([]string(nil), arg.ExcludeGlobs...),
		RespectGitIgnore:                        respectGitIgnore,
	}
}

func (s *Server) analysisExcelLocaleSetting() string {
	if s.settings.ExcelLocale == "" {
		return "auto"
	}
	return s.settings.ExcelLocale
}
