package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		in   string
		want casingForm
	}{
		{"", casingEmpty},
		{"-", casingEmpty},
		{"id", casingLower},
		{"userId", casingCamel},
		{"user_id", casingSnake},
		{"UserID", casingPascal},
		{"ID", casingScreaming},
		{"URL", casingScreaming},
		{"ORG_ID", casingMixed}, // SCREAMING_SNAKE
		{"user_Id", casingMixed},
	}
	for _, tc := range cases {
		got := classify(tc.in)
		if got != tc.want {
			t.Errorf("classify(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestTagName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"name", "name"},
		{"name,omitempty", "name"},
		{"-", "-"},
		{",omitempty", ""},
	}
	for _, tc := range cases {
		if got := tagName(tc.in); got != tc.want {
			t.Errorf("tagName(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	if normalizeName("userId") != "userid" {
		t.Errorf("userId should normalize to userid")
	}
	if normalizeName("user_id") != "userid" {
		t.Errorf("user_id should normalize to userid")
	}
	if normalizeName("USER_ID") != "userid" {
		t.Errorf("USER_ID should normalize to userid")
	}
	if normalizeName("user-id.2") != "userid2" {
		t.Errorf("user-id.2 should normalize to userid2, got %q", normalizeName("user-id.2"))
	}
}

func TestHasScreamingToken(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"id", false},
		{"name", false},
		{"userId", false}, // one isolated capital → not a screaming token
		{"userID", true},  // ID = 2 consecutive caps
		{"ContentURL", true},
		{"HPAReplicas", true},
		{"isMesheryUIRestricted", true},
		{"patternFile", false},
	}
	for _, tc := range cases {
		if got := hasScreamingToken(tc.in); got != tc.want {
			t.Errorf("hasScreamingToken(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestClassifyFieldMismatches(t *testing.T) {
	cases := []struct {
		name     string
		json, db string
		want     []classification
	}{
		{"perfectly consistent camel+snake (Option B canonical)", "userId", "user_id",
			[]classification{classJSONDBFormMismatch}}, // form differs (camel vs snake)
		{"snake+snake (legacy)", "user_id", "user_id", nil},
		{"value-name divergence", "type", "source_type",
			[]classification{classJSONDBFormMismatch, classJSONDBValueMismatch}},
		{"screaming json alone", "URL", "",
			[]classification{classScreamingJSON}},
		{"camel with acronym suffix", "roleID", "",
			[]classification{classScreamingJSON}},
		{"json dash", "-", "user_id",
			[]classification{classSuppressed}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyField(tc.json, tc.db)
			sortedEq := func(a, b []classification) bool {
				if len(a) != len(b) {
					return false
				}
				as := make([]string, len(a))
				bs := make([]string, len(b))
				for i := range a {
					as[i] = string(a[i])
				}
				for i := range b {
					bs[i] = string(b[i])
				}
				sort.Strings(as)
				sort.Strings(bs)
				for i := range as {
					if as[i] != bs[i] {
						return false
					}
				}
				return true
			}
			if !sortedEq(got, tc.want) {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}

func TestCasingGroup(t *testing.T) {
	if casingGroup(casingCamel) != casingGroup(casingLower) {
		t.Errorf("camelCase and lowercase should share a group (camel family)")
	}
	if casingGroup(casingSnake) == casingGroup(casingCamel) {
		t.Errorf("snake and camel should be different groups")
	}
	if casingGroup(casingEmpty) != "" {
		t.Errorf("empty casing should have empty group")
	}
}

func TestScanFileCoversAuditFindings(t *testing.T) {
	dir := t.TempDir()
	src := `package models

type MesheryPattern struct {
	ID          string ` + "`json:\"id,omitempty\" db:\"id\"`" + `
	UserID      string ` + "`json:\"user_id\"`" + `
	WorkspaceID string ` + "`json:\"workspace_id,omitempty\" db:\"-\"`" + `
	OrgID       string ` + "`json:\"orgId,omitempty\" db:\"-\"`" + `
	PatternFile string ` + "`json:\"patternFile\" db:\"pattern_file\"`" + `
}

type Mapping struct {
	ID string ` + "`json:\"ID,omitempty\" db:\"id\"`" + `
}
`
	path := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	records, structs, err := scanFile("test/repo", dir, path)
	if err != nil {
		t.Fatalf("scanFile: %v", err)
	}
	if structs != 2 {
		t.Errorf("got %d structs, want 2", structs)
	}
	byField := map[string]fieldRecord{}
	for _, r := range records {
		byField[r.Field] = r
	}

	orgID, ok := byField["OrgID"]
	if !ok {
		t.Fatal("missing OrgID record")
	}
	hasClass := func(list []classification, c classification) bool {
		for _, x := range list {
			if x == c {
				return true
			}
		}
		return false
	}
	if !hasClass(orgID.Classifications, classMixedStruct) {
		t.Errorf("OrgID should be flagged mixed_struct_conventions; got %v", orgID.Classifications)
	}

	patternFile := byField["PatternFile"]
	if !hasClass(patternFile.Classifications, classJSONDBFormMismatch) {
		t.Errorf("PatternFile should be flagged json_db_form_mismatch; got %v", patternFile.Classifications)
	}

	mappingID := byField["ID"]
	// Mapping.ID has json=ID (screaming) — should be flagged.
	if !hasClass(mappingID.Classifications, classScreamingJSON) {
		t.Errorf("Mapping.ID (json=ID) should be flagged screaming_json; got %v", mappingID.Classifications)
	}
	// Mapping struct has only one tagged field, so no mixed_struct flag.
	if hasClass(mappingID.Classifications, classMixedStruct) {
		t.Errorf("single-field Mapping struct should not carry mixed_struct_conventions")
	}
}
