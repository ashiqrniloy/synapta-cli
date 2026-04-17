package tools

import (
	"fmt"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

const maxDiffLines = 80

func computeDisplayDiff(oldContent, newContent string) (insertions, deletions int, changedRangeStrs []string, diffText string) {
	if oldContent == newContent {
		return 0, 0, nil, ""
	}

	edits := udiff.Lines(oldContent, newContent)
	if len(edits) == 0 {
		return 0, 0, nil, ""
	}

	unified, err := udiff.ToUnifiedDiff("old", "new", oldContent, edits, 0)
	if err != nil {
		return 0, 0, nil, ""
	}

	type lineRange struct{ start, end int }
	var newRanges []lineRange
	var diffLines []string

	for _, h := range unified.Hunks {
		oldLine := h.FromLine
		newLine := h.ToLine
		var rangeStart int

		for _, l := range h.Lines {
			switch l.Kind {
			case udiff.Delete:
				deletions++
				diffLines = append(diffLines, fmt.Sprintf("- %4d | %s", oldLine, strings.TrimSuffix(l.Content, "\n")))
				oldLine++
			case udiff.Insert:
				insertions++
				if rangeStart == 0 {
					rangeStart = newLine
				}
				diffLines = append(diffLines, fmt.Sprintf("+ %4d | %s", newLine, strings.TrimSuffix(l.Content, "\n")))
				newLine++
			case udiff.Equal:
				if rangeStart > 0 {
					newRanges = append(newRanges, lineRange{rangeStart, newLine - 1})
					rangeStart = 0
				}
				oldLine++
				newLine++
			}
		}
		if rangeStart > 0 {
			newRanges = append(newRanges, lineRange{rangeStart, newLine - 1})
		}
	}

	for _, r := range newRanges {
		if r.start == r.end {
			changedRangeStrs = append(changedRangeStrs, fmt.Sprintf("%d", r.start))
		} else {
			changedRangeStrs = append(changedRangeStrs, fmt.Sprintf("%d-%d", r.start, r.end))
		}
	}

	if len(diffLines) > 0 {
		hidden := 0
		if len(diffLines) > maxDiffLines {
			hidden = len(diffLines) - maxDiffLines
			diffLines = diffLines[:maxDiffLines]
		}
		var sb strings.Builder
		sb.WriteString("Changes:\n")
		sb.WriteString(strings.Join(diffLines, "\n"))
		if hidden > 0 {
			sb.WriteString(fmt.Sprintf("\n... (%d more diff lines)", hidden))
		}
		diffText = sb.String()
	}

	return insertions, deletions, changedRangeStrs, diffText
}

func splitContentLines(content string) []string {
	if content == "" {
		return []string{}
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func countLines(content string) int {
	return len(splitContentLines(content))
}
