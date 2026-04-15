package llm

import (
	"math"
	"unicode"
)

// EstimateTextTokens estimates token count for free-form text using a lightweight
// lexical tokenizer. It improves on a plain chars/4 heuristic by accounting for
// CJK text, punctuation runs, digits, and control characters.
func EstimateTextTokens(text string) int {
	if isAllWhitespace(text) {
		return 0
	}

	runes := []rune(text)
	tokens := 0
	for i := 0; i < len(runes); {
		r := runes[i]

		switch {
		case isCJKRune(r):
			// CJK is usually dense in tokenizers.
			tokens++
			i++

		case unicode.IsSpace(r):
			j := i + 1
			hasNewline := r == '\n' || r == '\r'
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				if runes[j] == '\n' || runes[j] == '\r' {
					hasNewline = true
				}
				j++
			}
			if hasNewline {
				tokens++
			}
			i = j

		case isDigitRune(r):
			j := i + 1
			for j < len(runes) && isDigitRune(runes[j]) {
				j++
			}
			tokens += ceilDiv(j-i, 3)
			i = j

		case isWordRune(r):
			j := i + 1
			ascii := r <= 127
			for j < len(runes) && isWordRune(runes[j]) {
				if runes[j] > 127 {
					ascii = false
				}
				j++
			}
			if ascii {
				tokens += ceilDiv(j-i, 4)
			} else {
				tokens += ceilDiv(j-i, 2)
			}
			i = j

		case unicode.IsControl(r):
			j := i + 1
			for j < len(runes) && unicode.IsControl(runes[j]) && !unicode.IsSpace(runes[j]) {
				j++
			}
			tokens += ceilDiv(j-i, 4)
			i = j

		default:
			j := i + 1
			for j < len(runes) {
				rj := runes[j]
				if isCJKRune(rj) || unicode.IsSpace(rj) || isWordRune(rj) || isDigitRune(rj) || unicode.IsControl(rj) {
					break
				}
				j++
			}
			// Punctuation/symbol runs often split, but not strictly 1:1.
			tokens += ceilDiv(j-i, 3)
			i = j
		}
	}

	if tokens < 1 {
		return 1
	}
	return tokens
}

// EstimateMessageTokens estimates token usage for a single chat message,
// including role and tool-call structural overhead.
func EstimateMessageTokens(msg Message) int {
	contentTokens := EstimateTextTokens(msg.Content)
	tokens := contentTokens

	if msg.Name != "" {
		tokens += 2 + EstimateTextTokens(msg.Name)
	}
	if msg.ToolCallID != "" {
		tokens += 4 + EstimateTextTokens(msg.ToolCallID)
	}
	for _, tc := range msg.ToolCalls {
		tokens += 6
		tokens += EstimateTextTokens(tc.ID)
		tokens += EstimateTextTokens(tc.Type)
		tokens += EstimateTextTokens(tc.Function.Name)
		tokens += EstimateTextTokens(tc.Function.Arguments)
	}

	// Preserve previous behavior: plain empty messages estimate to 0.
	if contentTokens == 0 && msg.Name == "" && msg.ToolCallID == "" && len(msg.ToolCalls) == 0 {
		return 0
	}

	switch msg.Role {
	case "system":
		tokens += 10
	case "user":
		tokens += 8
	case "assistant":
		tokens += 12
	case "tool":
		tokens += 15
	}

	return tokens
}

// EstimateMessagesTokens estimates total token usage for a message slice.
func EstimateMessagesTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || r == '_' || r == '-'
}

func isDigitRune(r rune) bool {
	return unicode.IsDigit(r)
}

func isAllWhitespace(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func isCJKRune(r rune) bool {
	return unicode.In(r,
		unicode.Han,
		unicode.Hiragana,
		unicode.Katakana,
		unicode.Hangul,
	)
}

func ceilDiv(n, d int) int {
	if n <= 0 {
		return 0
	}
	return int(math.Ceil(float64(n) / float64(d)))
}
