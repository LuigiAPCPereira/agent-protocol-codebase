package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	apiv1 "github.com/LuigiAPCPereira/agent-protocol-codebase/api/v1"
	"github.com/LuigiAPCPereira/agent-protocol-codebase/internal/engine"
)

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
	case "execute":
		if err := runExecute(context.Background(), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "status":
		if err := runStatus(context.Background(), os.Args[2:], os.Stdout); err != nil {
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
			"version":        engine.Version,
			"schema_version": apiv1.SchemaVersion,
		})
		return
	}

	fmt.Printf("ap-codebase %s (schema v%d)\n", engine.Version, apiv1.SchemaVersion)
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

func runExecute(ctx context.Context, r io.Reader, w io.Writer) error {
	var req apiv1.Request
	if err := json.NewDecoder(r).Decode(&req); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}

	result := engine.Execute(ctx, req)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	if result.Error != nil {
		return fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
	}
	return nil
}

func runStatus(ctx context.Context, args []string, w io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "repository path")
	jsonOutput := fs.Bool("json", false, "emit Codebase API JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("status does not accept positional arguments")
	}

	result := engine.Execute(ctx, apiv1.Request{
		SchemaVersion: apiv1.SchemaVersion,
		RequestID:     "cli-status",
		Operation:     apiv1.OperationStatus,
		Repository:    apiv1.Repository{Root: *repo},
	})
	if result.Error != nil {
		return fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
	}

	if *jsonOutput {
		return json.NewEncoder(w).Encode(result)
	}

	fmt.Fprintf(w, "commit: %s\n", result.Repository.Revision.Commit)
	fmt.Fprintf(w, "dirty: %t\n", result.Repository.Revision.Dirty)
	if result.Repository.Revision.WorkspaceFingerprint != "" {
		fmt.Fprintf(w, "workspace_fingerprint: %s\n", result.Repository.Revision.WorkspaceFingerprint)
	}
	fmt.Fprintf(w, "index: %s\n", result.Index.State)
	return nil
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  ap-codebase version [--json]")
	fmt.Fprintln(w, "  ap-codebase validate-request < request.json")
	fmt.Fprintln(w, "  ap-codebase execute < request.json")
	fmt.Fprintln(w, "  ap-codebase status [--repo PATH] [--json]")
}
