package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEstimateMessagesTokensForModel_UsesCalibrationConservatively(t *testing.T) {
	t.Setenv("SYNAPTA_DIR", t.TempDir())

	providerID := "test-provider"
	modelID := fmt.Sprintf("model-%d", time.Now().UnixNano())
	messages := []Message{{Role: RoleUser, Content: "hello world from calibration test"}}

	baseline := EstimateMessagesTokensForModel(providerID, modelID, messages)
	if baseline <= 0 {
		t.Fatalf("expected baseline estimate > 0, got %d", baseline)
	}

	ObserveTokenUsage(providerID, modelID, messages, &Usage{
		PromptTokens:     baseline * 2,
		CompletionTokens: 0,
		TotalTokens:      baseline * 2,
	})

	after := EstimateMessagesTokensForModel(providerID, modelID, messages)
	if after <= baseline {
		t.Fatalf("expected calibrated estimate to increase: before=%d after=%d", baseline, after)
	}

	snap := TokenCalibrationForModel(providerID, modelID)
	if snap.Samples < 1 {
		t.Fatalf("expected at least one calibration sample, got %d", snap.Samples)
	}
	if snap.PromptRatio <= 1.0 {
		t.Fatalf("expected prompt ratio > 1 after observation, got %f", snap.PromptRatio)
	}
}

func TestObserveTokenUsage_EmitsTelemetry(t *testing.T) {
	t.Setenv("SYNAPTA_DIR", t.TempDir())

	providerID := "telemetry-provider"
	modelID := fmt.Sprintf("telemetry-model-%d", time.Now().UnixNano())
	messages := []Message{{Role: RoleUser, Content: "telemetry sample"}}

	seen := make(chan TokenUsageObservation, 1)
	SetTokenUsageObserver(func(observation TokenUsageObservation) {
		select {
		case seen <- observation:
		default:
		}
	})
	t.Cleanup(func() { SetTokenUsageObserver(nil) })

	ObserveTokenUsage(providerID, modelID, messages, &Usage{
		PromptTokens:     42,
		CompletionTokens: 5,
		TotalTokens:      47,
	})

	select {
	case obs := <-seen:
		if obs.ProviderID != providerID || obs.ModelID != modelID {
			t.Fatalf("unexpected observation identity: %#v", obs)
		}
		if obs.EstimatedPromptTokens <= 0 {
			t.Fatalf("expected estimated prompt tokens > 0, got %d", obs.EstimatedPromptTokens)
		}
		if obs.ActualPromptTokens != 42 {
			t.Fatalf("expected actual prompt tokens 42, got %d", obs.ActualPromptTokens)
		}
	default:
		t.Fatal("expected telemetry observation callback to be invoked")
	}
}

func TestObserveTokenUsage_PersistsCalibrationFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNAPTA_DIR", dir)

	providerID := "persist-provider"
	modelID := fmt.Sprintf("persist-model-%d", time.Now().UnixNano())
	messages := []Message{{Role: RoleUser, Content: "persist me"}}

	est := EstimateMessagesTokensForModel(providerID, modelID, messages)
	if est <= 0 {
		t.Fatalf("expected estimate > 0, got %d", est)
	}

	ObserveTokenUsage(providerID, modelID, messages, &Usage{
		PromptTokens:     est,
		CompletionTokens: 1,
		TotalTokens:      est + 1,
	})

	path := filepath.Join(dir, calibrationFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected calibration file at %s: %v", path, err)
	}
}
