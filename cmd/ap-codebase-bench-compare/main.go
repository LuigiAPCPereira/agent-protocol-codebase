package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/benchmark"
)

func main() {
	if len(os.Args) != 6 {
		fmt.Fprintln(
			os.Stderr,
			"usage: ap-codebase-bench-compare <tasks.json> <prompts.json> <fixture-dir> <left.json> <right.json>",
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
	left, err := benchmark.LoadSubmission(os.Args[4])
	if err != nil {
		fail(err)
	}
	right, err := benchmark.LoadSubmission(os.Args[5])
	if err != nil {
		fail(err)
	}

	comparison, err := benchmark.CompareSubmissions(manifest, blind, os.Args[3], left, right)
	if err != nil {
		fail(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(comparison); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
