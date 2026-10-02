package benchmark

import "testing"

func TestScoreRunCompleteSubmission(t *testing.T) {
	manifest := testManifest()
	one := int64(1)
	run := Run{
		BenchmarkVersion:   1,
		FixtureID:          manifest.FixtureID,
		TaskID:             "task",
		Arm:                "codebase",
		RepositoryRevision: "abc123",
		Facts:              []string{"fact:a", "fact:b"},
		Cost: Cost{
			ToolCalls:   &one,
			FilesOpened: &one,
		},
	}

	score, err := ScoreRun(manifest, run)
	if err != nil {
		t.Fatalf("score run: %v", err)
	}
	if !score.Correctness.Complete {
		t.Fatalf("correct run marked incomplete: %+v", score.Correctness)
	}
	if len(score.Correctness.Matched) != 2 ||
		len(score.Correctness.Missing) != 0 ||
		len(score.Correctness.Unexpected) != 0 {
		t.Fatalf("unexpected correctness: %+v", score.Correctness)
	}
}

func TestScoreRunNegativeControlDetectsWrongFact(t *testing.T) {
	manifest := testManifest()
	run := Run{
		BenchmarkVersion:   1,
		FixtureID:          manifest.FixtureID,
		TaskID:             "task",
		Arm:                "raw",
		RepositoryRevision: "abc123",
		Facts:              []string{"fact:a", "fact:wrong"},
	}

	score, err := ScoreRun(manifest, run)
	if err != nil {
		t.Fatalf("score run: %v", err)
	}
	if score.Correctness.Complete {
		t.Fatalf("negative control passed unexpectedly: %+v", score.Correctness)
	}
	if len(score.Correctness.Missing) != 1 ||
		score.Correctness.Missing[0] != "fact:b" {
		t.Fatalf("missing = %v, want fact:b", score.Correctness.Missing)
	}
	if len(score.Correctness.Unexpected) != 1 ||
		score.Correctness.Unexpected[0] != "fact:wrong" {
		t.Fatalf("unexpected = %v, want fact:wrong", score.Correctness.Unexpected)
	}
}

func TestScoreRunRejectsFixtureMismatch(t *testing.T) {
	manifest := testManifest()
	_, err := ScoreRun(manifest, Run{
		BenchmarkVersion:   1,
		FixtureID:          "different",
		TaskID:             "task",
		Arm:                "raw",
		RepositoryRevision: "abc123",
	})
	if err == nil {
		t.Fatal("expected fixture mismatch error")
	}
}

func TestScoreRunAllowsUnavailableCostMetrics(t *testing.T) {
	manifest := testManifest()
	run := Run{
		BenchmarkVersion:   1,
		FixtureID:          manifest.FixtureID,
		TaskID:             "task",
		Arm:                "raw",
		RepositoryRevision: "abc123",
		Facts:              []string{"fact:a", "fact:b"},
	}
	if _, err := ScoreRun(manifest, run); err != nil {
		t.Fatalf("nil cost metrics must be allowed: %v", err)
	}
}

func testManifest() Manifest {
	return Manifest{
		BenchmarkVersion: 1,
		FixtureID:        "fixture-1",
		Fixture:          "fixture",
		Tasks: []Task{
			{
				ID:            "task",
				Prompt:        "answer the task",
				ExpectedFacts: []string{"fact:a", "fact:b"},
			},
		},
	}
}
