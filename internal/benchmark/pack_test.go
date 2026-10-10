package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareArmPacksProducesEquivalentIsolatedRepositories(t *testing.T) {
	fixture := filepath.Join(benchmarkRoot(t), "fixture")
	blind, err := LoadBlindManifest(filepath.Join(benchmarkRoot(t), "prompts.json"))
	if err != nil {
		t.Fatal(err)
	}

	engine := filepath.Join(t.TempDir(), "ap-codebase")
	if err := os.WriteFile(engine, []byte("fake engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "pack")
	report, err := PrepareArmPacks(blind, fixture, engine, output)
	if err != nil {
		t.Fatalf("prepare arm packs: %v", err)
	}

	if report.Raw.RepositoryRevision != report.Codebase.RepositoryRevision {
		t.Fatalf("arm revisions differ: raw=%s codebase=%s", report.Raw.RepositoryRevision, report.Codebase.RepositoryRevision)
	}
	if report.Raw.FixtureDigest != report.Codebase.FixtureDigest {
		t.Fatal("arm fixture digests differ")
	}
	if report.Raw.BundleDigest != report.Codebase.BundleDigest {
		t.Fatal("arm bundle digests differ")
	}

	for _, arm := range []string{"raw", "codebase"} {
		armDir := filepath.Join(output, arm)
		if _, err := os.Stat(filepath.Join(armDir, "repo", ".git")); err != nil {
			t.Fatalf("%s repo missing .git: %v", arm, err)
		}
		if _, err := os.Stat(filepath.Join(armDir, "prompts.json")); err != nil {
			t.Fatalf("%s missing prompts: %v", arm, err)
		}
		if _, err := os.Stat(filepath.Join(armDir, "identity.json")); err != nil {
			t.Fatalf("%s missing identity: %v", arm, err)
		}
		if _, err := os.Stat(filepath.Join(armDir, "submission.template.json")); err != nil {
			t.Fatalf("%s missing submission template: %v", arm, err)
		}
		if _, err := os.Stat(filepath.Join(armDir, "tasks.json")); !os.IsNotExist(err) {
			t.Fatalf("%s unexpectedly contains evaluator tasks.json", arm)
		}
	}

	if _, err := os.Stat(filepath.Join(output, "raw", "tools", "ap-codebase")); !os.IsNotExist(err) {
		t.Fatal("raw arm unexpectedly contains ap-codebase")
	}
	info, err := os.Stat(filepath.Join(output, "codebase", "tools", "ap-codebase"))
	if err != nil {
		t.Fatalf("codebase engine missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatal("codebase engine is not executable")
	}
}

func TestPrepareArmPacksDoesNotLeakOracleMaterial(t *testing.T) {
	root := benchmarkRoot(t)
	full, err := LoadManifest(filepath.Join(root, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	blind := Blind(full)
	engine := filepath.Join(t.TempDir(), "ap-codebase")
	if err := os.WriteFile(engine, []byte("fake engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "pack")
	if _, err := PrepareArmPacks(blind, filepath.Join(root, "fixture"), engine, output); err != nil {
		t.Fatal(err)
	}

	for _, arm := range []string{"raw", "codebase"} {
		err := filepath.WalkDir(filepath.Join(output, arm), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(data)
			if strings.Contains(text, "expected_facts") {
				t.Fatalf("%s leaked expected_facts in %s", arm, path)
			}
			for _, task := range full.Tasks {
				for _, fact := range task.ExpectedFacts {
					if strings.Contains(text, fact) {
						t.Fatalf("%s leaked expected fact %q in %s", arm, fact, path)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSubmissionTemplateCarriesImmutableIdentity(t *testing.T) {
	root := benchmarkRoot(t)
	blind, err := LoadBlindManifest(filepath.Join(root, "prompts.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "ap-codebase")
	if err := os.WriteFile(engine, []byte("fake engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "pack")
	report, err := PrepareArmPacks(blind, filepath.Join(root, "fixture"), engine, output)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(output, "raw", "submission.template.json"))
	if err != nil {
		t.Fatal(err)
	}
	var submission Submission
	if err := json.Unmarshal(data, &submission); err != nil {
		t.Fatal(err)
	}
	if submission.Run.Arm != "raw" || submission.Run.RepositoryRevision != report.Raw.RepositoryRevision {
		t.Fatalf("unexpected raw submission identity: %+v", submission)
	}
	if submission.BundleDigest != report.Raw.BundleDigest || submission.FixtureDigest != report.Raw.FixtureDigest {
		t.Fatalf("unexpected digests: %+v", submission)
	}
	if !submission.Isolation.FreshContext {
		t.Fatal("fresh_context should default true in pack template")
	}
}

func TestPrepareArmPacksRejectsOverlappingOutput(t *testing.T) {
	root := benchmarkRoot(t)
	blind, err := LoadBlindManifest(filepath.Join(root, "prompts.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(t.TempDir(), "ap-codebase")
	if err := os.WriteFile(engine, []byte("fake engine"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := PrepareArmPacks(blind, filepath.Join(root, "fixture"), engine, filepath.Join(root, "fixture", "out")); err == nil {
		t.Fatal("expected overlapping output rejection")
	}
}
