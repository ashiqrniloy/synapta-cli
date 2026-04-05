package tools

import (
	"strings"
	"unicode/utf8"
)

func formatSize(bytes int) string {
	if bytes < 1024 {
		return "" + itoa(bytes) + "B"
	}
	if bytes < 1024*1024 {
		return formatFloat(float64(bytes)/1024.0, 1) + "KB"
	}
	return formatFloat(float64(bytes)/(1024.0*1024.0), 1) + "MB"
}

func truncateHead(content string, maxLines, maxBytes int) (string, Truncation) {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	totalBytes := len([]byte(content))

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return content, Truncation{
			Truncated:             false,
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           totalLines,
			OutputBytes:           totalBytes,
			LastLinePartial:       false,
			FirstLineExceedsLimit: false,
			MaxLines:              maxLines,
			MaxBytes:              maxBytes,
		}
	}

	if totalLines > 0 && len([]byte(lines[0])) > maxBytes {
		return "", Truncation{
			Truncated:             true,
			TruncatedBy:           TruncationByBytes,
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           0,
			OutputBytes:           0,
			LastLinePartial:       false,
			FirstLineExceedsLimit: true,
			MaxLines:              maxLines,
			MaxBytes:              maxBytes,
		}
	}

	out := make([]string, 0, min(totalLines, maxLines))
	used := 0
	truncatedBy := TruncationByLines
	for i := 0; i < len(lines) && i < maxLines; i++ {
		lineBytes := len([]byte(lines[i]))
		if i > 0 {
			lineBytes++ // newline
		}
		if used+lineBytes > maxBytes {
			truncatedBy = TruncationByBytes
			break
		}
		out = append(out, lines[i])
		used += lineBytes
	}
	if len(out) >= maxLines && used <= maxBytes {
		truncatedBy = TruncationByLines
	}

	output := strings.Join(out, "\n")
	outputBytes := len([]byte(output))
	return output, Truncation{
		Truncated:             true,
		TruncatedBy:           truncatedBy,
		TotalLines:            totalLines,
		TotalBytes:            totalBytes,
		OutputLines:           len(out),
		OutputBytes:           outputBytes,
		LastLinePartial:       false,
		FirstLineExceedsLimit: false,
		MaxLines:              maxLines,
		MaxBytes:              maxBytes,
	}
}

func truncateTail(content string, maxLines, maxBytes int) (string, Truncation) {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	totalBytes := len([]byte(content))

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return content, Truncation{
			Truncated:             false,
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           totalLines,
			OutputBytes:           totalBytes,
			LastLinePartial:       false,
			FirstLineExceedsLimit: false,
			MaxLines:              maxLines,
			MaxBytes:              maxBytes,
		}
	}

	out := make([]string, 0, min(totalLines, maxLines))
	used := 0
	truncatedBy := TruncationByLines
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(out) < maxLines; i-- {
		lineBytes := len([]byte(lines[i]))
		if len(out) > 0 {
			lineBytes++
		}
		if used+lineBytes > maxBytes {
			truncatedBy = TruncationByBytes
			if len(out) == 0 {
				partial := truncateStringToBytesFromEnd(lines[i], maxBytes)
				out = append([]string{partial}, out...)
				used = len([]byte(partial))
				lastLinePartial = true
			}
			break
		}
		out = append([]string{lines[i]}, out...)
		used += lineBytes
	}

	if len(out) >= maxLines && used <= maxBytes {
		truncatedBy = TruncationByLines
	}

	output := strings.Join(out, "\n")
	outputBytes := len([]byte(output))
	return output, Truncation{
		Truncated:             true,
		TruncatedBy:           truncatedBy,
		TotalLines:            totalLines,
		TotalBytes:            totalBytes,
		OutputLines:           len(out),
		OutputBytes:           outputBytes,
		LastLinePartial:       lastLinePartial,
		FirstLineExceedsLimit: false,
		MaxLines:              maxLines,
		MaxBytes:              maxBytes,
	}
}

func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	start := len(b) - maxBytes
	for start < len(b) && !utf8.Valid(b[start:]) {
		start++
	}
	if start >= len(b) {
		return ""
	}
	return string(b[start:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
