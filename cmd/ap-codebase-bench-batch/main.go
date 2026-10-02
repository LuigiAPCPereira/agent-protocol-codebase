package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/benchmark"
)

func main() {
	if len(os.Args) < 5 {
		fmt.Fprintln(
			os.Stderr,
			"usage: ap-codebase-bench-batch <tasks.json> <prompts.json> <fixture-dir> <submission.json> [submission.json ...]",
		)
		os.Exit(2)
	}

	manifest, err := benchmark.LoadManifest(os.Args[1])
	if err != nil {
		fail(err)
	}
	blind, err := benchmark.LoadBlindManifest(os.Args[2])
	if err != nil {
		fail(err)
	}

	submissions := make([]benchmark.Submission, 0, len(os.Args)-4)
	for _, path := range os.Args[4:] {
		submission, err := benchmark.LoadSubmission(path)
		if err != nil {
			fail(err)
		}
		submissions = append(submissions, submission)
	}

	report, err := benchmark.EvaluateSubmissionBatch(manifest, blind, os.Args[3], submissions)
	if err != nil {
		fail(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
