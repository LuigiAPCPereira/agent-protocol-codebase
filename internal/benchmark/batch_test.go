package benchmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateSubmissionBatchRejectsMixedRevisions(t *testing.T) {
	full := testManifest()
	blind := Blind(full)
	fixture := batchFixture(t)
	left := validSubmission(t, full, blind, fixture, "raw", "ctx-raw")
	right := validSubmission(t, full, blind, fixture, "codebase", "ctx-codebase")
	right.Run.RepositoryRevision = "different"

	if _, err := EvaluateSubmissionBatch(full, blind, fixture, []Submission{left, right}); err == nil {
		t.Fatal("expected mixed-revision rejection")
	}
}

func TestEvaluateSubmissionBatchRejectsDuplicateArmTask(t *testing.T) {
	full := testManifest()
	blind := Blind(full)
	fixture := batchFixture(t)
	first := validSubmission(t, full, blind, fixture, "raw", "ctx-raw-a")
	second := validSubmission(t, full, blind, fixture, "raw", "ctx-raw-b")

	if _, err := EvaluateSubmissionBatch(full, blind, fixture, []Submission{first, second}); err == nil {
		t.Fatal("expected duplicate arm/task rejection")
	}
}

func TestEvaluateSubmissionBatchRejectsContextSharedAcrossArms(t *testing.T) {
	full := testManifest()
	blind := Blind(full)
	fixture := batchFixture(t)
	left := validSubmission(t, full, blind, fixture, "raw", "ctx-shared")
	right := validSubmission(t, full, blind, fixture, "codebase", "ctx-shared")

	if _, err := EvaluateSubmissionBatch(full, blind, fixture, []Submission{left, right}); err == nil {
		t.Fatal("expected shared-context rejection")
	}
}

func TestEvaluateSubmissionBatchAcceptsSameArmContextAcrossTasks(t *testing.T) {
	full := Manifest{
		BenchmarkVersion: 1,
		FixtureID:        "fixture-1",
		Fixture:          "fixture",
		Tasks: []Task{
			{ID: "task-a", Prompt: "a", ExpectedFacts: []string{"fact:a"}},
			{ID: "task-b", Prompt: "b", ExpectedFacts: []string{"fact:b"}},
		},
	}
	blind := Blind(full)
	fixture := batchFixture(t)

	first := batchSubmission(t, full, blind, fixture, "raw", "task-a", "ctx-raw", []string{"fact:a"})
	second := batchSubmission(t, full, blind, fixture, "raw", "task-b", "ctx-raw", []string{"fact:b"})
	third := batchSubmission(t, full, blind, fixture, "codebase", "task-a", "ctx-codebase", []string{"fact:a"})

	report, err := EvaluateSubmissionBatch(full, blind, fixture, []Submission{second, third, first})
	if err != nil {
		t.Fatalf("evaluate batch: %v", err)
	}
	if report.Submissions != 3 {
		t.Fatalf("submissions = %d, want 3", report.Submissions)
	}
	if len(report.Arms) != 2 || report.Arms[0] != "codebase" || report.Arms[1] != "raw" {
		t.Fatalf("arms = %v", report.Arms)
	}
	if len(report.Tasks) != 2 || report.Tasks[0] != "task-a" || report.Tasks[1] != "task-b" {
		t.Fatalf("tasks = %v", report.Tasks)
	}
	if got := []string{
		report.Scores[0].TaskID + "/" + report.Scores[0].Arm,
		report.Scores[1].TaskID + "/" + report.Scores[1].Arm,
		report.Scores[2].TaskID + "/" + report.Scores[2].Arm,
	}; got[0] != "task-a/codebase" || got[1] != "task-a/raw" || got[2] != "task-b/raw" {
		t.Fatalf("score order = %v", got)
	}
}

func batchFixture(t *testing.T) string {
	t.Helper()
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "x"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func batchSubmission(
	t *testing.T,
	full Manifest,
	blind BlindManifest,
	fixture string,
	arm string,
	taskID string,
	contextID string,
	facts []string,
) Submission {
	t.Helper()

	bundleDigest, err := BundleDigest(blind)
	if err != nil {
		t.Fatal(err)
	}
	fixtureDigest, err := FixtureDigest(fixture)
	if err != nil {
		t.Fatal(err)
	}

	return Submission{
		Run: Run{
			BenchmarkVersion:   full.BenchmarkVersion,
			FixtureID:          full.FixtureID,
			TaskID:             taskID,
			Arm:                arm,
			RepositoryRevision: "abc123",
			Facts:              facts,
		},
		BundleDigest:  bundleDigest,
		FixtureDigest: fixtureDigest,
		ContextID:     contextID,
		Isolation: IsolationAttestation{
			FreshContext: true,
		},
	}
}
