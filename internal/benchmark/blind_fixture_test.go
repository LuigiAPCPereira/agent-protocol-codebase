package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommittedBlindManifestMatchesEvaluatorManifest(t *testing.T) {
	root := benchmarkRoot(t)
	full, err := LoadManifest(filepath.Join(root, "tasks.json"))
	if err != nil {
		t.Fatalf("load evaluator manifest: %v", err)
	}
	blind, err := LoadBlindManifest(filepath.Join(root, "prompts.json"))
	if err != nil {
		t.Fatalf("load blind manifest: %v", err)
	}
	if err := ValidateBlindManifest(full, blind); err != nil {
		t.Fatalf("validate committed blind manifest: %v", err)
	}
}

func TestCommittedBlindManifestContainsNoOracleFacts(t *testing.T) {
	root := benchmarkRoot(t)
	full, err := LoadManifest(filepath.Join(root, "tasks.json"))
	if err != nil {
		t.Fatalf("load evaluator manifest: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "prompts.json"))
	if err != nil {
		t.Fatalf("read blind manifest: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "expected_facts") {
		t.Fatal("blind manifest contains expected_facts key")
	}
	for _, task := range full.Tasks {
		for _, fact := range task.ExpectedFacts {
			if strings.Contains(text, fact) {
				t.Fatalf("blind manifest leaked expected fact: %s", fact)
			}
		}
	}
}

func TestBlindCommandShapeIsStable(t *testing.T) {
	root := benchmarkRoot(t)
	full, err := LoadManifest(filepath.Join(root, "tasks.json"))
	if err != nil {
		t.Fatalf("load evaluator manifest: %v", err)
	}
	data, err := json.Marshal(Blind(full))
	if err != nil {
		t.Fatalf("marshal blind manifest: %v", err)
	}
	if strings.Contains(string(data), "expected_facts") {
		t.Fatalf("generated blind manifest leaked oracle field: %s", data)
	}
}
