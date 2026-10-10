package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/benchmark"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: ap-codebase-bench <tasks.json> <run.json>")
		os.Exit(2)
	}

	manifest, err := benchmark.LoadManifest(os.Args[1])
	if err != nil {
		fail(err)
	}
	run, err := benchmark.LoadRun(os.Args[2])
	if err != nil {
		fail(err)
	}
	score, err := benchmark.ScoreRun(manifest, run)
	if err != nil {
		fail(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(score); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
