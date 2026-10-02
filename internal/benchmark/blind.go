package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
)

type BlindManifest struct {
	BenchmarkVersion int         `json:"benchmark_version"`
	FixtureID        string      `json:"fixture_id"`
	Fixture          string      `json:"fixture"`
	Tasks            []BlindTask `json:"tasks"`
}

type BlindTask struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

func Blind(manifest Manifest) BlindManifest {
	tasks := make([]BlindTask, 0, len(manifest.Tasks))
	for _, task := range manifest.Tasks {
		tasks = append(tasks, BlindTask{
			ID:     task.ID,
			Prompt: task.Prompt,
		})
	}
	return BlindManifest{
		BenchmarkVersion: manifest.BenchmarkVersion,
		FixtureID:        manifest.FixtureID,
		Fixture:          manifest.Fixture,
		Tasks:            tasks,
	}
}

func LoadBlindManifest(path string) (BlindManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BlindManifest{}, fmt.Errorf("read blind benchmark manifest: %w", err)
	}
	var manifest BlindManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return BlindManifest{}, fmt.Errorf("decode blind benchmark manifest: %w", err)
	}
	return manifest, nil
}

func ValidateBlindManifest(full Manifest, blind BlindManifest) error {
	expected := Blind(full)
	if blind.BenchmarkVersion != expected.BenchmarkVersion {
		return fmt.Errorf("blind benchmark_version %d does not match %d", blind.BenchmarkVersion, expected.BenchmarkVersion)
	}
	if blind.FixtureID != expected.FixtureID {
		return fmt.Errorf("blind fixture_id %q does not match %q", blind.FixtureID, expected.FixtureID)
	}
	if blind.Fixture != expected.Fixture {
		return fmt.Errorf("blind fixture %q does not match %q", blind.Fixture, expected.Fixture)
	}
	if len(blind.Tasks) != len(expected.Tasks) {
		return fmt.Errorf("blind task count %d does not match %d", len(blind.Tasks), len(expected.Tasks))
	}
	for i := range expected.Tasks {
		if blind.Tasks[i] != expected.Tasks[i] {
			return fmt.Errorf("blind task %d does not match evaluator manifest", i)
		}
	}
	return nil
}
