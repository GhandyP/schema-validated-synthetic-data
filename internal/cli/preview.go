package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/GhandyP/schema-validated-synthetic-data/internal/spec"
)

// previewLimit caps how many records the preview prints per entity. The JSON
// output carries the whole batch; a terminal preview only has to be enough to see
// the shape of the data.
const previewLimit = 20

// preview prints the batch for a human: what it is, the records, how much was
// proven, and what the batch is not claiming.
func preview(w io.Writer, dataset spec.SyntheticDataset) {
	fmt.Fprintf(w, "batch: %s\n", dataset.Spec.Name)
	fmt.Fprintf(w, "seed:  %d\n", dataset.Seed)

	for _, data := range dataset.Entities {
		entity, ok := dataset.Spec.Entity(data.Name)
		if !ok {
			continue
		}

		fmt.Fprintf(w, "\n%s (%d records)\n", data.Name, len(data.Records))
		writeTable(w, entity, data.Records)

		if remaining := len(data.Records) - previewLimit; remaining > 0 {
			fmt.Fprintf(w, "  ... %d more records (use -json for the whole batch)\n", remaining)
		}
	}

	if dataset.Validation != nil {
		fmt.Fprintf(w, "\nvalidation: %d of %d declared rules hold\n", dataset.Validation.Total, dataset.Validation.Total)
	}

	if len(dataset.Assumptions) > 0 {
		fmt.Fprint(w, "\nassumptions:\n")
		for _, assumption := range dataset.Assumptions {
			fmt.Fprintf(w, "  - %s\n", assumption)
		}
	}

	fmt.Fprint(w, "\nsynthetic data: every value above was generated from the specification and the seed.\nNo real data source was read, and nothing was written to disk.\n")
}

func writeTable(w io.Writer, entity spec.Entity, records []spec.Record) {
	widths := make([]int, len(entity.Fields))
	for i, field := range entity.Fields {
		widths[i] = len(field.Name)
	}

	rows := make([][]string, 0, len(records))
	for i, record := range records {
		if i == previewLimit {
			break
		}

		row := make([]string, len(entity.Fields))
		for j, field := range entity.Fields {
			value, _ := record.Value(field.Name)
			row[j] = cell(value)
			widths[j] = max(widths[j], len(row[j]))
		}
		rows = append(rows, row)
	}

	header := make([]string, len(entity.Fields))
	for i, field := range entity.Fields {
		header[i] = field.Name
	}
	writeRow(w, header, widths)

	for _, row := range rows {
		writeRow(w, row, widths)
	}
}

func writeRow(w io.Writer, row []string, widths []int) {
	var line strings.Builder
	for i, value := range row {
		if i > 0 {
			line.WriteString("  ")
		}
		fmt.Fprintf(&line, "%-*s", widths[i], value)
	}
	fmt.Fprintf(w, "  %s\n", strings.TrimRight(line.String(), " "))
}

// cell renders one value the way the JSON output represents it: a null is null,
// and a number keeps only the digits it has.
func cell(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}
