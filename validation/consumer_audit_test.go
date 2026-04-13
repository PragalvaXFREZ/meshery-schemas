package validation

import (
	"strings"
	"testing"
)

// TestComputeSummaryCountsPartial verifies that Partial rows contribute to
// MesheryDrivenPartial / CloudDrivenPartial / SchemaDrivenPartial.
func TestComputeSummaryCountsPartial(t *testing.T) {
	rows := []AuditRow{
		{SchemaBacked: "TRUE", SchemaCompleteness: "TRUE", SchemaDrivenMeshery: "Partial", SchemaDrivenCloud: "Not Audited"},
		{SchemaBacked: "TRUE", SchemaCompleteness: "TRUE", SchemaDrivenMeshery: "TRUE", SchemaDrivenCloud: "Partial"},
		{SchemaBacked: "TRUE", SchemaCompleteness: "TRUE", SchemaDrivenMeshery: "FALSE", SchemaDrivenCloud: "Partial"},
		{SchemaBacked: "TRUE", SchemaCompleteness: "FALSE", SchemaDrivenMeshery: "Partial", SchemaDrivenCloud: ""},
	}
	idx := &schemaIndex{Endpoints: []schemaEndpoint{
		{Method: "GET", Path: "/a"},
		{Method: "GET", Path: "/b"},
		{Method: "GET", Path: "/c"},
		{Method: "GET", Path: "/d"},
	}}
	match := &matchResult{}
	got := computeSummary(idx, nil, nil, match, rows, true, true)

	if got.Meshery.DrivenPartial != 2 {
		t.Errorf("Meshery.DrivenPartial: got %d, want 2", got.Meshery.DrivenPartial)
	}
	if got.Cloud.DrivenPartial != 2 {
		t.Errorf("Cloud.DrivenPartial: got %d, want 2", got.Cloud.DrivenPartial)
	}
	if got.SchemaDrivenPartial != 4 {
		t.Errorf("SchemaDrivenPartial: got %d, want 4", got.SchemaDrivenPartial)
	}
	if got.Meshery.DrivenTrue != 1 {
		t.Errorf("Meshery.DrivenTrue: got %d, want 1", got.Meshery.DrivenTrue)
	}
	if got.Meshery.DrivenFalse != 1 {
		t.Errorf("Meshery.DrivenFalse: got %d, want 1", got.Meshery.DrivenFalse)
	}
}

// TestSortAuditRows confirms the canonical ordering: Category, SubCategory,
// Endpoint, Method. The ordering is what downstream sheet writes rely on
// for deterministic output.
func TestSortAuditRows(t *testing.T) {
	rows := []AuditRow{
		{Category: "Identity", SubCategory: "user", Endpoint: "/api/users", Method: "POST"},
		{Category: "Identity", SubCategory: "user", Endpoint: "/api/users", Method: "GET"},
		{Category: "Auth", SubCategory: "token", Endpoint: "/api/tokens", Method: "GET"},
		{Category: "Identity", SubCategory: "team", Endpoint: "/api/teams", Method: "GET"},
	}
	sortAuditRows(rows)

	wantOrder := []string{
		"Auth|token|/api/tokens|GET",
		"Identity|team|/api/teams|GET",
		"Identity|user|/api/users|GET",
		"Identity|user|/api/users|POST",
	}
	for i, r := range rows {
		key := strings.Join([]string{r.Category, r.SubCategory, r.Endpoint, r.Method}, "|")
		if key != wantOrder[i] {
			t.Errorf("rows[%d] = %q, want %q", i, key, wantOrder[i])
		}
	}
}

// TestReconcileNewExistingChangedDeleted walks through every reconciliation
// state transition in one pass. It is the single source of truth for the
// reconcile() contract; narrower per-state tests would just duplicate it.
func TestReconcileNewExistingChangedDeleted(t *testing.T) {
	previous := [][]string{
		append([]string(nil), auditHeader...),
		// Unchanged: same audited columns in current.
		{"Identity", "user", "/api/users", "GET", "TRUE", "TRUE", "TRUE", "", "", "", "+added 2026-01-01", "users/api.yml"},
		// Changed: Schema-Driven (Meshery) flips TRUE -> Partial below.
		{"Identity", "user", "/api/users", "POST", "TRUE", "TRUE", "TRUE", "", "", "", "+added 2026-01-01", "users/api.yml"},
		// Deleted: absent from current.
		{"Auth", "key", "/api/keys", "DELETE", "TRUE", "TRUE", "TRUE", "", "", "", "+added 2026-01-01", "keys/api.yml"},
	}

	current := []AuditRow{
		{Category: "Identity", SubCategory: "user", Endpoint: "/api/users", Method: "GET", SchemaBacked: "TRUE", SchemaCompleteness: "TRUE", SchemaDrivenMeshery: "TRUE"},
		{Category: "Identity", SubCategory: "user", Endpoint: "/api/users", Method: "POST", SchemaBacked: "TRUE", SchemaCompleteness: "TRUE", SchemaDrivenMeshery: "Partial"},
		// New row not present in previous.
		{Category: "Content", SubCategory: "design", Endpoint: "/api/designs", Method: "GET", SchemaBacked: "TRUE", SchemaCompleteness: "TRUE", SchemaDrivenMeshery: "TRUE"},
	}

	tracked := reconcile(current, previous)
	if len(tracked) != 4 {
		t.Fatalf("tracked length: got %d, want 4", len(tracked))
	}

	byKey := make(map[reconcileKey]TrackedEndpoint, len(tracked))
	for _, tr := range tracked {
		byKey[keyOf(tr.Row)] = tr
	}

	cases := []struct {
		key   reconcileKey
		state EndpointState
	}{
		{reconcileKey{"/api/users", "GET"}, StateExisting},
		{reconcileKey{"/api/users", "POST"}, StateChanged},
		{reconcileKey{"/api/designs", "GET"}, StateNew},
		{reconcileKey{"/api/keys", "DELETE"}, StateDeleted},
	}
	for _, c := range cases {
		got, ok := byKey[c.key]
		if !ok {
			t.Errorf("missing tracked row for %v", c.key)
			continue
		}
		if got.State != c.state {
			t.Errorf("state for %v: got %v, want %v", c.key, got.State, c.state)
		}
	}

	// Existing row must preserve the previous ChangeLog verbatim.
	if log := byKey[reconcileKey{"/api/users", "GET"}].ChangeLog; log != "+added 2026-01-01" {
		t.Errorf("existing ChangeLog: got %q, want preserved", log)
	}
	// Changed row must name the audited column that flipped.
	if log := byKey[reconcileKey{"/api/users", "POST"}].ChangeLog; !strings.Contains(log, "Schema-Driven (Meshery)") {
		t.Errorf("changed ChangeLog should mention Schema-Driven (Meshery): %q", log)
	}
	// New row's ChangeLog must begin with `+added`.
	if log := byKey[reconcileKey{"/api/designs", "GET"}].ChangeLog; !strings.HasPrefix(log, "+added") {
		t.Errorf("new ChangeLog should start with +added: %q", log)
	}
	// Deleted row's ChangeLog must begin with `-removed`.
	if log := byKey[reconcileKey{"/api/keys", "DELETE"}].ChangeLog; !strings.HasPrefix(log, "-removed") {
		t.Errorf("deleted ChangeLog should start with -removed: %q", log)
	}
}

// TestReconcileHeaderOptional confirms parseSheetRows tolerates a missing
// header row and does not mistake a data row for
// the header.
func TestReconcileHeaderOptional(t *testing.T) {
	previous := [][]string{
		{"Identity", "user", "/api/users", "GET", "TRUE", "TRUE", "TRUE", "", "", "", "+added 2026-01-01", "users/api.yml"},
	}
	current := []AuditRow{
		{Category: "Identity", SubCategory: "user", Endpoint: "/api/users", Method: "GET", SchemaBacked: "TRUE", SchemaCompleteness: "TRUE", SchemaDrivenMeshery: "TRUE"},
	}
	tracked := reconcile(current, previous)
	if len(tracked) != 1 {
		t.Fatalf("tracked length: got %d, want 1", len(tracked))
	}
	if tracked[0].State != StateExisting {
		t.Errorf("headerless reconcile should match as StateExisting, got %v", tracked[0].State)
	}
}

// TestAuditRowSheetRoundtrip ensures AuditRow -> []string -> AuditRow preserves
// every field. rowFromStrings is also used to parse sheet reads, so this
// covers the serialization contract in both directions.
func TestAuditRowSheetRoundtrip(t *testing.T) {
	original := AuditRow{
		Category:            "Identity",
		SubCategory:         "user",
		Endpoint:            "/api/users",
		Method:              "GET",
		SchemaBacked:        "TRUE",
		SchemaCompleteness:  "TRUE",
		SchemaDrivenMeshery: "Partial",
		SchemaDrivenCloud:   "",
		ImplementationDrift: "meshery request missing field \"name\" (string)",
		Notes:               "a; b",
		ChangeLog:           "~changed 2026-01-02: Schema-Driven (Meshery)",
		SchemaSource:        "users/api.yml",
	}

	cols := original.toRow()
	if len(cols) != len(auditHeader) {
		t.Fatalf("toRow length: got %d, want %d", len(cols), len(auditHeader))
	}

	round := rowFromStrings(cols)
	if round != original {
		t.Errorf("roundtrip mismatch:\n  got  %+v\n  want %+v", round, original)
	}

	// Extra/missing trailing columns must not panic; missing fields are "".
	short := rowFromStrings(cols[:4])
	if short.SchemaBacked != "" || short.SchemaSource != "" {
		t.Errorf("rowFromStrings on short input should leave trailing fields empty: %+v", short)
	}
}

// TestSheetRowsPrefersTracked verifies the sheet output picks reconciled rows
// when reconciliation has run, and falls back to plain rows otherwise. This
// is the contract the update path relies on after reconciliation.
func TestSheetRowsPrefersTracked(t *testing.T) {
	result := &ConsumerAuditResult{
		Rows: []AuditRow{{Endpoint: "/a", Method: "GET", ChangeLog: "plain"}},
	}
	out := result.SheetRows()
	if len(out) != 2 || out[1][10] != "plain" {
		t.Errorf("plain rows: got %v", out)
	}

	result.Tracked = []TrackedEndpoint{
		{Row: AuditRow{Endpoint: "/a", Method: "GET", ChangeLog: "reconciled"}, State: StateExisting},
	}
	out = result.SheetRows()
	if len(out) != 2 || out[1][10] != "reconciled" {
		t.Errorf("tracked rows: got %v", out)
	}

	// A nil receiver must still emit a header-only set of rows.
	var nilResult *ConsumerAuditResult
	out = nilResult.SheetRows()
	if len(out) != 1 || out[0][0] != "Category" {
		t.Errorf("nil SheetRows should be header-only: got %v", out)
	}
}
