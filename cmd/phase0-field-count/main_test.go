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
		{"name", casingLowerOnly},
		{"userId", casingCamel},
		{"orgId", casingCamel},
		{"user_id", casingSnake},
		{"organization_id", casingSnake},
		{"page_size", casingSnake},
		{"total_count", casingSnake},
		{"UserID", casingPascal}, // canonical Go field, not a JSON-tag form — still, it has lowercase so it's Pascal (not SCREAMING)
		{"ID", casingScreaming},
		{"ORG_ID", casingOther}, // SCREAMING_SNAKE: mixed uppercase + underscore, none of the named forms
		{"OrgID", casingPascal},
		{"Workspace", casingPascal},
		{"", casingOther},
		{"-", casingSuppressed},
		{"user_Id", casingOther},  // mixed snake+camel
		{"user_ID", casingOther},  // mixed snake+scream
		{"2bad", casingOther},     // starts with digit
	}
	for _, tc := range cases {
		got := classify(tc.in)
		if got != tc.want {
			t.Errorf("classify(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveTags(t *testing.T) {
	cases := []struct {
		name        string
		propName    string
		prop        map[string]any
		wantJSON    string
		wantDB      string
		wantBacked  bool
	}{
		{
			name:     "no extra tags falls back to property name",
			propName: "organizationId",
			prop:     map[string]any{"type": "string"},
			wantJSON: "organizationId",
		},
		{
			name:     "json override strips omitempty",
			propName: "name",
			prop: map[string]any{
				"x-oapi-codegen-extra-tags": map[string]any{
					"json": "displayName,omitempty",
					"db":   "display_name",
				},
			},
			wantJSON:   "displayName",
			wantDB:     "display_name",
			wantBacked: true,
		},
		{
			name:     "bare json override",
			propName: "metadata",
			prop: map[string]any{
				"x-oapi-codegen-extra-tags": map[string]any{
					"json": "metadata",
				},
			},
			wantJSON: "metadata",
		},
		{
			name:     "empty json defaults to property name",
			propName: "foo",
			prop: map[string]any{
				"x-oapi-codegen-extra-tags": map[string]any{
					"json": ",omitempty",
				},
			},
			wantJSON: "foo",
		},
		{
			name:     "db dash is not db-backed",
			propName: "virtualField",
			prop: map[string]any{
				"x-oapi-codegen-extra-tags": map[string]any{
					"db":   "-",
					"json": "virtualField",
				},
			},
			wantJSON: "virtualField",
		},
		{
			name:     "json dash is preserved verbatim",
			propName: "hiddenField",
			prop: map[string]any{
				"x-oapi-codegen-extra-tags": map[string]any{
					"json": "-",
					"db":   "hidden_field",
				},
			},
			wantJSON:   "-",
			wantDB:     "hidden_field",
			wantBacked: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j, d, b := resolveTags(tc.propName, tc.prop)
			if j != tc.wantJSON {
				t.Errorf("json = %q; want %q", j, tc.wantJSON)
			}
			if d != tc.wantDB {
				t.Errorf("db = %q; want %q", d, tc.wantDB)
			}
			if b != tc.wantBacked {
				t.Errorf("backed = %v; want %v", b, tc.wantBacked)
			}
		})
	}
}

func TestGormColumn(t *testing.T) {
	cases := []struct{ in, want string }{
		{"column:connection_id", "connection_id"},
		{"type:bytes;serializer:json;column:metadata", "metadata"},
		{"index:idx_foo,column:bar", "bar"},
		{"foreignKey:ModelId;references:ID", ""},
		{"", ""},
		{"column:-", ""},
		{"column:", ""},
	}
	for _, tc := range cases {
		if got := gormColumn(tc.in); got != tc.want {
			t.Errorf("gormColumn(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveTagsGormBacked(t *testing.T) {
	prop := map[string]any{
		"x-oapi-codegen-extra-tags": map[string]any{
			"json": "connectionId,omitempty",
			"gorm": "column:connection_id;type:bytes",
		},
	}
	j, db, backed := resolveTags("connectionId", prop)
	if j != "connectionId" {
		t.Errorf("json = %q; want connectionId", j)
	}
	if db != "connection_id" {
		t.Errorf("db = %q; want connection_id", db)
	}
	if !backed {
		t.Errorf("backed = false; want true (gorm tag present)")
	}

	// gorm: "-" means explicitly not persisted.
	prop2 := map[string]any{
		"x-oapi-codegen-extra-tags": map[string]any{
			"json": "transient",
			"gorm": "-",
		},
	}
	_, _, backed2 := resolveTags("transient", prop2)
	if backed2 {
		t.Errorf("gorm=- should mark property as not db-backed")
	}
}

func TestWalkSchemaCompositionAndItems(t *testing.T) {
	schema := map[string]any{
		"allOf": []any{
			map[string]any{
				"properties": map[string]any{
					"userId": map[string]any{"type": "string"},
					"organization_id": map[string]any{
						"type": "string",
						"x-oapi-codegen-extra-tags": map[string]any{
							"db":   "organization_id",
							"json": "organization_id",
						},
					},
				},
			},
			map[string]any{
				"oneOf": []any{
					map[string]any{
						"properties": map[string]any{
							"patternFile": map[string]any{"type": "string"},
						},
					},
					map[string]any{
						"properties": map[string]any{
							"file_name": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		"items": map[string]any{
			"properties": map[string]any{
				"nestedField": map[string]any{"type": "string"},
			},
		},
	}
	base := propertyRecord{Version: "v1test", Resource: "test", File: "test.yaml"}
	got := walkSchema(base, "Test", "", schema)

	seen := map[string]casingForm{}
	for _, r := range got {
		seen[r.PropertyName] = r.Casing
	}
	want := map[string]casingForm{
		"userId":          casingCamel,
		"organization_id": casingSnake,
		"patternFile":     casingCamel,
		"file_name":       casingSnake,
		"[].nestedField":  casingCamel,
	}
	for name, c := range want {
		got, ok := seen[name]
		if !ok {
			keys := make([]string, 0, len(seen))
			for k := range seen {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			t.Errorf("missing property %q from walk; got %v", name, keys)
			continue
		}
		if got != c {
			t.Errorf("property %q casing = %q; want %q", name, got, c)
		}
	}
}

func TestProcessFileDefinitionsContainer(t *testing.T) {
	dir := t.TempDir()
	content := `definitions:
  matchSelector:
    type: object
    properties:
      kind:
        type: string
      userId:
        type: string
  selector:
    type: object
    allOf:
      - properties:
          org_id:
            type: string
`
	path := filepath.Join(dir, "selector.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	recs, err := processFile(dir, path)
	if err != nil {
		t.Fatalf("processFile: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("no records emitted — definitions container not walked")
	}
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.PropertyName] = true
	}
	for _, want := range []string{"kind", "userId", "org_id"} {
		if !seen[want] {
			keys := make([]string, 0, len(seen))
			for k := range seen {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			t.Errorf("expected property %q among walked definitions records; got %v", want, keys)
		}
	}
}

func TestExtractVersionAndResource(t *testing.T) {
	cases := []struct {
		rel        string
		wantVersion string
		wantResource string
	}{
		{"schemas/constructs/v1beta1/workspace/api.yml", "v1beta1", "workspace"},
		{"schemas/constructs/v1beta1/workspace/workspace.yaml", "v1beta1", "workspace"},
		{"schemas/constructs/v1alpha1/model.yaml", "v1alpha1", "model"},
		{"schemas/constructs/v1beta2/design/design.yaml", "v1beta2", "design"},
		{"short/path", "", ""},
	}
	for _, tc := range cases {
		v, r := extractVersionAndResource(tc.rel)
		if v != tc.wantVersion || r != tc.wantResource {
			t.Errorf("extractVersionAndResource(%q) = (%q, %q); want (%q, %q)",
				tc.rel, v, r, tc.wantVersion, tc.wantResource)
		}
	}
}
