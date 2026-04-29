package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoImportPathRegex(t *testing.T) {
	cases := []struct {
		in           string
		wantVersion  string
		wantResource string
	}{
		{"github.com/meshery/schemas/models/v1beta1/workspace", "v1beta1", "workspace"},
		{"github.com/meshery/schemas/models/v1alpha3/relationship", "v1alpha3", "relationship"},
		{"github.com/meshery/schemas/models/v1beta2/design", "v1beta2", "design"},
		{"github.com/meshery/schemas/models/v1beta1", "v1beta1", ""},
		{"github.com/other/package", "", ""},
		{"github.com/meshery/schemas/validation", "", ""},
	}
	for _, tc := range cases {
		m := goImportPath.FindStringSubmatch(tc.in)
		var v, r string
		if len(m) >= 2 {
			v = m[1]
		}
		if len(m) >= 3 {
			r = m[2]
		}
		if v != tc.wantVersion || r != tc.wantResource {
			t.Errorf("%q -> (%q, %q); want (%q, %q)", tc.in, v, r, tc.wantVersion, tc.wantResource)
		}
	}
}

func TestTSImportLineRegex(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`import { v1beta1 } from "@meshery/schemas";`, true},
		{`import { mesheryApi } from "@meshery/schemas/mesheryApi";`, true},
		{`import { cloudApi } from '@meshery/schemas/cloudApi';`, true},
		{`import something from "other-package";`, false},
		{`import foo from "./local";`, false},
		{`// "@meshery/schemas" in a comment without from`, false},
	}
	for _, tc := range cases {
		got := tsImportLine.MatchString(tc.in)
		if got != tc.want {
			t.Errorf("%q -> %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestScanGoImportsAttributesPerResource(t *testing.T) {
	dir := t.TempDir()
	src := `package handlers

import (
	"fmt"

	"github.com/meshery/schemas/models/v1beta1/workspace"
	wsv2 "github.com/meshery/schemas/models/v1beta2/workspace"
	"github.com/other/pkg"
)

var _ = fmt.Sprintf
var _ = workspace.Workspace{}
var _ = wsv2.WorkspacePayload{}
`
	path := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	type call struct{ file, imp string }
	var calls []call
	count, err := scanGoImports(dir, "", func(f, imp string) {
		calls = append(calls, call{f, imp})
	})
	if err != nil {
		t.Fatalf("scanGoImports: %v", err)
	}
	if count != 1 {
		t.Errorf("got %d go files, want 1", count)
	}
	seen := map[string]bool{}
	for _, c := range calls {
		seen[c.imp] = true
	}
	for _, want := range []string{
		"github.com/meshery/schemas/models/v1beta1/workspace",
		"github.com/meshery/schemas/models/v1beta2/workspace",
	} {
		if !seen[want] {
			t.Errorf("missing import %q in scan results", want)
		}
	}
}

func TestScanTSImportsFindsBundledClient(t *testing.T) {
	dir := t.TempDir()
	srcWithImport := `import { mesheryApi } from "@meshery/schemas/mesheryApi";

export const foo = mesheryApi;
`
	srcWithoutImport := `export const bar = 42;
`
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte(srcWithImport), 0o644); err != nil {
		t.Fatalf("seed a.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.ts"), []byte(srcWithoutImport), 0o644); err != nil {
		t.Fatalf("seed b.ts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.test.ts"), []byte(srcWithImport), 0o644); err != nil {
		t.Fatalf("seed c.test.ts: %v", err)
	}
	count, bundled, err := scanTSImports(dir, "")
	if err != nil {
		t.Fatalf("scanTSImports: %v", err)
	}
	if count != 2 {
		t.Errorf("got %d ts files scanned, want 2 (the .test.ts file should be skipped)", count)
	}
	if len(bundled) != 1 {
		t.Errorf("got %d bundled consumers, want 1; got %v", len(bundled), bundled)
	}
}

func TestResourceAliasesCoverDesignPattern(t *testing.T) {
	if resourceAliases["pattern"] != "design" {
		t.Errorf("pattern should alias to design; got %q", resourceAliases["pattern"])
	}
}
