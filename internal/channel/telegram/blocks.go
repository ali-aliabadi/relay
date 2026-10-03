package telegram

import (
	"strconv"
	"strings"

	"github.com/ali-aliabadi/relay/internal/message"
)

// blockRenderer returns a function rendering b within a visible-length
// budget, shortening it when it doesn't fit.
func blockRenderer(b message.Block) func(int) frag {
	switch b.Type {
	case message.BlockText:
		return func(budget int) frag { return text(truncate(b.Text, budget)) }
	case message.BlockCode:
		return func(budget int) frag { return wrapIfAny("pre", text(truncate(b.Text, budget))) }
	case message.BlockFields:
		return func(budget int) frag { return renderFields(b.Items, budget) }
	case message.BlockTable:
		return func(budget int) frag { return wrapIfAny("pre", renderTable(b.Columns, b.Rows, budget)) }
	default:
		// Unknown types can't pass validation; render their text, if any, plainly.
		return func(budget int) frag { return text(truncate(b.Text, budget)) }
	}
}

func inline(tag, raw string) func(int) frag {
	return func(budget int) frag { return wrapIfAny(tag, text(truncate(raw, budget))) }
}

func wrapIfAny(tag string, f frag) frag {
	if f.empty() {
		return f
	}
	return wrap(tag, f)
}

// renderFields renders "<b>Label:</b> value" lines, as many as fit.
func renderFields(items []message.Field, budget int) frag {
	var lines []frag
	used := 0
	for _, it := range items {
		label := wrap("b", text(it.Label+":"))
		line := join(" ", label, text(it.Value))
		if it.Value == "" {
			line = label
		}
		cost := line.visible
		if len(lines) > 0 {
			cost++ // newline
		}
		if used+cost > budget {
			room := budget - used - (cost - line.visible) - label.visible - 1
			if room > len(ellipsis) && it.Value != "" {
				lines = append(lines, join(" ", label, text(truncate(it.Value, room))))
			}
			break
		}
		lines = append(lines, line)
		used += cost
	}
	return join("\n", lines...)
}

// Table layout limits keep rows readable on a phone.
const (
	maxColWidth  = 20
	maxLineWidth = 48
)

// renderTable renders an aligned monospace table (inside <pre>), with as
// many rows as fit. Cells are padded and truncated to the column width.
func renderTable(cols []string, rows [][]string, budget int) frag {
	widths := columnWidths(cols, rows)
	line := func(cells []string) string {
		parts := make([]string, len(widths))
		for i, w := range widths {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			cell = truncate(strings.Join(strings.Fields(cell), " "), w)
			parts[i] = cell + strings.Repeat(" ", w-visibleLen(cell))
		}
		return strings.TrimRight(strings.Join(parts, " "), " ")
	}
	total := len(widths) - 1
	for _, w := range widths {
		total += w
	}
	lines := []string{line(cols), strings.Repeat("─", total)}
	used := visibleLen(lines[0]) + 1 + visibleLen(lines[1])
	if used > budget {
		return frag{}
	}
	for i, r := range rows {
		l := line(r)
		if used+1+visibleLen(l) > budget {
			if more := "… " + strconv.Itoa(len(rows)-i) + " more rows"; used+1+visibleLen(more) <= budget {
				lines = append(lines, more)
			}
			break
		}
		lines = append(lines, l)
		used += 1 + visibleLen(l)
	}
	return text(strings.Join(lines, "\n"))
}

// columnWidths sizes each column to its widest cell, capped per column and
// shrunk evenly until a line fits maxLineWidth.
func columnWidths(cols []string, rows [][]string) []int {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = min(visibleLen(c), maxColWidth)
		for _, r := range rows {
			if i < len(r) {
				widths[i] = max(widths[i], min(visibleLen(r[i]), maxColWidth))
			}
		}
		widths[i] = max(widths[i], 1)
	}
	for {
		total := len(widths) - 1
		widest := 0
		for i, w := range widths {
			total += w
			if w > widths[widest] {
				widest = i
			}
		}
		if total <= maxLineWidth || widths[widest] <= 3 {
			return widths
		}
		widths[widest]--
	}
}
