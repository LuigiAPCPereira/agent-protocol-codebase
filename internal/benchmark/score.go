package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Manifest struct {
	BenchmarkVersion int    `json:"benchmark_version"`
	FixtureID        string `json:"fixture_id"`
	Fixture          string `json:"fixture"`
	Tasks            []Task `json:"tasks"`
}

type Task struct {
	ID            string   `json:"id"`
	Prompt        string   `json:"prompt"`
	ExpectedFacts []string `json:"expected_facts"`
}

type Cost struct {
	ToolCalls    *int64 `json:"tool_calls"`
	FilesOpened  *int64 `json:"files_opened"`
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
	ElapsedMS    *int64 `json:"elapsed_ms"`
}

type Run struct {
	BenchmarkVersion   int      `json:"benchmark_version"`
	FixtureID          string   `json:"fixture_id"`
	TaskID             string   `json:"task_id"`
	Arm                string   `json:"arm"`
	RepositoryRevision string   `json:"repository_revision"`
	Facts              []string `json:"facts"`
	Cost               Cost     `json:"cost"`
	Notes              []string `json:"notes,omitempty"`
}

type Correctness struct {
	Expected   int      `json:"expected"`
	Submitted  int      `json:"submitted"`
	Matched    []string `json:"matched"`
	Missing    []string `json:"missing"`
	Unexpected []string `json:"unexpected"`
	Complete   bool     `json:"complete"`
}

type Score struct {
	BenchmarkVersion   int         `json:"benchmark_version"`
	FixtureID          string      `json:"fixture_id"`
	TaskID             string      `json:"task_id"`
	Arm                string      `json:"arm"`
	RepositoryRevision string      `json:"repository_revision"`
	Correctness        Correctness `json:"correctness"`
	Cost               Cost        `json:"cost"`
	Notes              []string    `json:"notes,omitempty"`
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read benchmark manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode benchmark manifest: %w", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func LoadRun(path string) (Run, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Run{}, fmt.Errorf("read benchmark run: %w", err)
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return Run{}, fmt.Errorf("decode benchmark run: %w", err)
	}
	return run, nil
}

func ValidateManifest(manifest Manifest) error {
	if manifest.BenchmarkVersion != 1 {
		return fmt.Errorf("unsupported benchmark_version %d", manifest.BenchmarkVersion)
	}
	if strings.TrimSpace(manifest.FixtureID) == "" {
		return errors.New("fixture_id is required")
	}
	if len(manifest.Tasks) == 0 {
		return errors.New("at least one benchmark task is required")
	}

	seen := make(map[string]struct{}, len(manifest.Tasks))
	for _, task := range manifest.Tasks {
		if strings.TrimSpace(task.ID) == "" {
			return errors.New("task id is required")
		}
		if _, ok := seen[task.ID]; ok {
			return fmt.Errorf("duplicate task id %q", task.ID)
		}
		seen[task.ID] = struct{}{}
		if strings.TrimSpace(task.Prompt) == "" {
			return fmt.Errorf("task %q prompt is required", task.ID)
		}
		if len(task.ExpectedFacts) == 0 {
			return fmt.Errorf("task %q requires expected facts", task.ID)
		}
		if hasDuplicates(task.ExpectedFacts) {
			return fmt.Errorf("task %q contains duplicate expected facts", task.ID)
		}
	}
	return nil
}

func ScoreRun(manifest Manifest, run Run) (Score, error) {
	if err := ValidateManifest(manifest); err != nil {
		return Score{}, err
	}
	if run.BenchmarkVersion != manifest.BenchmarkVersion {
		return Score{}, fmt.Errorf(
			"run benchmark_version %d does not match manifest %d",
			run.BenchmarkVersion,
			manifest.BenchmarkVersion,
		)
	}
	if run.FixtureID != manifest.FixtureID {
		return Score{}, fmt.Errorf(
			"run fixture_id %q does not match manifest %q",
			run.FixtureID,
			manifest.FixtureID,
		)
	}
	switch run.Arm {
	case "raw", "codebase", "graphify":
	default:
		return Score{}, fmt.Errorf("unsupported benchmark arm %q", run.Arm)
	}
	if strings.TrimSpace(run.RepositoryRevision) == "" {
		return Score{}, errors.New("repository_revision is required")
	}
	if hasDuplicates(run.Facts) {
		return Score{}, errors.New("run facts must not contain duplicates")
	}

	task, ok := findTask(manifest.Tasks, run.TaskID)
	if !ok {
		return Score{}, fmt.Errorf("unknown task_id %q", run.TaskID)
	}
	if err := validateCost(run.Cost); err != nil {
		return Score{}, err
	}

	expected := set(task.ExpectedFacts)
	submitted := set(run.Facts)

	var matched []string
	var missing []string
	var unexpected []string

	for fact := range expected {
		if _, ok := submitted[fact]; ok {
			matched = append(matched, fact)
		} else {
			missing = append(missing, fact)
		}
	}
	for fact := range submitted {
		if _, ok := expected[fact]; !ok {
			unexpected = append(unexpected, fact)
		}
	}
	sort.Strings(matched)
	sort.Strings(missing)
	sort.Strings(unexpected)

	return Score{
		BenchmarkVersion:   run.BenchmarkVersion,
		FixtureID:          run.FixtureID,
		TaskID:             run.TaskID,
		Arm:                run.Arm,
		RepositoryRevision: run.RepositoryRevision,
		Correctness: Correctness{
			Expected:   len(expected),
			Submitted:  len(submitted),
			Matched:    matched,
			Missing:    missing,
			Unexpected: unexpected,
			Complete:   len(missing) == 0 && len(unexpected) == 0,
		},
		Cost:  run.Cost,
		Notes: append([]string(nil), run.Notes...),
	}, nil
}

func findTask(tasks []Task, id string) (Task, bool) {
	for _, task := range tasks {
		if task.ID == id {
			return task, true
		}
	}
	return Task{}, false
}

func validateCost(cost Cost) error {
	values := map[string]*int64{
		"tool_calls":    cost.ToolCalls,
		"files_opened":  cost.FilesOpened,
		"input_tokens":  cost.InputTokens,
		"output_tokens": cost.OutputTokens,
		"elapsed_ms":    cost.ElapsedMS,
	}
	for name, value := range values {
		if value != nil && *value < 0 {
			return fmt.Errorf("%s must not be negative", name)
		}
	}
	return nil
}

func hasDuplicates(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func set(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}
