// Package report renders deterministic text and JSON artefacts.
//
// Text output is fixed-width: column widths come from the content, padding is
// spaces only, the newline is always "\n", and floats are formatted through
// package numeric. The same inputs therefore always produce byte-identical
// output on every platform.
package report

import (
	"sort"
	"strings"

	"DebrisLedger/internal/numeric"
)

// Table is a fixed-width text table.
type Table struct {
	Header  []string
	Rows    [][]string
	Numeric []bool
}

// NewTable starts a table with the given header. Columns whose header ends with
// a unit or is listed in rightAligned are rendered right-aligned.
func NewTable(header []string, rightAligned ...int) *Table {
	numericCols := make([]bool, len(header))
	for _, idx := range rightAligned {
		if idx >= 0 && idx < len(numericCols) {
			numericCols[idx] = true
		}
	}
	return &Table{Header: header, Numeric: numericCols}
}

// Add appends a row. Rows with too few cells are padded with empty strings and
// rows with too many are truncated, so a table can never render ragged.
func (t *Table) Add(cells ...string) {
	row := make([]string, len(t.Header))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	t.Rows = append(t.Rows, row)
}

// SortRows orders rows by the given column indices, comparing as strings.
func (t *Table) SortRows(cols ...int) {
	sort.SliceStable(t.Rows, func(i, j int) bool {
		for _, c := range cols {
			if c < 0 || c >= len(t.Header) {
				continue
			}
			if t.Rows[i][c] != t.Rows[j][c] {
				return t.Rows[i][c] < t.Rows[j][c]
			}
		}
		return false
	})
}

// Render writes the table, or a placeholder when it holds no rows.
func (t *Table) Render(indent string) string {
	if len(t.Rows) == 0 {
		return indent + "(none)\n"
	}
	widths := make([]int, len(t.Header))
	for i, h := range t.Header {
		widths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	var b strings.Builder
	b.WriteString(indent)
	b.WriteString(joinCells(t.Header, widths, t.Numeric))
	b.WriteString("\n")
	b.WriteString(indent)
	seps := make([]string, len(widths))
	for i, w := range widths {
		seps[i] = strings.Repeat("-", w)
	}
	b.WriteString(strings.Join(seps, "  "))
	b.WriteString("\n")
	for _, row := range t.Rows {
		b.WriteString(indent)
		b.WriteString(joinCells(row, widths, t.Numeric))
		b.WriteString("\n")
	}
	return b.String()
}

func joinCells(cells []string, widths []int, numericCols []bool) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		if i < len(numericCols) && numericCols[i] {
			parts[i] = numeric.PadLeft(cell, widths[i])
		} else {
			parts[i] = numeric.Pad(cell, widths[i])
		}
	}
	return strings.TrimRight(strings.Join(parts, "  "), " ")
}

// Section writes a titled block.
type Section struct {
	b strings.Builder
}

// Title writes a top-level heading.
func (s *Section) Title(title string) {
	s.b.WriteString(title)
	s.b.WriteString("\n")
	s.b.WriteString(strings.Repeat("=", len(title)))
	s.b.WriteString("\n")
}

// Heading writes a second-level heading.
func (s *Section) Heading(title string) {
	s.b.WriteString("\n")
	s.b.WriteString(title)
	s.b.WriteString("\n")
	s.b.WriteString(strings.Repeat("-", len(title)))
	s.b.WriteString("\n")
}

// Field writes a "name: value" line.
func (s *Section) Field(name, value string) {
	s.b.WriteString(numeric.Pad(name+":", 26))
	s.b.WriteString(" ")
	s.b.WriteString(value)
	s.b.WriteString("\n")
}

// Line writes a raw line.
func (s *Section) Line(text string) {
	s.b.WriteString(text)
	s.b.WriteString("\n")
}

// Bullets writes a list, sorted for determinism when sorted is true.
func (s *Section) Bullets(items []string, sorted bool) {
	list := make([]string, len(items))
	copy(list, items)
	if sorted {
		sort.Strings(list)
	}
	if len(list) == 0 {
		s.Line("  (none)")
		return
	}
	for _, item := range list {
		s.Line("  - " + item)
	}
}

// Table writes a rendered table with a two-space indent.
func (s *Section) Table(t *Table) {
	s.b.WriteString(t.Render("  "))
}

// String returns the accumulated text.
func (s *Section) String() string { return s.b.String() }
