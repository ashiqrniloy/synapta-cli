package llm

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// TokenEstimator estimates token usage for text and messages.
//
// Estimators should be deterministic and side-effect free.
type TokenEstimator interface {
	EstimateTextTokens(text string) int
	EstimateMessageTokens(msg Message) int
	EstimateMessagesTokens(messages []Message) int
	ID() string
}

type heuristicTokenEstimator struct{}

func (heuristicTokenEstimator) ID() string { return "heuristic-v1" }

func (heuristicTokenEstimator) EstimateTextTokens(text string) int {
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

func (h heuristicTokenEstimator) EstimateMessageTokens(msg Message) int {
	contentTokens := h.EstimateTextTokens(msg.Content)
	tokens := contentTokens

	if msg.Name != "" {
		tokens += 2 + h.EstimateTextTokens(msg.Name)
	}
	if msg.ToolCallID != "" {
		tokens += 4 + h.EstimateTextTokens(msg.ToolCallID)
	}
	for _, tc := range msg.ToolCalls {
		tokens += 6
		tokens += h.EstimateTextTokens(tc.ID)
		tokens += h.EstimateTextTokens(tc.Type)
		tokens += h.EstimateTextTokens(tc.Function.Name)
		tokens += h.EstimateTextTokens(tc.Function.Arguments)
	}

	// Preserve previous behavior: plain empty messages estimate to 0.
	if contentTokens == 0 && msg.Name == "" && msg.ToolCallID == "" && len(msg.ToolCalls) == 0 {
		return 0
	}

	switch msg.Role {
	case RoleSystem:
		tokens += 10
	case RoleUser:
		tokens += 8
	case RoleAssistant:
		tokens += 12
	case RoleTool:
		tokens += 15
	}

	return tokens
}

func (h heuristicTokenEstimator) EstimateMessagesTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += h.EstimateMessageTokens(msg)
	}
	return total
}

type scaledTokenEstimator struct {
	id           string
	base         TokenEstimator
	textScale    float64
	messageScale float64
}

func (s scaledTokenEstimator) ID() string { return s.id }

func (s scaledTokenEstimator) EstimateTextTokens(text string) int {
	base := s.base.EstimateTextTokens(text)
	if base <= 0 {
		return 0
	}
	if s.textScale <= 0 {
		return base
	}
	return max(1, int(math.Ceil(float64(base)*s.textScale)))
}

func (s scaledTokenEstimator) EstimateMessageTokens(msg Message) int {
	base := s.base.EstimateMessageTokens(msg)
	if base <= 0 {
		return 0
	}
	scale := s.messageScale
	if scale <= 0 {
		scale = 1
	}
	return max(1, int(math.Ceil(float64(base)*scale)))
}

func (s scaledTokenEstimator) EstimateMessagesTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += s.EstimateMessageTokens(msg)
	}
	return total
}

// TokenUsageObservation captures one estimated-vs-actual token usage sample.
type TokenUsageObservation struct {
	ProviderID            string
	ModelID               string
	EstimatorID           string
	EstimatedPromptTokens int
	EstimatedTotalTokens  int
	ActualPromptTokens    int
	ActualTotalTokens     int
	PromptRatio           float64
	TotalRatio            float64
	RollingPromptRatio    float64
	RollingTotalRatio     float64
	Samples               int
}

type TokenUsageObserver func(observation TokenUsageObservation)

var (
	defaultEstimator TokenEstimator = heuristicTokenEstimator{}

	estimatorMu sync.RWMutex
	estimators  = map[string]TokenEstimator{}

	telemetryMu        sync.RWMutex
	tokenUsageObserver TokenUsageObserver

	calibrationMu      sync.RWMutex
	calibrationLoaded  bool
	calibrationEntries = map[string]tokenCalibrationEntry{}
)

const (
	calibrationFileName     = "token_estimation_calibration.json"
	calibrationEMAAlpha     = 0.20
	maxReasonableRatio      = 4.0
	minReasonableRatio      = 0.25
	conservativeExtraMargin = 0.05
)

type tokenCalibrationEntry struct {
	PromptRatio float64 `json:"promptRatio"`
	TotalRatio  float64 `json:"totalRatio"`
	Samples     int     `json:"samples"`
	UpdatedUnix int64   `json:"updatedUnix"`
}

type tokenCalibrationFile struct {
	Version int                      `json:"version"`
	Entries map[string]tokenCalibrationEntry `json:"entries"`
}

// TokenCalibrationSnapshot exposes persisted calibration stats for a model.
type TokenCalibrationSnapshot struct {
	ProviderID  string
	ModelID     string
	PromptRatio float64
	TotalRatio  float64
	Samples     int
	UpdatedAt   time.Time
}

func init() {
	// Optional provider-specific estimators. They remain heuristic-based but allow
	// small provider-level adjustments until calibrated statistics are learned.
	RegisterTokenEstimator("github-copilot", "", scaledTokenEstimator{
		id:           "scaled-github-copilot-v1",
		base:         defaultEstimator,
		textScale:    1.04,
		messageScale: 1.05,
	})
	RegisterTokenEstimator("kilo", "", scaledTokenEstimator{
		id:           "scaled-kilo-v1",
		base:         defaultEstimator,
		textScale:    1.02,
		messageScale: 1.03,
	})
}

// SetTokenUsageObserver installs a telemetry callback that receives each
// estimated-vs-actual observation when provider usage stats are available.
func SetTokenUsageObserver(observer TokenUsageObserver) {
	telemetryMu.Lock()
	defer telemetryMu.Unlock()
	tokenUsageObserver = observer
}

// RegisterTokenEstimator registers a provider/model-specific estimator.
//
// Resolution order is:
//  1. exact provider+model
//  2. provider-wide entry (model="")
//  3. default heuristic estimator
func RegisterTokenEstimator(providerID, modelID string, estimator TokenEstimator) {
	if estimator == nil {
		return
	}
	estimatorMu.Lock()
	defer estimatorMu.Unlock()
	estimators[tokenEstimatorKey(providerID, modelID)] = estimator
}

// EstimateTextTokens estimates token count with the default heuristic estimator.
func EstimateTextTokens(text string) int {
	return defaultEstimator.EstimateTextTokens(text)
}

// EstimateMessageTokens estimates token usage for a single chat message using
// the default heuristic estimator.
func EstimateMessageTokens(msg Message) int {
	return defaultEstimator.EstimateMessageTokens(msg)
}

// EstimateMessagesTokens estimates total token usage for a message slice using
// the default heuristic estimator.
func EstimateMessagesTokens(messages []Message) int {
	return defaultEstimator.EstimateMessagesTokens(messages)
}

// EstimateMessageTokensForModel estimates one message for a provider/model using
// provider-specific estimation and conservative calibration bias.
func EstimateMessageTokensForModel(providerID, modelID string, msg Message) int {
	est := resolveTokenEstimator(providerID, modelID)
	base := est.EstimateMessageTokens(msg)
	return applyConservativeCalibration(providerID, modelID, base)
}

// EstimateMessagesTokensForModel estimates a message list for a provider/model
// using provider-specific estimation and conservative calibration bias.
func EstimateMessagesTokensForModel(providerID, modelID string, messages []Message) int {
	est := resolveTokenEstimator(providerID, modelID)
	base := est.EstimateMessagesTokens(messages)
	return applyConservativeCalibration(providerID, modelID, base)
}

// ObserveTokenUsage records a telemetry sample comparing estimated input tokens
// against provider-reported usage stats, then updates rolling calibration.
func ObserveTokenUsage(providerID, modelID string, messages []Message, usage *Usage) {
	if usage == nil {
		return
	}

	est := resolveTokenEstimator(providerID, modelID)
	estimatedPrompt := max(0, est.EstimateMessagesTokens(messages))
	if estimatedPrompt <= 0 {
		return
	}

	actualPrompt := usage.PromptTokens
	if actualPrompt <= 0 && usage.TotalTokens > 0 && usage.CompletionTokens >= 0 {
		actualPrompt = max(0, usage.TotalTokens-usage.CompletionTokens)
	}
	if actualPrompt <= 0 {
		return
	}

	estimatedTotal := estimatedPrompt
	if usage.CompletionTokens > 0 {
		estimatedTotal += usage.CompletionTokens
	}
	actualTotal := usage.TotalTokens
	if actualTotal <= 0 {
		actualTotal = actualPrompt + max(0, usage.CompletionTokens)
	}

	promptRatio := clampRatio(float64(actualPrompt) / float64(max(estimatedPrompt, 1)))
	totalRatio := 1.0
	if estimatedTotal > 0 && actualTotal > 0 {
		totalRatio = clampRatio(float64(actualTotal) / float64(estimatedTotal))
	}

	entry := updateCalibration(providerID, modelID, promptRatio, totalRatio)

	telemetryMu.RLock()
	observer := tokenUsageObserver
	telemetryMu.RUnlock()
	if observer != nil {
		observer(TokenUsageObservation{
			ProviderID:            providerID,
			ModelID:               modelID,
			EstimatorID:           est.ID(),
			EstimatedPromptTokens: estimatedPrompt,
			EstimatedTotalTokens:  estimatedTotal,
			ActualPromptTokens:    actualPrompt,
			ActualTotalTokens:     actualTotal,
			PromptRatio:           promptRatio,
			TotalRatio:            totalRatio,
			RollingPromptRatio:    entry.PromptRatio,
			RollingTotalRatio:     entry.TotalRatio,
			Samples:               entry.Samples,
		})
	}
}

// TokenCalibrationForModel returns the current rolling calibration snapshot.
func TokenCalibrationForModel(providerID, modelID string) TokenCalibrationSnapshot {
	entry, ok := calibrationEntry(providerID, modelID)
	if !ok {
		return TokenCalibrationSnapshot{ProviderID: providerID, ModelID: modelID, PromptRatio: 1, TotalRatio: 1}
	}
	return TokenCalibrationSnapshot{
		ProviderID:  providerID,
		ModelID:     modelID,
		PromptRatio: entry.PromptRatio,
		TotalRatio:  entry.TotalRatio,
		Samples:     entry.Samples,
		UpdatedAt:   time.Unix(entry.UpdatedUnix, 0),
	}
}

func resolveTokenEstimator(providerID, modelID string) TokenEstimator {
	estimatorMu.RLock()
	defer estimatorMu.RUnlock()

	if est, ok := estimators[tokenEstimatorKey(providerID, modelID)]; ok && est != nil {
		return est
	}
	if est, ok := estimators[tokenEstimatorKey(providerID, "")]; ok && est != nil {
		return est
	}
	return defaultEstimator
}

func tokenEstimatorKey(providerID, modelID string) string {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return providerID + "|" + modelID
}

func applyConservativeCalibration(providerID, modelID string, estimated int) int {
	if estimated <= 0 {
		return 0
	}
	ratio := conservativePromptRatio(providerID, modelID)
	if ratio <= 1 {
		return estimated
	}
	return max(1, int(math.Ceil(float64(estimated)*ratio)))
}

func conservativePromptRatio(providerID, modelID string) float64 {
	entry, ok := calibrationEntry(providerID, modelID)
	if !ok {
		return 1
	}
	ratio := entry.PromptRatio
	if ratio < 1 {
		ratio = 1
	}
	if entry.Samples > 0 {
		ratio += conservativeExtraMargin
	}
	if ratio > maxReasonableRatio {
		ratio = maxReasonableRatio
	}
	return ratio
}

func calibrationEntry(providerID, modelID string) (tokenCalibrationEntry, bool) {
	calibrationMu.Lock()
	defer calibrationMu.Unlock()
	loadCalibrationLocked()

	if e, ok := calibrationEntries[tokenEstimatorKey(providerID, modelID)]; ok {
		return e, true
	}
	if e, ok := calibrationEntries[tokenEstimatorKey(providerID, "")]; ok {
		return e, true
	}
	return tokenCalibrationEntry{}, false
}

func updateCalibration(providerID, modelID string, promptRatio, totalRatio float64) tokenCalibrationEntry {
	calibrationMu.Lock()
	defer calibrationMu.Unlock()
	loadCalibrationLocked()

	key := tokenEstimatorKey(providerID, modelID)
	entry := calibrationEntries[key]
	if entry.Samples <= 0 {
		entry.PromptRatio = promptRatio
		entry.TotalRatio = totalRatio
		entry.Samples = 1
	} else {
		entry.PromptRatio = ema(entry.PromptRatio, promptRatio, calibrationEMAAlpha)
		entry.TotalRatio = ema(entry.TotalRatio, totalRatio, calibrationEMAAlpha)
		entry.Samples++
	}
	entry.UpdatedUnix = time.Now().Unix()
	calibrationEntries[key] = entry
	saveCalibrationLocked()
	return entry
}

func loadCalibrationLocked() {
	if calibrationLoaded {
		return
	}
	calibrationLoaded = true

	path := calibrationFilePath()
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var payload tokenCalibrationFile
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}
	if len(payload.Entries) == 0 {
		return
	}
	for key, entry := range payload.Entries {
		if key == "" {
			continue
		}
		if entry.PromptRatio <= 0 {
			entry.PromptRatio = 1
		}
		if entry.TotalRatio <= 0 {
			entry.TotalRatio = 1
		}
		if entry.Samples < 0 {
			entry.Samples = 0
		}
		calibrationEntries[key] = entry
	}
}

func saveCalibrationLocked() {
	path := calibrationFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}

	payload := tokenCalibrationFile{
		Version: 1,
		Entries: calibrationEntries,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func calibrationFilePath() string {
	return filepath.Join(GetAgentDir(), calibrationFileName)
}

func ema(prev, current, alpha float64) float64 {
	if alpha <= 0 {
		return current
	}
	if alpha >= 1 {
		return current
	}
	return (1-alpha)*prev + alpha*current
}

func clampRatio(r float64) float64 {
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return 1
	}
	if r < minReasonableRatio {
		return minReasonableRatio
	}
	if r > maxReasonableRatio {
		return maxReasonableRatio
	}
	return r
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
