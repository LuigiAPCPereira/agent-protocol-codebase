package benchmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureDigestIsPathAndContentSensitive(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := FixtureDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := FixtureDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("fixture digest did not change: %s", first)
	}
}

func TestValidateSubmissionRejectsContaminatedContext(t *testing.T) {
	full := testManifest()
	blind := Blind(full)
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "x"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	submission := validSubmission(t, full, blind, fixture, "raw", "ctx-a")
	submission.Isolation.SawEvaluatorManifest = true

	if err := ValidateSubmission(full, blind, fixture, submission); err == nil {
		t.Fatal("expected contamination error")
	}
}

func TestCompareSubmissionsRejectsSameContext(t *testing.T) {
	full := testManifest()
	blind := Blind(full)
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "x"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	left := validSubmission(t, full, blind, fixture, "raw", "ctx")
	right := validSubmission(t, full, blind, fixture, "codebase", "ctx")

	if _, err := CompareSubmissions(full, blind, fixture, left, right); err == nil {
		t.Fatal("expected same-context rejection")
	}
}

func TestCompareSubmissionsScoresBothArmsWithoutWinner(t *testing.T) {
	full := testManifest()
	blind := Blind(full)
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "x"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	left := validSubmission(t, full, blind, fixture, "raw", "ctx-raw")
	right := validSubmission(t, full, blind, fixture, "codebase", "ctx-codebase")

	comparison, err := CompareSubmissions(full, blind, fixture, left, right)
	if err != nil {
		t.Fatalf("compare submissions: %v", err)
	}
	if !comparison.Left.Correctness.Complete || !comparison.Right.Correctness.Complete {
		t.Fatalf("unexpected comparison: %+v", comparison)
	}
}

func validSubmission(
	t *testing.T,
	full Manifest,
	blind BlindManifest,
	fixture string,
	arm string,
	contextID string,
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
			TaskID:             "task",
			Arm:                arm,
			RepositoryRevision: "abc123",
			Facts:              []string{"fact:a", "fact:b"},
		},
		BundleDigest:  bundleDigest,
		FixtureDigest: fixtureDigest,
		ContextID:     contextID,
		Isolation: IsolationAttestation{
			FreshContext: true,
		},
	}
}
