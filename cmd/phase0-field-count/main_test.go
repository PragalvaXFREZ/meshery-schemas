package main

import "testing"

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
