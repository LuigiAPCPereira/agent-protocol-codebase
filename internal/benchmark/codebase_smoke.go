package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/engine"
)

type SmokeReport struct {
	BenchmarkVersion int     `json:"benchmark_version"`
	FixtureID        string  `json:"fixture_id"`
	Arm              string  `json:"arm"`
	RepositoryRevision string `json:"repository_revision"`
	Setup            Setup   `json:"setup"`
	Scores           []Score `json:"scores"`
	Notes            []string `json:"notes"`
}

type Setup struct {
	APICalls  int   `json:"api_calls"`
	Sources   int   `json:"sources"`
	ElapsedMS int64 `json:"elapsed_ms"`
}

func RunCodebaseSmoke(ctx context.Context, manifest Manifest, fixtureDir string) (SmokeReport, error) {
	root, err := isolatedFixtureRepository(fixtureDir)
	if err != nil {
		return SmokeReport{}, err
	}
	defer os.RemoveAll(root)

	startSetup := time.Now()
	status := engine.Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "bench-status",
		Operation:     apiv1.OperationStatus,
		Repository:    apiv1.Repository{Root: root},
	})
	if status.Error != nil {
		return SmokeReport{}, fmt.Errorf("status: %s: %s", status.Error.Code, status.Error.Message)
	}
	scan := engine.Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "bench-scan",
		Operation:     apiv1.OperationScan,
		Repository: apiv1.Repository{
			Root:     root,
			Revision: status.Repository.Revision,
		},
	})
	if scan.Error != nil {
		return SmokeReport{}, fmt.Errorf("scan: %s: %s", scan.Error.Code, scan.Error.Message)
	}
	var scanData apiv1.ScanData
	if err := json.Unmarshal(scan.Data, &scanData); err != nil {
		return SmokeReport{}, fmt.Errorf("decode scan: %w", err)
	}

	report := SmokeReport{
		BenchmarkVersion: manifest.BenchmarkVersion,
		FixtureID:        manifest.FixtureID,
		Arm:              "codebase",
		RepositoryRevision: status.Repository.Revision.Commit,
		Setup: Setup{
			APICalls:  2,
			Sources:   len(scanData.Snapshot.Sources),
			ElapsedMS: time.Since(startSetup).Milliseconds(),
		},
		Notes: []string{
			"This is a deterministic Codebase-arm tracer, not an isolated agent comparison.",
			"files_opened and token metrics are unknown because engine-internal reads and model context are not instrumented here.",
		},
	}

	for _, task := range manifest.Tasks {
		taskStart := time.Now()
		facts, calls, err := codebaseFactsForTask(ctx, root, status.Repository.Revision, task.ID)
		if err != nil {
			return SmokeReport{}, fmt.Errorf("task %s: %w", task.ID, err)
		}
		elapsed := time.Since(taskStart).Milliseconds()
		toolCalls := int64(calls)
		elapsedMS := elapsed

		score, err := ScoreRun(manifest, Run{
			BenchmarkVersion:   manifest.BenchmarkVersion,
			FixtureID:          manifest.FixtureID,
			TaskID:             task.ID,
			Arm:                "codebase",
			RepositoryRevision: status.Repository.Revision.Commit,
			Facts:              facts,
			Cost: Cost{
				ToolCalls: &toolCalls,
				ElapsedMS: &elapsedMS,
			},
			Notes: []string{
				"files_opened is unknown for this tracer; API consumer did not directly open source files.",
				"input_tokens/output_tokens are unavailable in deterministic engine execution.",
			},
		})
		if err != nil {
			return SmokeReport{}, err
		}
		report.Scores = append(report.Scores, score)
	}

	return report, nil
}

func codebaseFactsForTask(
	ctx context.Context,
	root string,
	revision apiv1.Revision,
	taskID string,
) ([]string, int, error) {
	switch taskID {
	case "locate-service":
		result, err := executeBenchmarkOperation(ctx, root, revision, apiv1.OperationQuery, map[string]any{
			"name":    "Service",
			"kind":    "TYPE",
			"package": "example.com/bench/lib",
		})
		if err != nil {
			return nil, 1, err
		}
		var data apiv1.QueryData
		if err := json.Unmarshal(result.Data, &data); err != nil {
			return nil, 1, err
		}
		for _, node := range data.Nodes {
			if node.ID == "go:symbol:example.com/bench/lib:Service" {
				return []string{
					"declares|" + node.Path + "|" + node.PackagePath + "." + node.Name,
				}, 1, nil
			}
		}
		return nil, 1, fmt.Errorf("target Service node not found")

	case "direct-importer":
		result, err := executeBenchmarkOperation(ctx, root, revision, apiv1.OperationQuery, map[string]any{
			"kind":    "PACKAGE",
			"package": "example.com/bench/lib",
		})
		if err != nil {
			return nil, 1, err
		}
		var data apiv1.QueryData
		if err := json.Unmarshal(result.Data, &data); err != nil {
			return nil, 1, err
		}
		nodes := graphNodeMap(data.Nodes)
		var facts []string
		for _, edge := range data.Edges {
			if edge.Relation != "IMPORTS" || edge.To != "go:package:example.com/bench/lib" {
				continue
			}
			from, okFrom := nodes[edge.From]
			to, okTo := nodes[edge.To]
			if okFrom && okTo {
				facts = append(facts, "imports|"+from.PackagePath+"|"+to.PackagePath)
			}
		}
		sort.Strings(facts)
		return facts, 1, nil

	case "structural-chain":
		query, err := executeBenchmarkOperation(ctx, root, revision, apiv1.OperationQuery, map[string]any{
			"name":    "Service",
			"kind":    "TYPE",
			"package": "example.com/bench/lib",
		})
		if err != nil {
			return nil, 1, err
		}
		var queryData apiv1.QueryData
		if err := json.Unmarshal(query.Data, &queryData); err != nil {
			return nil, 1, err
		}
		if len(queryData.Matches) != 1 {
			return nil, 1, fmt.Errorf("expected one Service match, got %d", len(queryData.Matches))
		}

		impact, err := executeBenchmarkOperation(ctx, root, revision, apiv1.OperationImpact, map[string]any{
			"target":    queryData.Matches[0],
			"max_depth": 4,
		})
		if err != nil {
			return nil, 2, err
		}
		var impactData apiv1.ImpactData
		if err := json.Unmarshal(impact.Data, &impactData); err != nil {
			return nil, 2, err
		}
		nodes := map[string]apiv1.GraphNode{impactData.Target.ID: impactData.Target}
		for _, entry := range impactData.Affected {
			nodes[entry.Node.ID] = entry.Node
		}

		var facts []string
		for _, edge := range impactData.Edges {
			from, okFrom := nodes[edge.From]
			to, okTo := nodes[edge.To]
			if !okFrom || !okTo {
				continue
			}
			switch edge.Relation {
			case "DECLARES":
				facts = append(facts, "declares|"+from.Path+"|"+to.PackagePath+"."+to.Name)
			case "CONTAINS":
				facts = append(facts, "contains|"+from.PackagePath+"|"+to.Path)
			case "IMPORTS":
				facts = append(facts, "imports|"+from.PackagePath+"|"+to.PackagePath)
			}
		}
		sort.Strings(facts)
		return facts, 2, nil
	default:
		return nil, 0, fmt.Errorf("unsupported smoke task %q", taskID)
	}
}

func executeBenchmarkOperation(
	ctx context.Context,
	root string,
	revision apiv1.Revision,
	operation apiv1.Operation,
	arguments map[string]any,
) (apiv1.Result, error) {
	result := engine.Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "bench-" + string(operation),
		Operation:     operation,
		Repository: apiv1.Repository{
			Root:     root,
			Revision: revision,
		},
		Arguments: arguments,
	})
	if result.Error != nil {
		return result, fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
	}
	return result, nil
}

func graphNodeMap(nodes []apiv1.GraphNode) map[string]apiv1.GraphNode {
	out := make(map[string]apiv1.GraphNode, len(nodes))
	for _, node := range nodes {
		out[node.ID] = node
	}
	return out
}

func isolatedFixtureRepository(fixtureDir string) (string, error) {
	root, err := os.MkdirTemp("", "ap-codebase-bench-*")
	if err != nil {
		return "", err
	}
	if err := copyDirectory(fixtureDir, root); err != nil {
		os.RemoveAll(root)
		return "", err
	}
	if err := runGit(root, "init"); err != nil {
		os.RemoveAll(root)
		return "", err
	}
	if err := runGit(root, "add", "."); err != nil {
		os.RemoveAll(root)
		return "", err
	}
	if err := runFixtureCommit(root); err != nil {
		os.RemoveAll(root)
		return "", err
	}
	return root, nil
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, out)
	}
	return nil
}

func runFixtureCommit(dir string) error {
	cmd := exec.Command(
		"git", "-C", dir,
		"-c", "user.name=Agent Protocol Benchmark",
		"-c", "user.email=benchmark@example.invalid",
		"commit", "-m", "fixture",
	)
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fixture commit: %w: %s", err, out)
	}
	return nil
}
