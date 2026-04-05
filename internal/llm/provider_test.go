package llm

import (
	"regexp"
	"testing"
)

func TestNormalizeResponsesItemID_LowercaseAndCharset(t *testing.T) {
	got := normalizeResponsesItemID("fc_sCe_U3AMvL_nt3BuaeX8feDLUHbo8AfVcMG4kOcWfTTJHCSH1Nufcc7Read2i")

	if got != "fc_sce_u3amvl_nt3buaex8fedluhbo8afvcmg4kocwfttjhcsh1nufcc7read2i" {
		t.Fatalf("unexpected normalized id: %q", got)
	}

	if len(got) > 64 {
		t.Fatalf("normalized id exceeds max length: %d", len(got))
	}

	validID := regexp.MustCompile(`^[a-z0-9_-]+$`)
	if !validID.MatchString(got) {
		t.Fatalf("normalized id contains invalid characters: %q", got)
	}
}

func TestNormalizeResponsesItemID_EmptyFallback(t *testing.T) {
	got := normalizeResponsesItemID("@@@")
	if got != "fc_tool_call" {
		t.Fatalf("expected fallback id, got: %q", got)
	}
}
