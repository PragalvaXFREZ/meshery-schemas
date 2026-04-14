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
	EndpointStatus      string
	XAnnotated          string
	SchemaBackedMeshery string
	SchemaBackedCloud   string
	SchemaCompleteness  string
	SchemaDrivenMeshery string
	SchemaDrivenCloud   string
	Notes               string
	ChangeLog           string
}

// AuditRow remains a short alias used throughout the validation package.
type AuditRow = ConsumerAuditRow

// auditHeader is the canonical header for sheet row output.
var auditHeader = []string{
	"Category",
	"Sub-Category",
	"Endpoint",
	"Method",
	"Endpoint Status",
	"x-annotated",
	"Schema-Backed (Meshery)",
	"Schema-Backed (Cloud)",
	"Schema Completeness",
	"Schema-Driven (Meshery)",
	"Schema-Driven (Cloud)",
	"Notes",
	"Change Log",
}

// toRow converts the audit row to its serialized string slice.
func (r ConsumerAuditRow) toRow() []string {
	return []string{
		r.Category,
		r.SubCategory,
		r.Endpoint,
		r.Method,
		r.EndpointStatus,
		r.XAnnotated,
		r.SchemaBackedMeshery,
		r.SchemaBackedCloud,
		r.SchemaCompleteness,
		r.SchemaDrivenMeshery,
		r.SchemaDrivenCloud,
		r.Notes,
		r.ChangeLog,
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
		EndpointStatus:      get(4),
		XAnnotated:          get(5),
		SchemaBackedMeshery: get(6),
		SchemaBackedCloud:   get(7),
		SchemaCompleteness:  get(8),
		SchemaDrivenMeshery: get(9),
		SchemaDrivenCloud:   get(10),
		Notes:               get(11),
		ChangeLog:           get(12),
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
	SchemaCompletenessOK int
	SchemaCompletenessNo int
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

	var mesheryEndpoints []consumerEndpoint
	if mesheryTree != nil {
		mesheryEndpoints, err = parseGorillaRoutes(mesheryTree)
		if err != nil {
			return nil, fmt.Errorf("consumer-audit: parse meshery routes: %w", err)
		}
		mesheryEndpoints = indexHandlers(mesheryTree, mesheryEndpoints)
	}

	var cloudEndpoints []consumerEndpoint
	if cloudTree != nil {
		cloudEndpoints, err = parseEchoRoutes(cloudTree)
		if err != nil {
			return nil, fmt.Errorf("consumer-audit: parse cloud routes: %w", err)
		}
		cloudEndpoints = indexHandlers(cloudTree, cloudEndpoints)
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

	type auditDisplayKey struct {
		Endpoint string
		Method   string
	}
	consumerOnlyByKey := make(map[auditDisplayKey][]consumerEndpoint, len(match.ConsumerOnly))
	for _, c := range match.ConsumerOnly {
		key := auditDisplayKey{Endpoint: c.Path, Method: c.Method}
		consumerOnlyByKey[key] = append(consumerOnlyByKey[key], c)
	}

	// Schema-defined endpoints (matched + schema-only).
	for _, ep := range idx.Endpoints {
		key := auditDisplayKey{Endpoint: ep.Path, Method: ep.Method}
		consumers := append([]consumerEndpoint(nil), matchIndex[schemaRowKeyOf(ep)].Consumers...)
		if extra := consumerOnlyByKey[key]; len(extra) > 0 {
			consumers = append(consumers, extra...)
			delete(consumerOnlyByKey, key)
		}
		row := newSchemaRow(ep, consumers, mesheryProvided, cloudProvided)
		rows = append(rows, row)
	}

	// Consumer-only rows are consolidated by (method, path) so the same
	// schema-less endpoint implemented in both consumers appears once.
	for _, consumers := range consumerOnlyByKey {
		row := newConsumerOnlyRow(consumers, mesheryProvided, cloudProvided)
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
		Category:    categoryFromTags(ep.Tags),
		SubCategory: ep.Construct,
		Endpoint:    ep.Path,
		Method:      ep.Method,
		XAnnotated:  classifyXAnnotated(ep.XInternal),
	}

	mesheryAllowed := xInternalAllows(ep.XInternal, "meshery")
	cloudAllowed := xInternalAllows(ep.XInternal, "cloud")

	completeness, schemaNote := classifySchemaCompleteness(ep)
	row.SchemaCompleteness = completeness

	mesheryConsumers := filterConsumersByRepo(consumers, "meshery")
	cloudConsumers := filterConsumersByRepo(consumers, "meshery-cloud")
	mesheryAssessment := assessConsumers(mesheryProvided && mesheryAllowed, "meshery", mesheryConsumers, ep.RequestShape, ep.ResponseShape)
	cloudAssessment := assessConsumers(cloudProvided && cloudAllowed, "meshery-cloud", cloudConsumers, ep.RequestShape, ep.ResponseShape)

	row.EndpointStatus = computeEndpointStatus(true, mesheryAllowed, cloudAllowed, len(mesheryConsumers) > 0, len(cloudConsumers) > 0)
	row.SchemaBackedMeshery = schemaBackedFor(mesheryProvided, mesheryAllowed, mesheryConsumers)
	row.SchemaBackedCloud = schemaBackedFor(cloudProvided, cloudAllowed, cloudConsumers)

	// When the schema itself has neither a comparable request nor response
	// shape (e.g. no $ref-backed 2xx, no requestBody), Schema-Driven cannot
	// be evaluated regardless of the consumer. Leave those cells blank so
	// schema-incomplete rows are not miscounted as consumer-side drift.
	bothShapesMissing := ep.RequestShape == nil && ep.ResponseShape == nil
	if mesheryProvided && mesheryAllowed {
		status := normalizeDrivenStatus(mesheryAssessment.Status)
		if status == "Not Audited" && bothShapesMissing {
			status = ""
		}
		row.SchemaDrivenMeshery = status
	}
	if cloudProvided && cloudAllowed {
		status := normalizeDrivenStatus(cloudAssessment.Status)
		if status == "Not Audited" && bothShapesMissing {
			status = ""
		}
		row.SchemaDrivenCloud = status
	}

	row.Notes = buildLabeledNotes(schemaNote, mesheryAssessment, cloudAssessment)
	return row
}

func newConsumerOnlyRow(consumers []consumerEndpoint, mesheryProvided, cloudProvided bool) ConsumerAuditRow {
	if len(consumers) == 0 {
		return ConsumerAuditRow{}
	}

	category, subCategory := deriveConsumerLocation(consumers[0].Path)
	row := ConsumerAuditRow{
		Category:    category,
		SubCategory: subCategory,
		Endpoint:    consumers[0].Path,
		Method:      consumers[0].Method,
		XAnnotated:  "No schema",
	}
	mesheryConsumers := filterConsumersByRepo(consumers, "meshery")
	cloudConsumers := filterConsumersByRepo(consumers, "meshery-cloud")

	row.EndpointStatus = computeEndpointStatus(false, false, false, len(mesheryConsumers) > 0, len(cloudConsumers) > 0)

	// No schema → Schema-Backed is FALSE for the repo that registered it, blank
	// for the other repo. Schema-Driven / Schema-Completeness stay blank (no
	// schema to evaluate against).
	if len(mesheryConsumers) > 0 {
		row.SchemaBackedMeshery = "FALSE"
	}
	if len(cloudConsumers) > 0 {
		row.SchemaBackedCloud = "FALSE"
	}

	mesheryAssessment := assessConsumers(mesheryProvided && len(mesheryConsumers) > 0, "meshery", mesheryConsumers, nil, nil)
	cloudAssessment := assessConsumers(cloudProvided && len(cloudConsumers) > 0, "meshery-cloud", cloudConsumers, nil, nil)
	row.Notes = buildConsumerOnlyAggregateNotes(mesheryAssessment, cloudAssessment)
	return row
}

// classifyXAnnotated returns the x-annotated column value derived from an
// endpoint's x-internal list.
func classifyXAnnotated(xInternal []string) string {
	has := func(s string) bool {
		for _, x := range xInternal {
			if x == s {
				return true
			}
		}
		return false
	}
	switch {
	case len(xInternal) == 0:
		return "None"
	case has("meshery") && has("cloud"):
		return "None"
	case has("cloud"):
		return "Cloud only"
	case has("meshery"):
		return "Meshery"
	}
	return "None"
}

// computeEndpointStatus reports whether the endpoint is live in each consumer
// relative to the schema's scope. See the column legend in the audit sheet.
func computeEndpointStatus(schemaPresent, mApplies, cApplies, mActive, cActive bool) string {
	if !schemaPresent {
		switch {
		case mActive && cActive:
			return "Active - Both"
		case mActive:
			return "Active - Meshery Server"
		case cActive:
			return "Active - Meshery Cloud"
		}
		return ""
	}
	switch {
	case mActive && cActive:
		return "Active - Both"
	case mActive && cApplies:
		return "Active Meshery Server, Unimplemented Meshery Cloud"
	case mActive:
		return "Active - Meshery Server"
	case cActive && mApplies:
		return "Active Meshery Cloud, Unimplemented Meshery Server"
	case cActive:
		return "Active - Meshery Cloud"
	case mApplies && cApplies:
		return "Unimplemented Both"
	case mApplies:
		return "Unimplemented Meshery Server"
	case cApplies:
		return "Unimplemented Meshery Cloud"
	}
	return ""
}

// schemaBackedFor returns the per-consumer Schema-Backed column value for a
// schema-backed row. Blank means the column does not apply (the consumer was
// not scanned, or the spec does not target that consumer and no handler was
// found). TRUE means the registered handler imports the shared schema types.
func schemaBackedFor(provided, applies bool, repoConsumers []consumerEndpoint) string {
	if !provided {
		return ""
	}
	if !applies && len(repoConsumers) == 0 {
		return ""
	}
	if len(repoConsumers) == 0 {
		return "FALSE"
	}
	for _, c := range repoConsumers {
		if c.ImportsSchemas {
			return "TRUE"
		}
	}
	return "FALSE"
}

// normalizeDrivenStatus folds internal assessment statuses into the small set
// surfaced to the sheet: TRUE, FALSE, or Not Audited. Partial drift is treated
// as FALSE; the diff details live in Notes.
func normalizeDrivenStatus(s string) string {
	if s == "Partial" {
		return "FALSE"
	}
	return s
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

// labeledNote is one actionable note paired with the source that produced it
// (schema / meshery / cloud). Rendered as "[source] message" one per line.
type labeledNote struct {
	Source  string
	Message string
}

// buildLabeledNotes produces the Notes column for a schema-backed row. Every
// note is attributed to its source and joined by a newline so the sheet shows
// one actionable line per issue.
func buildLabeledNotes(schemaNote string, meshery, cloud consumerAssessment) string {
	var notes []labeledNote
	for _, n := range splitSchemaNote(schemaNote) {
		notes = append(notes, labeledNote{Source: "schema", Message: n})
	}
	notes = append(notes, collectRepoNotes("meshery", "meshery", meshery)...)
	notes = append(notes, collectRepoNotes("cloud", "meshery-cloud", cloud)...)
	return joinLabeledNotes(notes)
}

func buildConsumerOnlyAggregateNotes(meshery, cloud consumerAssessment) string {
	var notes []labeledNote
	notes = append(notes, collectRepoNotes("meshery", "meshery", meshery)...)
	notes = append(notes, collectRepoNotes("cloud", "meshery-cloud", cloud)...)
	return joinLabeledNotes(notes)
}

func collectRepoNotes(source, repo string, a consumerAssessment) []labeledNote {
	var out []labeledNote
	for _, n := range a.Notes {
		out = append(out, labeledNote{Source: source, Message: stripRepoPrefix(n, repo)})
	}
	for _, n := range a.Drift {
		out = append(out, labeledNote{Source: source, Message: stripRepoPrefix(n, repo)})
	}
	return out
}

// stripRepoPrefix removes a "<repo>: " or "<repo> " prefix from a note so the
// repo name is not duplicated with the label tag.
func stripRepoPrefix(note, repo string) string {
	for _, prefix := range []string{repo + ": ", repo + " "} {
		if strings.HasPrefix(note, prefix) {
			return strings.TrimPrefix(note, prefix)
		}
	}
	return note
}

func splitSchemaNote(joined string) []string {
	if joined == "" {
		return nil
	}
	parts := strings.Split(joined, "; ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinLabeledNotes(notes []labeledNote) string {
	seen := make(map[string]bool, len(notes))
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		if n.Message == "" {
			continue
		}
		line := fmt.Sprintf("[%s] %s", n.Source, n.Message)
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return strings.Join(out, "\n")
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

// repoTally holds per-repo schema-backed and schema-driven counts.
type repoTally struct {
	BackedTrue   int
	DrivenTrue   int
	DrivenFalse  int
	DrivenNotAud int
}

// tallyRepo counts schema-backed and schema-driven metrics for a single repo.
// backed / driven extract the repo-specific column values from each row.
func tallyRepo(rows []ConsumerAuditRow, backed, driven func(ConsumerAuditRow) string) repoTally {
	var t repoTally
	for _, r := range rows {
		if backed(r) == "TRUE" {
			t.BackedTrue++
		}
		switch driven(r) {
		case "TRUE":
			t.DrivenTrue++
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
		switch r.SchemaCompleteness {
		case "TRUE":
			s.SchemaCompletenessOK++
		case "FALSE":
			s.SchemaCompletenessNo++
		}
	}
	if mesheryProvided {
		s.Meshery = tallyRepo(rows,
			func(r ConsumerAuditRow) string { return r.SchemaBackedMeshery },
			func(r ConsumerAuditRow) string { return r.SchemaDrivenMeshery })
	}
	if cloudProvided {
		s.Cloud = tallyRepo(rows,
			func(r ConsumerAuditRow) string { return r.SchemaBackedCloud },
			func(r ConsumerAuditRow) string { return r.SchemaDrivenCloud })
	}
	return s
}
