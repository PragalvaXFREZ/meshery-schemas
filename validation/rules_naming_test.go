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
				"zField":        mkStringProp(),
				"a_field":       mkStringProp(),
				"middleField":   mkStringProp(),
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
		{"name", "lowercase"}, // single-word lowercase — agnostic bucket
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

// ---------------------------------------------------------------------------
// Rule 4: URL-parameter casing (path + query) — Phase 1.E charter
// ---------------------------------------------------------------------------

// mkParamRef builds a *ParameterRef with a populated Value, matching the
// shape kin-openapi produces after resolving `$ref` to
// `components/parameters/*`. We don't bother filling the schema — Rule 4
// only inspects Name and In.
func mkParamRef(in, name string) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: &openapi3.Parameter{In: in, Name: name},
	}
}

// mkDocWithParamsAndPath builds a single-path doc. pathLevel parameters
// live on the PathItem; opLevel parameters live on a GET operation.
// Rule 4 walks both via collectAllParams, so either slot works.
func mkDocWithParamsAndPath(path string, pathLevel, opLevel openapi3.Parameters) *openapi3.T {
	item := &openapi3.PathItem{
		Parameters: pathLevel,
		Get: &openapi3.Operation{
			Parameters: opLevel,
			Responses:  openapi3.NewResponses(),
		},
	}
	doc := &openapi3.T{
		OpenAPI: "3.0.0",
		Info:    &openapi3.Info{Title: "Test", Version: "v1"},
		Paths:   openapi3.NewPaths(),
	}
	doc.Paths.Set(path, item)
	return doc
}

// TestCheckRule4_PathParamPasses — `{orgId}` is the canonical form (camelCase
// with `Id` suffix). No Rule 4 violation should fire.
func TestCheckRule4_PathParamPasses(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/orgs/{orgId}", nil, nil)
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("canonical {orgId} path param should not trigger Rule 4; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule4_PathParamScreamingFails — `{orgID}` is the classic SCREAMING
// drift. Rule 4 must flag it and suggest `{orgId}`.
func TestCheckRule4_PathParamScreamingFails(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/orgs/{orgID}", nil, nil)
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 Rule 4 violation for {orgID}; got %d: %+v", len(vs), vs)
	}
	if vs[0].RuleNumber != 4 {
		t.Errorf("expected RuleNumber=4, got %d", vs[0].RuleNumber)
	}
	for _, want := range []string{"path parameter", "{orgID}", "{orgId}"} {
		if !strings.Contains(vs[0].Message, want) {
			t.Errorf("message missing %q; got: %s", want, vs[0].Message)
		}
	}
}

// TestCheckRule4_PathParamSnakeFails — `{org_id}` uses snake_case; Rule 4
// must flag and suggest `{orgId}`.
func TestCheckRule4_PathParamSnakeFails(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/orgs/{org_id}", nil, nil)
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 Rule 4 violation for {org_id}; got %d: %+v", len(vs), vs)
	}
	for _, want := range []string{"{org_id}", "{orgId}", "path parameter"} {
		if !strings.Contains(vs[0].Message, want) {
			t.Errorf("message missing %q; got: %s", want, vs[0].Message)
		}
	}
}

// TestCheckRule4_QueryParamPasses — `orgId` as a query parameter is
// canonical (camelCase + `Id` suffix). No Rule 4 violation.
func TestCheckRule4_QueryParamPasses(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/things",
		openapi3.Parameters{mkParamRef("query", "orgId")}, nil)
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("canonical query param %q should not trigger Rule 4; got %d: %+v",
			"orgId", len(vs), vs)
	}
}

// TestCheckRule4_QueryParamSnakeFails — the `user_id` query parameter
// outlier from v1beta1/token. Must be flagged with a query-scoped message.
func TestCheckRule4_QueryParamSnakeFails(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/tokens/infinite",
		nil, openapi3.Parameters{mkParamRef("query", "user_id")})
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 Rule 4 violation for user_id query param; got %d: %+v", len(vs), vs)
	}
	if vs[0].RuleNumber != 4 {
		t.Errorf("expected RuleNumber=4, got %d", vs[0].RuleNumber)
	}
	for _, want := range []string{"query parameter", `"user_id"`, `"userId"`} {
		if !strings.Contains(vs[0].Message, want) {
			t.Errorf("message missing %q; got: %s", want, vs[0].Message)
		}
	}
	// The message must clearly say "query parameter" (not "path parameter")
	// so reviewers can locate the offender quickly.
	if strings.Contains(vs[0].Message, "path parameter") {
		t.Errorf("query-param message should not say 'path parameter'; got: %s", vs[0].Message)
	}
}

// TestCheckRule4_QueryParamScreamingFails — any `orgID` on the wire, path or
// query, should fail Rule 4.
func TestCheckRule4_QueryParamScreamingFails(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/things",
		openapi3.Parameters{mkParamRef("query", "orgID")}, nil)
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 Rule 4 violation for orgID query param; got %d: %+v", len(vs), vs)
	}
	for _, want := range []string{"query parameter", `"orgID"`, `"orgId"`} {
		if !strings.Contains(vs[0].Message, want) {
			t.Errorf("message missing %q; got: %s", want, vs[0].Message)
		}
	}
}

// TestCheckRule4_QueryParamLongFormCamelPasses — `organizationId` is valid
// camelCase + `Id` suffix; Rule 4 must not flag it even though other
// constructs prefer `orgId`. That's a cross-construct consistency concern,
// not a casing one, and belongs to a different rule.
func TestCheckRule4_QueryParamLongFormCamelPasses(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/things",
		openapi3.Parameters{mkParamRef("query", "organizationId")}, nil)
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("long-form camelCase query param %q should not trigger Rule 4; got %d: %+v",
			"organizationId", len(vs), vs)
	}
}

// TestCheckRule4_HeaderParamUntouched — header parameters are out of scope
// (some legitimate HTTP headers are non-camelCase like `Accept-Language`).
// Rule 4 must leave them alone; Rule 9 covers headers separately.
func TestCheckRule4_HeaderParamUntouched(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/things",
		nil, openapi3.Parameters{
			mkParamRef("header", "Accept-Language"),
			mkParamRef("header", "If-None-Match"),
			mkParamRef("header", "X-Request-ID"),
		})
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("header parameters must be out of scope for Rule 4; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule4_InlineAndRefParamsBothChecked — kin-openapi resolves
// `$ref: '#/components/parameters/...'` into the same `p.Value` shape as an
// inline parameter, so Rule 4 sees them uniformly. Construct a mixed list
// (one inline at path-level, one "referenced" at op-level — both look the
// same post-resolution) and confirm both are flagged.
func TestCheckRule4_InlineAndRefParamsBothChecked(t *testing.T) {
	inlineSnake := mkParamRef("query", "user_id")  // inline bad param
	refStyleScream := mkParamRef("query", "orgID") // ref-resolved bad param
	cleanPage := mkParamRef("query", "page")       // canonical passthrough
	doc := mkDocWithParamsAndPath("/api/things",
		openapi3.Parameters{inlineSnake, cleanPage},
		openapi3.Parameters{refStyleScream},
	)
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 2 {
		t.Fatalf("expected 2 query-param violations (user_id + orgID); got %d: %+v", len(vs), vs)
	}
	// Violations are emitted sorted by param name within a path, so
	// "orgID" comes before "user_id".
	wantOrder := []string{`"orgID"`, `"user_id"`}
	for i, want := range wantOrder {
		if !strings.Contains(vs[i].Message, want) {
			t.Errorf("violation[%d] missing %q; got: %s", i, want, vs[i].Message)
		}
	}
}

// TestCheckRule4_QueryParamDedupedAcrossLevels — when the same query
// parameter (e.g., a shared `$ref: '#/components/parameters/foo'`) is
// referenced at both path-level and op-level, the param appears twice in
// `collectAllParams`. Rule 4 dedupes by wire name within a path so the
// reviewer gets one violation per contract breach, not one per YAML
// reference.
func TestCheckRule4_QueryParamDedupedAcrossLevels(t *testing.T) {
	shared := mkParamRef("query", "user_id")
	doc := mkDocWithParamsAndPath("/api/things",
		openapi3.Parameters{shared},
		openapi3.Parameters{shared}, // same ref appears again at op-level
	)
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Errorf("expected 1 deduped Rule 4 violation, got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule4_MixedPathAndQueryParams — a path like
// `/api/tokens/{org_id}` with a bad query param at op-level should produce
// one path-parameter violation and one query-parameter violation,
// with each message identifying its role so reviewers can diff them.
func TestCheckRule4_MixedPathAndQueryParams(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/tokens/{org_id}",
		nil, openapi3.Parameters{mkParamRef("query", "user_id")})
	vs := checkRule4("api.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 2 {
		t.Fatalf("expected 2 violations (path {org_id} + query user_id); got %d: %+v", len(vs), vs)
	}
	// The path-parameter violation is emitted first because Rule 4
	// processes the path template before the parameter list.
	if !strings.Contains(vs[0].Message, "path parameter") ||
		!strings.Contains(vs[0].Message, "{org_id}") {
		t.Errorf("first violation should be the path param {org_id}; got: %s", vs[0].Message)
	}
	if !strings.Contains(vs[1].Message, "query parameter") ||
		!strings.Contains(vs[1].Message, `"user_id"`) {
		t.Errorf("second violation should be the query param user_id; got: %s", vs[1].Message)
	}
}

// TestCheckRule4_SuppressedWhenNotStyleDebt — Rule 4 is gated via
// classifyStyleIssue, so with no `--style-debt` / `--strict-consistency`
// flag the rule returns nothing. Default `make validate-schemas` keeps
// shipping green even with a bad query param in the tree — this is the
// safety valve while Phase 1.G baselines the known outliers.
func TestCheckRule4_SuppressedWhenNotStyleDebt(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/orgs/{org_id}",
		nil, openapi3.Parameters{mkParamRef("query", "user_id")})
	vs := checkRule4("api.yml", doc, AuditOptions{})
	if len(vs) != 0 {
		t.Errorf("expected Rule 4 suppressed without --style-debt; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule4_StrictBlocks — under `--strict-consistency`, Rule 4 fires
// at SeverityBlocking, matching every other style rule's gated behaviour.
func TestCheckRule4_StrictBlocks(t *testing.T) {
	doc := mkDocWithParamsAndPath("/api/things",
		nil, openapi3.Parameters{mkParamRef("query", "user_id")})
	vs := checkRule4("api.yml", doc, AuditOptions{Strict: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 blocking Rule 4 violation under --strict-consistency; got %d", len(vs))
	}
	if vs[0].Severity != SeverityBlocking {
		t.Errorf("expected SeverityBlocking under --strict-consistency, got %v", vs[0].Severity)
	}
}

// TestCheckRule4_Deterministic — violation ordering is stable across
// runs. Rule 4 sorts paths first, then query-parameter names within a
// path, so map-iteration order does not leak into the output. Baseline
// files rely on this (and Phase 1.G will pin it).
func TestCheckRule4_Deterministic(t *testing.T) {
	mk := func() *openapi3.T {
		item1 := &openapi3.PathItem{
			Get: &openapi3.Operation{
				Parameters: openapi3.Parameters{
					mkParamRef("query", "z_field"),
					mkParamRef("query", "a_field"),
					mkParamRef("query", "middle_field"),
				},
				Responses: openapi3.NewResponses(),
			},
		}
		item2 := &openapi3.PathItem{
			Get: &openapi3.Operation{
				Parameters: openapi3.Parameters{
					mkParamRef("query", "another_snake"),
				},
				Responses: openapi3.NewResponses(),
			},
		}
		doc := &openapi3.T{
			OpenAPI: "3.0.0",
			Info:    &openapi3.Info{Title: "T", Version: "v1"},
			Paths:   openapi3.NewPaths(),
		}
		doc.Paths.Set("/api/zeta", item1)
		doc.Paths.Set("/api/alpha", item2)
		return doc
	}
	first := checkRule4("t.yml", mk(), AuditOptions{StyleDebt: true})
	for i := 0; i < 10; i++ {
		again := checkRule4("t.yml", mk(), AuditOptions{StyleDebt: true})
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

// TestCheckRule4_NilDoc / TestCheckRule4_NoPaths — the defensive shells at
// the top of checkRule4 stay consistent with the rest of the validator.
func TestCheckRule4_NilDoc(t *testing.T) {
	vs := checkRule4("t.yml", nil, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("nil doc should yield no violations; got %d: %+v", len(vs), vs)
	}
}

func TestCheckRule4_NoPaths(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.0.0",
		Info:    &openapi3.Info{Title: "T", Version: "v1"},
	}
	vs := checkRule4("t.yml", doc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("doc with no paths should yield no violations; got %d: %+v", len(vs), vs)
	}
}
