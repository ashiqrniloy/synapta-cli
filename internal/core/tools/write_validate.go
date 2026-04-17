package tools

import (
	"fmt"
	"regexp"
	"strings"
)

func applyStringReplace(oldContent, find, replace string, expectedMatches, maxReplacements *int) (string, int, error) {
	if find == "" {
		return "", 0, fmt.Errorf("replace mode requires `find` (literal text to match). Provide non-empty find and content. Example: {\"mode\":\"replace\",\"path\":\"file.txt\",\"find\":\"old\",\"content\":\"new\"}")
	}
	count := strings.Count(oldContent, find)
	if expectedMatches != nil && count != *expectedMatches {
		return "", 0, fmt.Errorf("replace mode expected %d matches for %q, found %d. Update expected_matches, adjust `find`, or inspect the file first.", *expectedMatches, find, count)
	}
	if expectedMatches == nil && count == 0 {
		return "", 0, fmt.Errorf("replace mode found no matches for %q. Use read to confirm exact text, or use replace_regex/line_edit for flexible edits.", find)
	}
	if maxReplacements != nil {
		if *maxReplacements < 0 {
			return "", 0, fmt.Errorf("max_replacements must be >= 0 (got %d)", *maxReplacements)
		}
		applied := count
		if applied > *maxReplacements {
			applied = *maxReplacements
		}
		return strings.Replace(oldContent, find, replace, *maxReplacements), applied, nil
	}
	return strings.ReplaceAll(oldContent, find, replace), count, nil
}

func applyRegexReplace(oldContent, find, replace string, expectedMatches, maxReplacements *int) (string, int, error) {
	if strings.TrimSpace(find) == "" {
		return "", 0, fmt.Errorf("replace_regex mode requires `find` (RE2 pattern). Provide non-empty find and content. Example: {\"mode\":\"replace_regex\",\"path\":\"file.txt\",\"find\":\"foo(\\\\d+)\",\"content\":\"bar$1\"}")
	}
	re, err := regexp.Compile(find)
	if err != nil {
		return "", 0, fmt.Errorf("replace_regex mode received an invalid RE2 pattern %q: %w", find, err)
	}
	matches := re.FindAllStringSubmatchIndex(oldContent, -1)
	count := len(matches)
	if expectedMatches != nil && count != *expectedMatches {
		return "", 0, fmt.Errorf("replace_regex mode expected %d matches for pattern %q, found %d. Update expected_matches or adjust `find`.", *expectedMatches, find, count)
	}
	if expectedMatches == nil && count == 0 {
		return "", 0, fmt.Errorf("replace_regex mode found no matches for pattern %q. Use read to verify the target text and pattern.", find)
	}
	if maxReplacements != nil {
		if *maxReplacements < 0 {
			return "", 0, fmt.Errorf("max_replacements must be >= 0 (got %d)", *maxReplacements)
		}
		if *maxReplacements == 0 {
			return oldContent, 0, nil
		}
		limit := *maxReplacements
		if limit > count {
			limit = count
		}
		var b strings.Builder
		last := 0
		for i := 0; i < limit; i++ {
			rng := matches[i]
			b.WriteString(oldContent[last:rng[0]])
			b.Write(re.ExpandString(nil, replace, oldContent, rng))
			last = rng[1]
		}
		b.WriteString(oldContent[last:])
		return b.String(), limit, nil
	}
	return re.ReplaceAllString(oldContent, replace), count, nil
}

func applyLineEdit(oldContent string, startLine, endLine int, replacement string, preserveTrailingNL bool) (string, error) {
	endsWithNL := strings.HasSuffix(oldContent, "\n")
	oldLines := splitContentLines(oldContent)
	totalLines := len(oldLines)
	if endLine > totalLines {
		return "", fmt.Errorf("line_edit mode range %d-%d is out of bounds (file has %d lines). Use read with include_line_numbers=true to see the exact line numbers.", startLine, endLine, totalLines)
	}
	prefix := append([]string(nil), oldLines[:startLine-1]...)
	suffix := append([]string(nil), oldLines[endLine:]...)
	replLines := splitContentLines(replacement)
	merged := append(prefix, replLines...)
	merged = append(merged, suffix...)
	result := strings.Join(merged, "\n")
	if preserveTrailingNL && endsWithNL {
		result += "\n"
	}
	return result, nil
}

func applyInsertAfterLine(oldContent string, afterLine int, insertion string, preserveTrailingNL bool) (string, error) {
	endsWithNL := strings.HasSuffix(oldContent, "\n")
	oldLines := splitContentLines(oldContent)
	totalLines := len(oldLines)

	if afterLine > totalLines {
		return "", fmt.Errorf("insert_after_line: after_line=%d is out of bounds (file has %d lines). Use after_line=%d to insert at the end, or use mode=\"append\".", afterLine, totalLines, totalLines)
	}

	newLines := splitContentLines(insertion)

	result := make([]string, 0, totalLines+len(newLines))
	result = append(result, oldLines[:afterLine]...)
	result = append(result, newLines...)
	result = append(result, oldLines[afterLine:]...)

	out := strings.Join(result, "\n")
	if preserveTrailingNL && endsWithNL {
		out += "\n"
	}
	return out, nil
}
