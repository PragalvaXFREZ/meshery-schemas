package validation

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// Helpers for Rule 45 tests.
func mkStringPropWithJSON(jsonTag string) *openapi3.SchemaRef {
	var ext map[string]any
	if jsonTag != "" {
		ext = map[string]any{
			"x-oapi-codegen-extra-tags": map[string]any{
				"json": jsonTag,
			},
		}
	}
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        &openapi3.Types{"string"},
			Description: "placeholder",
			Extensions:  ext,
		},
	}
}

func mkStringProp() *openapi3.SchemaRef {
	return mkStringPropWithJSON("")
}

// docWithSchema returns a doc with a single component schema at the given
// name, simplifying the boilerplate for Rule 45 tests.
func docWithSchema(name string, schema *openapi3.Schema) *openapi3.T {
	return &openapi3.T{
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{
				name: &openapi3.SchemaRef{Value: schema},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Rule 45: Partial casing migrations forbidden
// ---------------------------------------------------------------------------

// TestCheckRule45ForAPI_AllCamelPasses — the first charter acceptance case:
// a struct whose properties are all camelCase (or single-word lowercase)
// surfaces no Rule 45 violation.
func TestCheckRule45ForAPI_AllCamelPasses(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":          mkStringProp(),
			"userId":      mkStringProp(),
			"orgId":       mkStringProp(),
			"name":        mkStringProp(),
			"patternFile": mkStringProp(),
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("Canonical", schema),
		AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("all-camel struct should not trip Rule 45; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule45ForAPI_AllSnakePasses — the second charter acceptance
// case: a pure legacy all-snake struct passes Rule 45. Lowercase
// single-word keys like `id` are in the agnostic "lowercase" bucket —
// they don't count as "the camel half of a snake/camel mix" because
// they're the canonical single-word form for either tradition. Rule 6
// flags the snake fields independently.
func TestCheckRule45ForAPI_AllSnakePasses(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":         mkStringProp(),
			"name":       mkStringProp(),
			"user_id":    mkStringProp(),
			"org_id":     mkStringProp(),
			"created_at": mkStringProp(),
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("LegacyUniform", schema),
		AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("all-snake legacy struct should not trip Rule 45; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule45ForAPI_LowercaseAgnostic confirms that a struct whose
// properties are all lowercase single-words (no underscore, no
// uppercase) does not trip Rule 45 — it's purely agnostic.
func TestCheckRule45ForAPI_LowercaseAgnostic(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"id":       mkStringProp(),
			"name":     mkStringProp(),
			"owner":    mkStringProp(),
			"metadata": mkStringProp(),
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("Simple", schema),
		AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("lowercase-only struct should not trip Rule 45; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule45ForAPI_MixedFails — the third charter acceptance case:
// the classic partial-migration drift class. The message must name each
// non-matching field so reviewers know exactly where the mix is.
func TestCheckRule45ForAPI_MixedFails(t *testing.T) {
	// Model the meshery/server MesheryPattern drift:
	//   OrgID       json:"orgId"         -> camel
	//   WorkspaceID json:"workspace_id"  -> snake
	//   UserID      json:"user_id"       -> snake
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"orgId":        mkStringProp(),
			"workspace_id": mkStringProp(),
			"user_id":      mkStringProp(),
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("MesheryPattern", schema),
		AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 Rule 45 violation, got %d: %+v", len(vs), vs)
	}
	msg := vs[0].Message
	for _, want := range []string{"MesheryPattern", "orgId", "user_id", "workspace_id", "camel", "snake"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q; got: %s", want, msg)
		}
	}
	if vs[0].RuleNumber != 45 {
		t.Errorf("expected RuleNumber=45, got %d", vs[0].RuleNumber)
	}
}

// TestCheckRule45ForAPI_ScreamingFails — a struct mixing camel with an
// ALL-CAPS ID/URL token fails Rule 45 (classic `orgID` drift).
func TestCheckRule45ForAPI_ScreamingFails(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"orgID":  mkStringProp(), // screaming ID
			"userId": mkStringProp(), // camel
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("Mixed", schema),
		AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 Rule 45 violation for camel+screaming mix; got %d: %+v", len(vs), vs)
	}
	if !strings.Contains(vs[0].Message, "camel") || !strings.Contains(vs[0].Message, "screaming") {
		t.Errorf("message should name both families; got: %s", vs[0].Message)
	}
}

// TestCheckRule45ForAPI_JSONTagOverrideWins — when a property's effective
// wire name comes from `x-oapi-codegen-extra-tags.json`, Rule 45 classifies
// on that override, not the OpenAPI property key. A schema that keeps
// snake_case property keys while overriding the wire form to camelCase on
// every field should pass.
func TestCheckRule45ForAPI_JSONTagOverrideWins(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"user_id": mkStringPropWithJSON("userId"),
			"org_id":  mkStringPropWithJSON("orgId"),
			"name":    mkStringProp(),
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("OverrideConsistent", schema),
		AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("JSON-tag override should set the effective wire name; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule45ForAPI_WalksComposition — inline allOf/anyOf/oneOf branches
// and array items contribute properties to the same family set, because
// they're part of the same struct's wire shape.
func TestCheckRule45ForAPI_WalksComposition(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"userId": mkStringProp(),
		},
		AllOf: []*openapi3.SchemaRef{
			{Value: &openapi3.Schema{
				Properties: openapi3.Schemas{
					"user_id": mkStringProp(), // snake nested in allOf
				},
			}},
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("CompositeDrift", schema),
		AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Fatalf("composition should contribute to the same family set; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule45ForAPI_SuppressedWhenNotStyleDebt — the rule is gated by
// classifyStyleIssue, so a plain AuditOptions (no --style-debt) returns
// nothing.
func TestCheckRule45ForAPI_SuppressedWhenNotStyleDebt(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"orgId":   mkStringProp(),
			"user_id": mkStringProp(),
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("Mixed", schema),
		AuditOptions{})
	if len(vs) != 0 {
		t.Errorf("expected Rule 45 suppressed without --style-debt; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule45ForAPI_StrictBlocks — under --strict-consistency Rule 45
// fires at SeverityBlocking (same path as every other style rule).
func TestCheckRule45ForAPI_StrictBlocks(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"orgId":   mkStringProp(),
			"user_id": mkStringProp(),
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("Mixed", schema),
		AuditOptions{Strict: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 Rule 45 violation under --strict-consistency; got %d", len(vs))
	}
	if vs[0].Severity != SeverityBlocking {
		t.Errorf("expected SeverityBlocking under --strict-consistency, got %v", vs[0].Severity)
	}
}

// TestCheckRule45ForAPI_Deterministic — violation message content must be
// stable across runs so the advisory baseline file doesn't diff from
// map-iteration order.
func TestCheckRule45ForAPI_Deterministic(t *testing.T) {
	mk := func() *openapi3.T {
		return docWithSchema("Drift", &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"zField":       mkStringProp(),
				"a_field":      mkStringProp(),
				"middleField":  mkStringProp(),
				"another_snake": mkStringProp(),
			},
		})
	}
	first := checkRule45ForAPI("t.yml", mk(), AuditOptions{StyleDebt: true})
	for i := 0; i < 10; i++ {
		again := checkRule45ForAPI("t.yml", mk(), AuditOptions{StyleDebt: true})
		if len(first) != len(again) {
			t.Fatalf("violation count changed between runs: %d vs %d", len(first), len(again))
		}
		for j := range first {
			if first[j].Message != again[j].Message {
				t.Errorf("violation text changed between runs at index %d:\n  a=%s\n  b=%s",
					j, first[j].Message, again[j].Message)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Casing-family classifier (shared helper)
// ---------------------------------------------------------------------------

func TestClassifyCasingFamily(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"userId", "camel"},
		{"orgId", "camel"},
		{"name", "lowercase"},     // single-word lowercase — agnostic bucket
		{"metadata", "lowercase"},
		{"a", "lowercase"},
		{"id", "lowercase"},
		{"user_id", "snake"},
		{"created_at", "snake"},
		{"page_size", "snake"},
		{"ID", "screaming"},
		{"URL", "screaming"},
		{"orgID", "screaming"},   // contains "ID" token
		{"pageURL", "screaming"}, // contains "URL" token
		{"UserID", "screaming"},  // PascalCase + acronym — rule 45 treats the "ID" token as screaming
		{"", "other"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := classifyCasingFamily(tc.in)
			if got != tc.want {
				t.Errorf("classifyCasingFamily(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEffectiveWireName(t *testing.T) {
	cases := []struct {
		name, override, want string
	}{
		{"userId", "", "userId"},
		{"user_id", "userId", "userId"},
		{"user_id", "userId,omitempty", "userId"},

		// json:"-" means the field is not serialized — excluded from the
		// wire-name set. effectiveWireName returns "" to signal that.
		{"model_id", "-", ""},
		{"model_id", "-,omitempty", ""}, // pathological form; still excluded
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.override, func(t *testing.T) {
			ref := mkStringPropWithJSON(tc.override)
			got := effectiveWireName(ref, tc.name)
			if got != tc.want {
				t.Errorf("effectiveWireName(%q, override=%q) = %q; want %q",
					tc.name, tc.override, got, tc.want)
			}
		})
	}
}

// TestCheckRule45ForAPI_JSONDashExcluded confirms that a DB-only
// snake_case property with `json:"-"` does NOT contribute a snake-family
// member to Rule 45's mixing check, because it is not on the wire.
// Without this, DB-joined fields like `model_id json:"-"` would
// incorrectly trigger Rule 45 on otherwise-canonical camelCase structs.
func TestCheckRule45ForAPI_JSONDashExcluded(t *testing.T) {
	schema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"userId":   mkStringProp(),
			"orgId":    mkStringProp(),
			"model_id": mkStringPropWithJSON("-"), // DB-only, never on wire
		},
	}
	vs := checkRule45ForAPI("t.yml", docWithSchema("HasDBOnly", schema),
		AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("json:\"-\" DB-only field should be excluded from Rule 45; got %d: %+v", len(vs), vs)
	}
}
