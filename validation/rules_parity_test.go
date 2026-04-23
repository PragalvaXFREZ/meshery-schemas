package validation

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// Helpers for Rule 46 tests.

func paramOrgIDQueryRef() *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Ref: orgIDQueryParamRef}
}

func paramInlineQuery(name string) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: &openapi3.Parameter{Name: name, In: "query"},
	}
}

// mkListOp builds a collection-list GET operation with the given 200
// response schema ref (e.g., "#/components/schemas/WorkspacePage") and
// query-parameter refs.
func mkListOp(opID, responseSchemaRef string, params ...*openapi3.ParameterRef) *openapi3.Operation {
	resp := openapi3.NewResponses()
	resp.Set("200", &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Content: openapi3.Content{
				"application/json": &openapi3.MediaType{
					Schema: &openapi3.SchemaRef{Ref: responseSchemaRef},
				},
			},
		},
	})
	return &openapi3.Operation{
		OperationID: opID,
		Parameters:  append(openapi3.Parameters{}, params...),
		Responses:   resp,
	}
}

func mkDocWithGETs(paths map[string]*openapi3.Operation) *openapi3.T {
	pathsMap := openapi3.NewPaths()
	for p, op := range paths {
		pi := &openapi3.PathItem{Get: op}
		pathsMap.Set(p, pi)
	}
	return &openapi3.T{Paths: pathsMap}
}

// ---------------------------------------------------------------------------
// Rule 46: Sibling-endpoint parameter parity
// ---------------------------------------------------------------------------

// TestCheckRule46_AllSiblingsDeclare — the first charter acceptance case:
// every collection-list GET in the same version declares orgIdQuery, so
// there is no violation.
func TestCheckRule46_AllSiblingsDeclare(t *testing.T) {
	acc := &parityAccumulator{}

	envDoc := mkDocWithGETs(map[string]*openapi3.Operation{
		"/api/environments": mkListOp("getEnvironments",
			"#/components/schemas/EnvironmentPage", paramOrgIDQueryRef()),
	})
	collectParityEndpoints("v1beta1/environment/api.yml", "v1beta1", envDoc, acc)

	viewDoc := mkDocWithGETs(map[string]*openapi3.Operation{
		"/api/views": mkListOp("getViews",
			"#/components/schemas/ViewPage", paramOrgIDQueryRef()),
	})
	collectParityEndpoints("v1beta1/view/api.yml", "v1beta1", viewDoc, acc)

	vs := reportParityViolations(acc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("all siblings declare orgIdQuery; expected 0 violations, got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule46_OneSiblingOmits — the second charter acceptance case:
// environment declares orgIdQuery, workspace doesn't; rule fires on
// workspace with a message naming the sibling that declared it.
func TestCheckRule46_OneSiblingOmits(t *testing.T) {
	acc := &parityAccumulator{}

	envDoc := mkDocWithGETs(map[string]*openapi3.Operation{
		"/api/environments": mkListOp("getEnvironments",
			"#/components/schemas/EnvironmentPage", paramOrgIDQueryRef()),
	})
	collectParityEndpoints("v1beta1/environment/api.yml", "v1beta1", envDoc, acc)

	wsDoc := mkDocWithGETs(map[string]*openapi3.Operation{
		"/api/workspaces": mkListOp("getWorkspaces",
			"#/components/schemas/WorkspacePage"), // NO orgIdQuery
	})
	collectParityEndpoints("v1beta1/workspace/api.yml", "v1beta1", wsDoc, acc)

	vs := reportParityViolations(acc, AuditOptions{StyleDebt: true})
	if len(vs) != 1 {
		t.Fatalf("workspace should fire Rule 46; got %d: %+v", len(vs), vs)
	}
	if vs[0].RuleNumber != 46 {
		t.Errorf("expected RuleNumber=46, got %d", vs[0].RuleNumber)
	}
	if vs[0].File != "v1beta1/workspace/api.yml" {
		t.Errorf("expected file=workspace/api.yml, got %q", vs[0].File)
	}
	for _, want := range []string{"/api/workspaces", "getWorkspaces", "orgIdQuery", "v1beta1"} {
		if !strings.Contains(vs[0].Message, want) {
			t.Errorf("message missing %q; got: %s", want, vs[0].Message)
		}
	}
}

// TestCheckRule46_NoSiblingEstablishesPattern — when no endpoint in the
// version declares orgIdQuery, the rule stays silent (it's a parity
// check, not an unconditional presence check).
func TestCheckRule46_NoSiblingEstablishesPattern(t *testing.T) {
	acc := &parityAccumulator{}
	doc := mkDocWithGETs(map[string]*openapi3.Operation{
		"/api/plans": mkListOp("getPlans",
			"#/components/schemas/PlanPage"),
	})
	collectParityEndpoints("v1beta1/plan/api.yml", "v1beta1", doc, acc)

	vs := reportParityViolations(acc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("no sibling declared orgIdQuery; rule should stay silent, got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule46_InlineOrgIDQueryCounts — a resource that spells out the
// same param inline instead of using the shared ref is still considered
// parity-compliant.
func TestCheckRule46_InlineOrgIDQueryCounts(t *testing.T) {
	acc := &parityAccumulator{}

	envDoc := mkDocWithGETs(map[string]*openapi3.Operation{
		"/api/environments": mkListOp("getEnvironments",
			"#/components/schemas/EnvironmentPage", paramOrgIDQueryRef()),
	})
	collectParityEndpoints("v1beta1/environment/api.yml", "v1beta1", envDoc, acc)

	wsDoc := mkDocWithGETs(map[string]*openapi3.Operation{
		"/api/workspaces": mkListOp("getWorkspaces",
			"#/components/schemas/WorkspacePage",
			paramInlineQuery("orgId")), // inline instead of $ref
	})
	collectParityEndpoints("v1beta1/workspace/api.yml", "v1beta1", wsDoc, acc)

	vs := reportParityViolations(acc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("inline orgId query should satisfy parity; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule46_VersionIsolation — endpoints are compared within a
// version bucket. A v1beta1 endpoint declaring orgIdQuery does NOT cause
// a v1beta2 endpoint without it to fire (they're separate versions).
func TestCheckRule46_VersionIsolation(t *testing.T) {
	acc := &parityAccumulator{}
	collectParityEndpoints("v1beta1/environment/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/environments": mkListOp("getEnvironments",
				"#/components/schemas/EnvironmentPage", paramOrgIDQueryRef()),
		}), acc)
	collectParityEndpoints("v1beta2/design/api.yml", "v1beta2",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/designs": mkListOp("getDesigns",
				"#/components/schemas/DesignPage"), // no orgIdQuery, but different version
		}), acc)

	vs := reportParityViolations(acc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("cross-version endpoints should not be compared; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule46_OnlyListEndpointsChecked — GETs whose response isn't a
// "*Page" schema (non-listing GETs like /api/system/version) are ignored.
func TestCheckRule46_OnlyListEndpointsChecked(t *testing.T) {
	acc := &parityAccumulator{}

	collectParityEndpoints("v1beta1/environment/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/environments": mkListOp("getEnvironments",
				"#/components/schemas/EnvironmentPage", paramOrgIDQueryRef()),
		}), acc)
	collectParityEndpoints("v1beta1/system/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/system/version": mkListOp("getVersion",
				"#/components/schemas/VersionInfo"), // not Page-shaped
		}), acc)

	vs := reportParityViolations(acc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("non-Page response should exclude endpoint from Rule 46; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule46_PathWithParamSkipped — endpoints whose path contains
// `{...}` are not collection-list endpoints and are not subject to Rule 46.
func TestCheckRule46_PathWithParamSkipped(t *testing.T) {
	acc := &parityAccumulator{}

	collectParityEndpoints("v1beta1/environment/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/environments": mkListOp("getEnvironments",
				"#/components/schemas/EnvironmentPage", paramOrgIDQueryRef()),
		}), acc)
	collectParityEndpoints("v1beta1/workspace/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/workspaces/{workspaceId}": mkListOp("getWorkspace",
				"#/components/schemas/Workspace"), // by-id endpoint, should be ignored
		}), acc)

	vs := reportParityViolations(acc, AuditOptions{StyleDebt: true})
	if len(vs) != 0 {
		t.Errorf("by-id endpoint (path has {param}) should be excluded; got %d: %+v", len(vs), vs)
	}
}

// TestCheckRule46_SuppressedWithoutStyleDebt — the rule is flag-gated.
func TestCheckRule46_SuppressedWithoutStyleDebt(t *testing.T) {
	acc := &parityAccumulator{}
	collectParityEndpoints("v1beta1/environment/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/environments": mkListOp("getEnvironments",
				"#/components/schemas/EnvironmentPage", paramOrgIDQueryRef()),
		}), acc)
	collectParityEndpoints("v1beta1/workspace/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/workspaces": mkListOp("getWorkspaces",
				"#/components/schemas/WorkspacePage"),
		}), acc)

	vs := reportParityViolations(acc, AuditOptions{})
	if len(vs) != 0 {
		t.Errorf("Rule 46 should be suppressed without --style-debt; got %d", len(vs))
	}
}

// TestCheckRule46_StrictBlocks — under --strict-consistency Rule 46
// emits at SeverityBlocking.
func TestCheckRule46_StrictBlocks(t *testing.T) {
	acc := &parityAccumulator{}
	collectParityEndpoints("v1beta1/environment/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/environments": mkListOp("getEnvironments",
				"#/components/schemas/EnvironmentPage", paramOrgIDQueryRef()),
		}), acc)
	collectParityEndpoints("v1beta1/workspace/api.yml", "v1beta1",
		mkDocWithGETs(map[string]*openapi3.Operation{
			"/api/workspaces": mkListOp("getWorkspaces",
				"#/components/schemas/WorkspacePage"),
		}), acc)

	vs := reportParityViolations(acc, AuditOptions{Strict: true})
	if len(vs) != 1 {
		t.Fatalf("expected 1 Rule 46 violation under --strict; got %d", len(vs))
	}
	if vs[0].Severity != SeverityBlocking {
		t.Errorf("expected SeverityBlocking, got %v", vs[0].Severity)
	}
}
