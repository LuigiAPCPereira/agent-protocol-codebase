package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/benchmark"
)

func main() {
	fs := flag.NewFlagSet("ap-codebase-bench-pack", flag.ExitOnError)
	prompts := fs.String("prompts", "", "path to blind prompts.json")
	fixture := fs.String("fixture", "", "path to fixture directory")
	engine := fs.String("engine", "", "path to ap-codebase binary")
	output := fs.String("output", "", "output directory")
	_ = fs.Parse(os.Args[1:])

	if *prompts == "" || *fixture == "" || *engine == "" || *output == "" || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: ap-codebase-bench-pack --prompts FILE --fixture DIR --engine FILE --output DIR")
		os.Exit(2)
	}

	blind, err := benchmark.LoadBlindManifest(*prompts)
	if err != nil {
		fail(err)
	}
	report, err := benchmark.PrepareArmPacks(blind, *fixture, *engine, *output)
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
