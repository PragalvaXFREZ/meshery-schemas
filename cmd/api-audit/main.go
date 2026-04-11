// Command api-audit walks meshery/schemas, joins it against handler
// implementations in meshery/meshery (Gorilla/mux) and meshery-cloud (Echo),
// and reports per-endpoint coverage.
//
// Usage:
//
//	go run ./cmd/api-audit                                                 # schema-only summary
//	go run ./cmd/api-audit --meshery-repo=../meshery --cloud-repo=../meshery-cloud
//
// Session 1 of the api-audit pipeline implements the analysis side only —
// the CLI prints a summary table to stdout. Session 2 adds Google Sheets
// integration, the local CSV cache, and the diff section.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/meshery/schemas/validation"
)

func main() {
	mesheryRepo := flag.String("meshery-repo", "", "Path to a meshery/meshery checkout (Gorilla router)")
	cloudRepo := flag.String("cloud-repo", "", "Path to a meshery-cloud checkout (Echo router)")
	verbose := flag.Bool("verbose", false, "Print per-construct breakdown and Schema-only / Consumer-only lists")
	flag.Parse()

	rootDir, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "api-audit: could not find repository root: %v\n", err)
		os.Exit(1)
	}

	opts := validation.APIAuditOptions{
		RootDir:     rootDir,
		MesheryRepo: *mesheryRepo,
		CloudRepo:   *cloudRepo,
		Verbose:     *verbose,
	}

	result, err := validation.RunAPIAudit(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "api-audit: %v\n", err)
		os.Exit(1)
	}

	printSummary(result, *mesheryRepo != "", *cloudRepo != "")

	if *verbose {
		printVerbose(result)
	}
}

// findRepoRoot walks up from the current working directory looking for go.mod.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

func printSummary(result *validation.APIAuditResult, mesheryProvided, cloudProvided bool) {
	s := result.Summary
	consumerOnly := 0
	if result.Match != nil {
		consumerOnly = len(result.Match.ConsumerOnly)
	}
	fmt.Println("api-audit: scanning schemas...")
	fmt.Printf("  found %d schema-defined endpoints (+ %d consumer-only handlers = %d audit rows)\n",
		s.SchemaEndpoints, consumerOnly, len(result.Rows))

	if mesheryProvided {
		fmt.Printf("\napi-audit: scanning meshery/meshery...\n")
		fmt.Printf("  parsed %d Gorilla route registrations\n", s.MesheryEndpoints)
	}
	if cloudProvided {
		fmt.Printf("\napi-audit: scanning meshery-cloud...\n")
		fmt.Printf("  parsed %d Echo route registrations\n", s.CloudEndpoints)
	}

	fmt.Println("\napi-audit: matching...")
	fmt.Println()
	fmt.Println("+---------------------------------+----------+----------+----------+")
	fmt.Println("|                                 |  Schema  | Meshery  |  Cloud   |")
	fmt.Println("+---------------------------------+----------+----------+----------+")
	fmt.Printf("| %-31s | %8d | %8d | %8d |\n", "Total endpoints", s.SchemaEndpoints, s.MesheryEndpoints, s.CloudEndpoints)
	fmt.Printf("| %-31s | %8d | %8s | %8s |\n", "Matched (schema <-> consumer)", s.Matched, "--", "--")
	fmt.Printf("| %-31s | %8d | %8s | %8s |\n", "Schema-only (no handler)", s.SchemaOnly, "--", "--")
	fmt.Printf("| %-31s | %8s | %8d | %8d |\n", "Consumer-only (no schema)", "--", consumerOnlyForRepo(result, "meshery"), consumerOnlyForRepo(result, "meshery-cloud"))
	fmt.Println("+---------------------------------+----------+----------+----------+")
	fmt.Printf("| %-31s | %8s | %8d | %8d |\n", "Schema-Backed = TRUE", "--", s.MesheryBackedTrue, s.CloudBackedTrue)
	fmt.Printf("| %-31s | %8s | %8d | %8d |\n", "Schema-Driven = TRUE", "--", s.MesheryDrivenTrue, s.CloudDrivenTrue)
	fmt.Printf("| %-31s | %8s | %8d | %8d |\n", "Schema-Driven = Partial", "--", s.MesheryDrivenPartial, s.CloudDrivenPartial)
	fmt.Printf("| %-31s | %8s | %8d | %8d |\n", "Schema-Driven = FALSE", "--", s.MesheryDrivenFalse, s.CloudDrivenFalse)
	fmt.Printf("| %-31s | %8s | %8d | %8d |\n", "Schema-Driven = Not Audited", "--", s.MesheryDrivenNotAud, s.CloudDrivenNotAud)
	fmt.Println("+---------------------------------+----------+----------+----------+")
}

func consumerOnlyForRepo(result *validation.APIAuditResult, repo string) int {
	if result == nil || result.Match == nil {
		return 0
	}
	count := 0
	for _, c := range result.Match.ConsumerOnly {
		if c.Repo == repo {
			count++
		}
	}
	return count
}

func printVerbose(result *validation.APIAuditResult) {
	if result == nil || result.Match == nil {
		return
	}
	fmt.Println()
	fmt.Println("Schema-only endpoints (defined but no handler):")
	for _, ep := range result.Match.SchemaOnly {
		fmt.Printf("  %-7s %s   (%s)\n", ep.Method, ep.Path, ep.SourceFile)
	}
	fmt.Println()
	fmt.Println("Consumer-only endpoints (registered but no schema):")
	for _, c := range result.Match.ConsumerOnly {
		fmt.Printf("  %-7s %s   (%s, %s)\n", c.Method, c.Path, c.Repo, c.HandlerName)
	}
}
