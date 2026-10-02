package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
)

const version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version":
		runVersion(os.Args[2:])
	case "validate-request":
		if err := runValidateRequest(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func runVersion(args []string) {
	jsonOutput := len(args) == 1 && args[0] == "--json"
	if len(args) > 1 || (len(args) == 1 && !jsonOutput) {
		usage(os.Stderr)
		os.Exit(2)
	}

	if jsonOutput {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"name":           "ap-codebase",
			"version":        version,
			"schema_version": apiv1.SchemaVersion,
		})
		return
	}

	fmt.Printf("ap-codebase %s (schema v%d)\n", version, apiv1.SchemaVersion)
}

func runValidateRequest(r io.Reader, w io.Writer) error {
	var req apiv1.Request
	if err := json.NewDecoder(r).Decode(&req); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if err := apiv1.ValidateRequest(req); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}

	return json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"schema_version": apiv1.SchemaVersion,
		"request_id":     req.RequestID,
	})
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  ap-codebase version [--json]")
	fmt.Fprintln(w, "  ap-codebase validate-request < request.json")
}
