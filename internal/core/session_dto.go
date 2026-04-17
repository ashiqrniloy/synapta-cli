package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ashiqrniloy/synapta-cli/internal/llm"
)

type sessionEntryDTO struct {
	Type                  string            `json:"type"`
	Version               int               `json:"version,omitempty"`
	ID                    string            `json:"id,omitempty"`
	Timestamp             string            `json:"timestamp,omitempty"`
	CWD                   string            `json:"cwd,omitempty"`
	Message               *llm.Message      `json:"message,omitempty"`
	Summary               string            `json:"summary,omitempty"`
	FirstKeptMessageIndex int               `json:"firstKeptMessageIndex,omitempty"`
	TokensBefore          int               `json:"tokensBefore,omitempty"`
	CompactionMethod      string            `json:"compactionMethod,omitempty"`
	Operation             *ContextOperation `json:"operation,omitempty"`
}

func decodeSessionEntryJSONLine(line string) (sessionEntry, error) {
	var dto sessionEntryDTO
	if err := json.Unmarshal([]byte(line), &dto); err != nil {
		return sessionEntry{}, err
	}
	return dto.toDomain()
}

func encodeSessionEntryJSONLine(entry sessionEntry) (string, error) {
	data, err := json.Marshal(entry.toDTO())
	if err != nil {
		return "", fmt.Errorf("marshal session entry: %w", err)
	}
	return string(data), nil
}

func (dto sessionEntryDTO) toDomain() (sessionEntry, error) {
	t := sessionEntryType(strings.TrimSpace(dto.Type))
	if t == "" {
		return sessionEntry{}, fmt.Errorf("missing entry type")
	}

	ts := time.Time{}
	rawTS := strings.TrimSpace(dto.Timestamp)
	if rawTS != "" {
		parsed, err := time.Parse(time.RFC3339, rawTS)
		if err != nil {
			return sessionEntry{}, fmt.Errorf("invalid timestamp: %w", err)
		}
		ts = parsed
	}

	entry := sessionEntry{
		Type:                  t,
		Version:               dto.Version,
		ID:                    dto.ID,
		Timestamp:             ts,
		CWD:                   dto.CWD,
		Message:               dto.Message,
		Summary:               dto.Summary,
		FirstKeptMessageIndex: dto.FirstKeptMessageIndex,
		TokensBefore:          dto.TokensBefore,
		CompactionMethod:      dto.CompactionMethod,
		Operation:             dto.Operation,
	}

	if entry.Type == sessionEntryTypeSession && strings.TrimSpace(entry.ID) == "" {
		return sessionEntry{}, fmt.Errorf("session header missing id")
	}

	return entry, nil
}

func (entry sessionEntry) toDTO() sessionEntryDTO {
	dto := sessionEntryDTO{
		Type:                  string(entry.Type),
		Version:               entry.Version,
		ID:                    entry.ID,
		CWD:                   entry.CWD,
		Message:               entry.Message,
		Summary:               entry.Summary,
		FirstKeptMessageIndex: entry.FirstKeptMessageIndex,
		TokensBefore:          entry.TokensBefore,
		CompactionMethod:      entry.CompactionMethod,
		Operation:             entry.Operation,
	}
	if !entry.Timestamp.IsZero() {
		dto.Timestamp = entry.Timestamp.UTC().Format(time.RFC3339)
	}
	return dto
}
