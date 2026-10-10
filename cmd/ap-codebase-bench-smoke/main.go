package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/benchmark"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: ap-codebase-bench-smoke <tasks.json> <fixture-dir>")
		os.Exit(2)
	}

	manifest, err := benchmark.LoadManifest(os.Args[1])
	if err != nil {
		fail(err)
	}
	report, err := benchmark.RunCodebaseSmoke(context.Background(), manifest, os.Args[2])
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
