package benchmark

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCodebaseSmokeCompletesAllBenchmarkTasks(t *testing.T) {
	root := benchmarkRoot(t)
	manifest, err := LoadManifest(filepath.Join(root, "tasks.json"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	report, err := RunCodebaseSmoke(
		context.Background(),
		manifest,
		filepath.Join(root, manifest.Fixture),
	)
	if err != nil {
		t.Fatalf("run codebase smoke: %v", err)
	}
	if report.Setup.APICalls != 2 {
		t.Fatalf("setup calls = %d, want 2", report.Setup.APICalls)
	}
	if len(report.Scores) != len(manifest.Tasks) {
		t.Fatalf("scores = %d, want %d", len(report.Scores), len(manifest.Tasks))
	}
	for _, score := range report.Scores {
		if !score.Correctness.Complete {
			t.Fatalf("task %s incomplete: %+v", score.TaskID, score.Correctness)
		}
	}
}
