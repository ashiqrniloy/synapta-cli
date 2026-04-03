package tools

import "strconv"

func itoa(v int) string {
	return strconv.Itoa(v)
}

func formatFloat(v float64, prec int) string {
	return strconv.FormatFloat(v, 'f', prec, 64)
}
