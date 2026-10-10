package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ArmPack struct {
	Arm                string `json:"arm"`
	BenchmarkVersion   int    `json:"benchmark_version"`
	FixtureID          string `json:"fixture_id"`
	RepositoryRevision string `json:"repository_revision"`
	BundleDigest       string `json:"bundle_digest"`
	FixtureDigest      string `json:"fixture_digest"`
}

type PackReport struct {
	Raw      ArmPack `json:"raw"`
	Codebase ArmPack `json:"codebase"`
}

func PrepareArmPacks(
	blind BlindManifest,
	fixtureDir string,
	engineBinary string,
	outputDir string,
) (PackReport, error) {
	if engineBinary == "" {
		return PackReport{}, errors.New("engine binary is required")
	}
	if strings.TrimSpace(outputDir) == "" {
		return PackReport{}, errors.New("output directory is required")
	}
	if err := requireDisjointPaths(fixtureDir, outputDir); err != nil {
		return PackReport{}, err
	}
	if err := os.RemoveAll(outputDir); err != nil {
		return PackReport{}, fmt.Errorf("clear output directory: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return PackReport{}, fmt.Errorf("create output directory: %w", err)
	}

	bundleDigest, err := BundleDigest(blind)
	if err != nil {
		return PackReport{}, err
	}
	fixtureDigest, err := FixtureDigest(fixtureDir)
	if err != nil {
		return PackReport{}, err
	}

	raw, err := prepareArmPack(blind, fixtureDir, "", outputDir, "raw", bundleDigest, fixtureDigest)
	if err != nil {
		return PackReport{}, err
	}
	codebase, err := prepareArmPack(blind, fixtureDir, engineBinary, outputDir, "codebase", bundleDigest, fixtureDigest)
	if err != nil {
		return PackReport{}, err
	}
	if raw.RepositoryRevision != codebase.RepositoryRevision {
		return PackReport{}, errors.New("generated arm repositories have different revisions")
	}

	return PackReport{Raw: raw, Codebase: codebase}, nil
}

func prepareArmPack(
	blind BlindManifest,
	fixtureDir string,
	engineBinary string,
	outputDir string,
	arm string,
	bundleDigest string,
	fixtureDigest string,
) (ArmPack, error) {
	armDir := filepath.Join(outputDir, arm)
	repoDir := filepath.Join(armDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return ArmPack{}, fmt.Errorf("create %s repo: %w", arm, err)
	}
	if err := copyDirectory(fixtureDir, repoDir); err != nil {
		return ArmPack{}, fmt.Errorf("copy %s fixture: %w", arm, err)
	}
	if err := runGit(repoDir, "init"); err != nil {
		return ArmPack{}, err
	}
	if err := runGit(repoDir, "add", "."); err != nil {
		return ArmPack{}, err
	}
	if err := runFixtureCommit(repoDir); err != nil {
		return ArmPack{}, err
	}
	revision, err := gitOutput(repoDir, "rev-parse", "HEAD")
	if err != nil {
		return ArmPack{}, err
	}

	if err := writeJSON(filepath.Join(armDir, "prompts.json"), blind); err != nil {
		return ArmPack{}, err
	}
	pack := ArmPack{
		Arm:                arm,
		BenchmarkVersion:   blind.BenchmarkVersion,
		FixtureID:          blind.FixtureID,
		RepositoryRevision: revision,
		BundleDigest:       bundleDigest,
		FixtureDigest:      fixtureDigest,
	}
	if err := writeJSON(filepath.Join(armDir, "identity.json"), pack); err != nil {
		return ArmPack{}, err
	}
	if err := os.WriteFile(filepath.Join(armDir, "AGENT_INSTRUCTIONS.md"), []byte(armInstructions(arm)), 0o644); err != nil {
		return ArmPack{}, fmt.Errorf("write %s instructions: %w", arm, err)
	}
	if err := writeSubmissionTemplate(filepath.Join(armDir, "submission.template.json"), pack); err != nil {
		return ArmPack{}, err
	}

	if arm == "codebase" {
		toolDir := filepath.Join(armDir, "tools")
		if err := os.MkdirAll(toolDir, 0o755); err != nil {
			return ArmPack{}, fmt.Errorf("create tool directory: %w", err)
		}
		if err := copyExecutable(engineBinary, filepath.Join(toolDir, "ap-codebase")); err != nil {
			return ArmPack{}, err
		}
	}

	return pack, nil
}

func writeSubmissionTemplate(path string, pack ArmPack) error {
	template := Submission{
		Run: Run{
			BenchmarkVersion:   pack.BenchmarkVersion,
			FixtureID:          pack.FixtureID,
			Arm:                pack.Arm,
			RepositoryRevision: pack.RepositoryRevision,
		},
		BundleDigest:  pack.BundleDigest,
		FixtureDigest: pack.FixtureDigest,
		Isolation: IsolationAttestation{
			FreshContext: true,
		},
	}
	return writeJSON(path, template)
}

func writeJSON(path string, value any) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %q: %w", path, err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	if encodeErr != nil {
		return fmt.Errorf("encode %q: %w", path, encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %q: %w", path, closeErr)
	}
	return nil
}

func copyExecutable(source, destination string) error {
	src, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open engine binary: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create engine binary copy: %w", err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		return fmt.Errorf("copy engine binary: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close engine binary copy: %w", closeErr)
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func requireDisjointPaths(fixtureDir, outputDir string) error {
	fixture, err := filepath.Abs(fixtureDir)
	if err != nil {
		return fmt.Errorf("resolve fixture directory: %w", err)
	}
	output, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if pathContains(fixture, output) || pathContains(output, fixture) {
		return errors.New("fixture and output directories must not overlap")
	}
	return nil
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func armInstructions(arm string) string {
	common := `# Blind benchmark arm

You are an evaluated agent. Work only inside this arm directory.

Start by changing into repo/ and keep repository inspection confined there.

Rules:
- Read ../prompts.json and process tasks in listed order.
- Repository truth is only repo/. Do not access parent directories or external network resources.
- Never look for evaluator files, expected facts, scorer output, another arm, or prior answers.
- Ordinary repository/file/search/shell tools are allowed.
- Record only facts supported by this repository.
- Do not fabricate tool-call, file-open, token, or elapsed-time metrics; leave unavailable metrics null.
- Produce one submission envelope per task using submission.template.json as the shape.
- Keep the same context_id for tasks completed in this arm session.
- Set saw_evaluator_manifest=false and saw_other_arm_output=false only when those statements are true.
`
	if arm == "raw" {
		return common + `\nArm: raw\n\nDo not use a precomputed code graph, LSP/index service, ctags-like index, or ap-codebase. Standard language/toolchain commands are allowed when they inspect this repository directly.\n`
	}
	return common + `\nArm: codebase\n\nYou have the same raw tools as the raw arm plus ../tools/ap-codebase. You may use Codebase API operations. Index setup cost must remain distinguishable from per-task exploration cost.\n\nTypical setup:\n\n    ../tools/ap-codebase status --repo . --json\n    ../tools/ap-codebase scan --repo . --json\n\nFor query/path/impact/diff, send a Codebase API v1 request to:\n\n    ../tools/ap-codebase execute\n\nDo not access source or evaluator material outside this arm directory.\n`
}
