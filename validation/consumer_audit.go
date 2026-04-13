package validation

import (
	"fmt"
	"sort"
	"strings"
)

// ConsumerAuditOptions configures a single consumer-audit run.
type ConsumerAuditOptions struct {
	// Schema repo root (required).
	RootDir string

	// Consumer repos. Empty = skip that consumer.
	MesheryRepo string
	CloudRepo   string

	// Google Sheets update. Empty = no sheet interaction (dry run).
	SheetID           string
	SheetsCredentials []byte

	Verbose bool
}

// ConsumerAuditResult is the output of RunConsumerAudit.
type ConsumerAuditResult struct {
	// Analysis results.
	SchemaIndex *schemaIndex
	Match       *matchResult

	// Reconciled state (nil if no previous state was provided).
	Tracked []TrackedEndpoint

	// Output rows for sheet serialization (sorted, deterministic).
	Rows []AuditRow

	// Summary counts for terminal display.
	Summary auditSummary
}

// ConsumerAuditRow is one row of the audit output.
type ConsumerAuditRow struct {
	Category            string
	SubCategory         string
	Endpoint            string
	Method              string
	SchemaBacked        string
	SchemaCompleteness  string
	SchemaDrivenMeshery string
	SchemaDrivenCloud   string
	ImplementationDrift string
	Notes               string
	ChangeLog           string
	SchemaSource        string
}

// AuditRow remains a short alias used throughout the validation package.
type AuditRow = ConsumerAuditRow

// auditHeader is the canonical header for sheet row output.
var auditHeader = []string{
	"Category",
	"Sub-Category",
	"Endpoint",
	"Method",
	"Schema-Backed",
	"Schema Completeness",
	"Schema-Driven (Meshery)",
	"Schema-Driven (Cloud)",
	"Implementation Drift",
	"Notes",
	"Change Log",
	"Schema Source",
}

// toRow converts the audit row to its serialized string slice.
func (r ConsumerAuditRow) toRow() []string {
	return []string{
		r.Category,
		r.SubCategory,
		r.Endpoint,
		r.Method,
		r.SchemaBacked,
		r.SchemaCompleteness,
		r.SchemaDrivenMeshery,
		r.SchemaDrivenCloud,
		r.ImplementationDrift,
		r.Notes,
		r.ChangeLog,
		r.SchemaSource,
	}
}

// rowFromStrings reconstructs an AuditRow from a serialized string slice.
// Missing trailing columns are tolerated.
func rowFromStrings(cols []string) ConsumerAuditRow {
	get := func(i int) string {
		if i < len(cols) {
			return cols[i]
		}
		return ""
	}
	return ConsumerAuditRow{
		Category:            get(0),
		SubCategory:         get(1),
		Endpoint:            get(2),
		Method:              get(3),
		SchemaBacked:        get(4),
		SchemaCompleteness:  get(5),
		SchemaDrivenMeshery: get(6),
		SchemaDrivenCloud:   get(7),
		ImplementationDrift: get(8),
		Notes:               get(9),
		ChangeLog:           get(10),
		SchemaSource:        get(11),
	}
}

// EndpointState enumerates the four reconciliation states an audit row can
// be in. Declaring the type here keeps the result surface self-contained.
type EndpointState int

const (
	StateNew EndpointState = iota
	StateExisting
	StateChanged
	StateDeleted
)

// TrackedEndpoint is one reconciled row with state transition. The CLI
// consumes this to render the diff section; fields are intentionally simple.
type TrackedEndpoint struct {
	Row       ConsumerAuditRow
	State     EndpointState
	ChangeLog string
}

// auditSummary captures the high-level counts shown in the terminal table.
type auditSummary struct {
	SchemaEndpoints      int
	MesheryEndpoints     int
	CloudEndpoints       int
	Matched              int
	SchemaOnly           int
	ConsumerOnly         int
	ConsumerOnlyMeshery  int
	ConsumerOnlyCloud    int
	SchemaBackedTrue     int
	SchemaCompletenessOK int
	SchemaCompletenessNo int
	SchemaDrivenTrue     int
	SchemaDrivenPartial  int
	SchemaDrivenFalse    int
	SchemaDrivenNotAud   int
	Meshery              repoTally
	Cloud                repoTally
}

// SheetRows returns the audit output as header-plus-rows [][]string suitable
// for Google Sheets writes. When reconciliation has run, the reconciled rows
// are used so the emitted Change Log column reflects state transitions;
// otherwise the plain analysis rows are returned.
func (r *ConsumerAuditResult) SheetRows() [][]string {
	if r == nil {
		return [][]string{append([]string(nil), auditHeader...)}
	}
	if len(r.Tracked) > 0 {
		return trackedToSheetRows(r.Tracked)
	}
	return rowsToSheetRows(r.Rows)
}

// RunConsumerAudit is the single entry point for the consumer audit pipeline.
func RunConsumerAudit(opts ConsumerAuditOptions) (*ConsumerAuditResult, error) {
	return runConsumerAudit(opts, nil, nil)
}

// runConsumerAudit is the test-friendly version that accepts pre-built
// sourceTrees in place of repo paths.
func runConsumerAudit(opts ConsumerAuditOptions, mesheryTree, cloudTree sourceTree) (*ConsumerAuditResult, error) {
	if opts.RootDir == "" {
		return nil, fmt.Errorf("consumer-audit: RootDir is required")
	}

	idx, err := buildEndpointIndex(opts.RootDir)
	if err != nil {
		return nil, fmt.Errorf("consumer-audit: build endpoint index: %w", err)
	}

	if mesheryTree == nil && opts.MesheryRepo != "" {
		mesheryTree = localTree{root: opts.MesheryRepo}
	}
	if cloudTree == nil && opts.CloudRepo != "" {
		cloudTree = localTree{root: opts.CloudRepo}
	}

	// Build the meshery-schemas Go-type index once. This drives field-level
	// verification of payloads in handlers that decode into a schemas type;
	// without it verifyShape always falls through to shapeUnverified.
	schemaTypes := loadSchemasGoTypes(opts.RootDir)

	var mesheryEndpoints []consumerEndpoint
	if mesheryTree != nil {
		mesheryEndpoints, err = parseGorillaRoutes(mesheryTree)
		if err != nil {
			return nil, fmt.Errorf("consumer-audit: parse meshery routes: %w", err)
		}
		mesheryEndpoints = indexHandlers(mesheryTree, mesheryEndpoints, schemaTypes)
	}

	var cloudEndpoints []consumerEndpoint
	if cloudTree != nil {
		cloudEndpoints, err = parseEchoRoutes(cloudTree)
		if err != nil {
			return nil, fmt.Errorf("consumer-audit: parse cloud routes: %w", err)
		}
		cloudEndpoints = indexHandlers(cloudTree, cloudEndpoints, schemaTypes)
	}

	match := matchEndpoints(idx, mesheryEndpoints, cloudEndpoints)

	mesheryProvided := mesheryTree != nil
	cloudProvided := cloudTree != nil

	rows := buildAuditRows(idx, match, mesheryEndpoints, cloudEndpoints, mesheryProvided, cloudProvided)
	sortAuditRows(rows)

	summary := computeSummary(idx, mesheryEndpoints, cloudEndpoints, match, rows, mesheryProvided, cloudProvided)

	result := &ConsumerAuditResult{
		SchemaIndex: idx,
		Match:       match,
		Rows:        rows,
		Summary:     summary,
	}

	if err := reconcileFromOpts(opts, result); err != nil {
		return result, err
	}

	return result, nil
}

// buildAuditRows materializes one AuditRow per endpoint, joining schema and
// consumer info. The result is unsorted; sortAuditRows is the canonical
// ordering used everywhere downstream.
func buildAuditRows(
	idx *schemaIndex,
	match *matchResult,
	mesheryEndpoints, cloudEndpoints []consumerEndpoint,
	mesheryProvided, cloudProvided bool,
) []ConsumerAuditRow {
	rows := make([]AuditRow, 0, len(idx.Endpoints)+len(match.ConsumerOnly))
	matchIndex := make(map[schemaRowKey]endpointMatch, len(match.Matched))
	for _, m := range match.Matched {
		matchIndex[schemaRowKeyOf(m.Schema)] = m
	}

	// Schema-defined endpoints (matched + schema-only).
	for _, ep := range idx.Endpoints {
		row := newSchemaRow(ep, matchIndex[schemaRowKeyOf(ep)].Consumers, mesheryProvided, cloudProvided)
		rows = append(rows, row)
	}

	// Consumer-only rows (no schema endpoint).
	for _, c := range match.ConsumerOnly {
		row := newConsumerOnlyRow(c, mesheryProvided, cloudProvided)
		rows = append(rows, row)
	}
	return rows
}

type schemaRowKey struct {
	SourceFile string
	Method     string
	Path       string
}

func schemaRowKeyOf(ep schemaEndpoint) schemaRowKey {
	return schemaRowKey{SourceFile: ep.SourceFile, Method: ep.Method, Path: ep.Path}
}

func newSchemaRow(ep schemaEndpoint, consumers []consumerEndpoint, mesheryProvided, cloudProvided bool) ConsumerAuditRow {
	row := ConsumerAuditRow{
		Category:     categoryFromTags(ep.Tags),
		SubCategory:  ep.Construct,
		Endpoint:     ep.Path,
		Method:       ep.Method,
		SchemaBacked: classifySchemaBacked(true, ep),
		SchemaSource: ep.SourceFile,
	}

	mesheryAllowed := xInternalAllows(ep.XInternal, "meshery")
	cloudAllowed := xInternalAllows(ep.XInternal, "cloud")
	row.SchemaCompleteness, row.Notes = classifySchemaCompleteness(ep)

	mesheryConsumers := filterConsumersByRepo(consumers, "meshery")
	cloudConsumers := filterConsumersByRepo(consumers, "meshery-cloud")
	mesheryAssessment := assessConsumers(mesheryProvided && mesheryAllowed, "meshery", mesheryConsumers, ep.RequestShape, ep.ResponseShape)
	cloudAssessment := assessConsumers(cloudProvided && cloudAllowed, "meshery-cloud", cloudConsumers, ep.RequestShape, ep.ResponseShape)

	if mesheryProvided && mesheryAllowed {
		row.SchemaDrivenMeshery = mesheryAssessment.Status
	}
	if cloudProvided && cloudAllowed {
		row.SchemaDrivenCloud = cloudAssessment.Status
	}
	row.ImplementationDrift = strings.Join(uniqueStrings(append(mesheryAssessment.Drift, cloudAssessment.Drift...)), "; ")
	row.Notes = buildNotes(ep, row.Notes, mesheryAssessment, cloudAssessment, mesheryAllowed, cloudAllowed)
	return row
}

func newConsumerOnlyRow(c consumerEndpoint, mesheryProvided, cloudProvided bool) ConsumerAuditRow {
	category, subCategory := deriveConsumerLocation(c.Path)
	row := ConsumerAuditRow{
		Category:     category,
		SubCategory:  subCategory,
		Endpoint:     c.Path,
		Method:       c.Method,
		SchemaBacked: "FALSE",
	}
	assessment := assessConsumers(true, c.Repo, []consumerEndpoint{c}, nil, nil)
	notes := []string{"schema not defined"}
	notes = append(notes, assessment.Notes...)

	switch c.Repo {
	case "meshery":
		row.SchemaDrivenMeshery = assessment.Status
	case "meshery-cloud":
		row.SchemaDrivenCloud = assessment.Status
	}
	row.Notes = strings.Join(uniqueStrings(notes), "; ")
	return row
}

func filterConsumersByRepo(consumers []consumerEndpoint, repo string) []consumerEndpoint {
	out := make([]consumerEndpoint, 0, len(consumers))
	for i := range consumers {
		if consumers[i].Repo == repo {
			out = append(out, consumers[i])
		}
	}
	return out
}

// categoryFromTags maps an operation's first tag (or "Uncategorized") to the
// Category column. The schema is the source of truth — no fallback table.
func categoryFromTags(tags []string) string {
	if len(tags) == 0 {
		return "Uncategorized"
	}
	return tags[0]
}

func deriveConsumerLocation(endpoint string) (string, string) {
	trimmed := strings.Trim(endpoint, "/")
	if trimmed == "" {
		return "Uncategorized", "(consumer-only)"
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return "Uncategorized", "(consumer-only)"
	}
	if parts[0] == "api" && len(parts) > 1 {
		category := parts[1]
		subCategory := category
		for _, part := range parts[2:] {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				continue
			}
			subCategory = part
			break
		}
		return category, subCategory
	}
	return parts[0], parts[0]
}

func buildNotes(ep schemaEndpoint, schemaNote string, meshery, cloud consumerAssessment, mesheryAllowed, cloudAllowed bool) string {
	var notes []string
	if schemaNote != "" {
		notes = append(notes, schemaNote)
	}
	if !mesheryAllowed {
		notes = append(notes, "schema applies only to meshery-cloud")
	}
	if !cloudAllowed {
		notes = append(notes, "schema applies only to meshery")
	}
	for _, assessment := range []consumerAssessment{meshery, cloud} {
		for _, n := range assessment.Notes {
			if n != "" {
				notes = append(notes, n)
			}
		}
	}
	return strings.Join(uniqueStrings(notes), "; ")
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// sortAuditRows orders rows by (Category, SubCategory, Endpoint, Method).
func sortAuditRows(rows []ConsumerAuditRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Category != rows[j].Category {
			return rows[i].Category < rows[j].Category
		}
		if rows[i].SubCategory != rows[j].SubCategory {
			return rows[i].SubCategory < rows[j].SubCategory
		}
		if rows[i].Endpoint != rows[j].Endpoint {
			return rows[i].Endpoint < rows[j].Endpoint
		}
		return rows[i].Method < rows[j].Method
	})
}

// repoTally holds per-repo driven counts produced by tallyRepo.
type repoTally struct {
	BackedTrue    int
	DrivenTrue    int
	DrivenPartial int
	DrivenFalse   int
	DrivenNotAud  int
}

// tallyRepo counts schema-backed and schema-driven metrics for a single repo.
// driven extracts the SchemaDriven field relevant to that repo from each row.
func tallyRepo(rows []ConsumerAuditRow, driven func(ConsumerAuditRow) string) repoTally {
	var t repoTally
	for _, r := range rows {
		if r.SchemaBacked == "TRUE" && driven(r) != "" {
			t.BackedTrue++
		}
		switch driven(r) {
		case "TRUE":
			t.DrivenTrue++
		case "Partial":
			t.DrivenPartial++
		case "FALSE":
			t.DrivenFalse++
		case "Not Audited":
			t.DrivenNotAud++
		}
	}
	return t
}

func computeSummary(
	idx *schemaIndex,
	meshery, cloud []consumerEndpoint,
	match *matchResult,
	rows []ConsumerAuditRow,
	mesheryProvided, cloudProvided bool,
) auditSummary {
	s := auditSummary{
		SchemaEndpoints:     len(idx.Endpoints),
		MesheryEndpoints:    len(meshery),
		CloudEndpoints:      len(cloud),
		Matched:             len(match.Matched),
		SchemaOnly:          len(match.SchemaOnly),
		ConsumerOnly:        len(match.ConsumerOnly),
		ConsumerOnlyMeshery: len(filterConsumersByRepo(match.ConsumerOnly, "meshery")),
		ConsumerOnlyCloud:   len(filterConsumersByRepo(match.ConsumerOnly, "meshery-cloud")),
	}
	for _, r := range rows {
		if r.SchemaBacked == "TRUE" {
			s.SchemaBackedTrue++
		}
		switch r.SchemaCompleteness {
		case "TRUE":
			s.SchemaCompletenessOK++
		case "FALSE":
			s.SchemaCompletenessNo++
		}
	}
	if mesheryProvided {
		s.Meshery = tallyRepo(rows, func(r ConsumerAuditRow) string { return r.SchemaDrivenMeshery })
	}
	if cloudProvided {
		s.Cloud = tallyRepo(rows, func(r ConsumerAuditRow) string { return r.SchemaDrivenCloud })
	}
	// Global driven counts are the union of per-repo tallies.
	s.SchemaDrivenTrue = s.Meshery.DrivenTrue + s.Cloud.DrivenTrue
	s.SchemaDrivenPartial = s.Meshery.DrivenPartial + s.Cloud.DrivenPartial
	s.SchemaDrivenFalse = s.Meshery.DrivenFalse + s.Cloud.DrivenFalse
	s.SchemaDrivenNotAud = s.Meshery.DrivenNotAud + s.Cloud.DrivenNotAud
	return s
}
