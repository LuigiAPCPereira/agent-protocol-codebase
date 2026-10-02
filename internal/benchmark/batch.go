package benchmark

import (
	"errors"
	"fmt"
	"sort"
)

type BatchReport struct {
	BenchmarkVersion   int      `json:"benchmark_version"`
	FixtureID          string   `json:"fixture_id"`
	RepositoryRevision string   `json:"repository_revision"`
	Submissions        int      `json:"submissions"`
	Arms               []string `json:"arms"`
	Tasks              []string `json:"tasks"`
	Scores             []Score  `json:"scores"`
}

func EvaluateSubmissionBatch(
	manifest Manifest,
	blind BlindManifest,
	fixtureDir string,
	submissions []Submission,
) (BatchReport, error) {
	if len(submissions) == 0 {
		return BatchReport{}, errors.New("at least one submission is required")
	}

	revision := submissions[0].Run.RepositoryRevision
	seenPairs := make(map[string]struct{}, len(submissions))
	contextArm := make(map[string]string)
	armSet := make(map[string]struct{})
	taskSet := make(map[string]struct{})
	scores := make([]Score, 0, len(submissions))

	for i, submission := range submissions {
		if err := ValidateSubmission(manifest, blind, fixtureDir, submission); err != nil {
			return BatchReport{}, fmt.Errorf("submission %d: %w", i, err)
		}
		if submission.Run.RepositoryRevision != revision {
			return BatchReport{}, errors.New("all submissions must use the same repository revision")
		}

		pairKey := submission.Run.Arm + "\x00" + submission.Run.TaskID
		if _, ok := seenPairs[pairKey]; ok {
			return BatchReport{}, fmt.Errorf(
				"duplicate submission for arm %q task %q",
				submission.Run.Arm,
				submission.Run.TaskID,
			)
		}
		seenPairs[pairKey] = struct{}{}

		if arm, ok := contextArm[submission.ContextID]; ok && arm != submission.Run.Arm {
			return BatchReport{}, fmt.Errorf(
				"context_id %q is shared across arms %q and %q",
				submission.ContextID,
				arm,
				submission.Run.Arm,
			)
		}
		contextArm[submission.ContextID] = submission.Run.Arm

		score, err := ScoreRun(manifest, submission.Run)
		if err != nil {
			return BatchReport{}, fmt.Errorf("submission %d score: %w", i, err)
		}
		scores = append(scores, score)
		armSet[submission.Run.Arm] = struct{}{}
		taskSet[submission.Run.TaskID] = struct{}{}
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].TaskID != scores[j].TaskID {
			return scores[i].TaskID < scores[j].TaskID
		}
		return scores[i].Arm < scores[j].Arm
	})

	return BatchReport{
		BenchmarkVersion:   manifest.BenchmarkVersion,
		FixtureID:          manifest.FixtureID,
		RepositoryRevision: revision,
		Submissions:        len(submissions),
		Arms:               sortedSet(armSet),
		Tasks:              sortedSet(taskSet),
		Scores:             scores,
	}, nil
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
