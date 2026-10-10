package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/benchmark"
)

type identity struct {
	BenchmarkVersion int    `json:"benchmark_version"`
	FixtureID        string `json:"fixture_id"`
	BundleDigest     string `json:"bundle_digest"`
	FixtureDigest    string `json:"fixture_digest"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(
			os.Stderr,
			"usage: ap-codebase-bench-identity <prompts.json> <fixture-dir>",
		)
		os.Exit(2)
	}

	blind, err := benchmark.LoadBlindManifest(os.Args[1])
	if err != nil {
		fail(err)
	}
	bundleDigest, err := benchmark.BundleDigest(blind)
	if err != nil {
		fail(err)
	}
	fixtureDigest, err := benchmark.FixtureDigest(os.Args[2])
	if err != nil {
		fail(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(identity{
		BenchmarkVersion: blind.BenchmarkVersion,
		FixtureID:        blind.FixtureID,
		BundleDigest:     bundleDigest,
		FixtureDigest:    fixtureDigest,
	}); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
