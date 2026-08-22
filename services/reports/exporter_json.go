package reports

import (
	"code-shield/models"
	"encoding/json"
	"io"
)

type JSONExporter struct{}

func (j *JSONExporter) ContentType() string {
	return "application/json; charset=utf-8"
}

func (j *JSONExporter) FileExtension() string {
	return "json"
}

func (j *JSONExporter) Export(report *models.TaskReport, w io.Writer, opts ExportOptions) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	switch opts.Scope {
	case "all":
		agg, err := GetReportAggregate(report.ID)
		if err != nil {
			return err
		}
		return encoder.Encode(agg)
	case "summary":
		sum, err := GetReportSummary(report.ID)
		if err != nil {
			return err
		}
		return encoder.Encode(sum)
	default:
		// 默认导出详细问题清单
		items, err := loadAllFindingsRaw(report)
		if err != nil {
			return err
		}
		return encoder.Encode(items)
	}
}
