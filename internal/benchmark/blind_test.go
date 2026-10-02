package benchmark

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBlindManifestExcludesExpectedFacts(t *testing.T) {
	full := testManifest()
	blind := Blind(full)

	data, err := json.Marshal(blind)
	if err != nil {
		t.Fatalf("marshal blind manifest: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "expected_facts") ||
		strings.Contains(text, "fact:a") ||
		strings.Contains(text, "fact:b") {
		t.Fatalf("blind manifest leaked oracle material: %s", text)
	}
}

func TestValidateBlindManifestDetectsPromptDrift(t *testing.T) {
	full := testManifest()
	blind := Blind(full)
	blind.Tasks[0].Prompt = "different prompt"

	if err := ValidateBlindManifest(full, blind); err == nil {
		t.Fatal("expected prompt drift error")
	}
}

func TestValidateBlindManifestAcceptsDerivedManifest(t *testing.T) {
	full := testManifest()
	if err := ValidateBlindManifest(full, Blind(full)); err != nil {
		t.Fatalf("validate derived blind manifest: %v", err)
	}
}
