package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type IsolationAttestation struct {
	FreshContext          bool `json:"fresh_context"`
	SawEvaluatorManifest  bool `json:"saw_evaluator_manifest"`
	SawOtherArmOutput     bool `json:"saw_other_arm_output"`
}

type Submission struct {
	Run               Run                  `json:"run"`
	BundleDigest      string               `json:"bundle_digest"`
	FixtureDigest     string               `json:"fixture_digest"`
	ContextID         string               `json:"context_id"`
	Isolation         IsolationAttestation `json:"isolation"`
}

type Comparison struct {
	TaskID             string `json:"task_id"`
	RepositoryRevision string `json:"repository_revision"`
	Left               Score  `json:"left"`
	Right              Score  `json:"right"`
}

func LoadSubmission(path string) (Submission, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Submission{}, fmt.Errorf("read benchmark submission: %w", err)
	}
	var submission Submission
	if err := json.Unmarshal(data, &submission); err != nil {
		return Submission{}, fmt.Errorf("decode benchmark submission: %w", err)
	}
	return submission, nil
}

func BundleDigest(blind BlindManifest) (string, error) {
	data, err := json.Marshal(blind)
	if err != nil {
		return "", fmt.Errorf("marshal blind manifest: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func FixtureDigest(root string) (string, error) {
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return "", fmt.Errorf("walk fixture: %w", err)
	}
	sort.Strings(paths)

	hash := sha256.New()
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", fmt.Errorf("read fixture file %q: %w", rel, err)
		}
		_, _ = hash.Write([]byte(rel))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func ValidateSubmission(
	manifest Manifest,
	blind BlindManifest,
	fixtureDir string,
	submission Submission,
) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	if err := ValidateBlindManifest(manifest, blind); err != nil {
		return err
	}

	wantBundle, err := BundleDigest(blind)
	if err != nil {
		return err
	}
	if submission.BundleDigest != wantBundle {
		return fmt.Errorf(
			"submission bundle_digest %q does not match %q",
			submission.BundleDigest,
			wantBundle,
		)
	}

	wantFixture, err := FixtureDigest(fixtureDir)
	if err != nil {
		return err
	}
	if submission.FixtureDigest != wantFixture {
		return fmt.Errorf(
			"submission fixture_digest %q does not match %q",
			submission.FixtureDigest,
			wantFixture,
		)
	}

	if strings.TrimSpace(submission.ContextID) == "" {
		return errors.New("context_id is required")
	}
	if !submission.Isolation.FreshContext {
		return errors.New("submission must attest fresh_context=true")
	}
	if submission.Isolation.SawEvaluatorManifest {
		return errors.New("submission is contaminated: evaluator manifest was visible")
	}
	if submission.Isolation.SawOtherArmOutput {
		return errors.New("submission is contaminated: other-arm output was visible")
	}

	_, err = ScoreRun(manifest, submission.Run)
	return err
}

func CompareSubmissions(
	manifest Manifest,
	blind BlindManifest,
	fixtureDir string,
	left Submission,
	right Submission,
) (Comparison, error) {
	if err := ValidateSubmission(manifest, blind, fixtureDir, left); err != nil {
		return Comparison{}, fmt.Errorf("left submission: %w", err)
	}
	if err := ValidateSubmission(manifest, blind, fixtureDir, right); err != nil {
		return Comparison{}, fmt.Errorf("right submission: %w", err)
	}
	if left.Run.TaskID != right.Run.TaskID {
		return Comparison{}, errors.New("submissions must target the same task")
	}
	if left.Run.RepositoryRevision != right.Run.RepositoryRevision {
		return Comparison{}, errors.New("submissions must use the same repository revision")
	}
	if left.Run.Arm == right.Run.Arm {
		return Comparison{}, errors.New("submissions must use different benchmark arms")
	}
	if left.ContextID == right.ContextID {
		return Comparison{}, errors.New("submissions must use different context_id values")
	}

	leftScore, err := ScoreRun(manifest, left.Run)
	if err != nil {
		return Comparison{}, err
	}
	rightScore, err := ScoreRun(manifest, right.Run)
	if err != nil {
		return Comparison{}, err
	}

	return Comparison{
		TaskID:             left.Run.TaskID,
		RepositoryRevision: left.Run.RepositoryRevision,
		Left:               leftScore,
		Right:              rightScore,
	}, nil
}
